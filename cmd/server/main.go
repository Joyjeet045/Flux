package main

import (
	"log"
	"nats-lite/internal/server"
	"os"
)

func main() {
	// Create data directory
	if err := os.MkdirAll("data", 0755); err != nil {
		log.Fatal(err)
	}

	srv, err := server.New(":4223", "data")
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Starting NATS-Lite Server on :4222...")
	if err := srv.Start(); err != nil {
		log.Fatal(err)
	}
}
