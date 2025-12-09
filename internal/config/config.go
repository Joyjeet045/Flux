package config

import (
	"encoding/json"
	"os"
	"time"
)

type Config struct {
	Server    ServerConfig    `json:"server"`
	Storage   StorageConfig   `json:"storage"`
	Retention RetentionConfig `json:"retention"`
	ACK       ACKConfig       `json:"ack"`
	Durable   DurableConfig   `json:"durable"`
	DLQ       DLQConfig       `json:"dlq"`
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
