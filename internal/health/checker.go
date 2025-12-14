package health

import (
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"
)

// HealthChecker provides Kubernetes-ready health endpoints
type HealthChecker struct {
	mu          sync.RWMutex
	healthy     bool
	ready       bool
	startTime   time.Time
	lastCheck   time.Time
	checkFunc   func() error
	brokerAddrs []string
}

type HealthStatus struct {
	Status    string        `json:"status"` // "UP" or "DOWN"
	Uptime    string        `json:"uptime"`
	Timestamp string        `json:"timestamp"`
	Checks    []CheckResult `json:"checks"`
}

type CheckResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

func NewHealthChecker(brokerAddrs []string, checkFunc func() error) *HealthChecker {
	hc := &HealthChecker{
		healthy:     true,
		ready:       false,
		startTime:   time.Now(),
		checkFunc:   checkFunc,
		brokerAddrs: brokerAddrs,
	}

	go hc.periodicCheck()
	return hc
}

func (hc *HealthChecker) periodicCheck() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		err := hc.checkFunc()
		hc.mu.Lock()
		hc.lastCheck = time.Now()
		hc.healthy = (err == nil)
		hc.ready = hc.healthy
		hc.mu.Unlock()
	}
}

// LivenessHandler returns 200 if the app is running (doesn't kill pod)
func (hc *HealthChecker) LivenessHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "UP",
		"type":   "liveness",
	})
}

// ReadinessHandler returns 200 only if the app can serve traffic
func (hc *HealthChecker) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	hc.mu.RLock()
	ready := hc.ready
	hc.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	if ready {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "READY",
			"type":   "readiness",
		})
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "NOT_READY",
			"type":   "readiness",
		})
	}
}

// HealthHandler provides detailed health status
func (hc *HealthChecker) HealthHandler(w http.ResponseWriter, r *http.Request) {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	uptime := time.Since(hc.startTime)
	status := HealthStatus{
		Status:    "UP",
		Uptime:    uptime.String(),
		Timestamp: time.Now().Format(time.RFC3339),
		Checks:    []CheckResult{},
	}

	// Check broker connectivity
	brokerStatus := hc.checkBrokers()
	status.Checks = append(status.Checks, brokerStatus...)

	// Overall status
	allHealthy := true
	for _, check := range status.Checks {
		if check.Status != "UP" {
			allHealthy = false
			break
		}
	}

	if !allHealthy {
		status.Status = "DOWN"
	}

	w.Header().Set("Content-Type", "application/json")
	if status.Status == "UP" {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	json.NewEncoder(w).Encode(status)
}

func (hc *HealthChecker) checkBrokers() []CheckResult {
	results := []CheckResult{}

	for _, addr := range hc.brokerAddrs {
		conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
		if err != nil {
			results = append(results, CheckResult{
				Name:    "broker_" + addr,
				Status:  "DOWN",
				Message: err.Error(),
			})
		} else {
			conn.Close()
			results = append(results, CheckResult{
				Name:   "broker_" + addr,
				Status: "UP",
			})
		}
	}

	return results
}

func (hc *HealthChecker) SetReady(ready bool) {
	hc.mu.Lock()
	hc.ready = ready
	hc.mu.Unlock()
}
