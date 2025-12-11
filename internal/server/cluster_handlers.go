package server

import (
	"encoding/json"
	"fmt"
	"log"

	"nats-lite/internal/protocol"
)

func (s *Server) handleJoin(client *Client, cmd *protocol.Command) {
	if s.cluster == nil {
		client.conn.Write([]byte("-ERR clustering not enabled\r\n"))
		return
	}

	nodeID := cmd.Subject
	addr := cmd.Sid

	log.Printf("Received JOIN request from %s at %s", nodeID, addr)

	if err := s.cluster.Join(nodeID, addr); err != nil {
		log.Printf("Failed to join node %s: %v", nodeID, err)
		client.conn.Write([]byte(fmt.Sprintf("-ERR %v\r\n", err)))
		return
	}

	client.conn.Write([]byte("+OK JOINED\r\n"))
}

func (s *Server) handleLeave(client *Client, cmd *protocol.Command) {
	if s.cluster == nil {
		client.conn.Write([]byte("-ERR clustering not enabled\r\n"))
		return
	}

	nodeID := cmd.Subject
	log.Printf("Received LEAVE request from %s", nodeID)

	if err := s.cluster.Leave(nodeID); err != nil {
		log.Printf("Failed to remove node %s: %v", nodeID, err)
		client.conn.Write([]byte(fmt.Sprintf("-ERR %v\r\n", err)))
		return
	}

	client.conn.Write([]byte("+OK LEFT\r\n"))
}

func (s *Server) handleInfo(client *Client, cmd *protocol.Command) {
	info := make(map[string]interface{})
	info["server_id"] = s.addr
	info["version"] = "1.0.0"
	info["go_version"] = "1.21"

	if s.config.Cluster.Enabled && s.cluster != nil {
		info["cluster_id"] = "nats-cluster"
		info["commited_idx"] = 0 // TODO: Expose from FSM
		info["node_id"] = s.config.Cluster.NodeID
		info["leader"] = s.cluster.GetLeader()
		info["is_leader"] = s.cluster.IsLeader()
		info["raft_stats"] = s.cluster.Stats()
	}

	data, err := json.Marshal(info)
	if err != nil {
		client.conn.Write([]byte(fmt.Sprintf("-ERR %v\r\n", err)))
		return
	}
	client.conn.Write([]byte(fmt.Sprintf("INFO %s\r\n", data)))
}
