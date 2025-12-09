package store

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	MaxSegmentSize = 10 * 1024 * 1024 // 10MB
)

type Store struct {
	mu        sync.RWMutex
	dir       string
	segments  []*Segment
	activeSeg *Segment
	index     map[uint64]Location
	nextSeq   uint64

	// Retention logic can be added later
}

type Location struct {
	SegmentBase uint64 // BaseSequence of the segment
	Offset      int64  // Offset in the file
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	s := &Store{
		dir:     dir,
		index:   make(map[uint64]Location),
		nextSeq: 1,
	}

	if err := s.recover(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Store) recover() error {
	files, err := ioutil.ReadDir(s.dir)
	if err != nil {
		return err
	}

	var bases []uint64
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".log") {
			baseStr := strings.TrimSuffix(f.Name(), ".log")
			base, err := strconv.ParseUint(baseStr, 10, 64)
			if err == nil {
				bases = append(bases, base)
			}
		}
	}

	sort.Slice(bases, func(i, j int) bool { return bases[i] < bases[j] })

	if len(bases) == 0 {
		// New store
		seg, err := NewSegment(filepath.Join(s.dir, fmt.Sprintf("%020d.log", 1)), 1)
		if err != nil {
			return err
		}
		s.segments = append(s.segments, seg)
		s.activeSeg = seg
		s.nextSeq = 1
		return nil
	}

	// Replay segments to build index
	for _, base := range bases {
		path := filepath.Join(s.dir, fmt.Sprintf("%020d.log", base))
		seg, err := NewSegment(path, base)
		if err != nil {
			return err
		}
		s.segments = append(s.segments, seg)

		// Scan segment
		offset := int64(0)
		for {
			// Check EOF
			if offset >= seg.currentSize {
				break
			}

			rec, n, err := seg.ReadAt(offset)
			if err != nil {
				if err == io.EOF {
					break
				}
				// If corrupt, maybe truncate? For now, error
				fmt.Printf("Error reading segment %d at %d: %v\n", base, offset, err)
				break
			}

			s.index[rec.Sequence] = Location{SegmentBase: base, Offset: offset}
			if rec.Sequence >= s.nextSeq {
				s.nextSeq = rec.Sequence + 1
			}
			offset += n
		}

		seg.nextOffset = offset // Ensure next write is at end
	}

	s.activeSeg = s.segments[len(s.segments)-1]
	return nil
}

func (s *Store) Append(subject string, data []byte) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Rotation check
	if s.activeSeg.currentSize >= MaxSegmentSize {
		if err := s.rotate(); err != nil {
			return 0, err
		}
	}

	rec := &Record{
		Sequence:  s.nextSeq,
		Timestamp: time.Now().UnixNano(),
		Subject:   subject,
		Data:      data,
	}

	offset, _, err := s.activeSeg.Append(rec)
	if err != nil {
		return 0, err
	}

	seq := s.nextSeq
	s.index[seq] = Location{SegmentBase: s.activeSeg.BaseSequence, Offset: offset}
	s.nextSeq++

	return seq, nil
}

func (s *Store) rotate() error {
	newBase := s.nextSeq
	path := filepath.Join(s.dir, fmt.Sprintf("%020d.log", newBase)) // 20 digits for sortable
	seg, err := NewSegment(path, newBase)
	if err != nil {
		return err
	}
	s.segments = append(s.segments, seg)
	s.activeSeg = seg
	return nil
}

func (s *Store) SegmentCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.segments)
}

func (s *Store) OldestSegmentBase() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.segments) == 0 {
		return 0
	}
	return s.segments[0].BaseSequence
}

func (s *Store) RemoveSegment(baseSeq uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.segments) == 0 {
		return nil
	}

	if s.segments[0].BaseSequence != baseSeq {
		return fmt.Errorf("can only remove oldest segment")
	}

	seg := s.segments[0]
	if seg == s.activeSeg {
		return fmt.Errorf("cannot remove active segment")
	}

	// Remove from list
	s.segments = s.segments[1:]

	// Cleanup index (expensive? iterate all? For MVP, index cleanup is tricky without reverse map)
	// We can lazily leak index entries or iterate.
	// Optimize: Delete from index where Seq < next segment base
	nextBase := s.segments[0].BaseSequence
	for seq := range s.index {
		if seq < nextBase {
			delete(s.index, seq)
		}
	}

	return seg.Remove()
}

func (s *Store) TotalSize() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var size int64
	for _, seg := range s.segments {
		size += seg.currentSize
	}
	return size
}

func (s *Store) Read(seq uint64) (*Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	loc, ok := s.index[seq]
	if !ok {
		return nil, fmt.Errorf("msg not found")
	}

	// Find segment
	// Optimization: could map base -> segment, but simple list is fine for MVP (usually few segments)
	var targetSeg *Segment
	for _, seg := range s.segments {
		if seg.BaseSequence == loc.SegmentBase {
			targetSeg = seg
			break
		}
	}

	if targetSeg == nil {
		return nil, fmt.Errorf("segment missing")
	}

	rec, _, err := targetSeg.ReadAt(loc.Offset)
	return rec, err
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, seg := range s.segments {
		seg.Close()
	}
	return nil
}
