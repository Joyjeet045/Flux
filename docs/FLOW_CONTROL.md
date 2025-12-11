# Flow Control System

## Overview

The NATS-Lite flow control system provides sophisticated backpressure handling and rate limiting for both push and pull consumers. This prevents slow consumers from overwhelming the system and allows fine-grained control over message delivery rates.

## Features

### 1. **Push Mode**
- **Active Message Delivery**: Server actively pushes messages to consumers
- **Rate Limiting**: Token bucket algorithm limits messages per second
- **Backpressure Handling**: Buffered queue with configurable overflow behavior
- **Slow Consumer Detection**: Automatic detection based on buffer usage

### 2. **Pull Mode**
- **Consumer-Controlled**: Consumer explicitly requests messages in batches
- **Natural Backpressure**: Consumer pulls at their own pace
- **Rate Limiting**: Optional rate limits on pull requests
- **Efficient Batching**: Reduces network overhead

### 3. **Backpressure Modes**

#### Drop Mode (Default)
- Drops messages when buffer is full
- Best for real-time data where old messages lose value
- Tracks dropped message count

#### Block Mode
- Blocks publisher until buffer space available
- Guarantees delivery but may slow publishers
- Use for critical data

#### Shed Mode
- Drops messages only for slow consumers
- Protects fast consumers from slow ones
- Hybrid approach

## Protocol Commands

### FLOWCTL - Configure Flow Control

```
FLOWCTL <sid> <mode> [rate] [burst] [buffer]
```

**Parameters:**
- `sid`: Subscription ID
- `mode`: PUSH or PULL
- `rate`: Messages per second (0 = unlimited)
- `burst`: Burst capacity for rate limiter
- `buffer`: Buffer size (messages)

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

### STATS - Get Flow Control Statistics

```
STATS [sid]
```

**Parameters:**
- `sid`: (Optional) Specific subscription ID, omit for all subscriptions

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

## Configuration

### config.json

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

**Parameters:**
- `enable_rate_limit`: Enable rate limiting globally
- `enable_backpressure`: Enable backpressure handling
- `default_rate_limit`: Default messages per second
- `default_rate_burst`: Default burst capacity
- `default_buffer_size`: Default buffer size per consumer
- `default_slow_threshold`: Buffer usage threshold for slow consumer (0.0-1.0)
- `backpressure_mode`: "drop", "block", or "shed"

## Usage Examples

### Example 1: High-Throughput Consumer

```go
// Subscribe to subject
client.Send("SUB orders.created sub1\r\n")

// Configure for high throughput
client.Send("FLOWCTL sub1 PUSH 5000 500 10000\r\n")
// 5000 msg/sec, burst of 500, buffer 10000 messages
```

### Example 2: Slow Consumer Protection

```go
// Subscribe with small buffer
client.Send("SUB logs.debug sub2\r\n")

// Low rate limit with drop mode
client.Send("FLOWCTL sub2 PUSH 10 5 100\r\n")
// 10 msg/sec, burst 5, buffer 100 (drops excess)
```

### Example 3: Pull-Based Consumer

```go
// Subscribe
client.Send("SUB events.analytics sub3\r\n")

// Configure pull mode
client.Send("FLOWCTL sub3 PULL 0 0 1000\r\n")

// Consumer pulls when ready
for {
    client.Send("PULL events.analytics 50\r\n")
    // Process 50 messages
    time.Sleep(1 * time.Second)
}
```

### Example 4: Dynamic Reconfiguration

```go
// Start with conservative settings
client.Send("FLOWCTL sub1 PUSH 100 10 500\r\n")

// Monitor performance
client.Send("STATS sub1\r\n")

// Increase throughput based on stats
client.Send("FLOWCTL sub1 PUSH 1000 100 2000\r\n")
```

## Monitoring

### Key Metrics

1. **buffered**: Current messages in buffer
2. **dropped**: Total dropped messages (drop/shed mode)
3. **delivered**: Total delivered messages
4. **buffer_usage**: 0.0-1.0, percentage of buffer used
5. **slow_consumer**: Boolean flag if usage > threshold
6. **rate_limit_tokens**: Available tokens for rate limiter

### Slow Consumer Detection

A consumer is marked as "slow" when:
```
buffer_usage > slow_threshold (default: 0.8)
```

This triggers different behavior based on backpressure mode:
- **Drop**: Continues dropping
- **Block**: Continues blocking
- **Shed**: Starts dropping for this consumer only

## Performance Tuning

### Rate Limiting

**Token Bucket Algorithm:**
- `rate`: Tokens added per second
- `burst`: Maximum tokens (bucket capacity)
- Each message consumes 1 token

**Tuning:**
- High throughput: `rate=5000, burst=500`
- Moderate: `rate=1000, burst=100`
- Low/controlled: `rate=100, burst=10`

### Buffer Sizing

**Guidelines:**
- **Real-time data**: Small buffer (100-500)
- **Batch processing**: Large buffer (5000-10000)
- **Critical data**: Medium buffer with block mode (1000-2000)

**Formula:**
```
buffer_size = rate * latency_tolerance_seconds
```

Example: 1000 msg/sec with 2s tolerance = 2000 buffer

### Backpressure Mode Selection

| Use Case | Mode | Rationale |
|----------|------|-----------|
| Real-time metrics | Drop | Old data loses value |
| Financial transactions | Block | No data loss acceptable |
| Mixed consumers | Shed | Protect fast from slow |
| Logs/debugging | Drop | Volume over completeness |

## Architecture

### Components

1. **TokenBucket** (`limiter.go`)
   - Rate limiting implementation
   - Thread-safe token management
   - Automatic refill

2. **BackpressureHandler** (`backpressure.go`)
   - Buffered message queue
   - Overflow detection
   - Slow consumer tracking

3. **FlowControlledConsumer** (`consumer.go`)
   - Combines rate limiting + backpressure
   - Push/Pull mode support
   - Statistics collection

### Message Flow

#### Push Mode:
```
Publisher → Store → Matcher → FlowController → Buffer → RateLimiter → Consumer
                                      ↓
                                  (if full)
                                   Drop/Block
```

#### Pull Mode:
```
Consumer Request → FlowController → RateLimiter → Buffer → Dequeue → Consumer
                                                      ↑
                          Publisher → Store → Matcher → Enqueue
```

## Testing

Run the comprehensive test suite:

```bash
# Build and run server
go build -o server.exe ./cmd/server
./server.exe

# In another terminal, run flow control tests
go build -o flowcontrol-test.exe ./cmd/flowcontrol-test
./flowcontrol-test.exe
```

### Test Scenarios

1. **Rate Limit Test**: Verifies token bucket limits msg/sec
2. **Backpressure Test**: Confirms drop behavior on overflow
3. **Pull Mode Test**: Validates consumer-controlled delivery
4. **Slow Consumer Test**: Checks slow consumer detection
5. **Dynamic Reconfig Test**: Tests runtime configuration changes

## Best Practices

1. **Start Conservative**: Begin with lower rates and increase based on monitoring
2. **Monitor Stats**: Regularly check `STATS` to detect issues
3. **Match Buffer to Rate**: `buffer_size ≈ rate * expected_latency`
4. **Use Pull for Batch**: Pull mode ideal for batch processing
5. **Separate Critical Paths**: Use different subscriptions for critical vs. non-critical data
6. **Test Under Load**: Simulate peak load to tune settings
7. **Set Alerts**: Alert on high `buffer_usage` or `dropped` counts

## Troubleshooting

### Problem: Messages Being Dropped

**Symptoms:** `dropped` count increasing in stats

**Solutions:**
1. Increase `buffer_size`
2. Increase `rate_limit`
3. Switch to `block` mode if data loss unacceptable
4. Optimize consumer processing speed

### Problem: Slow Consumer Flag Set

**Symptoms:** `slow_consumer: true` in stats

**Solutions:**
1. Increase consumer processing capacity
2. Use pull mode for better control
3. Reduce message rate
4. Increase buffer size temporarily

### Problem: Publisher Blocking

**Symptoms:** Publisher hangs or slows down

**Solutions:**
1. Switch from `block` to `drop` or `shed` mode
2. Increase buffer sizes
3. Increase rate limits
4. Scale out consumers

### Problem: Uneven Message Delivery

**Symptoms:** Bursty delivery despite rate limiting

**Solutions:**
1. Reduce `burst` parameter
2. Ensure `rate` is appropriate
3. Check network latency
4. Monitor `rate_limit_tokens`

## Future Enhancements

- [ ] Per-subject flow control policies
- [ ] Adaptive rate limiting based on consumer performance
- [ ] Priority queues for message ordering
- [ ] Flow control metrics export (Prometheus)
- [ ] Circuit breaker integration
- [ ] Automatic slow consumer disconnection
