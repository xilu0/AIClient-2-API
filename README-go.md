# Go Kiro Server

High-performance Go implementation of the `/claude-kiro-oauth/v1/messages` endpoint for AIClient-2-API.

## Overview

This service implements the Strangler Fig pattern migration from Node.js to Go, targeting 500+ concurrent streaming connections. It shares Redis data with the existing Node.js application for account pools, usage tracking, and authentication.

## Features

- **High Concurrency**: Designed for 500+ concurrent streaming connections
- **Lock-Free Selection**: Atomic Redis INCR for round-robin account selection
- **Connection Pooling**: HTTP keep-alive for upstream Kiro API calls
- **Health-Aware Routing**: Automatic failover with 60-second passive recovery
- **Token Distribution**: 1:2:25 cache token distribution ratio
- **SSE Streaming**: Direct streaming without response buffering
- **Graceful Shutdown**: Completes in-flight requests before termination

## Quick Start

### Prerequisites

- Go 1.22+
- Redis 7.x (shared with Node.js service)
- Existing Node.js AIClient-2-API with accounts configured

### Development

```bash
# Build (from repo root)
go build -o bin/kiro-server ./cmd/kiro-server

# Run
export REDIS_URL="redis://localhost:6379"
./bin/kiro-server

# Or run directly
go run ./cmd/kiro-server
```

### Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `GO_KIRO_PORT` | `8081` | Server port |
| `GO_KIRO_HOST` | `0.0.0.0` | Server host |
| `REDIS_URL` | `redis://localhost:6379` | Redis connection URL |
| `REDIS_KEY_PREFIX` | `aiclient:` | Redis key prefix |
| `GO_KIRO_REDIS_POOL_SIZE` | `50` | Redis connection pool size |
| `GO_KIRO_API_KEY` | (from Redis) | API key for authentication |
| `GO_KIRO_MAX_CONNS` | `100` | Max HTTP connections to Kiro API |
| `GO_KIRO_LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `GO_KIRO_LOG_JSON` | `true` | Enable JSON logging |
| `GO_KIRO_HEALTH_COOLDOWN` | `60s` | Health recovery cooldown |

### Testing

```bash
# Unit tests
go test ./tests/unit/...

# With verbose output
go test -v ./tests/unit/...

# Coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Benchmarks
go test -bench=. -benchmem ./tests/benchmark/...

# Specific benchmark
go test -bench=BenchmarkConcurrent500Connections -benchtime=10s ./tests/benchmark/...
```

### Docker

```bash
# Build image (using Go-specific Dockerfile)
docker build -f Dockerfile.go -t kiro-server:dev .

# Run container
docker run -p 8081:8081 \
  -e REDIS_URL=redis://host.docker.internal:6379 \
  kiro-server:dev
```

## API Endpoints

### Health Check

```bash
curl http://localhost:8081/health
```

Response:
```json
{"status":"healthy","redis":"connected","accounts":{"total":5,"healthy":4}}
```

### Messages (Streaming)

```bash
curl -X POST http://localhost:8081/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: YOUR_API_KEY" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "stream": true,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      HTTP Request                           │
└─────────────────────┬───────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────┐
│                    Middleware                               │
│              (Logging → Auth → Handler)                     │
└─────────────────────┬───────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────┐
│                  MessagesHandler                            │
│    ┌────────────────────────────────────────────────────┐   │
│    │ 1. Validate Request                                │   │
│    │ 2. Select Account (lock-free round-robin)          │   │
│    │ 3. Get Token                                       │   │
│    │ 4. Send to Kiro API                                │   │
│    │ 5. Stream Response (SSE)                           │   │
│    │ 6. Retry on 429/403 (up to 3 times)                │   │
│    └────────────────────────────────────────────────────┘   │
└─────────────────────┬───────────────────────────────────────┘
                      │
           ┌──────────┴──────────┐
           │                     │
           ▼                     ▼
┌─────────────────┐    ┌─────────────────┐
│   Redis         │    │   Kiro API      │
│ (Account Pool,  │    │ (AWS Event      │
│  Tokens, Config)│    │  Stream)        │
└─────────────────┘    └─────────────────┘
```

## Key Implementation Details

### Lock-Free Account Selection

Uses atomic Redis INCR for round-robin selection without locks:

```go
counter, _ := redis.Incr(ctx, "aiclient:kiro:round-robin-counter")
selected := healthyAccounts[counter % len(healthyAccounts)]
```

### Token Distribution (1:2:25)

Distributes input tokens across three categories:

```go
// Total parts = 28 (1 + 2 + 25)
input := tokens * 1 / 28
creation := tokens * 2 / 28
read := tokens - input - creation  // remainder
```

### Health Recovery

Passive recovery with 60-second cooldown:

1. Account receives 429/403 → marked unhealthy
2. After 60 seconds → eligible for retry
3. Successful request → marked healthy again

## Directory Structure

```
# Go service at repository root (coexists with Node.js in src/)
cmd/kiro-server/
└── main.go                  # Entry point
internal/
├── config/                  # Configuration
├── redis/                   # Redis client, pool, tokens
├── account/                 # Selection, health tracking
├── kiro/                    # Kiro API client, event stream
├── claude/                  # Types, SSE, converter
└── handler/                 # HTTP handlers
pkg/middleware/              # Auth, logging
tests/
├── unit/                    # Go unit tests
├── integration/             # Go integration tests
├── benchmark/               # Go benchmark tests
└── *.js                     # Node.js tests (existing)
go.mod                       # Go module at repo root
Dockerfile.go                # Go service Dockerfile
```

## License

GPL v3 (same as parent project)
