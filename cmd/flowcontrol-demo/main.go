package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
	"time"
)

func main() {
	log.Println("=== Flow Control Quick Demo ===")

	log.Println("Connecting to server...")
	conn, err := net.Dial("tcp", "localhost:4223")
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)

	log.Println("✓ Connected to NATS-Lite server")

	log.Println("--- Demo 1: Subscribe with Default Flow Control ---")
	conn.Write([]byte("SUB demo.test sub1\r\n"))
	response, _ := reader.ReadString('\n')
	log.Printf("Response: %s", strings.TrimSpace(response))

	time.Sleep(500 * time.Millisecond)

	log.Println("\n--- Demo 2: Configure Push Mode with Rate Limit ---")
	conn.Write([]byte("FLOWCTL sub1 PUSH 100 20 1000\r\n"))
	response, _ = reader.ReadString('\n')
	log.Printf("Response: %s", strings.TrimSpace(response))

	time.Sleep(500 * time.Millisecond)

	log.Println("\n--- Demo 3: Get Flow Control Stats ---")
	conn.Write([]byte("STATS sub1\r\n"))
	response, _ = reader.ReadString('\n')
	log.Printf("Stats: %s", strings.TrimSpace(response))

	time.Sleep(500 * time.Millisecond)

	log.Println("\n--- Demo 4: Publish Some Messages ---")
	for i := 1; i <= 5; i++ {
		msg := fmt.Sprintf("PUB demo.test %d\r\nMessage %d\r\n", 9, i)
		conn.Write([]byte(msg))
		log.Printf("Published message %d", i)
		time.Sleep(100 * time.Millisecond)
	}

	time.Sleep(1 * time.Second)

	log.Println("\n--- Demo 5: Check Stats After Publishing ---")
	conn.Write([]byte("STATS sub1\r\n"))
	response, _ = reader.ReadString('\n')
	log.Printf("Stats: %s", strings.TrimSpace(response))

	time.Sleep(500 * time.Millisecond)

	log.Println("\n--- Demo 6: Reconfigure to Pull Mode ---")
	conn.Write([]byte("FLOWCTL sub1 PULL 0 0 500\r\n"))
	response, _ = reader.ReadString('\n')
	log.Printf("Response: %s", strings.TrimSpace(response))

	time.Sleep(500 * time.Millisecond)

	log.Println("\n--- Demo 7: Final Stats ---")
	conn.Write([]byte("STATS sub1\r\n"))
	response, _ = reader.ReadString('\n')
	log.Printf("Stats: %s\n", strings.TrimSpace(response))

	log.Println("\n=== Demo Complete ===")
	log.Println("\nFlow Control Features Demonstrated:")
	log.Println("✓ Automatic flow control on subscription")
	log.Println("✓ Push mode with rate limiting")
	log.Println("✓ Real-time statistics")
	log.Println("✓ Dynamic reconfiguration (Push → Pull)")
	log.Println("✓ Message delivery with backpressure")
}
