package store

import (
	"log"
	"time"
)

type RetentionPolicy struct {
	MaxAge   time.Duration
	MaxBytes int64
}

func (s *Store) EnforceRetention(p RetentionPolicy) {
	// Size-based
	if p.MaxBytes > 0 {
		for {
			total := s.TotalSize()
			if total <= p.MaxBytes {
				break
			}

			// Need to delete oldest
			oldestBase := s.OldestSegmentBase()
			if oldestBase == 0 {
				break
			}

			// Can't delete active segment
			s.mu.RLock()
			isActive := s.activeSeg.BaseSequence == oldestBase
			s.mu.RUnlock()

			if isActive {
				break // Can't delete the only/active segment even if full
			}

			log.Printf("Retention: Removing segment %d (Total Size: %d > %d)", oldestBase, total, p.MaxBytes)
			if err := s.RemoveSegment(oldestBase); err != nil {
				log.Println("Error removing segment:", err)
				break
			}
		}
	}

	// Time-based
	if p.MaxAge > 0 {
		for {
			oldestBase := s.OldestSegmentBase()
			if oldestBase == 0 {
				break
			}

			s.mu.RLock()
			isActive := s.activeSeg.BaseSequence == oldestBase
			seg := s.segments[0]
			s.mu.RUnlock()

			if isActive {
				break
			}

			// Read first record to check time
			// Optimization: Cache timestamp in Segment struct? For now, read it.
			rec, _, err := seg.ReadAt(0)
			if err != nil {
				log.Println("Error reading segment for retention check:", err)
				break
			}

			age := time.Since(time.Unix(0, rec.Timestamp))
			if age > p.MaxAge {
				log.Printf("Retention: Removing segment %d (Age: %v > %v)", oldestBase, age, p.MaxAge)
				if err := s.RemoveSegment(oldestBase); err != nil {
					log.Println("Error removing segment:", err)
					break
				}
			} else {
				break // Oldest is new enough, so subsequent ones are too
			}
		}
	}
}
