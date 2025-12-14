package client

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"
)

// BrokerPool manages connections to multiple brokers with failover
type BrokerPool struct {
	brokers       []string
	conn          net.Conn
	connMu        sync.RWMutex
	currentBroker string
	maxRetries    int
	retryDelay    time.Duration
}

func NewBrokerPool(brokers []string) *BrokerPool {
	return &BrokerPool{
		brokers:    brokers,
		maxRetries: 3,
		retryDelay: 100 * time.Millisecond,
	}
}

// ConnectWithRetry attempts to connect with exponential backoff
func (bp *BrokerPool) ConnectWithRetry(ctx context.Context) error {
	attempt := 0
	delay := bp.retryDelay

	for attempt < bp.maxRetries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := bp.tryConnect(ctx)
		if err == nil {
			return nil
		}

		attempt++
		if attempt < bp.maxRetries {
			jitter := time.Duration(rand.Int63n(int64(delay / 2)))
			time.Sleep(delay + jitter)
			delay *= 2 // Exponential backoff
			if delay > 5*time.Second {
				delay = 5 * time.Second
			}
		}
	}

	return fmt.Errorf("failed to connect after %d attempts", bp.maxRetries)
}

func (bp *BrokerPool) tryConnect(ctx context.Context) error {
	// Shuffle brokers for load distribution
	shuffled := make([]string, len(bp.brokers))
	copy(shuffled, bp.brokers)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	for _, broker := range shuffled {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		dialer := &net.Dialer{
			Timeout: 2 * time.Second,
		}

		conn, err := dialer.DialContext(ctx, "tcp", broker)
		if err != nil {
			log.Printf("Failed to connect to %s: %v", broker, err)
			continue
		}

		bp.connMu.Lock()
		bp.conn = conn
		bp.currentBroker = broker
		bp.connMu.Unlock()

		log.Printf("Connected to broker: %s", broker)
		return nil
	}

	return fmt.Errorf("all brokers unreachable")
}

// WriteWithRetry writes with automatic reconnection on failure
func (bp *BrokerPool) WriteWithRetry(ctx context.Context, data []byte) error {
	attempt := 0
	for attempt < bp.maxRetries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		bp.connMu.RLock()
		conn := bp.conn
		bp.connMu.RUnlock()

		if conn == nil {
			if err := bp.ConnectWithRetry(ctx); err != nil {
				return err
			}
			bp.connMu.RLock()
			conn = bp.conn
			bp.connMu.RUnlock()
		}

		// Set write deadline
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_, err := conn.Write(data)
		if err == nil {
			return nil
		}

		log.Printf("Write failed to %s: %v, reconnecting", bp.currentBroker, err)

		bp.connMu.Lock()
		bp.conn.Close()
		bp.conn = nil
		bp.connMu.Unlock()

		attempt++
		if err := bp.ConnectWithRetry(ctx); err != nil && attempt >= bp.maxRetries {
			return fmt.Errorf("write failed after %d retries: %v", bp.maxRetries, err)
		}
	}

	return fmt.Errorf("write failed after %d attempts", bp.maxRetries)
}

// ReadWithTimeout reads with context-based timeout
func (bp *BrokerPool) ReadWithTimeout(ctx context.Context, buf []byte) (int, error) {
	bp.connMu.RLock()
	conn := bp.conn
	bp.connMu.RUnlock()

	if conn == nil {
		return 0, fmt.Errorf("no connection")
	}

	deadline, ok := ctx.Deadline()
	if ok {
		conn.SetReadDeadline(deadline)
	} else {
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	}

	n, err := conn.Read(buf)
	if err != nil {
		// Check if it's a "NOT_LEADER" error and handle redirect
		if err == io.EOF || strings.Contains(err.Error(), "connection") {
			bp.connMu.Lock()
			bp.conn.Close()
			bp.conn = nil
			bp.connMu.Unlock()
		}
	}

	return n, err
}

func (bp *BrokerPool) GetConn() net.Conn {
	bp.connMu.RLock()
	defer bp.connMu.RUnlock()
	return bp.conn
}

func (bp *BrokerPool) Close() error {
	bp.connMu.Lock()
	defer bp.connMu.Unlock()

	if bp.conn != nil {
		return bp.conn.Close()
	}
	return nil
}

func (bp *BrokerPool) CurrentBroker() string {
	bp.connMu.RLock()
	defer bp.connMu.RUnlock()
	return bp.currentBroker
}
