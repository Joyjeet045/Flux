package ack

import (
	"sync"
	"time"
)

type PendingMessage struct {
	Seq       uint64
	Timestamp time.Time
	Retries   int
	Client    Resender
	Subject   string
	Payload   []byte
	Sid       string
}

type Resender interface {
	Send(sid string, subject string, payload []byte, seq uint64)
}

type Tracker struct {
	mu         sync.Mutex
	pending    map[uint64]*PendingMessage
	timeout    time.Duration
	maxRetry   int
	dlqHandler func(seq uint64, subject string, payload []byte)
}

func NewTracker(timeout time.Duration, maxRetry int) *Tracker {
	t := &Tracker{
		pending:  make(map[uint64]*PendingMessage),
		timeout:  timeout,
		maxRetry: maxRetry,
	}
	go t.loop()
	return t
}

func (t *Tracker) SetDLQHandler(handler func(seq uint64, subject string, payload []byte)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.dlqHandler = handler
}

func (t *Tracker) Track(seq uint64, client Resender, sid, subject string, payload []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending[seq] = &PendingMessage{
		Seq:       seq,
		Timestamp: time.Now(),
		Client:    client,
		Subject:   subject,
		Payload:   payload,
		Sid:       sid,
		Retries:   0,
	}
}

func (t *Tracker) Ack(seq uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.pending, seq)
}

func (t *Tracker) loop() {
	ticker := time.NewTicker(500 * time.Millisecond) // Check often
	for range ticker.C {
		t.checkTimeouts()
	}
}

func (t *Tracker) checkTimeouts() {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	for seq, msg := range t.pending {
		if now.Sub(msg.Timestamp) > t.timeout {
			if msg.Retries >= t.maxRetry {
				// Move to DLQ
				if t.dlqHandler != nil {
					// Run in goroutine to prevent deadlock (handler might call Track, which needs lock)
					go t.dlqHandler(seq, msg.Subject, msg.Payload)
				}
				delete(t.pending, seq)
				continue
			}

			// Resend
			msg.Client.Send(msg.Sid, msg.Subject, msg.Payload, msg.Seq)
			msg.Retries++
			msg.Timestamp = now // Reset timer
		}
	}
}
