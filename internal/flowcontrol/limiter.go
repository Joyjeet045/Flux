package flowcontrol

import (
	"sync"
	"time"
)

type TokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	capacity   float64
	refillRate float64
	lastRefill time.Time
}

func NewTokenBucket(rate float64, burst int) *TokenBucket {
	return &TokenBucket{
		tokens:     float64(burst),
		capacity:   float64(burst),
		refillRate: rate,
		lastRefill: time.Now(),
	}
}

func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()

	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		return true
	}
	return false
}

func (tb *TokenBucket) AllowN(n int) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()

	needed := float64(n)
	if tb.tokens >= needed {
		tb.tokens -= needed
		return true
	}
	return false
}

func (tb *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()

	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}

	tb.lastRefill = now
}

func (tb *TokenBucket) Available() int {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	return int(tb.tokens)
}
