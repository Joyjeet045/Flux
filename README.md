![Flux Logo](logo.png)

# Flux

Flux is a high-performance, lightweight, and distributed message broker written in Go. It is designed to provide robust Pub/Sub capabilities, strong persistence, and horizontal scalability through Raft-based clustering. Flux bridges the gap between lightweight queues and heavy enterprise brokers by offering durability and clustering in a compiled, single-binary footprint.

## Core Features

### 1. Messaging Fundamentals
*   **High Performance Protocol**: Uses a text-based TCP protocol optimized for low latency and high throughput. Supports pipelining to minimize round-trip times.
*   **Subject-Based Addressing**: Messages are routed via hierarchical subjects (e.g., `orders.us.created`).
    *   Wildcards: `*` matches a single token (e.g., `orders.*.created`), `>` matches all subsequent tokens (e.g., `orders.>`).
*   **Queue Groups**: Load balance message consumption across multiple subscribers by assigning them to a Queue Group. Ideally suited for microservices worker pools.

### 2. Persistence & Durability
*   **Write-Ahead Log (WAL)**: An append-only disk storage engine ensures O(1) write performance.
*   **In-Memory Index**: Maps message sequences to disk offsets for blazing fast lookups.
*   **Durable Subscriptions**: Clients can establish durable sessions. Flux tracks their last seen sequence, ensuring zero message loss even if the consumer disconnects for hours.
*   **Retention Policies**:
    *   **Time-Based**: Automatically expire messages older than N duration (e.g., 24h).
    *   **Capacity-Based**: Rotate logs when storage exceeds a size limit (e.g., 5GB).

### 3. Clustering & High Availability (HA)
*   **Raft Consensus**: Uses the industry-standard Raft algorithm for leader election and log replication.
*   **Zero Data Loss**: Writes are confirmed only after being replicated to a quorum of nodes.
*   **Automatic Failover**: If the Leader node fails, a Follower is automatically promoted to Leader within seconds.
*   **Dynamic Membership**: Nodes can join or leave the cluster at runtime without service interruption via the `JOIN` command.

### 4. Advanced Capabilities
*   **Message Deduplication**: Enforces exactly-once processing windows using strict Message-IDs.
*   **Flow Control**: Protects the broker and clients using rate limiting (Token Bucket) and backpressure.
*   **Atomic Headers**: Messages support key-value metadata headers (e.g., `Trace-ID: 123`) separate from the payload.
*   **Dead Letter Queues (DLQ)**: Failed deliveries are automatically routed to DLQ subjects for forensic analysis.
*   **Review & Replay**: Clients can request to replay historical data from any point in time or sequence.

## Architecture Highlights

Flux separates concerns into modular subsystems:
*   **WAL Store**: Manages segmented log files on disk. Old segments are rotated and pruned based on retention policy.
*   **Durable Engine**: Persists subscription cursors (Ack offsets) asynchronously, allowing fast recovery on restart.
*   **Raft FSM**: The Finite State Machine (FSM) applies replicated log entries to the local Store, ensuring all nodes in the cluster eventually hold the exact same data.
*   **Protocol Layer**: A zero-allocation text parser handles high-throughput command processing.

## Configuration

Flux is configured via a JSON file. Example `config.json`:

```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 4222
  },
  "storage": {
    "dir": "./data",
    "retention": {
      "max_age": "24h",
      "max_bytes": 1073741824
    }
  },
  "cluster": {
    "enabled": true,
    "node_id": "node-1",
    "raft_addr": "127.0.0.1:7001",
    "raft_dir": "./data/raft"
  }
}
```

## Protocol Overview

Flux uses a line-based text protocol similar to other lightweight brokers.

### Client Commands

| Command | Syntax | Description |
| :--- | :--- | :--- |
| **PUB** | `PUB <subject> <len>` | Publish a message payload. follows with `\r\n<payload>` |
| **HPUB** | `HPUB <subj> <hdr_len> <tot_len>` | Publish with headers. Follows with `\r\n<headers><payload>` |
| **SUB** | `SUB <subject> <sid> [group]` | Subscribe to a topic with a Subscriber ID. |
| **ACK** | `ACK <seq> [durable_key]` | Acknowledge receipt of a message (for At-Least-Once). |
| **PULL** | `PULL <subject> <count>` | Request `count` messages (Pull Consumer). |
| **FLOWCTL** | `FLOWCTL <sid> <mode> [args...]` | Configure flow control. E.g., `PUSH 100 10` or `PULL`. |
| **STATS** | `STATS [sid]` | Request statistics for a subscriber or the connection. |
| **PING** | `PING` | Keep-alive check. |
| **INFO** | `INFO` | Request broker topology and details. |
| **REPLAY** | `REPLAY <seq\|time> <val>` | Request replay from sequence or timestamp. |

### Server Responses

| Command | Syntax | Description |
| :--- | :--- | :--- |
| **MSG** | `MSG <subj> <sid> <len>` | Message pushed to client. |
| **+OK** | `+OK [msg]` | Operation success. |
| **-ERR** | `-ERR <error_message>` | Operation error (e.g., `-ERR NOT_LEADER`). |
| **PONG** | `PONG` | Response to PING. |
| **INFO** | `INFO <json>` | Broker information and topology. |

### Cluster Commands (Admin)

| Command | Syntax | Description |
| :--- | :--- | :--- |
| **JOIN** | `JOIN <node_id> <addr>` | Add a new node to the cluster. |
| **LEAVE** | `LEAVE <node_id>` | Remove a node from the cluster. |

## Build & Run

### Build
```bash
go build -o flux ./cmd/server
```

### Run
```bash
# Standalone
./flux -config config.json

# Cluster Node
./flux -config node1.json
```
