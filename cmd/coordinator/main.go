package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"nats-lite/internal/scheduler"
)

type Coordinator struct {
	conn    net.Conn
	mu      sync.RWMutex
	workers map[string]scheduler.WorkerHeartbeat
}

func NewCoordinator(brokerAddr string) (*Coordinator, error) {
	conn, err := net.Dial("tcp", brokerAddr)
	if err != nil {
		return nil, err
	}
	return &Coordinator{
		conn:    conn,
		workers: make(map[string]scheduler.WorkerHeartbeat),
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
		log.Printf("No available workers for Job %s! Re-queueing logic needed here.", job.ID)
		return
	}

	log.Printf("Assigning Job %s -> %s", job.ID, workerID)

	// Forward to worker.<ID>.jobs
	topic := fmt.Sprintf("worker.%s.jobs", workerID)
	msg := fmt.Sprintf("PUB %s %d\r\n%s\r\n", topic, len(data), string(data))
	c.conn.Write([]byte(msg))
}

func (c *Coordinator) selectBestWorker() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var bestWorker string
	minCPU := 1000.0
	now := time.Now().Unix()

	for id, stats := range c.workers {
		// Ignore stale workers (> 30s)
		if now-stats.LastSeen > 30 {
			continue
		}

		if stats.CPUUsage < minCPU {
			minCPU = stats.CPUUsage
			bestWorker = id
		}
	}
	return bestWorker
}

func main() {
	for {
		coord, err := NewCoordinator("localhost:4223")
		if err == nil {
			coord.Listen()
			break
		}
		log.Println("Waiting for Broker...")
		time.Sleep(3 * time.Second)
	}
}
