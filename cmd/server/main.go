package main

import (
	"flag"
	"log"
	"nats-lite/internal/config"
	"nats-lite/internal/server"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	configPath := flag.String("config", "config.json", "Path to config file")
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadFromFile(*configPath)
	if err != nil {
		log.Printf("Failed to load config from %s, using defaults: %v", *configPath, err)
		cfg = config.Default()
	}

	log.Printf("Starting NATS-Lite Server on %s...", cfg.Server.Port)
	log.Printf("Data directory: %s", cfg.Server.DataDir)
	log.Printf("Retention: MaxAge=%s, MaxBytes=%d", cfg.Retention.MaxAge, cfg.Retention.MaxBytes)

	srv, err := server.New(cfg)
	if err != nil {
		log.Fatal(err)
	}

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sigChan
		log.Println("Shutting down gracefully...")
		srv.Shutdown()
		os.Exit(0)
	}()

	if err := srv.Start(); err != nil {
		log.Fatal(err)
	}
}
