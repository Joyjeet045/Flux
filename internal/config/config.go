package config

import (
	"encoding/json"
	"os"
	"time"
)

type Config struct {
	Server        ServerConfig        `json:"server"`
	Storage       StorageConfig       `json:"storage"`
	Retention     RetentionConfig     `json:"retention"`
	ACK           ACKConfig           `json:"ack"`
	Durable       DurableConfig       `json:"durable"`
	DLQ           DLQConfig           `json:"dlq"`
	FlowControl   FlowControlConfig   `json:"flow_control"`
	Deduplication DeduplicationConfig `json:"deduplication"`
}

type ServerConfig struct {
	Port    string `json:"port"`
	DataDir string `json:"data_dir"`
}

type StorageConfig struct {
	MaxSegmentSize int64 `json:"max_segment_size"` // bytes
}

type RetentionConfig struct {
	MaxAge        string `json:"max_age"`        // duration string: "24h"
	MaxBytes      int64  `json:"max_bytes"`      // bytes
	CheckInterval string `json:"check_interval"` // duration string: "10s"
}

type ACKConfig struct {
	Timeout    string `json:"timeout"` // duration string: "5s"
	MaxRetries int    `json:"max_retries"`
}

type DurableConfig struct {
	FlushInterval string `json:"flush_interval"` // duration string: "1s"
	ReplayBatch   int    `json:"replay_batch"`
}

type DLQConfig struct {
	Prefix string `json:"prefix"` // e.g., "DLQ."
}

type FlowControlConfig struct {
	EnableRateLimit      bool    `json:"enable_rate_limit"`
	EnableBackpressure   bool    `json:"enable_backpressure"`
	DefaultRateLimit     float64 `json:"default_rate_limit"`     // messages per second
	DefaultRateBurst     int     `json:"default_rate_burst"`     // burst capacity
	DefaultBufferSize    int     `json:"default_buffer_size"`    // buffer size per consumer
	DefaultSlowThreshold float64 `json:"default_slow_threshold"` // 0.0-1.0, buffer usage threshold
	BackpressureMode     string  `json:"backpressure_mode"`      // "drop", "block", "shed"
}

type DeduplicationConfig struct {
	Enabled    bool   `json:"enabled"`
	WindowSize string `json:"window_size"` // duration string: "5m"
	MaxEntries int    `json:"max_entries"` // max messages in dedup window
}

// Default returns a config with sensible defaults
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Port:    ":4223",
			DataDir: "data",
		},
		Storage: StorageConfig{
			MaxSegmentSize: 10 * 1024 * 1024, // 10MB
		},
		Retention: RetentionConfig{
			MaxAge:        "24h",
			MaxBytes:      1 * 1024 * 1024 * 1024, // 1GB
			CheckInterval: "10s",
		},
		ACK: ACKConfig{
			Timeout:    "5s",
			MaxRetries: 3,
		},
		Durable: DurableConfig{
			FlushInterval: "1s",
			ReplayBatch:   100,
		},
		DLQ: DLQConfig{
			Prefix: "DLQ.",
		},
		FlowControl: FlowControlConfig{
			EnableRateLimit:      true,
			EnableBackpressure:   true,
			DefaultRateLimit:     1000.0,
			DefaultRateBurst:     100,
			DefaultBufferSize:    1000,
			DefaultSlowThreshold: 0.8,
			BackpressureMode:     "drop",
		},
		Deduplication: DeduplicationConfig{
			Enabled:    true,
			WindowSize: "5m",
			MaxEntries: 100000,
		},
	}
}

// LoadFromFile loads config from JSON file
func LoadFromFile(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cfg := Default()
	if err := json.NewDecoder(f).Decode(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// SaveToFile saves config to JSON file
func (c *Config) SaveToFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(c)
}

// Helper methods to parse durations
func (c *Config) GetRetentionMaxAge() (time.Duration, error) {
	return time.ParseDuration(c.Retention.MaxAge)
}

func (c *Config) GetRetentionCheckInterval() (time.Duration, error) {
	return time.ParseDuration(c.Retention.CheckInterval)
}

func (c *Config) GetACKTimeout() (time.Duration, error) {
	return time.ParseDuration(c.ACK.Timeout)
}

func (c *Config) GetDurableFlushInterval() (time.Duration, error) {
	return time.ParseDuration(c.Durable.FlushInterval)
}

func (c *Config) GetDeduplicationWindowSize() (time.Duration, error) {
	return time.ParseDuration(c.Deduplication.WindowSize)
}
