package server

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"strconv"

	"nats-lite/internal/ack"
	"nats-lite/internal/durable"
	"nats-lite/internal/protocol"
	"nats-lite/internal/store"
	"nats-lite/internal/topics"
)

type Server struct {
	addr    string
	matcher *topics.Matcher
	store   *store.Store
	ack     *ack.Tracker
	durable *durable.Store
	mu      sync.Mutex
	clients map[*Client]bool
}

type Client struct {
	conn   net.Conn
	srv    *Server
	parser *protocol.Parser
}

func New(addr string, dataDir string) (*Server, error) {
	st, err := store.NewStore(dataDir)
	if err != nil {
		return nil, err
	}

	ds, err := durable.NewStore(dataDir + "/cursors.json")
	if err != nil {
		return nil, err
	}

	return &Server{
		addr:    addr,
		matcher: topics.NewMatcher(),
		store:   st,
		ack:     ack.NewTracker(5*time.Second, 3), // 5s timeout, 3 retries
		durable: ds,
		clients: make(map[*Client]bool),
	}, nil
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
	ticker := time.NewTicker(10 * time.Second) // Check every 10s
	defer ticker.Stop()

	policy := store.RetentionPolicy{
		MaxAge:   24 * time.Hour,
		MaxBytes: 1 * 1024 * 1024 * 1024, // 1GB
	}

	for range ticker.C {
		s.store.EnforceRetention(policy)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	client := &Client{
		conn:   conn,
		srv:    s,
		parser: protocol.NewParser(conn),
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
		case protocol.ACK:
			seq, _ := strconv.ParseUint(cmd.Subject, 10, 64)
			s.ack.Ack(seq)
			// Update durable cursor if name provided
			if cmd.Sid != "" {
				s.durable.Update(cmd.Sid, seq)
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
		// Resume
		lastSeq := s.durable.Get(durableName) // Keying by Name only (global)
		if lastSeq > 0 {
			// Replay from lastSeq+1
			// This is heavy IO in loop, usually done async
			go func() {
				// Naive replay: Read everything store has > lastSeq
				// Store doesn't have "ReadAfter" yet. We'd read 1 by 1.
				// Optimization: Store.ReadBatch(startSeq)
				// For MVP: Just read next 10.
				for i := 0; i < 10; i++ {
					rec, err := s.store.Read(lastSeq + 1 + uint64(i))
					if err == nil {
						c.Send(sub.Sid, rec.Subject, rec.Data, rec.Sequence)
					}
				}
			}()
		}
	}

	c.conn.Write([]byte("+OK\r\n"))
}

func (s *Server) handleReplay(c *Client, cmd *protocol.Command) {
	// REPLAY <seq>
	// We cheat and put seq in Subject field for now
	seq, err := strconv.Atoi(cmd.Subject)
	if err != nil {
		return
	}

	rec, err := s.store.Read(uint64(seq))
	if err != nil {
		c.conn.Write([]byte(fmt.Sprintf("-ERR %v\r\n", err)))
		return
	}

	// Send as normal MSG with sid "0" or distinct?
	// For REPLAY, usually we deliver to a specific subscription.
	// MVP: Just blast it out as a MSG with sid "replay".
	c.Send("replay", rec.Subject, rec.Data, rec.Sequence)
}

func (c *Client) Send(sid string, subject string, payload []byte, seq uint64) {
	// MSG <subject> <sid> <size> <seq>\r\n<payload>\r\n
	msg := fmt.Sprintf("MSG %s %s %d %d\r\n%s\r\n", subject, sid, len(payload), seq, payload)
	c.conn.Write([]byte(msg))
}
