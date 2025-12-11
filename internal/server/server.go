package server

import (
	"encoding/json"
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
	"nats-lite/internal/flowcontrol"
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

type PullCursorStore struct {
	mu      sync.RWMutex
	cursors map[string]uint64
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
	conn            net.Conn
	srv             *Server
	parser          *protocol.Parser
	durableSubs     map[string]string
	flowControllers map[string]*flowcontrol.FlowControlledConsumer
	mu              sync.Mutex
}

func (c *Client) Send(sid string, subject string, payload []byte, seq uint64) {
	c.mu.Lock()
	fc, hasFC := c.flowControllers[sid]
	c.mu.Unlock()

	if hasFC {
		msg := flowcontrol.Message{
			Sid:      sid,
			Subject:  subject,
			Payload:  payload,
			Sequence: seq,
		}
		fc.Publish(msg)
		return
	}

	msg := fmt.Sprintf("MSG %s %s %d %d\r\n%s\r\n", subject, sid, len(payload), seq, payload)
	c.conn.Write([]byte(msg))
}

func New(cfg *config.Config) (*Server, error) {
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
		conn:            conn,
		srv:             s,
		parser:          protocol.NewParser(conn),
		durableSubs:     make(map[string]string),
		flowControllers: make(map[string]*flowcontrol.FlowControlledConsumer),
	}

	s.mu.Lock()
	s.clients[client] = true
	s.mu.Unlock()

	defer func() {
		client.mu.Lock()
		for _, fc := range client.flowControllers {
			fc.Close()
		}
		client.mu.Unlock()

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
		case protocol.FLOWCTL:
			s.handleFlowControl(client, cmd)
		case protocol.STATS:
			s.handleStats(client, cmd)
		case protocol.ACK:
			seq, _ := strconv.ParseUint(cmd.Subject, 10, 64)
			s.ack.Ack(seq)
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
	seq, err := s.store.Append(cmd.Subject, cmd.Payload)
	if err != nil {
		log.Println("Storage error:", err)
	}

	subs := s.matcher.Match(cmd.Subject)
	for _, sub := range subs {
		s.ack.Track(seq, sub.Client, sub.Sid, cmd.Subject, cmd.Payload)
		sub.Client.Send(sub.Sid, cmd.Subject, cmd.Payload, seq)
	}
}

func (s *Server) handleSub(c *Client, cmd *protocol.Command) {
	queue := ""
	if len(cmd.Payload) > 0 {
		queue = string(cmd.Payload)
	}

	durableName := ""
	if len(queue) > 8 && queue[:8] == "DURABLE:" {
		durableName = queue[8:]
		queue = ""
	}

	sub := &topics.Subscription{
		Subject: cmd.Subject,
		Sid:     cmd.Sid,
		Queue:   queue,
		Client:  c,
	}
	s.matcher.Subscribe(sub)

	c.mu.Lock()
	if _, exists := c.flowControllers[cmd.Sid]; !exists {
		fcConfig := s.getDefaultFlowControlConfig()
		c.flowControllers[cmd.Sid] = flowcontrol.NewFlowControlledConsumer(
			cmd.Sid,
			fcConfig,
			c,
		)
	}
	c.mu.Unlock()

	if durableName != "" {
		cursorKey := cmd.Subject + ":" + durableName
		lastSeq := s.durable.Get(cursorKey)

		c.mu.Lock()
		c.durableSubs[sub.Sid] = cursorKey
		c.mu.Unlock()

		if lastSeq > 0 {
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
						break
					}

					for _, rec := range records {
						if rec.Subject == cmd.Subject {
							c.Send(sub.Sid, rec.Subject, rec.Data, rec.Sequence)
							currentSeq = rec.Sequence + 1
						}
					}

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
	mode := cmd.Subject
	args := cmd.Sid

	var req store.ReplayRequest
	req.MaxCount = 100

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
	subject := cmd.Subject
	count, err := strconv.Atoi(cmd.Sid)
	if err != nil || count <= 0 {
		c.conn.Write([]byte("-ERR invalid count\r\n"))
		return
	}

	consumerID := c.conn.RemoteAddr().String()
	lastSeq := s.pullCursors.Get(subject, consumerID)

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

	if maxSeq > 0 {
		s.pullCursors.Update(subject, consumerID, maxSeq)
	}

	c.conn.Write([]byte(fmt.Sprintf("+OK %d\r\n", sent)))
}

func (s *Server) handleFlowControl(c *Client, cmd *protocol.Command) {
	sid := cmd.Subject
	args := strings.Fields(cmd.Sid)

	if len(args) < 1 {
		c.conn.Write([]byte("-ERR invalid FLOWCTL arguments\r\n"))
		return
	}

	mode := strings.ToUpper(args[0])

	fcConfig := s.getDefaultFlowControlConfig()

	if mode == "PUSH" {
		fcConfig.Mode = flowcontrol.PushMode
	} else if mode == "PULL" {
		fcConfig.Mode = flowcontrol.PullMode
	} else {
		c.conn.Write([]byte("-ERR invalid mode (PUSH or PULL)\r\n"))
		return
	}

	if len(args) > 1 {
		if rate, err := strconv.ParseFloat(args[1], 64); err == nil && rate > 0 {
			fcConfig.RateLimit = rate
		}
	}
	if len(args) > 2 {
		if burst, err := strconv.Atoi(args[2]); err == nil && burst > 0 {
			fcConfig.RateBurst = burst
		}
	}
	if len(args) > 3 {
		if bufSize, err := strconv.Atoi(args[3]); err == nil && bufSize > 0 {
			fcConfig.BufferSize = bufSize
		}
	}

	c.mu.Lock()
	if oldFC, exists := c.flowControllers[sid]; exists {
		oldFC.Close()
	}
	c.flowControllers[sid] = flowcontrol.NewFlowControlledConsumer(sid, fcConfig, c)
	c.mu.Unlock()

	c.conn.Write([]byte(fmt.Sprintf("+OK mode=%s rate=%.0f burst=%d buffer=%d\r\n",
		mode, fcConfig.RateLimit, fcConfig.RateBurst, fcConfig.BufferSize)))
}

func (s *Server) handleStats(c *Client, cmd *protocol.Command) {
	c.mu.Lock()
	defer c.mu.Unlock()

	stats := make(map[string]interface{})

	if cmd.Sid == "" {
		for sid, fc := range c.flowControllers {
			stats[sid] = fc.Stats()
		}
	} else {
		if fc, exists := c.flowControllers[cmd.Sid]; exists {
			stats[cmd.Sid] = fc.Stats()
		}
	}

	jsonData, _ := json.Marshal(stats)
	c.conn.Write([]byte(fmt.Sprintf("+STATS %s\r\n", jsonData)))
}

func (s *Server) getDefaultFlowControlConfig() flowcontrol.ConsumerConfig {
	bpMode := flowcontrol.ModeDrop
	switch s.config.FlowControl.BackpressureMode {
	case "block":
		bpMode = flowcontrol.ModeBlock
	case "shed":
		bpMode = flowcontrol.ModeShed
	}

	return flowcontrol.ConsumerConfig{
		Mode:               flowcontrol.PushMode,
		RateLimit:          s.config.FlowControl.DefaultRateLimit,
		RateBurst:          s.config.FlowControl.DefaultRateBurst,
		BufferSize:         s.config.FlowControl.DefaultBufferSize,
		BackpressureMode:   bpMode,
		SlowThreshold:      s.config.FlowControl.DefaultSlowThreshold,
		EnableRateLimit:    s.config.FlowControl.EnableRateLimit,
		EnableBackpressure: s.config.FlowControl.EnableBackpressure,
	}
}

func (s *Server) Shutdown() error {
	log.Println("Flushing durable cursors...")
	s.durable.ForceFlush()
	s.durable.Close()
	s.store.Close()
	log.Println("Shutdown complete")
	return nil
}
