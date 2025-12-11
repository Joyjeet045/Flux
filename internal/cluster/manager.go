package cluster

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"nats-lite/internal/config"
	"nats-lite/internal/durable"
	internalraft "nats-lite/internal/raft"
	"nats-lite/internal/store"
)

// Global constants
const (
	StateLeader    = "Leader"
	StateFollower  = "Follower"
	StateCandidate = "Candidate"
)

type Manager struct {
	config *config.ClusterConfig
	node   *internalraft.Node
	fsm    *internalraft.FSM

	mu    sync.RWMutex
	peers map[string]string // ID -> Address
	ready bool

	// Channels for signaling status changes
	LeaderCh chan bool
}

func NewManager(cfg *config.ClusterConfig, st *store.Store, ds *durable.Store) (*Manager, error) {
	// Create FSM
	fsm := internalraft.NewFSM(st, ds)

	// Create Raft Node
	node, err := internalraft.NewNode(cfg, fsm)
	if err != nil {
		return nil, fmt.Errorf("failed to create raft node: %v", err)
	}

	m := &Manager{
		config:   cfg,
		node:     node,
		fsm:      fsm,
		peers:    make(map[string]string),
		LeaderCh: make(chan bool, 1),
	}

	// Initialize peers from config
	for _, peerAddr := range cfg.Peers {
		// For static config, we might not know the ID yet.
		// Real discovery would handle this dynamically.
		// For now, we assume simple static peering if needed or rely on manual join.
		// Use address as ID placeholder if needed, or better, waiting for Join commands.
		m.peers[peerAddr] = peerAddr
	}

	go m.monitorState()

	return m, nil
}

func (m *Manager) SetOnApply(fn func(cmd *internalraft.LogCommand, seq uint64)) {
	m.fsm.SetOnApply(fn)
}

func (m *Manager) Apply(cmd *internalraft.LogCommand) (interface{}, error) {
	if !m.IsLeader() {
		return nil, fmt.Errorf("not leader")
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}

	return m.node.Apply(data, 5*time.Second) // 5s timeout
}

func (m *Manager) Join(nodeID string, addr string) error {
	log.Printf("Received join request for remote node %s at %s", nodeID, addr)

	if !m.IsLeader() {
		return fmt.Errorf("not leader, cannot join node")
	}

	if err := m.node.AddPeer(nodeID, addr); err != nil {
		return err
	}

	m.mu.Lock()
	m.peers[nodeID] = addr
	m.mu.Unlock()

	return nil
}

func (m *Manager) Leave(nodeID string) error {
	log.Printf("Received leave request for node %s", nodeID)

	if !m.IsLeader() {
		return fmt.Errorf("not leader")
	}

	if err := m.node.RemovePeer(nodeID); err != nil {
		return err
	}

	m.mu.Lock()
	delete(m.peers, nodeID)
	m.mu.Unlock()

	return nil
}

func (m *Manager) IsLeader() bool {
	return m.node.IsLeader()
}

func (m *Manager) GetLeader() string {
	addr := m.node.Leader()
	return string(addr)
}

func (m *Manager) Stats() map[string]string {
	return m.node.Stats()
}

func (m *Manager) Shutdown() error {
	return m.node.Close()
}

// monitorState watches for leadership changes to trigger callbacks or signals
func (m *Manager) monitorState() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var wasLeader bool

	for range ticker.C {
		isLeader := m.IsLeader()
		if isLeader != wasLeader {
			log.Printf("Cluster Leadership Change: IsLeader=%v", isLeader)
			// Non-blocking send
			select {
			case m.LeaderCh <- isLeader:
			default:
			}
			wasLeader = isLeader
		}
	}
}
