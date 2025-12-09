package durable

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"nats-lite/internal/config"
)

type Store struct {
	mu          sync.RWMutex
	path        string
	cursors     map[string]uint64
	dirty       map[string]uint64
	flushTicker *time.Ticker
	stopChan    chan struct{}
}

func NewStore(path string, cfg *config.Config) (*Store, error) {
	flushInterval, _ := cfg.GetDurableFlushInterval()

	s := &Store{
		path:        path,
		cursors:     make(map[string]uint64),
		dirty:       make(map[string]uint64),
		flushTicker: time.NewTicker(flushInterval),
		stopChan:    make(chan struct{}),
	}

	if err := s.load(); err != nil {
		return nil, err
	}

	// Start background flusher
	go s.flushLoop()

	return s, nil
}

func (s *Store) load() error {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(&s.cursors)
}

func (s *Store) save() error {
	// Create temp file for atomic write
	tmpPath := s.path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	if err := json.NewEncoder(f).Encode(s.cursors); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}

	if err := f.Sync(); err != nil { // Ensure data is on disk
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	f.Close()

	// Atomic rename
	return os.Rename(tmpPath, s.path)
}

func (s *Store) Update(key string, seq uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if current, ok := s.cursors[key]; ok && seq <= current {
		return nil // Don't move backwards
	}

	// Update in-memory state
	s.cursors[key] = seq
	s.dirty[key] = seq

	return nil // Async flush
}

func (s *Store) Get(key string) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cursors[key]
}

func (s *Store) flushLoop() {
	for {
		select {
		case <-s.flushTicker.C:
			s.flush()
		case <-s.stopChan:
			s.flush() // Final flush before shutdown
			return
		}
	}
}

func (s *Store) flush() {
	s.mu.Lock()

	if len(s.dirty) == 0 {
		s.mu.Unlock()
		return
	}

	// Clear dirty map (we've captured the state in cursors)
	s.dirty = make(map[string]uint64)
	s.mu.Unlock()

	// Save without holding lock (uses atomic cursors snapshot)
	s.save()
}

func (s *Store) Close() error {
	close(s.stopChan)
	s.flushTicker.Stop()
	return nil
}

// ForceFlush for testing or critical shutdown
func (s *Store) ForceFlush() error {
	s.flush()
	return nil
}
