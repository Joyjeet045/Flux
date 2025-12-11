package dedup

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

type Deduplicator struct {
	mu           sync.RWMutex
	window       map[string]time.Time
	windowSize   time.Duration
	maxEntries   int
	cleanupTimer *time.Ticker
	stopChan     chan struct{}
	stats        Stats
}

type Stats struct {
	TotalChecked    uint64
	DuplicatesFound uint64
	UniqueMessages  uint64
	WindowSize      int
}

func New(windowSize time.Duration, maxEntries int) *Deduplicator {
	d := &Deduplicator{
		window:       make(map[string]time.Time),
		windowSize:   windowSize,
		maxEntries:   maxEntries,
		cleanupTimer: time.NewTicker(windowSize / 10),
		stopChan:     make(chan struct{}),
	}

	go d.cleanupLoop()

	return d
}

func (d *Deduplicator) IsDuplicate(messageID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.stats.TotalChecked++

	if messageID == "" {
		d.stats.UniqueMessages++
		return false
	}

	now := time.Now()

	if timestamp, exists := d.window[messageID]; exists {
		if now.Sub(timestamp) < d.windowSize {
			d.stats.DuplicatesFound++
			return true
		}
		delete(d.window, messageID)
	}

	if len(d.window) >= d.maxEntries {
		d.evictOldest()
	}

	d.window[messageID] = now
	d.stats.UniqueMessages++
	return false
}

func (d *Deduplicator) GenerateID(subject string, payload []byte) string {
	hash := sha256.New()
	hash.Write([]byte(subject))
	hash.Write(payload)
	hash.Write([]byte(time.Now().Format(time.RFC3339Nano)))
	return hex.EncodeToString(hash.Sum(nil))[:32]
}

func (d *Deduplicator) cleanupLoop() {
	for {
		select {
		case <-d.cleanupTimer.C:
			d.cleanup()
		case <-d.stopChan:
			return
		}
	}
}

func (d *Deduplicator) cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	for id, timestamp := range d.window {
		if now.Sub(timestamp) >= d.windowSize {
			delete(d.window, id)
		}
	}
}

func (d *Deduplicator) evictOldest() {
	var oldestID string
	var oldestTime time.Time
	first := true

	for id, timestamp := range d.window {
		if first || timestamp.Before(oldestTime) {
			oldestID = id
			oldestTime = timestamp
			first = false
		}
	}

	if oldestID != "" {
		delete(d.window, oldestID)
	}
}

func (d *Deduplicator) Stats() Stats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	stats := d.stats
	stats.WindowSize = len(d.window)
	return stats
}

func (d *Deduplicator) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.window = make(map[string]time.Time)
	d.stats = Stats{}
}

func (d *Deduplicator) Close() {
	close(d.stopChan)
	d.cleanupTimer.Stop()
}
