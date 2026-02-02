# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

AIClient-2-API is a proxy service that converts client-only LLM APIs (Gemini, Claude/Kiro, Qwen, etc.) into OpenAI-compatible interfaces. It enables tools like Cherry-Studio, NextChat, and Cline to access multiple AI providers through a unified API.

- **Language**: Node.js with ES modules (`"type": "module"`)
- **License**: GPL v3
- **No framework**: Uses raw Node.js HTTP server (no Express)

## ⚠️ 架构原则：纯 Redis 存储（严格执行）

**本服务采用纯 Redis 架构，严禁使用文件配置存储任何运行时数据。**

### 禁止事项
- ❌ 不得使用 JSON 文件存储 provider pools 或其他运行时配置
- ❌ 不得将运行时状态写入文件系统
- ❌ 不得在新功能中引入文件存储依赖
- ❌ 不得回退到文件存储作为"降级方案"

### 允许的文件
- ✅ `configs/config.json` — 仅用于启动参数（端口、API Key 等）
- ✅ `configs/pwd` — 仅用于 Web UI 密码

### 开发要求
添加新功能需要持久化数据时，必须使用 Redis：
```javascript
import { getStorageAdapter } from '../core/storage-factory.js';
const storage = getStorageAdapter();
await storage.set('key', value);
```

## Commands

### Node.js Service

```bash
# Start
npm start                        # Production via process manager (master.js → api-server.js)
npm run start:standalone         # Direct single-process start
npm run start:dev                # Development mode
npm run start:debug              # Debug mode with inspector

# Test
npm run test:unit                # Unit tests (basic, gemini-converter, openai-responses-converter)
npm run test:integration         # Docker integration tests (requires running container)
npm run test:integration:full    # Full API integration tests
npm run test:coverage            # Jest coverage report
npx jest tests/basic.test.js    # Run a single test file

# Redis CLI Tools
npm run migrate:redis            # Migrate config files to Redis
npm run export:redis             # Export Redis data to config files
npm run init:redis               # Initialize Redis with default config
npm run sync:pools               # Sync provider pools to Redis
```

**Test environment**: Jest with babel-jest for ESM transform. Test timeout: 30s. Test API key for integration: `AI_club2026`.

### Go Kiro Service

```bash
# Build
go build -o bin/kiro-server ./cmd/kiro-server
make build                       # Same as above

# Run
go run ./cmd/kiro-server         # Direct run
./bin/kiro-server                # Run built binary

# Test
go test ./tests/unit/...         # Unit tests
go test -v ./tests/unit/...      # Verbose output
go test -coverprofile=coverage.out ./...  # Coverage report
go tool cover -html=coverage.out # View coverage in browser
go test -bench=. -benchmem ./tests/benchmark/...  # Benchmarks

# Docker
make update-go                   # Build Go image + compose up
make update                      # Build both JS and Go images + compose up
```

**Test environment**: Go 1.24+, uses testify for assertions. Default Redis URL: `redis://localhost:6379`.

## Architecture

### Dual-Language Architecture

The project uses a **Strangler Fig pattern** migration strategy:
- **Node.js service** (port 8080): Full-featured proxy with Web UI, multi-provider support, protocol conversion
- **Go service** (port 8081): High-performance `/v1/messages` endpoint for Kiro provider, optimized for 500+ concurrent streaming connections

Both services share the same Redis instance for account pools, tokens, and configuration.

### Request Flow (Node.js)

```
HTTP Request → master.js (child process manager)
             → api-server.js (HTTP server, ~3000 lines)
             → request-handler.js (route dispatch)
             → {UI API | Static files | Plugin routes | API routes | Ollama protocol}
```

API requests go through: `request-handler.js` → `api-manager.js` → `adapter.js` (factory) → Provider service → `ConverterFactory` (protocol conversion) → Response

### Request Flow (Go)

```
HTTP Request → Middleware (Logging → Auth)
             → MessagesHandler
             → Account Selector (lock-free round-robin via Redis INCR)
             → Token Manager (Redis)
             → Kiro Client (HTTP connection pool)
             → SSE Streaming Response
```

### Core Patterns

**Adapter Pattern** — `src/providers/adapter.js` defines `ApiServiceAdapter` base class. Each provider implements `generateContent()`, `generateContentStream()`, `listModels()`. Factory function `getServiceAdapter()` dispatches by provider type string.

**Strategy Pattern** — `src/converters/` converts between protocols. `ConverterFactory` selects converter (Gemini, OpenAI, Claude, Ollama, Codex) based on model name prefix. Internal protocol is Gemini-native; other protocols are converted to/from it.

**Account Pooling** — `src/providers/provider-pool-manager.js` manages multi-account round-robin, automatic failover on 429/errors, health checks, usage/error tracking, and provider/model fallback chains.

### Directory Layout

| Directory | Purpose |
|-----------|---------|
| `cmd/kiro-server/` | **Go**: Entry point for high-performance Kiro service |
| `internal/` | **Go**: Core packages (config, redis, account, kiro, claude, handler) |
| `pkg/middleware/` | **Go**: HTTP middleware (auth, logging) |
| `src/providers/` | **Node.js**: Provider implementations (claude/, gemini/, openai/, forward/) and adapter/pool manager |
| `src/converters/` | **Node.js**: Protocol converters and token usage normalization |
| `src/services/` | **Node.js**: Server entry point (`api-server.js`), service init (`service-manager.js`), UI (`ui-manager.js`) |
| `src/handlers/` | **Node.js**: Request routing (`request-handler.js`, `ollama-handler.js`) |
| `src/core/` | **Node.js**: Process manager (`master.js`), config loading (`config-manager.js`), plugin system, Redis storage adapters |
| `src/ui-modules/` | **Node.js**: Web UI backend API endpoints (13 modules: oauth, provider, config, usage, etc.) |
| `src/utils/` | **Node.js**: Shared utilities — `common.js` has constants including `MODEL_PROVIDER` enum |
| `configs/` | Runtime config (`config.json`, `pwd`) — provider pools now stored in Redis |
| `static/` | Web UI frontend |
| `tests/unit/*.go` | **Go**: Unit tests for Go service |
| `tests/benchmark/*.go` | **Go**: Benchmark tests for Go service |
| `tests/*.test.js` | **Node.js**: Jest test suite |
| `go.mod`, `go.sum` | **Go**: Module dependencies (go-redis, uuid, testify) |
| `Dockerfile` | **Node.js**: Docker image for main service |
| `Dockerfile-go` | **Go**: Docker image for Kiro service |

### Provider Types

Providers are identified by string constants in `src/utils/common.js` `MODEL_PROVIDER`:
- `gemini-cli-oauth`, `gemini-antigravity` — Gemini providers
- `claude-kiro-oauth`, `claude-custom` — Claude providers
- `openai-custom`, `openai-qwen-oauth`, `openai-iflow`, `openai-codex` — OpenAI-protocol providers
- `forward` — Generic HTTP forward proxy

### Adding a New Provider

See `PROVIDER_ADAPTER_GUIDE.md` for the full 5-step process:
1. Add constant to `src/utils/common.js` `MODEL_PROVIDER`
2. Implement service class in `src/providers/`
3. Register adapter in `src/providers/adapter.js`
4. Add models to `provider-models.js` and health check defaults to `provider-pool-manager.js`
5. Update frontend UI (provider-manager.js, file-upload.js, section-config.html, section-guide.html)
6. Add system-level mappings in `service-manager.js`, `provider-utils.js`, `usage-api.js`, `oauth-api.js`

### Configuration

- `configs/config.json` — Main settings (port, API key, default provider, proxy, fallback chains, Redis config)
- `configs/pwd` — Web UI password
- **Provider pools** — Stored in Redis (no longer uses `provider_pools.json`)
- CLI args override config: `--host`, `--port`, `--api-key`, `--model-provider`, etc.

### Redis Configuration (Required)

Redis is **required** for provider pool storage. It provides atomic operations for concurrent access, eliminating race conditions and enabling accurate usage tracking across multiple instances.

**Environment Variables**:
| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_ENABLED` | `true` | Redis storage (required for provider pools) |
| `REDIS_URL` | - | Full Redis URL (overrides host/port) |
| `REDIS_HOST` | `localhost` | Redis host |
| `REDIS_PORT` | `6379` | Redis port |
| `REDIS_PASSWORD` | - | Redis password |
| `REDIS_DB` | `0` | Redis database number |
| `REDIS_KEY_PREFIX` | `aiclient:` | Key prefix for all operations |

**Docker Compose** (recommended):
```yaml
environment:
  - REDIS_ENABLED=true
  - REDIS_URL=redis://redis:6379
```

**CLI Tools**:
```bash
# Migrate existing config files to Redis
npm run migrate:redis -- --config-dir ./configs --redis-url redis://localhost:6379

# Export Redis data back to config files
npm run export:redis -- --output-dir ./backup --redis-url redis://localhost:6379

# Initialize Redis with default config (for fresh deployments without config files)
npm run init:redis -- --redis-url redis://localhost:6379
```

**Volume-Free Deployment** (Redis-only mode):
```yaml
# docker-compose.yml - No config volume needed
services:
  redis:
    image: redis:8-alpine
    volumes:
      - redis-data:/data
  aiclient-api:
    image: heishui/aiclient-2-api:latest
    environment:
      - REDIS_ENABLED=true
      - REDIS_URL=redis://redis:6379
    # No ./configs volume mount required!
    depends_on:
      redis:
        condition: service_healthy
```

Before first start, initialize Redis:
```bash
npm run init:redis -- --redis-url redis://redis:6379 --api-key YOUR_API_KEY
```

**API Endpoints**:
- `GET /api/redis/status` — Redis connection status, health, and metrics
- `GET /api/storage/status` — Storage adapter type and state

**Storage Architecture** (`src/core/`):
| File | Purpose |
|------|---------|
| `redis-client.js` | Redis connection manager with auto-reconnect |
| `redis-config-manager.js` | Redis storage adapter with atomic operations |
| `file-storage-adapter.js` | File-based storage adapter |
| `storage-factory.js` | Creates appropriate adapter based on config |
| `storage-adapter.js` | Base interface definition |
| `write-queue.js` | Queues writes during Redis unavailability |

**Runtime Behavior**: Redis is required at startup. Once connected, if Redis temporarily disconnects, the service continues using in-memory cache and queues writes for replay on reconnection. Provider pools are stored exclusively in Redis.

### Protocol Routing

Model names can be prefixed to force provider routing: `[Kiro]`, `[Gemini]`, `[Claude]`, etc.

**Node.js Service Endpoints** (port 8080):
- `/v1/chat/completions` — OpenAI-compatible
- `/v1beta/generateContent` — Gemini-native
- `/ollama/api/chat`, `/ollama/api/tags` — Ollama protocol
- `/api/*` — Web UI backend APIs
- `/` — Web UI frontend

**Go Service Endpoints** (port 8081):
- `POST /v1/messages` — Claude Messages API (Kiro provider only, streaming optimized)
- `GET /health` — Health check with Redis and account status
- `POST /api/event_logging/batch` — No-op stub (returns 200)

---

## Go Kiro Service: High-Performance Implementation

The Go service implements a high-concurrency version of the Kiro provider's `/v1/messages` endpoint, designed for 500+ concurrent streaming connections.

### Key Features

- **Lock-Free Selection**: Uses atomic Redis `INCR` for round-robin account selection (no distributed locks)
- **Connection Pooling**: HTTP keep-alive with configurable pool size for upstream Kiro API
- **Health-Aware Routing**: Automatic failover with 6-second passive recovery cooldown
- **Token Distribution**: Implements 1:2:25 cache token distribution ratio (same as Node.js)
- **SSE Streaming**: Direct streaming without response buffering
- **Graceful Shutdown**: Completes in-flight requests before termination

### Configuration (Environment Variables)

| Variable | Default | Description |
|----------|---------|-------------|
| `GO_KIRO_PORT` | `8081` | Server port |
| `GO_KIRO_HOST` | `0.0.0.0` | Server host |
| `REDIS_URL` | `redis://localhost:6379` | Redis connection URL (shared with Node.js) |
| `REDIS_KEY_PREFIX` | `aiclient:` | Redis key prefix (must match Node.js) |
| `GO_KIRO_REDIS_POOL_SIZE` | `50` | Redis connection pool size |
| `GO_KIRO_API_KEY` | (from Redis) | API key for authentication |
| `GO_KIRO_MAX_CONNS` | `100` | Max HTTP connections to Kiro API |
| `GO_KIRO_MAX_IDLE_CONNS_PER_HOST` | `10` | Max idle connections per host |
| `GO_KIRO_IDLE_CONN_TIMEOUT` | `90s` | Idle connection timeout |
| `GO_KIRO_KIRO_API_TIMEOUT` | `300s` | Kiro API request timeout |
| `GO_KIRO_LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `GO_KIRO_LOG_JSON` | `true` | Enable JSON logging |
| `GO_KIRO_HEALTH_COOLDOWN` | `6s` | Health recovery cooldown |
| `GO_KIRO_ACCOUNT_CACHE_TTL` | `5s` | Account list cache TTL |
| `GO_KIRO_MAX_RETRIES` | `3` | Max retry attempts on 429/403 |
| `GO_KIRO_GRACEFUL_TIMEOUT` | `30s` | Graceful shutdown timeout |

### Architecture Details

**Lock-Free Account Selection**:
```go
// Atomic increment for round-robin without locks
counter, _ := redis.Incr(ctx, "aiclient:kiro:round-robin-counter")
selected := healthyAccounts[counter % len(healthyAccounts)]
```

**Token Distribution (1:2:25)**:
```go
// Total parts = 28 (1 + 2 + 25)
input := tokens * 1 / 28
creation := tokens * 2 / 28
read := tokens - input - creation  // remainder
```

**Health Recovery**: Passive recovery with cooldown:
1. Account receives 429/403 → marked unhealthy
2. After 6 seconds → eligible for retry
3. Successful request → marked healthy again

### When to Use Go vs Node.js

**Use Go service** (`/v1/messages` on port 8081):
- High-concurrency streaming workloads (100+ concurrent connections)
- Kiro provider only
- Lower memory footprint and CPU usage required

**Use Node.js service** (port 8080):
- Web UI management
- Multi-provider support (Gemini, OpenAI, Forward, etc.)
- Protocol conversion (OpenAI ↔ Claude ↔ Gemini)
- Ollama protocol support
- OAuth flows and token refresh

### Development Notes

**Go Service**:
- Module path: `github.com/anthropics/AIClient-2-API`
- Package structure follows Go conventions: `internal/` (private), `pkg/` (public), `cmd/` (executables)
- All Go code must share Redis data structures with Node.js (same key prefixes, same data formats)
- Token distribution logic must match Node.js implementation exactly (1:2:25 ratio)
- Use `slog` for structured logging (JSON format in production)
- Tests use table-driven test pattern with testify assertions

**Node.js Service**:
- ES modules only (`"type": "module"` in package.json)
- No framework dependencies (raw Node.js HTTP server)
- All new features requiring persistence must use Redis (see architecture principles)
- Protocol converters in `src/converters/` must maintain backward compatibility

---

## Kiro Provider: Cache Token Distribution (Merge-Protected)

Kiro provider implements Claude API-compatible cache billing, distributing input tokens in a **1:2:25** ratio across three dimensions:
- `input_tokens`: base input tokens (ratio: 1)
- `cache_creation_input_tokens`: cache creation tokens (ratio: 2)
- `cache_read_input_tokens`: cache read tokens (ratio: 25)

### Protected Implementation Locations

These must survive upstream merges in `src/providers/claude/claude-kiro.js`:

1. **Import** (~line 13): `import { calculateKiroTokenDistribution } from '../../converters/usage/index.js';`
2. **Stream initial allocation** (~line 1931): `calculateKiroTokenDistribution(estimatedInputTokens)`
3. **Stream final allocation** (~line 2282): `calculateKiroTokenDistribution(inputTokens)`
4. **Non-stream allocation** (~line 2376): `calculateKiroTokenDistribution(inputTokens)`

**Implementation**: `src/converters/usage/token-distribution/RatioTokenDistribution.js`
**Threshold**: 100 tokens (below this, no distribution applied)

### Merge Protection Checklist
- [ ] Import statement present
- [ ] Stream initial allocation call present
- [ ] Stream final allocation call present
- [ ] Non-stream allocation call present
- [ ] Usage response includes all three cache fields
- [ ] Verify: `node -e "import('./src/converters/usage/index.js').then(m => console.log(m.calculateKiroTokenDistribution(1000)))"`
  Expected: `{ input_tokens: 35, cache_creation_input_tokens: 71, cache_read_input_tokens: 894 }`

**This is a core billing feature — merges must not break this design.**

## Changelog
- 2026-01-30: Added Go Kiro service documentation (high-performance `/v1/messages` endpoint, 500+ concurrent connections)
- 2026-01-26: Added explicit architecture principle section prohibiting file-based configuration
- 2026-01-26: **BREAKING**: Redis-only architecture for provider pools, removed `provider_pools.json` (fixes high-concurrency CPU bottleneck)
- 2026-01-25: Added Redis configuration storage with graceful fallback, CLI migration tools, and /api/redis/status endpoint
- 2026-01-25: Expanded CLAUDE.md with architecture, commands, and development guidance
- 2026-01-24: Created document with Kiro cache token distribution protection notes

## Active Technologies

### Node.js Service
- Node.js ES Modules (ES2022+, `"type": "module"`)
- ioredis 5.x (Redis client for atomic operations)
- axios, undici (HTTP clients)
- ws (WebSocket for SSE)
- Jest + babel-jest (testing)

### Go Service
- Go 1.24+ (with generics support)
- github.com/redis/go-redis/v9 (Redis client)
- net/http (standard library HTTP server and client)
- log/slog (structured logging)
- github.com/stretchr/testify (testing assertions)

### Infrastructure
- Redis 7.x Alpine (via Docker, with AOF persistence) — **required for provider pools**
- Docker + Docker Compose (multi-service orchestration)

## Testing Endpoints

### Node.js Claude Kiro Endpoint
- URL: `http://localhost:8080/claude-kiro-oauth/v1/messages`
- Used for comparing Go Kiro implementation with Node.js reference

### Go Kiro Endpoint
- URL: `http://localhost:8081/v1/messages`
- High-performance implementation
