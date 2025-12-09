package server

import (
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"nats-lite/internal/ack"
	"nats-lite/internal/config"
	"nats-lite/internal/durable"
	"nats-lite/internal/protocol"
	"nats-lite/internal/store"
	"nats-lite/internal/topics"
)

type Server struct {
	addr        string
	matcher     *topics.Matcher
	store       *store.Store
	ack         *ack.Tracker
	durable     *durable.Store
	pullCursors *PullCursorStore
	config      *config.Config
	mu          sync.Mutex
	clients     map[*Client]bool
}

// PullCursorStore tracks pull consumer positions
type PullCursorStore struct {
	mu      sync.RWMutex
	cursors map[string]uint64 // "subject:consumerID" -> last pulled seq
}

func newPullCursorStore() *PullCursorStore {
	return &PullCursorStore{
		cursors: make(map[string]uint64),
	}
}

func (pcs *PullCursorStore) Get(subject, consumerID string) uint64 {
	pcs.mu.RLock()
	defer pcs.mu.RUnlock()
	key := subject + ":" + consumerID
	return pcs.cursors[key]
}

func (pcs *PullCursorStore) Update(subject, consumerID string, seq uint64) {
	pcs.mu.Lock()
	defer pcs.mu.Unlock()
	key := subject + ":" + consumerID
	if current, ok := pcs.cursors[key]; !ok || seq > current {
		pcs.cursors[key] = seq
	}
}

type Client struct {
	conn        net.Conn
	srv         *Server
	parser      *protocol.Parser
	durableSubs map[string]string // sid -> "subject:durableName" for ACK tracking
	mu          sync.Mutex
}

func New(cfg *config.Config) (*Server, error) {
	// Create data directory
	if err := os.MkdirAll(cfg.Server.DataDir, 0755); err != nil {
		return nil, err
	}

	st, err := store.NewStore(cfg.Server.DataDir, cfg.Storage.MaxSegmentSize)
	if err != nil {
		return nil, err
	}

	ds, err := durable.NewStore(cfg.Server.DataDir+"/cursors.json", cfg)
	if err != nil {
		return nil, err
	}

	ackTimeout, _ := cfg.GetACKTimeout()
	tracker := ack.NewTracker(ackTimeout, cfg.ACK.MaxRetries)

	srv := &Server{
		addr:        cfg.Server.Port,
		matcher:     topics.NewMatcher(),
		store:       st,
		ack:         tracker,
		durable:     ds,
		pullCursors: newPullCursorStore(),
		clients:     make(map[*Client]bool),
		config:      cfg,
	}

	// Setup DLQ handler
	tracker.SetDLQHandler(func(seq uint64, subject string, payload []byte) {
		dlqSubject := cfg.DLQ.Prefix + subject
		log.Printf("Moving message %d to DLQ: %s", seq, dlqSubject)
		srv.store.Append(dlqSubject, payload)
	})

	return srv, nil
}

func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	log.Printf("Listening on %s", s.addr)

	// Start Retention Loop
	go s.retentionLoop()

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Println("Accept error:", err)
			continue
		}
		go s.handleConnection(conn)
	}
}

func (s *Server) retentionLoop() {
	checkInterval, _ := s.config.GetRetentionCheckInterval()
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	maxAge, _ := s.config.GetRetentionMaxAge()
	policy := store.RetentionPolicy{
		MaxAge:   maxAge,
		MaxBytes: s.config.Retention.MaxBytes,
	}

	for range ticker.C {
		s.store.EnforceRetention(policy)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	client := &Client{
		conn:        conn,
		srv:         s,
		parser:      protocol.NewParser(conn),
		durableSubs: make(map[string]string),
	}

	s.mu.Lock()
	s.clients[client] = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, client)
		s.mu.Unlock()
		conn.Close()
	}()

	for {
		cmd, err := client.parser.Parse()
		if err != nil {
			return
		}

		switch cmd.Type {
		case protocol.PUB:
			s.handlePub(cmd)
		case protocol.SUB:
			s.handleSub(client, cmd)
		case protocol.PING:
			client.conn.Write([]byte("PONG\r\n"))
		case protocol.REPLAY:
			s.handleReplay(client, cmd)
		case protocol.PULL:
			s.handlePull(client, cmd)
		case protocol.ACK:
			seq, _ := strconv.ParseUint(cmd.Subject, 10, 64)
			s.ack.Ack(seq)
			// Update durable cursor if this is a durable subscription
			// ACK format: ACK <seq> [sid]
			if cmd.Sid != "" {
				client.mu.Lock()
				if cursorKey, ok := client.durableSubs[cmd.Sid]; ok {
					s.durable.Update(cursorKey, seq)
				}
				client.mu.Unlock()
			}
		}
	}
}

func (s *Server) handlePub(cmd *protocol.Command) {
	// Persist
	seq, err := s.store.Append(cmd.Subject, cmd.Payload)
	if err != nil {
		log.Println("Storage error:", err)
	}

	// Match and Dispach
	subs := s.matcher.Match(cmd.Subject)
	for _, sub := range subs {
		// Track for retry
		s.ack.Track(seq, sub.Client, sub.Sid, cmd.Subject, cmd.Payload)
		sub.Client.Send(sub.Sid, cmd.Subject, cmd.Payload, seq)
	}
}

func (s *Server) handleSub(c *Client, cmd *protocol.Command) {
	// If Queue group is present, it's in Payload from parser hack
	queue := ""
	if len(cmd.Payload) > 0 {
		queue = string(cmd.Payload)
	}

	// Check for Durable Replay
	// Hack: if Queue starts with "DURABLE:", treat as durable name
	// This is Protocol V0.2 Hack
	durableName := ""
	if len(queue) > 8 && queue[:8] == "DURABLE:" {
		durableName = queue[8:]
		queue = "" // It's not a queue group then
	}

	sub := &topics.Subscription{
		Subject: cmd.Subject,
		Sid:     cmd.Sid,
		Queue:   queue,
		Client:  c,
	}
	s.matcher.Subscribe(sub)

	if durableName != "" {
		// Resume from last acknowledged position
		// Use composite key: subject + durableName for proper namespacing
		cursorKey := cmd.Subject + ":" + durableName
		lastSeq := s.durable.Get(cursorKey)

		// Track this durable subscription for ACK handling
		c.mu.Lock()
		c.durableSubs[sub.Sid] = cursorKey
		c.mu.Unlock()

		if lastSeq > 0 {
			// Stream ALL missed messages until caught up
			go func() {
				batchSize := s.config.Durable.ReplayBatch
				currentSeq := lastSeq + 1

				for {
					req := store.ReplayRequest{
						Mode:     store.ReplayFromSeq,
						StartSeq: currentSeq,
						MaxCount: batchSize,
					}

					records, err := s.store.ReadBatch(req)
					if err != nil || len(records) == 0 {
						break // Caught up or error
					}

					// Filter by subject and send
					for _, rec := range records {
						if rec.Subject == cmd.Subject {
							c.Send(sub.Sid, rec.Subject, rec.Data, rec.Sequence)
							currentSeq = rec.Sequence + 1
						}
					}

					// If we got less than batch size, we're caught up
					if len(records) < batchSize {
						break
					}
				}
			}()
		}
	}

	c.conn.Write([]byte("+OK\r\n"))
}

func (s *Server) handleReplay(c *Client, cmd *protocol.Command) {
	// REPLAY <seq>
	// REPLAY FIRST <count>
	// REPLAY LAST
	// REPLAY TIME <unix_timestamp> <count>

	mode := cmd.Subject
	args := cmd.Sid

	var req store.ReplayRequest
	req.MaxCount = 100 // Default

	switch mode {
	case "FIRST":
		req.Mode = store.ReplayFromFirst
		if args != "" {
			if count, err := strconv.Atoi(args); err == nil {
				req.MaxCount = count
			}
		}

	case "LAST":
		req.Mode = store.ReplayFromLast
		req.MaxCount = 1

	case "TIME":
		req.Mode = store.ReplayFromTime
		parts := strings.Split(args, " ")
		if len(parts) >= 1 {
			if ts, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
				req.StartTime = time.Unix(ts, 0)
			}
		}
		if len(parts) >= 2 {
			if count, err := strconv.Atoi(parts[1]); err == nil {
				req.MaxCount = count
			}
		}

	default:
		// Assume it's a sequence number
		if seq, err := strconv.ParseUint(mode, 10, 64); err == nil {
			req.Mode = store.ReplayFromSeq
			req.StartSeq = seq
			req.MaxCount = 1
		} else {
			c.conn.Write([]byte("-ERR invalid replay mode\r\n"))
			return
		}
	}

	records, err := s.store.ReadBatch(req)
	if err != nil {
		c.conn.Write([]byte(fmt.Sprintf("-ERR %v\r\n", err)))
		return
	}

	for _, rec := range records {
		c.Send("replay", rec.Subject, rec.Data, rec.Sequence)
	}

	c.conn.Write([]byte("+OK\r\n"))
}

func (s *Server) handlePull(c *Client, cmd *protocol.Command) {
	// PULL <subject> <count> [consumerID]
	// For now, use connection address as consumerID
	subject := cmd.Subject
	count, err := strconv.Atoi(cmd.Sid)
	if err != nil || count <= 0 {
		c.conn.Write([]byte("-ERR invalid count\r\n"))
		return
	}

	// Use client connection as unique consumer ID
	consumerID := c.conn.RemoteAddr().String()

	// Get last pulled position for this consumer
	lastSeq := s.pullCursors.Get(subject, consumerID)

	// Read from last position + 1
	req := store.ReplayRequest{
		Mode:     store.ReplayFromSeq,
		StartSeq: lastSeq + 1,
		MaxCount: count,
	}

	records, err := s.store.ReadBatch(req)
	if err != nil {
		c.conn.Write([]byte(fmt.Sprintf("-ERR %v\r\n", err)))
		return
	}

	// Filter by subject and send
	sent := 0
	var maxSeq uint64
	for _, rec := range records {
		if rec.Subject == subject {
			c.Send("pull", rec.Subject, rec.Data, rec.Sequence)
			sent++
			if rec.Sequence > maxSeq {
				maxSeq = rec.Sequence
			}
		}
	}

	// Update cursor to highest sent sequence
	if maxSeq > 0 {
		s.pullCursors.Update(subject, consumerID, maxSeq)
	}

	c.conn.Write([]byte(fmt.Sprintf("+OK %d\r\n", sent)))
}

func (c *Client) Send(sid string, subject string, payload []byte, seq uint64) {
	// MSG <subject> <sid> <size> <seq>\r\n<payload>\r\n
	msg := fmt.Sprintf("MSG %s %s %d %d\r\n%s\r\n", subject, sid, len(payload), seq, payload)
	c.conn.Write([]byte(msg))
}

func (s *Server) Shutdown() error {
	log.Println("Flushing durable cursors...")
	s.durable.ForceFlush()
	s.durable.Close()
	s.store.Close()
	log.Println("Shutdown complete")
	return nil
}
