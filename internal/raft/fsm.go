package raft

import (
	"encoding/json"
	"fmt"
	"io"
	"log"

	"nats-lite/internal/durable"
	"nats-lite/internal/headers"
	"nats-lite/internal/store"

	"github.com/hashicorp/raft"
)

// Command types for Raft log entries
const (
	CmdPublish = "PUB"
	CmdAck     = "ACK"
)

type LogCommand struct {
	Type      string          `json:"type"`
	Subject   string          `json:"subject,omitempty"`
	Data      []byte          `json:"data,omitempty"`
	Headers   headers.Headers `json:"headers,omitempty"`
	Sequence  uint64          `json:"sequence,omitempty"`   // For ACKs
	CursorKey string          `json:"cursor_key,omitempty"` // For ACKs
}

type FSM struct {
	store   *store.Store
	durable *durable.Store
	OnApply func(cmd *LogCommand, seq uint64)
}

func NewFSM(st *store.Store, ds *durable.Store) *FSM {
	return &FSM{
		store:   st,
		durable: ds,
	}
}

func (f *FSM) SetOnApply(fn func(cmd *LogCommand, seq uint64)) {
	f.OnApply = fn
}

func (f *FSM) Apply(l *raft.Log) interface{} {
	var cmd LogCommand
	if err := json.Unmarshal(l.Data, &cmd); err != nil {
		log.Printf("failed to unmarshal command: %v", err)
		return nil
	}

	switch cmd.Type {
	case CmdPublish:
		// Append to local store. Note: Sequence IDs are generated locally and may differ
		// across nodes if stores were pre-populated differently. We rely on Raft Index
		// for global ordering consistency.
		seq, err := f.store.AppendWithHeaders(cmd.Subject, cmd.Headers, cmd.Data)
		if err != nil {
			log.Printf("failed to apply publish: %v", err)
			return err
		}
		if f.OnApply != nil {
			f.OnApply(&cmd, seq)
		}
		return seq

	case CmdAck:
		if f.durable != nil {
			f.durable.Update(cmd.CursorKey, cmd.Sequence)
		}
		return nil

	default:
		return fmt.Errorf("unknown command type: %s", cmd.Type)
	}
}

func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
	// For now, we only snapshot the durable cursors.
	// The message store is assumed to be persistent on disk.
	// A full production impl would snapshot the Store segments too or use a specialized sync.
	return &fsmSnapshot{durable: f.durable}, nil
}

func (f *FSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()
	// Restore logic: Read the snapshot and apply it.
	// In our simplified case, we restore cursors.
	return f.durable.Restore(rc)
}

// fsmSnapshot implements raft.FSMSnapshot
type fsmSnapshot struct {
	durable *durable.Store
}

func (s *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	err := func() error {
		// Encode durable cursors to the sink
		return s.durable.Snapshot(sink)
	}()
	if err != nil {
		sink.Cancel()
		return err
	}
	return sink.Close()
}

func (s *fsmSnapshot) Release() {}
