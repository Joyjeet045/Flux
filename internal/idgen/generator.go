package idgen

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Snowflake-inspired ID generator for distributed systems
// Format: timestamp(41 bits) + node(10 bits) + sequence(12 bits)
type Generator struct {
	mu       sync.Mutex
	nodeID   uint64
	sequence uint64
	lastTime int64
	epoch    int64 // Custom epoch (2024-01-01)
}

const (
	nodeBits     = 10
	sequenceBits = 12
	maxNodeID    = (1 << nodeBits) - 1
	maxSequence  = (1 << sequenceBits) - 1
	timeShift    = nodeBits + sequenceBits
	nodeShift    = sequenceBits
)

func NewGenerator(nodeID uint64) *Generator {
	if nodeID > maxNodeID {
		nodeID = nodeID % (maxNodeID + 1)
	}

	// Custom epoch: 2024-01-01 00:00:00 UTC
	epoch := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()

	return &Generator{
		nodeID: nodeID,
		epoch:  epoch,
	}
}

func (g *Generator) NextID() string {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now().UnixMilli()
	if now < g.lastTime {
		// Clock moved backwards, wait
		now = g.lastTime
	}

	if now == g.lastTime {
		g.sequence = (g.sequence + 1) & maxSequence
		if g.sequence == 0 {
			// Sequence overflow, wait for next millisecond
			for now <= g.lastTime {
				now = time.Now().UnixMilli()
			}
		}
	} else {
		g.sequence = 0
	}

	g.lastTime = now

	timestamp := uint64(now - g.epoch)
	id := (timestamp << timeShift) | (g.nodeID << nodeShift) | g.sequence

	return fmt.Sprintf("job-%016x", id)
}

// UUID v4 generator (simpler, globally unique)
func GenerateUUID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)

	// Set version (4) and variant bits
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80

	return fmt.Sprintf("job-%s-%s-%s-%s-%s",
		hex.EncodeToString(bytes[0:4]),
		hex.EncodeToString(bytes[4:6]),
		hex.EncodeToString(bytes[6:8]),
		hex.EncodeToString(bytes[8:10]),
		hex.EncodeToString(bytes[10:16]))
}

// Simple UUID for when you don't need ordering
func SimpleID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return fmt.Sprintf("job-%s", hex.EncodeToString(bytes))
}
