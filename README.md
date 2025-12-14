<div align="center">
  <img src="logo.png" alt="Flux Logo" width="200"/>
</div>

# Flux

**Distributed job scheduler using a custom Raft-based broker with WAL persistence, time-based scheduling, and intelligent worker coordination.**

Flux is a production-grade distributed message broker written in Go, featuring a complete job scheduler implementation built on top. The system demonstrates real-world distributed systems patterns including Raft consensus clustering, write-ahead logging, and smart task distribution across dynamic worker pools.

## System Components

### Message Broker (Core Infrastructure)
A high-performance Pub/Sub broker providing the communication backbone:
- **Raft Consensus Clustering**: Leader election and log replication for fault tolerance
- **Write-Ahead Log (WAL)**: Persistent, append-only storage with O(1) writes
- **Message Deduplication**: Exactly-once processing using strict Message-IDs
- **Flow Control**: Rate limiting and backpressure protection
- **Atomic Headers**: Key-value metadata support separate from payload
- **Dead Letter Queues (DLQ)**: Automatic routing of failed deliveries

### Job Scheduler (Application Layer)
A distributed task execution system using the broker:
- **Time-Based Scheduling**: Execute jobs at specific future timestamps
- **Smart Load Balancing**: CPU-aware worker selection via heartbeats
- **Worker Discovery**: Dynamic worker registration and health monitoring
- **Shell & Docker Execution**: Support for both shell commands and containerized tasks
- **Reliable Delivery**: ACK-based guarantees prevent job loss
- **Status Tracking**: Real-time job execution monitoring

## Quick Start

### Running the Job Scheduler

1. **Start the Broker**
```bash
./server.exe
```

2. **Start the Scheduler**
```bash
./scheduler.exe
```

3. **Start the Coordinator**
```bash
./coordinator.exe
```

4. **Start a Worker**
```bash
./worker.exe worker-1
```

5. **Submit a Job** (via PowerShell)
```powershell
$body = @{
    type = "shell"
    payload = "echo Hello World"
    run_in_seconds = 5
} | ConvertTo-Json

Invoke-RestMethod -Uri "http://localhost:8080/submit" -Method Post -Body $body -ContentType "application/json"
```

### Automated Test
```powershell
.\test_flow.ps1
```

## Architecture

### Architecture Diagram
```mermaid
graph TD
    %% -- Client Layer --
    subgraph Clients
        User[Client Script]
    end

    %% -- Scheduler Layer --
    subgraph "Flux Scheduler (:8080)"
        Scheduler
        Results[(Results)]
        Scheduler <-->|RW| Results
    end

    User -->|POST| Scheduler
    User -->|GET| Scheduler

    %% -- Broker Cluster (Raft) --
    subgraph "Flux Broker Cluster (:4223)"
        direction TB
        BrokerL[Leader]
        WAL[(WAL)]
        
        subgraph Followers
            F1[Replica 1]
            F2[Replica 2]
        end
        
        BrokerL -->|Log| WAL
        BrokerL -.->|Raft RPC| F1
        BrokerL -.->|Raft RPC| F2
    end

    Scheduler -->|PUB jobs| BrokerL

    %% -- Coordination --
    subgraph "Coordination Layer"
        Coord1[Coordinator 1]
        Coord2[Coordinator 2]
    end
    
    BrokerL -->|Queue Group| Coord1
    BrokerL -->|Queue Group| Coord2

    %% -- Execution --
    subgraph "Worker Swarm"
        W1[Worker A]
        W2[Worker B]
    end

    Coord1 & Coord2 -->|Least Conn| W1 & W2
    W1 & W2 -.->|Heartbeat| Coord1 & Coord2
    W1 & W2 -->|Status| BrokerL
    BrokerL -->|Result| Scheduler
```

### Component Deep Dive

#### 1. The Broker (Raft & WAL)
The heart of Flux is the message broker, which utilizes the **Raft Consensus Algorithm** to ensure data consistency across the cluster.
- **Leader-Follower Model**: All writes go to the Leader, which replicates entries to Followers.
- **WAL Persistence**: Writes are appended to a segmented **Write-Ahead Log** on disk before being acknowledged, guaranteeing **Zero Data Loss** even if the process crashes.
- **Quorum Writes**: A message is considered "committed" only when a majority of nodes have persisted it.

#### 2. The Coordinator (Smart Load Balancer)
The Coordinator acts as the brain of the distribution layer. Instead of simple Round-Robin or Random dispatch, it implements a **Least Connections** strategy:
- **Active Job Tracking**: It knows exactly how many jobs each worker is currently processing.
- **Random Tie-Breaker**: If two workers have the same job count, one is selected randomly to ensure even distribution.
- **Push-Based Dispatch**: Jobs are **pushed** to the best worker immediately, minimizing latency compared to worker polling.

#### 3. The Scheduler (API & State)
The Scheduler provides the user-facing HTTP API (`/submit`, `/metrics`).
- **Idempotency**: It tracks Job IDs to prevent duplicate execution.
- **Metrics Aggregation**: It listens to the `jobs.status` stream to build real-time performance reports (e.g., success rates, latencies).

### Message Flow
1. **Submission**: User POSTs job → Scheduler persists to Broker (`jobs.queue`).
2. **Replication**: Broker Leader replicates to Followers → Writes to Disk (WAL).
3. **Dispatch**: Coordinator picks up job → Selects best Worker (Least Active Jobs).
4. **Execution**: Worker runs task → Publishes result to `jobs.status`.
5. **Ack**: Scheduler receives result → Updates History → User queries `/metrics`.

### Broker Channels
| Channel | Purpose |
| :--- | :--- |
| `jobs.queue` | Pending jobs ready for assignment |
| `worker.heartbeat` | Worker active job counts and liveness |
| `worker.{id}.jobs` | Direct job delivery to specific worker |
| `jobs.status` | Job execution results and status updates |

## Core Features

### Messaging Fundamentals
- **High Performance Protocol**: Text-based TCP with pipelining support
- **Subject-Based Addressing**: Hierarchical routing (e.g., `orders.us.created`)
  - Wildcards: `*` (single token), `>` (all subsequent tokens)
- **Queue Groups**: Load balanced consumption across subscriber pools

### Persistence & Durability
- **Write-Ahead Log (WAL)**: Segmented log files with automatic rotation
- **Durable Subscriptions**: Consumer cursor tracking for crash recovery
- **Retention Policies**:
  - Time-based expiration (e.g., 24h)
  - Capacity-based rotation (e.g., 5GB limit)

### Clustering & High Availability
- **Raft Consensus**: Industry-standard leader election and replication
- **Zero Data Loss**: Quorum-based write confirmation
- **Automatic Failover**: Sub-second leader promotion
- **Dynamic Membership**: Runtime JOIN/LEAVE operations

### Advanced Capabilities
- **Flow Control**: Token bucket rate limiting + backpressure
- **Review & Replay**: Historical data access by sequence or timestamp
- **Message Deduplication**: Exactly-once delivery windows

## Protocol Overview

### Client Commands

| Command | Syntax | Description |
| :--- | :--- | :--- |
| **PUB** | `PUB <subject> <len>` | Publish message payload |
| **HPUB** | `HPUB <subj> <hdr_len> <tot_len>` | Publish with headers |
| **SUB** | `SUB <subject> <sid> [group]` | Subscribe to topic |
| **ACK** | `ACK <seq> [durable_key]` | Acknowledge message receipt |
| **PULL** | `PULL <subject> <count>` | Request messages (pull mode) |
| **FLOWCTL** | `FLOWCTL <sid> <mode> [args]` | Configure flow control |
| **STATS** | `STATS [sid]` | Request statistics |
| **PING** | `PING` | Keep-alive check |
| **REPLAY** | `REPLAY <seq|time> <val>` | Replay from sequence/timestamp |

### Server Responses

| Command | Syntax | Description |
| :--- | :--- | :--- |
| **MSG** | `MSG <subj> <sid> <len> <seq>` | Message delivery |
| **+OK** | `+OK [msg]` | Operation success |
| **-ERR** | `-ERR <error>` | Operation error |
| **PONG** | `PONG` | PING response |

### Cluster Commands (Admin)

| Command | Syntax | Description |
| :--- | :--- | :--- |
| **JOIN** | `JOIN <node_id> <addr>` | Add node to cluster |
| **LEAVE** | `LEAVE <node_id>` | Remove node from cluster |

## Configuration

Example `config.json`:

```json
{
  "server": {
    "port": ":4223",
    "data_dir": "data"
  },
  "ack": {
    "timeout": "60s",
    "max_retries": 3
  },
  "retention": {
    "max_age": "24h",
    "max_bytes": 1073741824
  },
  "flow_control": {
    "enable_rate_limit": true,
    "default_buffer_size": 10000,
    "backpressure_mode": "block"
  }
}
```

## Build

```bash
# Build all components
go build -o server.exe ./cmd/server
go build -o scheduler.exe ./cmd/scheduler
go build -o coordinator.exe ./cmd/coordinator
go build -o worker.exe ./cmd/worker
```

## Use Cases

- **Task Scheduling**: Cron-like job execution with distributed workers
- **Message Queue**: Reliable Pub/Sub for microservices
- **Event Streaming**: Real-time data pipeline backbone
- **Job Processing**: Background task execution (video encoding, data processing)
- **Distributed Systems Learning**: Reference implementation of Raft, WAL, and consensus patterns
