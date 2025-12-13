package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"nats-lite/internal/scheduler"
)

type Coordinator struct {
	conn      net.Conn
	connMu    sync.Mutex
	mu        sync.RWMutex
	workers   map[string]scheduler.WorkerHeartbeat
	retries   map[string]int
	retriesMu sync.Mutex
	shutdown  chan struct{}
}

func NewCoordinator(brokerAddr string) (*Coordinator, error) {
	conn, err := net.Dial("tcp", brokerAddr)
	if err != nil {
		return nil, err
	}
	return &Coordinator{
		conn:     conn,
		workers:  make(map[string]scheduler.WorkerHeartbeat),
		retries:  make(map[string]int),
		shutdown: make(chan struct{}),
	}, nil
}

func (c *Coordinator) Listen() {
	log.Println("Coordinator connecting...")

	// Give the TCP connection a moment to settle
	time.Sleep(100 * time.Millisecond)

	// 1. Subscribe (No handshake needed for our simple broker)
	_, err := c.conn.Write([]byte("SUB worker.heartbeat 1\r\n"))
	if err != nil {
		log.Fatalf("Failed to subscribe to heartbeats: %v", err)
	}
	log.Println("Subscribed to worker.heartbeat")

	_, err = c.conn.Write([]byte("SUB jobs.queue coordinators 2\r\n"))
	if err != nil {
		log.Fatalf("Failed to subscribe to jobs.queue: %v", err)
	}
	log.Println("Subscribed to jobs.queue with queue group 'coordinators'")

	log.Println("Coordinator Listening loop starting...")

	reader := bufio.NewReader(c.conn)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			log.Fatal("Connection lost:", err)
		}
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "PING") {
			c.conn.Write([]byte("PONG\r\n"))
			continue
		}

		if strings.HasPrefix(line, "MSG") {
			log.Printf("DEBUG: Received MSG: %s", line)
			parts := strings.Split(line, " ")
			// MSG <subject> <sid> <size> [reply] (Standard NATS is inconsistent here depending on impl, simpler for now)
			// My Broker: MSG <subject> <sid> <size>
			if len(parts) < 4 {
				continue
			}

			subject := parts[1]
			size, _ := strconv.Atoi(parts[3])

			// Extract sequence number if present (parts[4])
			var seq string
			if len(parts) > 4 {
				seq = parts[4]
			}

			payloadBuf := make([]byte, size)
			reader.Read(payloadBuf)
			reader.ReadString('\n') // Eat trailing \r\n

			// Send ACK immediately to prevent DLQ timeout
			if seq != "" {
				ackCmd := fmt.Sprintf("ACK %s\r\n", seq)
				c.conn.Write([]byte(ackCmd))
				log.Printf("DEBUG: Sent ACK for seq=%s subject=%s", seq, subject)
			} else {
				log.Printf("DEBUG: No seq number in MSG, parts=%v", parts)
			}

			if subject == "worker.heartbeat" {
				c.handleHeartbeat(payloadBuf)
			} else if subject == "jobs.queue" {
				c.handleJob(payloadBuf)
			}
		}
	}
}

func (c *Coordinator) handleHeartbeat(data []byte) {
	var hb scheduler.WorkerHeartbeat
	if err := json.Unmarshal(data, &hb); err != nil {
		return
	}

	c.mu.Lock()
	c.workers[hb.WorkerID] = hb
	c.mu.Unlock()
	// log.Printf("Heartbeat: %s (CPU: %.1f%%)", hb.WorkerID, hb.CPUUsage)
}

func (c *Coordinator) handleJob(data []byte) {
	var job scheduler.Job
	if err := json.Unmarshal(data, &job); err != nil {
		log.Println("Invalid Job:", err)
		return
	}

	workerID := c.selectBestWorker()
	if workerID == "" {
		// Check retry count
		c.retriesMu.Lock()
		retryCount := c.retries[job.ID]
		if retryCount >= 5 {
			c.retriesMu.Unlock()
			log.Printf("Job %s exceeded max retries (5), giving up", job.ID)
			delete(c.retries, job.ID)
			return
		}
		c.retries[job.ID] = retryCount + 1
		c.retriesMu.Unlock()

		// Exponential backoff: 2^retry seconds, max 30s
		delay := time.Duration(math.Pow(2, float64(retryCount))) * time.Second
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}

		log.Printf("No available workers for Job %s, retry %d/%d in %v...", job.ID, retryCount+1, 5, delay)

		// Requeue with backoff
		go func() {
			time.Sleep(delay)
			// Republish to jobs.queue with thread-safe write
			requeueMsg := fmt.Sprintf("PUB jobs.queue %d\r\n%s\r\n", len(data), string(data))
			c.connMu.Lock()
			c.conn.Write([]byte(requeueMsg))
			c.connMu.Unlock()
			log.Printf("Requeued Job %s (attempt %d)", job.ID, retryCount+1)
		}()
		return
	}

	// Success - clear retry count
	c.retriesMu.Lock()
	delete(c.retries, job.ID)
	c.retriesMu.Unlock()

	log.Printf("Assigning Job %s -> %s", job.ID, workerID)

	// Forward to worker.<ID>.jobs with thread-safe write
	topic := fmt.Sprintf("worker.%s.jobs", workerID)
	msg := fmt.Sprintf("PUB %s %d\r\n%s\r\n", topic, len(data), string(data))
	c.connMu.Lock()
	c.conn.Write([]byte(msg))
	c.connMu.Unlock()
}

func (c *Coordinator) selectBestWorker() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var bestWorker string
	minCPU := 1000.0
	now := time.Now().Unix()
	activeWorkers := 0

	for id, stats := range c.workers {
		// Ignore stale workers (>30s)
		age := now - stats.LastSeen
		if age > 30 {
			continue
		}

		activeWorkers++
		if stats.CPUUsage < minCPU {
			minCPU = stats.CPUUsage
			bestWorker = id
		}
	}

	if bestWorker != "" {
		log.Printf("Selected %s (CPU: %.1f%%) from %d active workers", bestWorker, minCPU, activeWorkers)
	} else if activeWorkers == 0 {
		log.Printf("No active workers available")
	}

	return bestWorker
}

func (c *Coordinator) cleanupStaleWorkers() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			now := time.Now().Unix()
			removed := 0
			for id, stats := range c.workers {
				if now-stats.LastSeen > 120 {
					delete(c.workers, id)
					removed++
				}
			}
			c.mu.Unlock()
			if removed > 0 {
				log.Printf("Cleaned up %d stale workers", removed)
			}
		case <-c.shutdown:
			return
		}
	}
}

func (c *Coordinator) gracefulShutdown() {
	close(c.shutdown)
	c.connMu.Lock()
	c.conn.Close()
	c.connMu.Unlock()
	log.Println("Coordinator shutdown complete")
}

func main() {
	var coord *Coordinator
	var err error

	// Connect to broker with retry
	for {
		coord, err = NewCoordinator("localhost:4223")
		if err == nil {
			break
		}
		log.Println("Waiting for Broker...")
		time.Sleep(3 * time.Second)
	}

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start background cleanup
	go coord.cleanupStaleWorkers()

	// Handle shutdown signal
	go func() {
		<-sigChan
		log.Println("Shutdown signal received...")
		coord.gracefulShutdown()
		os.Exit(0)
	}()

	// Start listening (blocks)
	coord.Listen()
}
