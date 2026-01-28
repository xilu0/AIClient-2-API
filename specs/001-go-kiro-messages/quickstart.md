# Quickstart: Go Kiro Messages Service

**Feature**: 001-go-kiro-messages | **Date**: 2026-01-27

## Prerequisites

- Go 1.22+ installed
- Redis 7.x running (shared with Node.js service)
- Existing Node.js AIClient-2-API service with accounts configured

## Quick Setup

### 1. Initialize Go Module

```bash
cd /path/to/AIClient-2-API
mkdir -p go-kiro/cmd/kiro-server
cd go-kiro
go mod init github.com/your-repo/AIClient-2-API/go-kiro
```

### 2. Install Dependencies

```bash
go get github.com/redis/go-redis/v9
go get golang.org/x/sync/singleflight
go get github.com/stretchr/testify  # for testing
```

### 3. Environment Configuration

```bash
# Required
export REDIS_URL="redis://localhost:6379"
export REDIS_KEY_PREFIX="aiclient:"

# Optional (with defaults)
export GO_KIRO_PORT="8081"
export GO_KIRO_API_KEY=""              # If empty, reads from Redis config
export GO_KIRO_LOG_LEVEL="info"
export GO_KIRO_MAX_CONNS="100"
export GO_KIRO_REDIS_POOL_SIZE="50"
```

### 4. Run Development Server

```bash
cd go-kiro
go run ./cmd/kiro-server
```

### 5. Test Endpoint

```bash
# Health check
curl http://localhost:8081/health

# Streaming request (requires valid account in Redis)
curl -X POST http://localhost:8081/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: YOUR_API_KEY" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 100,
    "stream": true,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

---

## Project Structure Setup

```bash
# Create directory structure
mkdir -p go-kiro/{cmd/kiro-server,internal/{config,redis,account,kiro,claude,handler},pkg/middleware,tests/{unit,integration,benchmark}}

# Create placeholder files
touch go-kiro/cmd/kiro-server/main.go
touch go-kiro/internal/config/config.go
touch go-kiro/internal/redis/{client,pool,tokens}.go
touch go-kiro/internal/account/{selector,health,refresh}.go
touch go-kiro/internal/kiro/{client,eventstream,types}.go
touch go-kiro/internal/claude/{converter,sse,usage}.go
touch go-kiro/internal/handler/messages.go
touch go-kiro/pkg/middleware/{auth,logging}.go
```

---

## Development Workflow

### Running Tests

```bash
# Unit tests
cd go-kiro
go test ./tests/unit/...

# Integration tests (requires Redis)
REDIS_URL="redis://localhost:6379" go test ./tests/integration/...

# Benchmarks
go test -bench=. ./tests/benchmark/...

# Coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Building

```bash
# Local build
cd go-kiro
go build -o bin/kiro-server ./cmd/kiro-server

# Docker build
docker build -t kiro-server:dev -f go-kiro/Dockerfile go-kiro/
```

### Linting

```bash
# Install golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run linter
cd go-kiro
golangci-lint run
```

---

## Docker Compose Integration

Add to existing `docker/docker-compose.yml`:

```yaml
services:
  # Existing services...
  redis:
    image: redis:8-alpine
    # ... existing config

  aiclient-api:
    # ... existing Node.js service

  # NEW: Go Kiro service
  kiro-server:
    build:
      context: ../go-kiro
      dockerfile: Dockerfile
    ports:
      - "8081:8081"
    environment:
      - REDIS_URL=redis://redis:6379
      - REDIS_KEY_PREFIX=aiclient:
      - GO_KIRO_PORT=8081
    depends_on:
      redis:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8081/health"]
      interval: 10s
      timeout: 5s
      retries: 3
```

---

## Reverse Proxy Configuration (nginx)

Add to `docker/nginx.conf`:

```nginx
upstream nodejs_api {
    server aiclient-api:3000;
}

upstream go_kiro {
    server kiro-server:8081;
}

server {
    listen 80;

    # Route Kiro OAuth messages to Go service
    location /claude-kiro-oauth/v1/messages {
        proxy_pass http://go_kiro/v1/messages;
        proxy_http_version 1.1;
        proxy_set_header Connection '';
        proxy_buffering off;
        proxy_cache off;
        chunked_transfer_encoding off;
    }

    # All other routes to Node.js
    location / {
        proxy_pass http://nodejs_api;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
    }
}
```

---

## Debugging Tips

### Check Redis Connection

```bash
# Verify keys exist
redis-cli -u redis://localhost:6379 KEYS "aiclient:*"

# Check account pool
redis-cli -u redis://localhost:6379 HGETALL "aiclient:pools:claude-kiro-oauth"

# Monitor Redis operations
redis-cli -u redis://localhost:6379 MONITOR
```

### Enable Debug Logging

```bash
export GO_KIRO_LOG_LEVEL="debug"
go run ./cmd/kiro-server
```

### Profile Concurrency

```bash
# Run with pprof enabled
go run ./cmd/kiro-server -pprof

# In another terminal
go tool pprof http://localhost:8082/debug/pprof/goroutine
```

---

## Key Implementation Patterns

### Lock-Free Account Selection

```go
// internal/account/selector.go
func (s *Selector) Select(ctx context.Context) (*Account, error) {
    // Atomic increment
    counter, err := s.redis.Incr(ctx, "aiclient:kiro:round-robin-counter").Result()
    if err != nil {
        return nil, err
    }

    // Get healthy accounts (cached with 5s TTL)
    healthy := s.getHealthyAccounts()
    if len(healthy) == 0 {
        return nil, ErrNoHealthyAccounts
    }

    // Round-robin selection
    idx := counter % int64(len(healthy))
    return &healthy[idx], nil
}
```

### SSE Streaming

```go
// internal/claude/sse.go
func (w *SSEWriter) WriteEvent(eventType string, data interface{}) error {
    jsonData, err := json.Marshal(data)
    if err != nil {
        return err
    }

    fmt.Fprintf(w.w, "event: %s\n", eventType)
    fmt.Fprintf(w.w, "data: %s\n\n", jsonData)

    if flusher, ok := w.w.(http.Flusher); ok {
        flusher.Flush()
    }
    return nil
}
```

### Token Distribution

```go
// internal/claude/usage.go
func DistributeTokens(inputTokens int) TokenUsage {
    if inputTokens < 100 {
        return TokenUsage{InputTokens: inputTokens}
    }
    // 1:2:25 ratio, total 28 parts
    input := inputTokens / 28
    creation := inputTokens * 2 / 28
    read := inputTokens - input - creation
    return TokenUsage{
        InputTokens:              input,
        CacheCreationInputTokens: creation,
        CacheReadInputTokens:     read,
    }
}
```

---

## Validation Checklist

Before deployment, verify:

- [ ] `go test ./...` passes all tests
- [ ] `golangci-lint run` reports no issues
- [ ] Health endpoint returns `{"status":"healthy"}`
- [ ] Redis keys are read correctly (same format as Node.js)
- [ ] Streaming responses match Claude API format
- [ ] Token distribution matches 1:2:25 ratio
- [ ] Graceful shutdown completes in-flight requests
- [ ] No goroutine leaks under load (pprof check)
