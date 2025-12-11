package flowcontrol

import (
	"fmt"
	"sync"
	"time"
)

type ConsumerMode int

const (
	PushMode ConsumerMode = iota
	PullMode
)

type ConsumerConfig struct {
	Mode               ConsumerMode
	RateLimit          float64
	RateBurst          int
	BufferSize         int
	BackpressureMode   BackpressureMode
	SlowThreshold      float64
	EnableRateLimit    bool
	EnableBackpressure bool
}

type FlowControlledConsumer struct {
	mu           sync.RWMutex
	id           string
	config       ConsumerConfig
	rateLimiter  *TokenBucket
	backpressure *BackpressureHandler
	sender       MessageSender
	active       bool
	stopChan     chan struct{}
	wg           sync.WaitGroup
}

type MessageSender interface {
	Send(sid string, subject string, payload []byte, seq uint64)
}

func NewFlowControlledConsumer(id string, config ConsumerConfig, sender MessageSender) *FlowControlledConsumer {
	fc := &FlowControlledConsumer{
		id:       id,
		config:   config,
		sender:   sender,
		active:   true,
		stopChan: make(chan struct{}),
	}

	if config.EnableRateLimit {
		fc.rateLimiter = NewTokenBucket(config.RateLimit, config.RateBurst)
	}

	if config.EnableBackpressure {
		fc.backpressure = NewBackpressureHandler(
			config.BufferSize,
			config.BackpressureMode,
			config.SlowThreshold,
		)
	}

	if config.Mode == PushMode && config.EnableBackpressure {
		fc.wg.Add(1)
		go fc.pushLoop()
	}

	return fc
}

func (fc *FlowControlledConsumer) Publish(msg Message) error {
	fc.mu.RLock()
	if !fc.active {
		fc.mu.RUnlock()
		return fmt.Errorf("consumer inactive")
	}
	fc.mu.RUnlock()

	if fc.config.Mode == PushMode {
		return fc.handlePushPublish(msg)
	}
	return fc.handlePullPublish(msg)
}

func (fc *FlowControlledConsumer) handlePushPublish(msg Message) error {
	if fc.config.EnableBackpressure {
		if !fc.backpressure.Enqueue(msg) {
			return fmt.Errorf("backpressure: message dropped")
		}
		return nil
	}

	if fc.config.EnableRateLimit {
		if !fc.rateLimiter.Allow() {
			return fmt.Errorf("rate limit exceeded")
		}
	}

	fc.sender.Send(msg.Sid, msg.Subject, msg.Payload, msg.Sequence)
	return nil
}

func (fc *FlowControlledConsumer) handlePullPublish(msg Message) error {
	if !fc.config.EnableBackpressure {
		return fmt.Errorf("pull mode requires backpressure enabled")
	}

	if !fc.backpressure.Enqueue(msg) {
		return fmt.Errorf("buffer full")
	}
	return nil
}

func (fc *FlowControlledConsumer) Pull(count int) ([]Message, error) {
	fc.mu.RLock()
	if !fc.active {
		fc.mu.RUnlock()
		return nil, fmt.Errorf("consumer inactive")
	}
	if fc.config.Mode != PullMode {
		fc.mu.RUnlock()
		return nil, fmt.Errorf("not in pull mode")
	}
	fc.mu.RUnlock()

	if fc.config.EnableRateLimit {
		if !fc.rateLimiter.AllowN(count) {
			available := fc.rateLimiter.Available()
			if available == 0 {
				return nil, fmt.Errorf("rate limit exceeded")
			}
			count = available
		}
	}

	messages := make([]Message, 0, count)
	timeout := 100 * time.Millisecond

	for i := 0; i < count; i++ {
		msg, ok := fc.backpressure.DequeueTimeout(timeout)
		if !ok {
			break
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

func (fc *FlowControlledConsumer) pushLoop() {
	defer fc.wg.Done()

	for {
		select {
		case <-fc.stopChan:
			return
		default:
			msg, ok := fc.backpressure.DequeueTimeout(100 * time.Millisecond)
			if !ok {
				continue
			}

			if fc.config.EnableRateLimit {
				for !fc.rateLimiter.Allow() {
					select {
					case <-fc.stopChan:
						return
					case <-time.After(10 * time.Millisecond):
					}
				}
			}

			fc.sender.Send(msg.Sid, msg.Subject, msg.Payload, msg.Sequence)
		}
	}
}

func (fc *FlowControlledConsumer) Stats() map[string]interface{} {
	stats := map[string]interface{}{
		"id":     fc.id,
		"mode":   fc.config.Mode,
		"active": fc.active,
	}

	if fc.config.EnableRateLimit {
		stats["rate_limit_tokens"] = fc.rateLimiter.Available()
	}

	if fc.config.EnableBackpressure {
		buffered, dropped, delivered, usage := fc.backpressure.Stats()
		stats["buffered"] = buffered
		stats["dropped"] = dropped
		stats["delivered"] = delivered
		stats["buffer_usage"] = usage
		stats["slow_consumer"] = fc.backpressure.IsSlowConsumer()
	}

	return stats
}

func (fc *FlowControlledConsumer) Close() {
	fc.mu.Lock()
	if !fc.active {
		fc.mu.Unlock()
		return
	}
	fc.active = false
	fc.mu.Unlock()

	close(fc.stopChan)
	fc.wg.Wait()

	if fc.config.EnableBackpressure {
		fc.backpressure.Close()
	}
}

func (fc *FlowControlledConsumer) IsSlowConsumer() bool {
	if !fc.config.EnableBackpressure {
		return false
	}
	return fc.backpressure.IsSlowConsumer()
}
