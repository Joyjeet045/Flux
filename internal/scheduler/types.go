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
	CronExpr     string            `json:"cron_expr,omitempty"` // e.g., "0 9 * * *" for daily at 9am
	IsRecurring  bool              `json:"is_recurring"`        // true if using cron
}

// WorkerHeartbeat announces availability.
type WorkerHeartbeat struct {
	WorkerID   string   `json:"worker_id"`
	CPUUsage   float64  `json:"cpu_usage"`
	RAMUsage   float64  `json:"ram_usage"`
	ActiveJobs int      `json:"active_jobs"`
	Tags       []string `json:"tags"`
	LastSeen   int64    `json:"last_seen"`
}

// JobResult is the execution output.
type JobResult struct {
	JobID       string    `json:"job_id"`
	WorkerID    string    `json:"worker_id"`
	Status      JobStatus `json:"status"`
	Output      string    `json:"output"`
	DurationMs  int64     `json:"duration_ms"`
	StartTime   int64     `json:"start_time"`   // Unix timestamp
	EndTime     int64     `json:"end_time"`     // Unix timestamp
	SubmittedAt int64     `json:"submitted_at"` // When job was created
}
