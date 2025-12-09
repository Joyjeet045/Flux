package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

func main() {
	cmd := flag.String("cmd", "", "Command: pub or sub")
	subject := flag.String("sub", "", "Subject")
	payload := flag.String("msg", "", "Message payload")
	queue := flag.String("queue", "", "Queue Group")
	addr := flag.String("addr", "localhost:4223", "Server address")
	flag.Parse()

	if *cmd == "" || *subject == "" {
		fmt.Println("Usage: -cmd=[pub|sub] -sub=<subject> [-queue=<group>] [-msg=<message>]")
		os.Exit(1)
	}

	conn, err := net.Dial("tcp", *addr)
	if err != nil {
		fmt.Println("Error connecting:", err)
		os.Exit(1)
	}
	defer conn.Close()

	if *cmd == "pub" {
		if *payload == "" {
			fmt.Println("Payload required for PUB")
			os.Exit(1)
		}
		// Protocol: PUB <subject> <size>\r\n<payload>\r\n
		msg := fmt.Sprintf("PUB %s %d\r\n%s\r\n", *subject, len(*payload), *payload)
		conn.Write([]byte(msg))
		fmt.Println("Published:", *payload)
	} else if *cmd == "sub" {
		// Protocol: SUB <subject> [queue] <sid>\r\n
		sid := "1" // Static SID
		msg := ""
		if *queue != "" {
			msg = fmt.Sprintf("SUB %s %s %s\r\n", *subject, *queue, sid)
		} else {
			msg = fmt.Sprintf("SUB %s %s\r\n", *subject, sid)
		}
		conn.Write([]byte(msg))
		if *queue != "" {
			fmt.Printf("Subscribed to %s in group %s. Waiting...\n", *subject, *queue)
		} else {
			fmt.Printf("Subscribed to %s. Waiting...\n", *subject)
		}

		// Read loop
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "MSG") {
				// MSG <subject> <sid> <size>
				parts := strings.Split(line, " ")
				if len(parts) < 4 {
					continue
				}
				size, _ := strconv.Atoi(parts[3])

				// Read payload (simple approach for this CLI)
				// In a real reader we'd use io.ReadFull based on size
				// scanner.Scan() reads until newline, so it might read the payload if it's one line
				if scanner.Scan() {
					payloadLine := scanner.Text()
					// Truncate to size if needed, though scanner strips \r\n usually
					if len(payloadLine) > size {
						payloadLine = payloadLine[:size]
					}
					var seqStr string
					if len(parts) > 4 {
						seqStr = fmt.Sprintf(" [Seq: %s]", parts[4])
					}
					fmt.Printf("[Received on %s]%s: %s\n", parts[1], seqStr, payloadLine)

					// Auto-Ack for testing
					if len(parts) > 4 {
						// Protocol: ACK <seq> [durable]\r\n
						ackCmd := ""
						durableArg := ""
						if *queue != "" && strings.HasPrefix(*queue, "DURABLE:") {
							durableArg = (*queue)[8:]
						}

						if durableArg != "" {
							ackCmd = fmt.Sprintf("ACK %s %s\r\n", parts[4], durableArg)
						} else {
							ackCmd = fmt.Sprintf("ACK %s\r\n", parts[4])
						}
						conn.Write([]byte(ackCmd))
					}
				}
			} else if line == "PING" {
				conn.Write([]byte("PONG\r\n"))
			} else if line == "+OK" {
				// Ack
			}
		}
	} else if *cmd == "replay" {
		// Protocol: REPLAY <seq>\r\n -> Uses "sub" arg as seq for now
		msg := fmt.Sprintf("REPLAY %s\r\n", *subject)
		conn.Write([]byte(msg))

		// Read one message (simplification)
		scanner := bufio.NewScanner(conn)
		if scanner.Scan() {
			fmt.Println(scanner.Text())
			if scanner.Scan() {
				fmt.Println(scanner.Text())
			}
		}
	}
}
