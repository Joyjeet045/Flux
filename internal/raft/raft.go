package raft

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"nats-lite/internal/config"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb/v2"
)

type Node struct {
	raft    *raft.Raft
	store   *raftboltdb.BoltStore
	fsm     raft.FSM
	config  *config.ClusterConfig
	dataDir string
}

func NewNode(cfg *config.ClusterConfig, fsm raft.FSM) (*Node, error) {
	node := &Node{
		config:  cfg,
		fsm:     fsm,
		dataDir: cfg.DataDir,
	}

	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data dir: %v", err)
	}

	if err := node.setupRaft(); err != nil {
		node.Close()
		return nil, err
	}

	if cfg.Bootstrap {
		log.Printf("Bootstrapping cluster with node %s", cfg.NodeID)
		configuration := raft.Configuration{
			Servers: []raft.Server{
				{
					ID:      raft.ServerID(cfg.NodeID),
					Address: raft.ServerAddress(cfg.AdvertiseAddr),
				},
			},
		}
		node.raft.BootstrapCluster(configuration)
	}

	return node, nil
}

func (n *Node) setupRaft() error {
	c := raft.DefaultConfig()
	c.LocalID = raft.ServerID(n.config.NodeID)

	// Parse durations
	if t, err := time.ParseDuration(n.config.ElectionTimeout); err == nil {
		c.ElectionTimeout = t
	}
	if t, err := time.ParseDuration(n.config.HeartbeatInterval); err == nil {
		c.HeartbeatTimeout = t
	}

	// Create sync/async replication mode
	// Hashicorp raft handles this internally via transport/pipeline, simple config usually fine

	if t, err := time.ParseDuration(n.config.SnapshotInterval); err == nil {
		c.SnapshotInterval = t
	}
	if n.config.SnapshotThreshold > 0 {
		c.SnapshotThreshold = n.config.SnapshotThreshold
	}

	// Setup BoltDB store for logs and stable store
	storePath := filepath.Join(n.dataDir, "raft.db")
	store, err := raftboltdb.NewBoltStore(storePath)
	if err != nil {
		return fmt.Errorf("failed to create bolt store: %v", err)
	}
	n.store = store

	// File snapshot store
	snapshots, err := raft.NewFileSnapshotStore(n.dataDir, 2, os.Stderr)
	if err != nil {
		return fmt.Errorf("failed to create snapshot store: %v", err)
	}

	// TCP Transport
	addr, err := net.ResolveTCPAddr("tcp", n.config.BindAddr)
	if err != nil {
		return err
	}
	transport, err := raft.NewTCPTransport(n.config.BindAddr, addr, 3, 10*time.Second, os.Stderr)
	if err != nil {
		return fmt.Errorf("failed to create tcp transport: %v", err)
	}

	// Create Raft instance
	r, err := raft.NewRaft(c, n.fsm, store, store, snapshots, transport)
	if err != nil {
		return fmt.Errorf("failed to create raft instance: %v", err)
	}
	n.raft = r

	return nil
}

func (n *Node) Apply(cmd []byte, timeout time.Duration) (interface{}, error) {
	future := n.raft.Apply(cmd, timeout)
	if err := future.Error(); err != nil {
		return nil, err
	}
	return future.Response(), nil
}

func (n *Node) IsLeader() bool {
	return n.raft.State() == raft.Leader
}

func (n *Node) State() raft.RaftState {
	return n.raft.State()
}

func (n *Node) Leader() raft.ServerAddress {
	return n.raft.Leader()
}

func (n *Node) Stats() map[string]string {
	return n.raft.Stats()
}

func (n *Node) AddPeer(id string, addr string) error {
	future := n.raft.AddVoter(raft.ServerID(id), raft.ServerAddress(addr), 0, 0)
	if err := future.Error(); err != nil {
		return err
	}
	return nil
}

func (n *Node) RemovePeer(id string) error {
	future := n.raft.RemoveServer(raft.ServerID(id), 0, 0)
	if err := future.Error(); err != nil {
		return err
	}
	return nil
}

func (n *Node) Close() error {
	if n.raft != nil {
		future := n.raft.Shutdown()
		if err := future.Error(); err != nil {
			return err
		}
	}
	if n.store != nil {
		n.store.Close()
	}
	return nil
}
