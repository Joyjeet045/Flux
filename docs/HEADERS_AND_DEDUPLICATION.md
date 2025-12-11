# Headers and Message Deduplication

## Overview

NATS-Lite supports message headers and metadata for enhanced message tracing, along with automatic message deduplication to ensure exactly-once semantics. These features enable production-grade messaging patterns including distributed tracing, idempotent operations, and reliable message delivery.

---

## Headers and Metadata

### Capabilities

Message headers provide key-value metadata that can be attached to messages for various purposes including tracing, routing, and content negotiation.

**Key Features:**
- Custom key-value headers attached to messages
- Standard header constants for common use cases
- HPUB protocol command for header-based publishing
- Persistent storage of headers in the Write-Ahead Log
- Efficient header encoding and decoding

### Standard Header Constants

```go
const (
    MessageID     = "Msg-ID"
    Timestamp     = "Timestamp"
    TraceID       = "Trace-ID"
    UserID        = "User-ID"
    CorrelationID = "Correlation-ID"
    ContentType   = "Content-Type"
    Priority      = "Priority"
    ReplyTo       = "Reply-To"
)
```

### HPUB Protocol Command

The HPUB command enables publishing messages with headers.

**Syntax:**
```
HPUB <subject> <header_size> <total_size>\r\n
<headers>\r\n
<payload>\r\n
```

**Parameters:**
- `subject`: Target message subject
- `header_size`: Size of headers in bytes (including trailing `\r\n\r\n`)
- `total_size`: Combined size of headers and payload

**Header Format:**
```
Key1: Value1\r\n
Key2: Value2\r\n
\r\n
```

**Example:**
```
HPUB orders.created 52 75\r\n
Msg-ID: order-12345\r\n
Trace-ID: trace-abc-789\r\n
\r\n
{"order_id": 12345, "amount": 99.99}\r\n
```

### Implementation Examples

#### Publishing with Headers

```go
headers := "Msg-ID: unique-123\r\nTrace-ID: trace-456\r\nUser-ID: user-789\r\n\r\n"
payload := `{"event": "user_login"}`

headerSize := len(headers)
totalSize := headerSize + len(payload)

cmd := fmt.Sprintf("HPUB events.login %d %d\r\n%s%s\r\n", 
    headerSize, totalSize, headers, payload)
conn.Write([]byte(cmd))
```

#### Standard PUB with Auto-Generated Headers

```go
payload := "Simple message"
cmd := fmt.Sprintf("PUB test.subject %d\r\n%s\r\n", len(payload), payload)
conn.Write([]byte(cmd))
```

The server automatically generates a Msg-ID header for messages published via PUB.

### Use Cases

| Use Case | Description |
|----------|-------------|
| Distributed Tracing | Track requests across services using Trace-ID |
| Message Deduplication | Ensure exactly-once delivery using Msg-ID |
| User Attribution | Track message origin with User-ID |
| Request-Reply | Specify reply subjects for asynchronous responses |
| Content Negotiation | Declare payload format via Content-Type |
| Priority Queuing | Enable priority-based message ordering |

---

## Message Deduplication

### Architecture

The deduplication system uses a sliding time window to detect and prevent duplicate message processing based on message identifiers.

**Core Components:**
- Time-based sliding window for duplicate detection
- Message ID-based deduplication logic
- Automatic ID generation for messages without explicit Msg-ID
- Configurable window duration and capacity
- Real-time statistics and monitoring

### Deduplication Algorithm

1. **Message Reception**: Server receives PUB or HPUB command
2. **ID Resolution**: 
   - Use Msg-ID header if present
   - Generate ID from `hash(subject + payload + timestamp)` otherwise
3. **Duplicate Check**: Verify if ID exists in deduplication window
4. **Processing**:
   - Duplicate: Drop message and log event
   - Unique: Store message and add ID to window
5. **Maintenance**: Automatically remove expired IDs from window

### Configuration

Add the following to `config.json`:

```json
{
  "deduplication": {
    "enabled": true,
    "window_size": "5m",
    "max_entries": 100000
  }
}
```

**Configuration Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable or disable deduplication |
| `window_size` | duration | `"5m"` | Time window for duplicate detection |
| `max_entries` | integer | `100000` | Maximum messages in dedup window |

**Valid Duration Formats:** `"1m"`, `"5m"`, `"1h"`, `"24h"`

### Deduplication Behavior

#### Explicit Message ID (HPUB)

```go
// First message - accepted
headers1 := "Msg-ID: order-12345\r\n\r\n"
payload1 := "Order data"
// Status: Stored

// Duplicate message - rejected
headers2 := "Msg-ID: order-12345\r\n\r\n"
payload2 := "Different order data"
// Status: Dropped (duplicate Msg-ID)
```

#### Auto-Generated Message ID (PUB)

```go
// First message
payload1 := "Hello World"
// Status: Stored with Msg-ID: hash(subject+payload+timestamp1)

// Same payload, different timestamp
payload2 := "Hello World"
// Status: Stored with Msg-ID: hash(subject+payload+timestamp2)
// Result: Not considered duplicate due to different timestamp
```

### Statistics

The deduplication system tracks the following metrics:

- **TotalChecked**: Total messages processed
- **DuplicatesFound**: Number of duplicates detected
- **UniqueMessages**: Number of unique messages accepted
- **WindowSize**: Current number of IDs in deduplication window

### Memory Management

**Automatic Cleanup:**
- Background process runs every `window_size / 10`
- Removes expired IDs older than `window_size`
- Evicts oldest entries when `max_entries` limit is reached

**Memory Estimation:**
```
Memory ≈ max_entries × (32 bytes ID + 24 bytes timestamp + overhead)
       ≈ 100,000 × 80 bytes
       ≈ 8 MB
```

---

## Combined Usage Patterns

### Exactly-Once Delivery

```go
msgID := generateUUID()

headers := fmt.Sprintf("Msg-ID: %s\r\nTrace-ID: %s\r\n\r\n", msgID, traceID)
payload := `{"action": "transfer", "amount": 1000}`

headerSize := len(headers)
totalSize := headerSize + len(payload)

cmd := fmt.Sprintf("HPUB banking.transfer %d %d\r\n%s%s\r\n", 
    headerSize, totalSize, headers, payload)

conn.Write([]byte(cmd))

// Retry with same msgID will be detected as duplicate
conn.Write([]byte(cmd))
```

### Distributed Tracing

```go
// Service A
traceID := "trace-" + generateID()
headers := fmt.Sprintf("Trace-ID: %s\r\nService: ServiceA\r\n\r\n", traceID)
payload := `{"user_id": 123, "action": "checkout"}`
publishWithHeaders("orders.process", headers, payload)

// Service B
headers2 := fmt.Sprintf("Trace-ID: %s\r\nService: ServiceB\r\n\r\n", traceID)
payload2 := `{"order_id": 456, "status": "processing"}`
publishWithHeaders("orders.status", headers2, payload2)
```

---

## Testing

### Unit Tests

Execute unit tests for individual components:

```bash
go test -v ./internal/headers/
go test -v ./internal/dedup/
```

### Integration Tests

Run the comprehensive integration test suite:

```bash
# Start server
./server.exe

# Execute integration tests
./headers-dedup-test.exe
```

**Test Coverage:**
- HPUB command with custom headers
- Duplicate message detection and prevention
- Auto-generated Msg-ID for PUB commands
- Header persistence in Write-Ahead Log
- Mixed HPUB and PUB message handling
- Deduplication window behavior
- Concurrent deduplication operations

---

## Performance Considerations

### Headers

**Overhead Analysis:**
- Header encoding: 50-200 bytes typical
- Storage: Headers persisted in WAL with message
- Network: Additional transmission overhead

**Optimization Guidelines:**
- Use concise header keys
- Include only necessary headers
- Account for header size in total_size calculation

### Deduplication

**Performance Characteristics:**
- Duplicate check: O(1) complexity (hash map lookup)
- Insertion: O(1) complexity
- Cleanup: O(n) complexity, runs every `window_size / 10`

**Memory Optimization:**
```json
{
  "deduplication": {
    "window_size": "1m",
    "max_entries": 50000
  }
}
```

Shorter window duration and fewer max entries reduce memory consumption.

---

## Best Practices

### Headers

1. Use predefined header constants when applicable
2. Minimize header size to reduce overhead
3. Include Msg-ID for deduplication and tracing
4. Add Trace-ID for distributed system correlation
5. Document custom application-specific headers

### Deduplication

1. Use client-generated UUIDs for critical messages
2. Design idempotent operations for safe retries
3. Monitor deduplication statistics regularly
4. Tune window duration based on workload
5. Test retry logic thoroughly

### Recommended Pattern

```go
// Recommended: Explicit Msg-ID for critical operations
headers := fmt.Sprintf("Msg-ID: %s\r\nTrace-ID: %s\r\nUser-ID: %s\r\n\r\n", 
    uuid.New(), traceID, userID)

// Not recommended: No Msg-ID for critical operations
headers := fmt.Sprintf("Trace-ID: %s\r\n\r\n", traceID)
```

---

## Troubleshooting

### Headers Not Persisting

**Symptoms:** Headers not appearing in replayed messages

**Resolution:**
- Verify HPUB command is used (not PUB)
- Confirm header_size and total_size are correct
- Ensure headers terminate with `\r\n\r\n`

### Duplicates Not Detected

**Symptoms:** Duplicate messages not being dropped

**Resolution:**
- Verify `deduplication.enabled` is `true` in configuration
- Confirm identical Msg-ID is being used
- Check message is within deduplication window
- Review server logs for duplicate detection entries

### Memory Growth

**Symptoms:** Deduplication window consuming excessive memory

**Resolution:**
- Reduce window_size (e.g., `"5m"` to `"1m"`)
- Lower max_entries (e.g., `100000` to `50000`)
- Monitor WindowSize metric in deduplication statistics

---

## Migration Guide

### Upgrading from PUB to HPUB

**Before:**
```go
payload := "message data"
cmd := fmt.Sprintf("PUB subject %d\r\n%s\r\n", len(payload), payload)
```

**After:**
```go
headers := "Msg-ID: unique-id\r\n\r\n"
payload := "message data"
headerSize := len(headers)
totalSize := headerSize + len(payload)
cmd := fmt.Sprintf("HPUB subject %d %d\r\n%s%s\r\n", 
    headerSize, totalSize, headers, payload)
```

### Enabling Deduplication

1. Update configuration file:
```json
{
  "deduplication": {
    "enabled": true,
    "window_size": "5m",
    "max_entries": 100000
  }
}
```

2. Restart the server
3. Verify activation in server logs

---

## Summary

The headers and deduplication features provide enterprise-grade messaging capabilities:

- **Headers/Metadata**: Complete support for message metadata via HPUB command
- **Deduplication**: Sliding window deduplication with Msg-ID tracking
- **Auto-Generation**: Automatic Msg-ID for messages without explicit headers
- **Configurable**: Flexible window size and capacity settings
- **Tested**: Comprehensive unit and integration test coverage
- **Production-Ready**: Memory-efficient with automatic cleanup

These features enable exactly-once semantics, distributed tracing, and enhanced message routing capabilities in NATS-Lite.
