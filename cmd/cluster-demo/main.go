package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	log.Println("Starting Cluster Demo...")

	// 1. Build Server
	log.Println("Building server...")
	buildCmd := exec.Command("go", "build", "-o", "server.exe", "./cmd/server")
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		log.Fatalf("Failed to build server: %v", err)
	}

	// Clean data directory
	os.RemoveAll("data")
	time.Sleep(500 * time.Millisecond)

	// 2. Start Node 1 (Bootstrap)
	log.Println("Starting Node 1 (Bootstrap)...")
	node1 := startNode("cmd/cluster-demo/node1.json")
	defer stopNode(node1)
	time.Sleep(2 * time.Second) // Wait for leader election

	// 3. Start Node 2 & 3
	log.Println("Starting Node 2...")
	node2 := startNode("cmd/cluster-demo/node2.json")
	defer stopNode(node2)

	log.Println("Starting Node 3...")
	node3 := startNode("cmd/cluster-demo/node3.json")
	defer stopNode(node3)
	time.Sleep(2 * time.Second)

	// 4. Connect to Node 1 and Join others
	conn1, err := net.Dial("tcp", "localhost:4223")
	if err != nil {
		log.Fatalf("Failed to connect to Node 1: %v", err)
	}
	defer conn1.Close()
	reader1 := bufio.NewReader(conn1)

	log.Println("Joining Node 2...")
	conn1.Write([]byte("JOIN node-2 127.0.0.1:7002\r\n"))
	readResponse(reader1)

	log.Println("Joining Node 3...")
	conn1.Write([]byte("JOIN node-3 127.0.0.1:7003\r\n"))
	readResponse(reader1)

	time.Sleep(2 * time.Second) // Wait for replication/join

	// 5. Connect subscribers to Node 2 and Node 3
	// 5. Connect subscribers to Node 2 and Node 3
	log.Println("Connecting Subscriber to Node 2...")
	sub2, err := net.Dial("tcp", "localhost:4224")
	if err != nil {
		log.Fatalf("Failed to connect to Node 2: %v", err)
	}
	defer sub2.Close()
	reader2 := bufio.NewReader(sub2)
	sub2.Write([]byte("SUB test.topic sub2\r\n"))
	readResponse(reader2)

	log.Println("Connecting Subscriber to Node 3...")
	sub3, err := net.Dial("tcp", "localhost:4225")
	if err != nil {
		log.Fatalf("Failed to connect to Node 3: %v", err)
	}
	defer sub3.Close()
	reader3 := bufio.NewReader(sub3)
	sub3.Write([]byte("SUB test.topic sub3\r\n"))
	readResponse(reader3)

	time.Sleep(1 * time.Second)

	// 6. Publish to Node 1
	log.Println("Publishing 10 messages to Node 1 to trigger Snapshot...")
	payload := "Hello Cluster!"
	pubCmd := fmt.Sprintf("PUB test.topic %d\r\n%s\r\n", len(payload), payload)

	for i := 0; i < 10; i++ {
		conn1.Write([]byte(pubCmd))
		time.Sleep(100 * time.Millisecond)
	}

	time.Sleep(1 * time.Second) // Wait for replication

	// 7. Verify Delivery
	log.Println("Verifying delivery on Node 2...")
	for i := 0; i < 10; i++ {
		verifyMessage(reader2, payload)
	}

	log.Println("Verifying delivery on Node 3...")
	for i := 0; i < 10; i++ {
		verifyMessage(reader3, payload)
	}

	log.Println("\n✅ Cluster Demo Successful! Replication works.")
}

func startNode(configPath string) *exec.Cmd {
	absConfig, _ := filepath.Abs(configPath)
	cmd := exec.Command("./server.exe", "-config", absConfig)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Fatalf("Failed to start node with config %s: %v", configPath, err)
	}
	return cmd
}

func stopNode(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
	}
}

func readResponse(r *bufio.Reader) {
	resp, err := r.ReadString('\n')
	if err != nil {
		log.Printf("Read error: %v", err)
	}
	log.Printf("Response: %s", strings.TrimSpace(resp))
}

func verifyMessage(r *bufio.Reader, expectedPayload string) {
	// Read MSG line
	msgLine, err := r.ReadString('\n')
	if err != nil {
		log.Fatalf("Failed to read MSG line: %v", err)
	}
	log.Printf("Received: %s", strings.TrimSpace(msgLine))

	// Read Payload
	payload, err := r.ReadString('\n')
	if err != nil {
		log.Fatalf("Failed to read payload: %v", err)
	}
	if strings.TrimSpace(payload) != expectedPayload {
		log.Fatalf("Payload mismatch. Expected '%s', got '%s'", expectedPayload, strings.TrimSpace(payload))
	}
	log.Println("Payload matched!")
}
