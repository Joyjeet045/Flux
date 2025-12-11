package protocol

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
)

type CommandType int

const (
	PUB CommandType = iota
	SUB
	MSG
	OK
	ERR
	PING
	PONG
	REPLAY
	ACK
	PULL
	FLOWCTL
	STATS
)

type Command struct {
	Type    CommandType
	Subject string
	Sid     string
	Payload []byte
}

type Parser struct {
	reader *bufio.Reader
}

func NewParser(r io.Reader) *Parser {
	return &Parser{reader: bufio.NewReader(r)}
}

func (p *Parser) Parse() (*Command, error) {
	line, err := p.reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return p.Parse() // Skip empty lines
	}

	parts := bytes.Split(line, []byte(" "))
	op := string(bytes.ToUpper(parts[0]))

	switch op {
	case "PUB":
		if len(parts) < 3 {
			return nil, fmt.Errorf("invalid PUB arguments")
		}
		subject := string(parts[1])
		size, err := strconv.Atoi(string(parts[2]))
		if err != nil {
			return nil, fmt.Errorf("invalid payload size")
		}

		payload := make([]byte, size)
		_, err = io.ReadFull(p.reader, payload)
		if err != nil {
			return nil, err
		}
		// Read trailing \r\n
		p.reader.ReadBytes('\n')

		return &Command{Type: PUB, Subject: subject, Payload: payload}, nil

	case "SUB":
		if len(parts) < 3 {
			return nil, fmt.Errorf("invalid SUB arguments")
		}
		// SUB <subject> <sid>
		// SUB <subject> <queue> <sid>
		if len(parts) == 3 {
			return &Command{Type: SUB, Subject: string(parts[1]), Sid: string(parts[2])}, nil
		}
		// Queue group
		return &Command{Type: SUB, Subject: string(parts[1]), Sid: string(parts[3]), Payload: []byte(parts[2])}, nil

	case "PING":
		return &Command{Type: PING}, nil

	case "PONG":
		return &Command{Type: PONG}, nil

	case "REPLAY":
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid REPLAY arguments")
		}
		// REPLAY <seq>
		// REPLAY FIRST <count>
		// REPLAY LAST
		// REPLAY TIME <unix_timestamp> <count>
		return &Command{Type: REPLAY, Subject: string(parts[1]), Sid: string(bytes.Join(parts[2:], []byte(" ")))}, nil

	case "ACK":
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid ACK arguments")
		}
		// ACK <seq>
		// ACK <seq> <durable_name>
		sid := ""
		if len(parts) > 2 {
			sid = string(parts[2])
		}
		return &Command{Type: ACK, Subject: string(parts[1]), Sid: sid}, nil

	case "PULL":
		if len(parts) < 3 {
			return nil, fmt.Errorf("invalid PULL arguments")
		}
		// PULL <subject> <count>
		return &Command{Type: PULL, Subject: string(parts[1]), Sid: string(parts[2])}, nil

	case "FLOWCTL":
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid FLOWCTL arguments")
		}
		// FLOWCTL <sid> <mode> [rate] [burst] [buffer]
		// FLOWCTL sub1 PUSH 100 10 1000
		// FLOWCTL sub1 PULL 0 0 500
		return &Command{Type: FLOWCTL, Subject: string(parts[1]), Sid: string(bytes.Join(parts[2:], []byte(" ")))}, nil

	case "STATS":
		// STATS [sid]
		sid := ""
		if len(parts) > 1 {
			sid = string(parts[1])
		}
		return &Command{Type: STATS, Sid: sid}, nil
	}

	return nil, fmt.Errorf("unknown command: %s", op)
}
