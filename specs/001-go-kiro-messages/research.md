# Research: Go-based High-Concurrency Kiro Messages Endpoint

**Feature**: 001-go-kiro-messages | **Date**: 2026-01-27 | **Phase**: 0

## Research Tasks Completed

### 1. AWS Event Stream Binary Protocol

**Context**: Kiro API returns responses in AWS event stream format, which needs to be parsed in Go.

**Decision**: Use custom binary parser based on AWS event stream specification

**Rationale**:
- AWS event stream uses a well-documented binary format: prelude (8 bytes: total length + headers length), headers (variable), payload (variable), message CRC (4 bytes)
- Go's `encoding/binary` package provides efficient parsing primitives
- Existing Node.js implementation in `claude-kiro.js` serves as reference (lines 1700-1900)

**Alternatives Considered**:
- `github.com/aws/aws-sdk-go-v2/service/bedrockruntime` - Too heavy, includes full SDK overhead
- Direct stream parsing without validation - Rejected due to risk of data corruption

**Implementation Notes**:
```go
// AWS Event Stream Message Structure
type EventMessage struct {
    TotalLength   uint32 // 4 bytes
    HeadersLength uint32 // 4 bytes
    PreludeCRC    uint32 // 4 bytes
    Headers       map[string]string
    Payload       []byte
    MessageCRC    uint32 // 4 bytes
}
```

### 2. Lock-Free Round-Robin Account Selection

**Context**: Constitution requires lock-free patterns for hot paths. Account selection happens on every request.

**Decision**: Atomic Redis INCR counter with modulo operation

**Rationale**:
- Redis INCR is atomic and returns the new value in a single round-trip
- Modulo by healthy account count gives round-robin distribution
- No distributed locks needed; single Redis command per selection
- Constitution explicitly forbids mutex in hot paths (Principle III)

**Alternatives Considered**:
- Redis RPOPLPUSH for queue-based selection - More complex, still single-threaded in Redis
- Local mutex with periodic sync - Violates constitution; creates bottleneck
- Consistent hashing - Overkill for this use case; accounts are interchangeable

**Implementation Pattern**:
```go
// Lock-free selection
counter, _ := redis.Incr(ctx, "aiclient:kiro:round-robin-counter").Result()
healthyAccounts := getHealthyAccounts() // from cached pool
if len(healthyAccounts) == 0 {
    return nil, ErrNoHealthyAccounts
}
selected := healthyAccounts[counter % int64(len(healthyAccounts))]
```

### 3. Token Refresh Deduplication (Singleflight)

**Context**: Multiple concurrent requests for the same account might trigger redundant token refreshes.

**Decision**: Use `golang.org/x/sync/singleflight` package

**Rationale**:
- Standard Go pattern for deduplicating function calls
- Returns same result to all callers while only executing once
- In-process deduplication is sufficient; cross-process conflicts handled by Redis atomic update
- FR-009 explicitly requires this pattern

**Alternatives Considered**:
- Redis distributed lock - Too slow for hot path; adds latency
- No deduplication - Would hammer refresh endpoint under load
- Custom channel-based deduplication - Reinventing the wheel

**Implementation Pattern**:
```go
var refreshGroup singleflight.Group

func (m *Manager) RefreshToken(accountUUID string) (*Token, error) {
    result, err, _ := refreshGroup.Do(accountUUID, func() (interface{}, error) {
        // Actual refresh logic - only runs once per concurrent batch
        return m.doRefresh(accountUUID)
    })
    return result.(*Token), err
}
```

### 4. HTTP Connection Pooling Strategy

**Context**: Upstream Kiro API calls need efficient connection management for 500+ concurrent requests.

**Decision**: `net/http.Transport` with custom configuration

**Rationale**:
- Go's standard HTTP client uses connection pooling by default
- Configure `MaxIdleConnsPerHost`, `IdleConnTimeout`, and `MaxConnsPerHost` for high throughput
- Keep-alive enabled by default in Go
- FR-010 requires configurable connection limits

**Configuration**:
```go
transport := &http.Transport{
    MaxIdleConns:        100,
    MaxIdleConnsPerHost: 50,
    MaxConnsPerHost:     100,
    IdleConnTimeout:     90 * time.Second,
    TLSHandshakeTimeout: 10 * time.Second,
    DisableKeepAlives:   false,
}
client := &http.Client{
    Transport: transport,
    Timeout:   0, // No timeout for streaming
}
```

### 5. Redis Connection Pool Configuration

**Context**: High-concurrency access to Redis requires proper pooling.

**Decision**: `go-redis/redis/v9` with connection pooling

**Rationale**:
- Most widely used Go Redis client with excellent pool management
- Supports pipelining natively (FR-006a)
- Connection pool size configurable (constitution requirement)
- Compatible with Redis 7.x

**Configuration**:
```go
rdb := redis.NewClient(&redis.Options{
    Addr:         "redis:6379",
    Password:     "",
    DB:           0,
    PoolSize:     50,     // Max connections
    MinIdleConns: 10,     // Warm pool
    PoolTimeout:  4 * time.Second,
    ReadTimeout:  3 * time.Second,
    WriteTimeout: 3 * time.Second,
})
```

### 6. SSE Response Streaming

**Context**: Claude API clients expect Server-Sent Events format.

**Decision**: Direct streaming using `http.ResponseWriter` with flush

**Rationale**:
- Go's `http.Flusher` interface enables immediate data delivery
- No buffering entire response (constitution streaming requirement)
- Match existing Node.js SSE format exactly for compatibility

**Implementation Pattern**:
```go
func writeSSE(w http.ResponseWriter, eventType, data string) {
    flusher, ok := w.(http.Flusher)
    if !ok {
        // Handle error
    }
    fmt.Fprintf(w, "event: %s\n", eventType)
    fmt.Fprintf(w, "data: %s\n\n", data)
    flusher.Flush()
}
```

### 7. Token Distribution Algorithm (1:2:25)

**Context**: Kiro doesn't report cache tokens; must be simulated with fixed ratio.

**Decision**: Port existing `RatioTokenDistribution.js` algorithm to Go

**Rationale**:
- Exact algorithm documented in CLAUDE.md (merge-protected)
- 1:2:25 ratio with 100-token threshold
- Remainder goes to `cache_read_input_tokens`
- Critical billing feature; must match Node.js exactly

**Reference Implementation** (from Node.js):
```javascript
// Total parts = 1 + 2 + 25 = 28
// input_tokens = floor(tokens * 1 / 28)
// cache_creation_input_tokens = floor(tokens * 2 / 28)
// cache_read_input_tokens = tokens - input - creation (gets remainder)
```

**Go Implementation**:
```go
func DistributeTokens(inputTokens int) TokenUsage {
    if inputTokens < 100 {
        return TokenUsage{InputTokens: inputTokens}
    }
    totalParts := 28
    input := inputTokens * 1 / totalParts
    creation := inputTokens * 2 / totalParts
    read := inputTokens - input - creation
    return TokenUsage{
        InputTokens:              input,
        CacheCreationInputTokens: creation,
        CacheReadInputTokens:     read,
    }
}
```

### 8. Health Check and Passive Recovery

**Context**: Unhealthy accounts need automatic recovery without active polling.

**Decision**: Passive recovery with timestamp-based cooldown

**Rationale**:
- FR-007a specifies 60-second cooldown
- Check `lastErrorTime` + 60s before including in selection
- Mark healthy on successful request
- No background goroutines for health checks (simpler, less resource usage)

**State Machine**:
```
healthy → (429/403 error) → unhealthy
unhealthy → (60s elapsed + selected for retry + success) → healthy
unhealthy → (60s elapsed + selected for retry + failure) → unhealthy (reset timer)
```

### 9. Graceful Shutdown Strategy

**Context**: FR-016 requires completing in-flight requests before termination.

**Decision**: Context-based cancellation with `http.Server.Shutdown`

**Rationale**:
- Go's `http.Server.Shutdown(ctx)` handles graceful shutdown natively
- Pass context to downstream operations for cancellation propagation
- SC-007 allows 30 seconds for recovery

**Implementation Pattern**:
```go
// Signal handling
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()

// Start server in goroutine
go func() { server.ListenAndServe() }()

// Wait for shutdown signal
<-ctx.Done()

// Graceful shutdown with timeout
shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
server.Shutdown(shutdownCtx)
```

### 10. Structured JSON Logging

**Context**: FR-017 requires structured JSON logging for log aggregation.

**Decision**: `log/slog` (Go 1.21+ standard library)

**Rationale**:
- Standard library, no external dependency
- Native JSON output handler
- Structured fields for request metadata
- Level-based filtering

**Alternatives Considered**:
- `zerolog` - Excellent performance but external dependency
- `zap` - Feature-rich but overkill for this use case
- `logrus` - Slower, structured logging as afterthought

**Usage Pattern**:
```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
logger.Info("request completed",
    "request_id", reqID,
    "duration_ms", duration.Milliseconds(),
    "status", status,
    "account_uuid", accountUUID,
)
```

## Redis Key Compatibility Matrix

| Key Pattern | Node.js Usage | Go Usage | Compatible |
|-------------|--------------|----------|------------|
| `aiclient:pools:claude-kiro-oauth` | HGETALL, HGET, HSET | HGETALL, HGET, HSET | ✅ |
| `aiclient:tokens:claude-kiro-oauth:{uuid}` | GET, SET | GET, SET | ✅ |
| `aiclient:kiro:refresh-index:{hash}` | GET, SET | GET (read-only) | ✅ |
| `aiclient:config` | GET | GET | ✅ |

## Unresolved Items

None. All technical decisions have been made based on spec clarifications and constitution requirements.
