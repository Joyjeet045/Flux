# Flow Control System

## Overview

The NATS-Lite flow control system provides sophisticated backpressure handling and rate limiting for both push and pull consumers. This system prevents slow consumers from overwhelming the broker and enables fine-grained control over message delivery rates.

---

## Core Features

### Push Mode

Push mode enables server-driven message delivery with the following capabilities:

- **Active Message Delivery**: Server actively pushes messages to consumers
- **Rate Limiting**: Token bucket algorithm controls messages per second
- **Backpressure Handling**: Buffered queue with configurable overflow behavior
- **Slow Consumer Detection**: Automatic detection based on buffer utilization

### Pull Mode

Pull mode provides consumer-controlled message delivery:

- **Consumer-Controlled Delivery**: Consumers explicitly request messages in batches
- **Natural Backpressure**: Consumers pull messages at their own processing rate
- **Optional Rate Limiting**: Configurable rate limits on pull requests
- **Efficient Batching**: Reduces network overhead through batch operations

### Backpressure Modes

#### Drop Mode (Default)

- Drops messages when buffer reaches capacity
- Optimal for real-time data where message freshness is critical
- Tracks dropped message count for monitoring

#### Block Mode

- Blocks publisher until buffer space becomes available
- Guarantees message delivery at the cost of publisher throughput
- Recommended for critical data that cannot be lost

#### Shed Mode

- Drops messages only for slow consumers
- Protects fast consumers from being impacted by slow ones
- Provides balanced approach between drop and block modes

---

## Protocol Commands

### FLOWCTL Command

Configure flow control parameters for a subscription.

**Syntax:**
```
FLOWCTL <sid> <mode> [rate] [burst] [buffer]
```

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `sid` | string | Subscription identifier |
| `mode` | enum | PUSH or PULL |
| `rate` | float | Messages per second (0 = unlimited) |
| `burst` | integer | Burst capacity for rate limiter |
| `buffer` | integer | Buffer size in messages |

**Examples:**
```
FLOWCTL sub1 PUSH 100 20 1000
FLOWCTL sub2 PULL 0 0 500
FLOWCTL sub3 PUSH 1000 100 5000
```

**Response:**
```
+OK mode=PUSH rate=100 burst=20 buffer=1000
```

### STATS Command

Retrieve flow control statistics for subscriptions.

**Syntax:**
```
STATS [sid]
```

**Parameters:**
- `sid`: Optional subscription ID. Omit to retrieve statistics for all subscriptions.

**Response:**
```json
+STATS {
  "sub1": {
    "id": "sub1",
    "mode": 0,
    "active": true,
    "rate_limit_tokens": 15,
    "buffered": 42,
    "dropped": 5,
    "delivered": 1234,
    "buffer_usage": 0.42,
    "slow_consumer": false
  }
}
```

---

## Configuration

### Server Configuration

Add the following to `config.json`:

```json
{
  "flow_control": {
    "enable_rate_limit": true,
    "enable_backpressure": true,
    "default_rate_limit": 1000.0,
    "default_rate_burst": 100,
    "default_buffer_size": 1000,
    "default_slow_threshold": 0.8,
    "backpressure_mode": "drop"
  }
}
```

**Configuration Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `enable_rate_limit` | boolean | `true` | Enable global rate limiting |
| `enable_backpressure` | boolean | `true` | Enable backpressure handling |
| `default_rate_limit` | float | `1000.0` | Default messages per second |
| `default_rate_burst` | integer | `100` | Default burst capacity |
| `default_buffer_size` | integer | `1000` | Default buffer size per consumer |
| `default_slow_threshold` | float | `0.8` | Buffer usage threshold (0.0-1.0) |
| `backpressure_mode` | string | `"drop"` | Mode: "drop", "block", or "shed" |

---

## Implementation Examples

### High-Throughput Consumer

```go
client.Send("SUB orders.created sub1\r\n")

client.Send("FLOWCTL sub1 PUSH 5000 500 10000\r\n")
```

Configuration: 5000 msg/sec, burst of 500, buffer of 10000 messages

### Slow Consumer Protection

```go
client.Send("SUB logs.debug sub2\r\n")

client.Send("FLOWCTL sub2 PUSH 10 5 100\r\n")
```

Configuration: 10 msg/sec, burst of 5, buffer of 100 (drops excess)

### Pull-Based Consumer

```go
client.Send("SUB events.analytics sub3\r\n")

client.Send("FLOWCTL sub3 PULL 0 0 1000\r\n")

for {
    client.Send("PULL events.analytics 50\r\n")
    time.Sleep(1 * time.Second)
}
```

### Dynamic Reconfiguration

```go
client.Send("FLOWCTL sub1 PUSH 100 10 500\r\n")

client.Send("STATS sub1\r\n")

client.Send("FLOWCTL sub1 PUSH 1000 100 2000\r\n")
```

---

## Monitoring

### Key Metrics

| Metric | Description |
|--------|-------------|
| `buffered` | Current messages in buffer |
| `dropped` | Total dropped messages (drop/shed mode) |
| `delivered` | Total delivered messages |
| `buffer_usage` | Buffer utilization (0.0-1.0) |
| `slow_consumer` | Boolean flag if usage exceeds threshold |
| `rate_limit_tokens` | Available tokens for rate limiter |

### Slow Consumer Detection

A consumer is marked as slow when:
```
buffer_usage > slow_threshold (default: 0.8)
```

Behavior based on backpressure mode:
- **Drop Mode**: Continues dropping messages
- **Block Mode**: Continues blocking publisher
- **Shed Mode**: Begins dropping for this consumer only

---

## Performance Tuning

### Rate Limiting

The token bucket algorithm operates as follows:
- `rate`: Tokens added per second
- `burst`: Maximum token capacity
- Each message consumes one token

**Tuning Guidelines:**

| Throughput | Rate | Burst |
|------------|------|-------|
| High | 5000 | 500 |
| Moderate | 1000 | 100 |
| Low/Controlled | 100 | 10 |

### Buffer Sizing

**Sizing Guidelines:**

| Use Case | Buffer Size |
|----------|-------------|
| Real-time data | 100-500 |
| Batch processing | 5000-10000 |
| Critical data (block mode) | 1000-2000 |

**Sizing Formula:**
```
buffer_size = rate × latency_tolerance_seconds
```

Example: 1000 msg/sec with 2s tolerance = 2000 buffer size

### Backpressure Mode Selection

| Use Case | Mode | Rationale |
|----------|------|-----------|
| Real-time metrics | Drop | Message freshness over completeness |
| Financial transactions | Block | Zero data loss requirement |
| Mixed consumers | Shed | Protect fast from slow consumers |
| Logs/debugging | Drop | Volume prioritized over completeness |

---

## Architecture

### Components

**TokenBucket** (`limiter.go`)
- Rate limiting implementation
- Thread-safe token management
- Automatic token refill

**BackpressureHandler** (`backpressure.go`)
- Buffered message queue
- Overflow detection
- Slow consumer tracking

**FlowControlledConsumer** (`consumer.go`)
- Combines rate limiting and backpressure
- Push/Pull mode support
- Statistics collection

### Message Flow

#### Push Mode
```
Publisher → Store → Matcher → FlowController → Buffer → RateLimiter → Consumer
                                      ↓
                                  (if full)
                                 Drop/Block
```

#### Pull Mode
```
Consumer Request → FlowController → RateLimiter → Buffer → Dequeue → Consumer
                                                     ↑
                          Publisher → Store → Matcher → Enqueue
```

---

## Testing

Execute the comprehensive test suite:

```bash
# Build and start server
go build -o server.exe ./cmd/server
./server.exe

# Run flow control tests
go build -o flowcontrol-test.exe ./cmd/flowcontrol-test
./flowcontrol-test.exe
```

**Test Scenarios:**
1. Rate limit verification using token bucket
2. Backpressure confirmation with drop behavior
3. Pull mode validation for consumer-controlled delivery
4. Slow consumer detection verification
5. Dynamic reconfiguration testing

---

## Best Practices

1. **Start Conservative**: Begin with lower rates and increase based on monitoring
2. **Monitor Statistics**: Regularly check STATS to detect issues
3. **Match Buffer to Rate**: `buffer_size ≈ rate × expected_latency`
4. **Use Pull for Batch**: Pull mode optimal for batch processing workloads
5. **Separate Critical Paths**: Use different subscriptions for critical vs. non-critical data
6. **Test Under Load**: Simulate peak load conditions to tune settings
7. **Set Alerts**: Configure alerts for high buffer_usage or dropped counts

---

## Troubleshooting

### Messages Being Dropped

**Symptoms:** Increasing `dropped` count in statistics

**Resolution:**
1. Increase `buffer_size`
2. Increase `rate_limit`
3. Switch to `block` mode if data loss is unacceptable
4. Optimize consumer processing speed

### Slow Consumer Flag Set

**Symptoms:** `slow_consumer: true` in statistics

**Resolution:**
1. Increase consumer processing capacity
2. Switch to pull mode for better control
3. Reduce message rate
4. Increase buffer size temporarily

### Publisher Blocking

**Symptoms:** Publisher hangs or experiences slowdown

**Resolution:**
1. Switch from `block` to `drop` or `shed` mode
2. Increase buffer sizes
3. Increase rate limits
4. Scale out consumers horizontally

### Uneven Message Delivery

**Symptoms:** Bursty delivery despite rate limiting

**Resolution:**
1. Reduce `burst` parameter
2. Verify `rate` is appropriate
3. Check network latency
4. Monitor `rate_limit_tokens`

---

## Summary

The flow control system provides production-grade message delivery control:

- **Push and Pull Modes**: Flexible delivery patterns
- **Rate Limiting**: Token bucket algorithm for precise control
- **Backpressure Handling**: Multiple strategies (drop, block, shed)
- **Slow Consumer Detection**: Automatic identification and handling
- **Dynamic Configuration**: Runtime reconfiguration without restart
- **Comprehensive Monitoring**: Real-time statistics and metrics

These features enable reliable, high-performance message delivery in NATS-Lite.
