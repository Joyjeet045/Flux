package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"nats-lite/internal/scheduler"
)

type Worker struct {
	ID         string
	conn       net.Conn
	activeJobs int32 // Atomic counter
}

func NewWorker(id string, brokerAddr string) (*Worker, error) {
	conn, err := net.Dial("tcp", brokerAddr)
	if err != nil {
		return nil, err
	}
	return &Worker{
		ID:   id,
		conn: conn,
	}, nil
}

func (w *Worker) StartHeartbeat() {
	ticker := time.NewTicker(5 * time.Second)
	for range ticker.C {
		hb := scheduler.WorkerHeartbeat{
			WorkerID:   w.ID,
			ActiveJobs: int(atomic.LoadInt32(&w.activeJobs)),
			LastSeen:   time.Now().Unix(),
		}

		data, _ := json.Marshal(hb)
		msg := fmt.Sprintf("PUB worker.heartbeat %d\r\n%s\r\n", len(data), string(data))
		w.conn.Write([]byte(msg))
	}
}

func (w *Worker) Listen() {
	// Subscribe to my personal channel
	// SUB worker.<ID>.jobs <sid>
	subCmd := fmt.Sprintf("SUB worker.%s.jobs 1\r\n", w.ID)
	w.conn.Write([]byte(subCmd))
	log.Printf("Worker %s listening for jobs...", w.ID)

	reader := bufio.NewReader(w.conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			log.Fatal("Connection lost:", err)
		}
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "PING") {
			w.conn.Write([]byte("PONG\r\n"))
			continue
		}

		if strings.HasPrefix(line, "MSG") {
			// MSG <subject> <sid> <size> <seq>
			parts := strings.Split(line, " ")
			size, _ := strconv.Atoi(parts[3])

			// Extract sequence number if present (parts[4])
			var seq string
			if len(parts) > 4 {
				seq = parts[4]
			}

			// Read payload
			payloadBuf := make([]byte, size)
			_, err := io.ReadFull(reader, payloadBuf)
			if err != nil {
				continue
			}
			// Read the trailing \r\n
			reader.ReadString('\n')

			// MOVE ACK TO PROCESS JOB TO ALLOW CRASH BEFORE ACK
			go w.ProcessJob(payloadBuf, seq)
		}
	}
}

func (w *Worker) ProcessJob(data []byte, seq string) {
	// Increment active jobs
	atomic.AddInt32(&w.activeJobs, 1)
	defer atomic.AddInt32(&w.activeJobs, -1)

	var job scheduler.Job
	if err := json.Unmarshal(data, &job); err != nil {
		log.Println("Invalid job format:", err)
		return
	}

	log.Printf("Received Job %s: %s", job.ID, job.Payload)

	// CRASH SIMULATION
	if job.Payload == "CRASH_IMMEDIATELY" {
		log.Printf("💥 Worker %s simulates CRASH now!", w.ID)
		os.Exit(1)
	}

	// ACK now if we didn't crash
	if seq != "" {
		ackCmd := fmt.Sprintf("ACK %s\r\n", seq)
		w.conn.Write([]byte(ackCmd))
	}

	start := time.Now()
	result := scheduler.JobResult{
		JobID:       job.ID,
		WorkerID:    w.ID,
		Status:      scheduler.StatusRunning,
		StartTime:   start.Unix(),
		SubmittedAt: job.CreatedAt,
	}

	var output string
	var runErr error

	if job.Type == scheduler.JobTypeShell {
		// Windows specific shell execution
		cmd := exec.Command("cmd", "/C", job.Payload)
		out, err := cmd.CombinedOutput()
		output = string(out)
		runErr = err
	} else if job.Type == scheduler.JobTypeDocker {
		// Placeholder for Docker execution
		output = "Docker execution not yet implemented"
	}

	end := time.Now()
	duration := end.Sub(start).Milliseconds()

	result.DurationMs = duration
	result.EndTime = end.Unix()
	result.Output = output

	if runErr != nil {
		result.Status = scheduler.StatusFailed
		result.Output += fmt.Sprintf("\nError: %s", runErr.Error())
	} else {
		result.Status = scheduler.StatusCompleted
	}

	w.SendResult(result)
}

func (w *Worker) SendResult(res scheduler.JobResult) {
	data, _ := json.Marshal(res)
	msg := fmt.Sprintf("PUB jobs.status %d\r\n%s\r\n", len(data), string(data))
	w.conn.Write([]byte(msg))
	log.Printf("Job %s completed (%s)", res.JobID, res.Status)
}

func main() {
	workerID := fmt.Sprintf("worker-%d", rand.Intn(1000))
	if len(os.Args) > 1 {
		workerID = os.Args[1]
	}

	log.Printf("Starting Worker: %s", workerID)

	// Retry loop for connection
	for {
		worker, err := NewWorker(workerID, "localhost:4223")
		if err == nil {
			go worker.StartHeartbeat()
			worker.Listen()
			break
		}
		log.Println("Waiting for Broker...")
		time.Sleep(3 * time.Second)
	}
}
