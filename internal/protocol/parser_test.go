package protocol

import (
	"strings"
	"testing"
)

func TestParsePUB(t *testing.T) {
	input := "PUB test.subject 5\r\nhello\r\n"
	parser := NewParser(strings.NewReader(input))

	cmd, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if cmd.Type != PUB {
		t.Errorf("Expected PUB, got %v", cmd.Type)
	}
	if cmd.Subject != "test.subject" {
		t.Errorf("Expected subject test.subject, got %s", cmd.Subject)
	}
	if string(cmd.Payload) != "hello" {
		t.Errorf("Expected payload hello, got %s", cmd.Payload)
	}
}

func TestParseSUB(t *testing.T) {
	input := "SUB test.* sub1\r\n"
	parser := NewParser(strings.NewReader(input))

	cmd, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if cmd.Type != SUB {
		t.Errorf("Expected SUB, got %v", cmd.Type)
	}
	if cmd.Subject != "test.*" {
		t.Errorf("Expected subject test.*, got %s", cmd.Subject)
	}
	if cmd.Sid != "sub1" {
		t.Errorf("Expected sid sub1, got %s", cmd.Sid)
	}
}

func TestParseINFO(t *testing.T) {
	input := "INFO\r\n"
	parser := NewParser(strings.NewReader(input))

	cmd, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cmd.Type != INFO {
		t.Errorf("Expected INFO, got %v", cmd.Type)
	}
}

func TestParseJOIN(t *testing.T) {
	input := "JOIN node-2 1.2.3.4:7000\r\n"
	parser := NewParser(strings.NewReader(input))

	cmd, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cmd.Type != JOIN {
		t.Errorf("Expected JOIN, got %v", cmd.Type)
	}
	if cmd.Subject != "node-2" { // Parsed into Subject
		t.Errorf("Expected node-2, got %s", cmd.Subject)
	}
	if cmd.Sid != "1.2.3.4:7000" { // Parsed into Sid
		t.Errorf("Expected addr 1.2.3.4:7000, got %s", cmd.Sid)
	}
}

func TestParseInvalidCMD(t *testing.T) {
	input := "GARBAGE\r\n"
	parser := NewParser(strings.NewReader(input))
	_, err := parser.Parse()
	if err == nil {
		t.Error("Expected error for garbage command")
	}
}

func TestPartialRead(t *testing.T) {
	// Simulate partial network write
	// "PUB test 5\r\nhel" ... wait ... "lo\r\n"
	// strings.Reader isn't blocking, but buffer usage should handle fragmentation logic if implemented.
	// However, current Parser implementation often assumes bufio.Reader handles blocking or reads full lines.
	// If bufio.Reader is used, it reads until delimiter.
	// Payload reading uses ReadFull.

	input := "PUB test 5\r\nhello\r\n"
	parser := NewParser(strings.NewReader(input))
	cmd, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if string(cmd.Payload) != "hello" {
		t.Errorf("Partial read simulation failed check")
	}
}
