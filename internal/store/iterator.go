package store

import (
	"time"
)

type ReplayMode int

const (
	ReplayFromSeq ReplayMode = iota
	ReplayFromTime
	ReplayFromFirst
	ReplayFromLast
)

type ReplayRequest struct {
	Mode      ReplayMode
	StartSeq  uint64
	StartTime time.Time
	MaxCount  int
}

func (s *Store) ReadBatch(req ReplayRequest) ([]*Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*Record

	switch req.Mode {
	case ReplayFromSeq:
		// Read from specific sequence
		for i := 0; i < req.MaxCount; i++ {
			rec, err := s.readUnlocked(req.StartSeq + uint64(i))
			if err != nil {
				break
			}
			results = append(results, rec)
		}

	case ReplayFromTime:
		// Scan all segments to find messages after timestamp
		targetNano := req.StartTime.UnixNano()
		count := 0

		for _, seg := range s.segments {
			offset := int64(0)
			for offset < seg.currentSize && count < req.MaxCount {
				rec, n, err := seg.ReadAt(offset)
				if err != nil {
					break
				}

				if rec.Timestamp >= targetNano {
					results = append(results, rec)
					count++
				}
				offset += n
			}
			if count >= req.MaxCount {
				break
			}
		}

	case ReplayFromFirst:
		// Read from oldest available
		if len(s.segments) == 0 {
			return results, nil
		}

		firstSeg := s.segments[0]
		offset := int64(0)
		count := 0

		for offset < firstSeg.currentSize && count < req.MaxCount {
			rec, n, err := firstSeg.ReadAt(offset)
			if err != nil {
				break
			}
			results = append(results, rec)
			count++
			offset += n
		}

	case ReplayFromLast:
		// Read only the last message
		if s.nextSeq > 1 {
			rec, err := s.readUnlocked(s.nextSeq - 1)
			if err == nil {
				results = append(results, rec)
			}
		}
	}

	return results, nil
}

func (s *Store) readUnlocked(seq uint64) (*Record, error) {
	loc, ok := s.index[seq]
	if !ok {
		return nil, nil
	}

	var targetSeg *Segment
	for _, seg := range s.segments {
		if seg.BaseSequence == loc.SegmentBase {
			targetSeg = seg
			break
		}
	}

	if targetSeg == nil {
		return nil, nil
	}

	rec, _, err := targetSeg.ReadAt(loc.Offset)
	return rec, err
}
