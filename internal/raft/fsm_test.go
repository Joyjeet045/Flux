package raft

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"nats-lite/internal/config"
	"nats-lite/internal/durable"
	"nats-lite/internal/store"
	"os"
	"path/filepath"
	"testing"

	hashiraft "github.com/hashicorp/raft"
)

func TestFSMApply(t *testing.T) {
	// Setup stores
	dir, _ := ioutil.TempDir("", "fsm-test")
	defer os.RemoveAll(dir)
	st, _ := store.NewStore(dir, 1024*1024)

	cfg := &config.Config{
		Durable: config.DurableConfig{
			FlushInterval: "5s",
		},
	}
	ds, _ := durable.NewStore(filepath.Join(dir, "cursors.json"), cfg)

	fsm := NewFSM(st, ds)

	// Test PUB
	cmd := LogCommand{
		Type:    CmdPublish,
		Subject: "test",
		Data:    []byte("payload"),
	}
	data, _ := json.Marshal(cmd)
	logEntry := &hashiraft.Log{
		Index: 1,
		Term:  1,
		Data:  data,
	}

	result := fsm.Apply(logEntry)
	seq, ok := result.(uint64)
	if !ok {
		t.Fatalf("Expected uint64 seq result")
	}
	if seq != 1 {
		t.Errorf("Expected seq 1, got %d", seq)
	}

	// Verify in store
	rec, _ := st.Read(seq)
	if string(rec.Data) != "payload" {
		t.Errorf("Data mismatch in store")
	}
}

func TestFSMSnapshotRestore(t *testing.T) {
	// Setup stores
	dir, _ := ioutil.TempDir("", "fsm-test-snap")
	defer os.RemoveAll(dir)
	st, _ := store.NewStore(dir, 1024*1024)

	cfg := &config.Config{
		Durable: config.DurableConfig{
			FlushInterval: "5s",
		},
	}
	ds, _ := durable.NewStore(filepath.Join(dir, "cursors.json"), cfg)

	fsm := NewFSM(st, ds)

	// Add some state (Update cursor)
	// We handle Ack command in FSM
	ackCmd := LogCommand{
		Type:      CmdAck,
		CursorKey: "durable1",
		Sequence:  100,
	}
	ackData, _ := json.Marshal(ackCmd)
	fsm.Apply(&hashiraft.Log{Index: 1, Data: ackData})

	// Snapshot
	snap, err := fsm.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}

	sink := &mockSnapshotSink{data: make([]byte, 0)}
	if err := snap.Persist(sink); err != nil {
		t.Fatalf("Persist failed: %v", err)
	}

	// Restore to NEW FSM
	dir2, _ := ioutil.TempDir("", "fsm-test-restore")
	defer os.RemoveAll(dir2)
	ds2, _ := durable.NewStore(filepath.Join(dir2, "cursors.json"), cfg)
	fsm2 := NewFSM(nil, ds2) // Store nil as we only restore cursors

	// We need a ReadCloser for Restore
	reader := &mockReadCloser{data: sink.data}
	if err := fsm2.Restore(reader); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	// Verify restored state
	val := ds2.Get("durable1")
	if val != 100 {
		t.Errorf("Expected cursor 100, got %d", val)
	}
}

type mockSnapshotSink struct {
	data []byte
}

func (m *mockSnapshotSink) Write(p []byte) (n int, err error) {
	m.data = append(m.data, p...)
	return len(p), nil
}
func (m *mockSnapshotSink) Close() error  { return nil }
func (m *mockSnapshotSink) ID() string    { return "1" }
func (m *mockSnapshotSink) Cancel() error { return nil }

type mockReadCloser struct {
	data []byte
	pos  int
}

func (m *mockReadCloser) Read(p []byte) (n int, err error) {
	if m.pos >= len(m.data) {
		return 0, io.EOF
	}
	n = copy(p, m.data[m.pos:])
	m.pos += n
	return n, nil
}
func (m *mockReadCloser) Close() error { return nil }
