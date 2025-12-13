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

	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	mu         sync.RWMutex
	jobs       map[string]*scheduler.Job
	conn       net.Conn
	brokerAddr string
	cronSched  *cron.Cron
	results    map[string]*scheduler.JobResult
	resultsMu  sync.RWMutex
	metrics    *Metrics
	metricsMu  sync.RWMutex
}

type Metrics struct {
	JobsTotal     int64
	JobsCompleted int64
	JobsFailed    int64
	JobsPending   int64
	AvgLatencyMs  int64
	WorkersActive int
}

func NewScheduler(brokerAddr string) *Scheduler {
	return &Scheduler{
		jobs:       make(map[string]*scheduler.Job),
		brokerAddr: brokerAddr,
		cronSched:  cron.New(),
		results:    make(map[string]*scheduler.JobResult),
		metrics:    &Metrics{},
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
		Type     scheduler.JobType `json:"type"`
		Payload  string            `json:"payload"`
		RunIn    int64             `json:"run_in_seconds"` // Convenience: run once after N seconds
		CronExpr string            `json:"cron_expr"`      // Cron expression: "0 9 * * *"
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	jobID := fmt.Sprintf("job-%d", time.Now().UnixNano())

	job := &scheduler.Job{
		ID:        jobID,
		Type:      req.Type,
		Payload:   req.Payload,
		CreatedAt: time.Now().Unix(),
	}

	// Handle cron scheduling
	if req.CronExpr != "" {
		job.CronExpr = req.CronExpr
		job.IsRecurring = true

		// Parse and add to cron scheduler
		_, err := s.cronSched.AddFunc(req.CronExpr, func() {
			// Create new job instance for each cron trigger
			newJobID := fmt.Sprintf("job-%d", time.Now().UnixNano())
			newJob := &scheduler.Job{
				ID:           newJobID,
				Type:         req.Type,
				Payload:      req.Payload,
				ScheduleTime: time.Now().Unix(),
				CreatedAt:    time.Now().Unix(),
				CronExpr:     req.CronExpr,
				IsRecurring:  true,
			}
			log.Printf("Cron triggered job: %s", newJobID)
			s.publishJob(newJob)
		})

		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid cron expression: %v", err), http.StatusBadRequest)
			return
		}

		log.Printf("Cron job registered: %s with schedule %s", jobID, req.CronExpr)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": jobID, "status": "RECURRING", "cron": req.CronExpr})
		return
	}

	// One-time job
	scheduleTime := time.Now().Add(time.Duration(req.RunIn) * time.Second).Unix()
	job.ScheduleTime = scheduleTime
	job.IsRecurring = false

	s.mu.Lock()
	s.jobs[jobID] = job
	s.mu.Unlock()

	// Update metrics
	s.metricsMu.Lock()
	s.metrics.JobsTotal++
	s.metrics.JobsPending++
	s.metricsMu.Unlock()

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
		if job.ScheduleTime <= now && !job.IsRecurring {
			log.Printf("Scheduling Job: %s", id)
			s.publishJob(job)
			delete(s.jobs, id) // Remove from pending list

			// Update metrics
			s.metricsMu.Lock()
			s.metrics.JobsPending--
			s.metricsMu.Unlock()
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

func (s *Scheduler) StoreResult(result *scheduler.JobResult) {
	s.resultsMu.Lock()
	s.results[result.JobID] = result
	s.resultsMu.Unlock()

	// Update metrics
	s.metricsMu.Lock()
	if result.Status == scheduler.StatusCompleted {
		s.metrics.JobsCompleted++
	} else if result.Status == scheduler.StatusFailed {
		s.metrics.JobsFailed++
	}

	// Update average latency
	if result.DurationMs > 0 {
		totalLatency := s.metrics.AvgLatencyMs * (s.metrics.JobsCompleted + s.metrics.JobsFailed - 1)
		s.metrics.AvgLatencyMs = (totalLatency + result.DurationMs) / (s.metrics.JobsCompleted + s.metrics.JobsFailed)
	}
	s.metricsMu.Unlock()
}

func (s *Scheduler) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	s.metricsMu.RLock()
	s.mu.RLock()

	metrics := map[string]interface{}{
		"jobs_total":     s.metrics.JobsTotal,
		"jobs_completed": s.metrics.JobsCompleted,
		"jobs_failed":    s.metrics.JobsFailed,
		"jobs_pending":   len(s.jobs),
		"avg_latency_ms": s.metrics.AvgLatencyMs,
		"success_rate":   float64(s.metrics.JobsCompleted) / float64(s.metrics.JobsTotal) * 100,
		"results_stored": len(s.results),
	}

	s.mu.RUnlock()
	s.metricsMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

func (s *Scheduler) JobResultHandler(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("id")
	if jobID == "" {
		http.Error(w, "Missing job ID parameter", http.StatusBadRequest)
		return
	}

	s.resultsMu.RLock()
	result, exists := s.results[jobID]
	s.resultsMu.RUnlock()

	if !exists {
		http.Error(w, "Job result not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
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

	// Start cron scheduler
	sched.cronSched.Start()
	log.Println("Cron scheduler started")

	// Register HTTP endpoints
	http.HandleFunc("/submit", sched.SubmitHandler)
	http.HandleFunc("/metrics", sched.MetricsHandler)
	http.HandleFunc("/result", sched.JobResultHandler)

	log.Println("Scheduler listening on :8080")
	log.Println("  POST /submit - Submit jobs")
	log.Println("  GET /metrics - View metrics")
	log.Println("  GET /result?id=<job-id> - Get job result")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
