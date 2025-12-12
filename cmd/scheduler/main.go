package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"nats-lite/internal/scheduler"
)

type Scheduler struct {
	mu         sync.RWMutex
	jobs       map[string]*scheduler.Job
	conn       net.Conn
	brokerAddr string
}

func NewScheduler(brokerAddr string) *Scheduler {
	return &Scheduler{
		jobs:       make(map[string]*scheduler.Job),
		brokerAddr: brokerAddr,
	}
}

func (s *Scheduler) Connect() error {
	conn, err := net.Dial("tcp", s.brokerAddr)
	if err != nil {
		return err
	}
	s.conn = conn
	return nil
}

func (s *Scheduler) SubmitHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Type    scheduler.JobType `json:"type"`
		Payload string            `json:"payload"`
		RunIn   int64             `json:"run_in_seconds"` // Convenience
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	jobID := fmt.Sprintf("job-%d", time.Now().UnixNano())
	scheduleTime := time.Now().Add(time.Duration(req.RunIn) * time.Second).Unix()

	job := &scheduler.Job{
		ID:           jobID,
		Type:         req.Type,
		Payload:      req.Payload,
		ScheduleTime: scheduleTime,
		CreatedAt:    time.Now().Unix(),
	}

	s.mu.Lock()
	s.jobs[jobID] = job
	s.mu.Unlock()

	log.Printf("Job submitted: %s, run at: %d", jobID, scheduleTime)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": jobID, "status": "PENDING"})
}

func (s *Scheduler) RunTicker() {
	ticker := time.NewTicker(1 * time.Second)
	for range ticker.C {
		s.checkJobs()
	}
}

func (s *Scheduler) checkJobs() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()

	// Find pending jobs that are due
	for id, job := range s.jobs {
		// In a real DB we'd have a status field check too, here we just remove from map upon scheduling for simplicity
		// or we can mark them. Let's assume jobs in map are pending/scheduled.
		if job.ScheduleTime <= now {
			log.Printf("Scheduling Job: %s", id)
			s.publishJob(job)
			delete(s.jobs, id) // Remove from pending list
		}
	}
}

func (s *Scheduler) publishJob(job *scheduler.Job) {
	if s.conn == nil {
		log.Println("Broker not connected, cannot publish")
		return
	}

	data, _ := json.Marshal(job)

	// Protocol: PUB <subject> <size>\r\n<payload>\r\n
	subject := "jobs.queue"
	msg := fmt.Sprintf("PUB %s %d\r\n%s\r\n", subject, len(data), string(data))

	_, err := s.conn.Write([]byte(msg))
	if err != nil {
		log.Printf("Failed to publish job %s: %v", job.ID, err)
		// Reconnect logic would go here
		s.conn.Close()
		s.conn = nil
	}
}

func main() {
	sched := NewScheduler("localhost:4223")

	// Retry connection loop
	go func() {
		for {
			if sched.conn == nil {
				if err := sched.Connect(); err != nil {
					log.Println("Waiting for broker...")
					time.Sleep(3 * time.Second)
					continue
				}
				log.Println("Connected to Broker!")
			}
			time.Sleep(5 * time.Second)
		}
	}()

	go sched.RunTicker()

	http.HandleFunc("/submit", sched.SubmitHandler)

	log.Println("Scheduler listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
