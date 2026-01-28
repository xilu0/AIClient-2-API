# Implementation Plan: Go-based High-Concurrency Kiro Messages Endpoint

**Branch**: `001-go-kiro-messages` | **Date**: 2026-01-27 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/001-go-kiro-messages/spec.md`

## Summary

Implement a high-performance Go service to handle `/claude-kiro-oauth/v1/messages` endpoint using the Strangler Fig pattern. The Go service will share Redis data with the existing Node.js application, targeting 500+ concurrent streaming connections. Key technical approaches:
- Lock-free round-robin account selection via atomic Redis INCR
- Singleflight pattern for token refresh deduplication
- Connection pooling for both Redis and upstream Kiro API
- AWS event stream binary parsing for Kiro responses

## Technical Context

**Language/Version**: Go 1.22+ (for improved HTTP server performance and better generics support)
**Primary Dependencies**:
- `go-redis/redis/v9` - Redis client with connection pooling and pipelining
- `golang.org/x/sync/singleflight` - Token refresh deduplication
- Standard library `net/http` - High-performance HTTP server
- Standard library `encoding/binary` - AWS event stream parsing

**Storage**: Redis 7.x (shared with Node.js, key prefix: `aiclient:`)
**Testing**: Go testing package + `testify` for assertions, `httptest` for HTTP mocks
**Target Platform**: Linux server (Docker/docker-compose deployment)
**Project Type**: Standalone binary coexisting with Node.js service

**Performance Goals**:
- 500+ concurrent streaming connections (SC-001)
- <500ms median time to first token (SC-002)
- <2s p99 latency for streaming (SC-003)
- <10ms Redis operations (SC-005)

**Constraints**:
- Stable memory under sustained load (SC-004)
- 30s graceful shutdown recovery (SC-007)
- Must not block on token refresh (SC-008)

**Scale/Scope**:
- Single endpoint: `/claude-kiro-oauth/v1/messages`
- Shared Redis with existing Node.js service
- Reverse proxy (nginx/traefik) for traffic routing

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Evidence |
|-----------|--------|----------|
| **I. Multi-Protocol Compatibility** | ✅ PASS | Claude API message format preserved; SSE output matches existing Claude protocol; 1:2:25 token distribution maintained |
| **II. High Availability Architecture** | ✅ PASS | Round-robin account selection with health-aware routing; automatic failover on 429/403; passive recovery with 60s cooldown |
| **III. Performance-First Concurrency** | ✅ PASS | Atomic Redis INCR for lock-free round-robin; singleflight for token refresh; connection pooling with pipelining; HTTP keep-alive |
| **IV. Testing Discipline** | ⚠️ PLANNED | Unit tests for token distribution, health checking, failover logic; integration tests for full request flow; benchmark tests for concurrency validation |
| **V. Modular Extensibility** | ✅ PASS | Adapter pattern for potential future providers; Redis data structures unchanged; backward-compatible with Node.js |

**Redis Operations Compliance**:
- ✅ Using atomic primitives (INCR, HGET, HSET)
- ✅ Optimistic retry pattern (not Lua scripts for selection)
- ✅ Connection pooling with configurable size
- ✅ Pipelining for batch reads

**HTTP Connections Compliance**:
- ✅ Pooled connections with keep-alive
- ✅ Configurable connection limits
- ✅ Timeouts enforced

**Streaming Compliance**:
- ✅ SSE responses streamed directly (no buffering)
- ✅ Bounded goroutine pool
- ✅ Resource cleanup on connection close

## Project Structure

### Documentation (this feature)

```text
specs/001-go-kiro-messages/
├── plan.md              # This file
├── research.md          # Phase 0 output - technology decisions
├── data-model.md        # Phase 1 output - entity definitions
├── quickstart.md        # Phase 1 output - development guide
├── contracts/           # Phase 1 output - API schemas
│   ├── messages-api.yaml    # OpenAPI spec for messages endpoint
│   └── redis-schema.md      # Redis key structure documentation
└── tasks.md             # Phase 2 output (/speckit.tasks command)
```

### Source Code (repository root)

```text
# Go service at repository root (coexists with Node.js)
cmd/
└── kiro-server/
    └── main.go                    # Entry point, config loading, graceful shutdown
internal/
├── config/
│   └── config.go                  # Configuration from env/flags
├── redis/
│   ├── client.go                  # Redis connection pool
│   ├── pool.go                    # Provider pool operations
│   └── tokens.go                  # Token CRUD operations
├── account/
│   ├── selector.go                # Lock-free round-robin selection
│   ├── health.go                  # Health tracking and failover
│   └── refresh.go                 # Token refresh with singleflight
├── kiro/
│   ├── client.go                  # HTTP client for Kiro API
│   ├── eventstream.go             # AWS event stream parser
│   └── types.go                   # Request/response types
├── claude/
│   ├── converter.go               # Kiro → Claude format conversion
│   ├── sse.go                     # SSE event writer
│   └── usage.go                   # Token distribution (1:2:25)
└── handler/
    └── messages.go                # HTTP handler for /v1/messages
pkg/
└── middleware/
    ├── auth.go                    # API key validation
    └── logging.go                 # Structured JSON logging
tests/
├── unit/
│   ├── selector_test.go
│   ├── usage_test.go
│   └── eventstream_test.go
├── integration/
│   ├── messages_test.go
│   └── redis_test.go
└── benchmark/
    └── concurrent_test.go
go.mod                             # Go module at repo root for IDE support
go.sum
Dockerfile.go                      # Go service Dockerfile (separate from Node.js)

# Deployment integration
docker/
├── docker-compose.yml             # Updated with Go service
└── nginx.conf                     # Reverse proxy routing rules
```

**Structure Decision**: Go module at repository root with `go.mod` for optimal IDE support (gopls, autocompletion). Uses Go standard project layout with `cmd/` for entry point, `internal/` for private packages, and `pkg/` for reusable middleware. Coexists with Node.js code (src/, static/, configs/). Integration via shared Redis and reverse proxy routing.

## Complexity Tracking

> No constitution violations requiring justification.

| Aspect | Decision | Rationale |
|--------|----------|-----------|
| Separate Go service | Required | Node.js performance bottleneck for 500+ concurrent connections |
| Redis as shared state | Required | Constitution mandates Redis for provider pools; enables zero-downtime migration |
| Reverse proxy routing | Required | Strangler Fig pattern for gradual migration |
