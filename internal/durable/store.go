package durable

import (
	"encoding/json"
	"os"
	"sync"
)

// Store tracks consumer progress (Subject + Sid -> Last Acked Seq)
type Store struct {
	mu   sync.RWMutex
	path string
	// map key format: "subject#queue#sid" or simplified
	// Durable is usually identified by a "Durable Name" sent by client.
	// For MVP, we'll assume SID is the durable name if flag provided in SUB.
	cursors map[string]uint64
}

func NewStore(path string) (*Store, error) {
	s := &Store{
		path:    path,
		cursors: make(map[string]uint64),
	}
	s.load()
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
	f, err := os.Create(s.path)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(s.cursors)
}

func (s *Store) Update(key string, seq uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if current, ok := s.cursors[key]; ok && seq <= current {
		return nil // Don't move backwards
	}

	s.cursors[key] = seq
	// For MVP, simplistic sync save. In prod, batch or WAL.
	return s.save()
}

func (s *Store) Get(key string) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cursors[key]
}
