package flowcontrol

import (
	"sync"
	"sync/atomic"
	"time"
)

type Message struct {
	Sid      string
	Subject  string
	Payload  []byte
	Sequence uint64
}

type BackpressureMode int

const (
	ModeDrop BackpressureMode = iota
	ModeBlock
	ModeShed
)

type BackpressureHandler struct {
	mu             sync.RWMutex
	buffer         chan Message
	bufferSize     int
	mode           BackpressureMode
	droppedCount   uint64
	slowConsumer   bool
	slowThreshold  float64
	lastCheck      time.Time
	deliveredCount uint64
	checkInterval  time.Duration
}

func NewBackpressureHandler(bufferSize int, mode BackpressureMode, slowThreshold float64) *BackpressureHandler {
	return &BackpressureHandler{
		buffer:        make(chan Message, bufferSize),
		bufferSize:    bufferSize,
		mode:          mode,
		slowThreshold: slowThreshold,
		lastCheck:     time.Now(),
		checkInterval: 5 * time.Second,
	}
}

func (bh *BackpressureHandler) Enqueue(msg Message) bool {
	bh.checkSlowConsumer()

	switch bh.mode {
	case ModeDrop:
		select {
		case bh.buffer <- msg:
			return true
		default:
			atomic.AddUint64(&bh.droppedCount, 1)
			return false
		}

	case ModeBlock:
		bh.buffer <- msg
		return true

	case ModeShed:
		if bh.IsSlowConsumer() {
			atomic.AddUint64(&bh.droppedCount, 1)
			return false
		}
		select {
		case bh.buffer <- msg:
			return true
		default:
			atomic.AddUint64(&bh.droppedCount, 1)
			return false
		}
	}

	return false
}

func (bh *BackpressureHandler) Dequeue() (Message, bool) {
	msg, ok := <-bh.buffer
	if ok {
		atomic.AddUint64(&bh.deliveredCount, 1)
	}
	return msg, ok
}

func (bh *BackpressureHandler) DequeueTimeout(timeout time.Duration) (Message, bool) {
	select {
	case msg, ok := <-bh.buffer:
		if ok {
			atomic.AddUint64(&bh.deliveredCount, 1)
		}
		return msg, ok
	case <-time.After(timeout):
		return Message{}, false
	}
}

func (bh *BackpressureHandler) BufferUsage() float64 {
	return float64(len(bh.buffer)) / float64(bh.bufferSize)
}

func (bh *BackpressureHandler) IsSlowConsumer() bool {
	bh.mu.RLock()
	defer bh.mu.RUnlock()
	return bh.slowConsumer
}

func (bh *BackpressureHandler) checkSlowConsumer() {
	now := time.Now()
	if now.Sub(bh.lastCheck) < bh.checkInterval {
		return
	}

	bh.mu.Lock()
	defer bh.mu.Unlock()

	usage := bh.BufferUsage()
	bh.slowConsumer = usage > bh.slowThreshold
	bh.lastCheck = now
}

func (bh *BackpressureHandler) Stats() (buffered int, dropped uint64, delivered uint64, usage float64) {
	return len(bh.buffer),
		atomic.LoadUint64(&bh.droppedCount),
		atomic.LoadUint64(&bh.deliveredCount),
		bh.BufferUsage()
}

func (bh *BackpressureHandler) Close() {
	close(bh.buffer)
}
