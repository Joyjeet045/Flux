package scheduler

// JobType defines the work type.
type JobType string

const (
	JobTypeDocker JobType = "docker"
	JobTypeShell  JobType = "shell"
)

// JobStatus track job state.
type JobStatus string

const (
	StatusPending   JobStatus = "PENDING"
	StatusScheduled JobStatus = "SCHEDULED"
	StatusRunning   JobStatus = "RUNNING"
	StatusCompleted JobStatus = "COMPLETED"
	StatusFailed    JobStatus = "FAILED"
)

// Job is the unit of work.
type Job struct {
	ID           string            `json:"id"`
	Type         JobType           `json:"type"`
	Payload      string            `json:"payload"`
	Args         []string          `json:"args"`
	Env          map[string]string `json:"env"`
	ScheduleTime int64             `json:"schedule_time"`
	CreatedAt    int64             `json:"created_at"`
}

// WorkerHeartbeat announces availability.
type WorkerHeartbeat struct {
	WorkerID string   `json:"worker_id"`
	CPUUsage float64  `json:"cpu_usage"`
	RAMUsage float64  `json:"ram_usage"`
	Tags     []string `json:"tags"`
	LastSeen int64    `json:"last_seen"`
}

// JobResult is the execution output.
type JobResult struct {
	JobID      string    `json:"job_id"`
	WorkerID   string    `json:"worker_id"`
	Status     JobStatus `json:"status"`
	Output     string    `json:"output"`
	DurationMs int64     `json:"duration_ms"`
}
