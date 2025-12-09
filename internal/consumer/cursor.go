package consumer

import (
	"sync"
)

// PullCursor tracks per-consumer pull positions
type PullCursor struct {
	mu      sync.RWMutex
	cursors map[string]uint64 // key: "subject:consumerID" -> last pulled seq
}

func NewPullCursor() *PullCursor {
	return &PullCursor{
		cursors: make(map[string]uint64),
	}
}

func (pc *PullCursor) Get(subject, consumerID string) uint64 {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	key := subject + ":" + consumerID
	return pc.cursors[key]
}

func (pc *PullCursor) Update(subject, consumerID string, seq uint64) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	key := subject + ":" + consumerID
	if current, ok := pc.cursors[key]; !ok || seq > current {
		pc.cursors[key] = seq
	}
}

// QueueGroupBalancer implements round-robin load balancing
type QueueGroupBalancer struct {
	mu       sync.Mutex
	counters map[string]int // queue name -> next index
}

func NewQueueGroupBalancer() *QueueGroupBalancer {
	return &QueueGroupBalancer{
		counters: make(map[string]int),
	}
}

func (qgb *QueueGroupBalancer) SelectMember(queueName string, memberCount int) int {
	qgb.mu.Lock()
	defer qgb.mu.Unlock()

	idx := qgb.counters[queueName] % memberCount
	qgb.counters[queueName]++

	return idx
}
