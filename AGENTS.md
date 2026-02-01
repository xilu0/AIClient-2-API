# AGENTS.md

This file provides guidance to AI coding agents when working with this repository.

## Project Overview

AIClient-2-API is a proxy service that converts client-only LLM APIs (Gemini, Claude/Kiro, Qwen, etc.) into OpenAI-compatible interfaces. It enables tools like Cherry-Studio, NextChat, and Cline to access multiple AI providers through a unified API.

- **Language**: Node.js with ES modules (`"type": "module"`) + Go
- **License**: GPL v3
- **No framework**: Uses raw Node.js HTTP server (no Express)

## Architecture Principle: Redis-Only Storage (Strictly Enforced)

**This service uses a pure Redis architecture. File-based configuration for runtime data is prohibited.**

### Prohibited
- JSON files for provider pools or runtime configuration
- Writing runtime state to filesystem
- Introducing file storage dependencies in new features
- Using file storage as a "fallback"

### Allowed Files
- `configs/config.json` — Startup parameters only (port, API Key, etc.)
- `configs/pwd` — Web UI password only

### Development Requirement
New features requiring persistence must use Redis:
```javascript
import { getStorageAdapter } from '../core/storage-factory.js';
const storage = getStorageAdapter();
await storage.set('key', value);
```

## Commands

### Node.js Service

```bash
# Start
npm start                        # Production via process manager
npm run start:standalone         # Direct single-process start
npm run start:dev                # Development mode

# Test
npm run test:unit                # Unit tests
npm run test:integration         # Docker integration tests
npm run test:coverage            # Jest coverage report
npx jest tests/basic.test.js    # Single test file

# Redis CLI Tools
npm run migrate:redis            # Migrate config files to Redis
npm run export:redis             # Export Redis data to config files
npm run init:redis               # Initialize Redis with default config
```

### Go Kiro Service

```bash
# Build
go build -o bin/kiro-server ./cmd/kiro-server
make build

# Run
go run ./cmd/kiro-server
./bin/kiro-server

# Test
go test ./tests/unit/...         # Unit tests
go test -v ./tests/unit/...      # Verbose output
go test -coverprofile=coverage.out ./...

# Docker
make update-go                   # Build Go image + compose up
make update                      # Build both JS and Go images + compose up
```

## Architecture

### Dual-Language Architecture

The project uses a **Strangler Fig pattern** migration strategy:
- **Node.js service** (port 8080): Full-featured proxy with Web UI, multi-provider support, protocol conversion
- **Go service** (port 8081): High-performance `/v1/messages` endpoint for Kiro provider, optimized for 500+ concurrent streaming connections

Both services share the same Redis instance for account pools, tokens, and configuration.

### Request Flow (Node.js)

```
HTTP Request → master.js → api-server.js → request-handler.js
             → {UI API | Static files | Plugin routes | API routes | Ollama protocol}
```

API requests: `request-handler.js` → `api-manager.js` → `adapter.js` → Provider service → `ConverterFactory` → Response

### Request Flow (Go)

```
HTTP Request → Middleware (Logging → Auth) → MessagesHandler
             → Account Selector (Redis INCR) → Token Manager → Kiro Client → SSE Response
```

### Core Patterns

- **Adapter Pattern**: `src/providers/adapter.js` — base class `ApiServiceAdapter`, factory `getServiceAdapter()`
- **Strategy Pattern**: `src/converters/` — protocol conversion (Gemini, OpenAI, Claude, Ollama, Codex)
- **Account Pooling**: `src/providers/provider-pool-manager.js` — multi-account round-robin, failover, health checks

### Directory Layout

| Directory | Purpose |
|-----------|---------|
| `cmd/kiro-server/` | **Go**: Entry point for Kiro service |
| `internal/` | **Go**: Core packages (config, redis, account, kiro, claude, handler) |
| `pkg/middleware/` | **Go**: HTTP middleware (auth, logging) |
| `src/providers/` | **Node.js**: Provider implementations and adapter/pool manager |
| `src/converters/` | **Node.js**: Protocol converters |
| `src/services/` | **Node.js**: Server entry (`api-server.js`), service init, UI |
| `src/handlers/` | **Node.js**: Request routing |
| `src/core/` | **Node.js**: Process manager, config loading, Redis storage |
| `src/ui-modules/` | **Node.js**: Web UI backend API endpoints |
| `src/utils/` | **Node.js**: Shared utilities, `MODEL_PROVIDER` enum |
| `configs/` | Runtime config (`config.json`, `pwd`) |
| `static/` | Web UI frontend |
| `tests/` | Go unit tests, Go benchmarks, Node.js Jest tests |

### Provider Types

Constants in `src/utils/common.js` `MODEL_PROVIDER`:
- `gemini-cli-oauth`, `gemini-antigravity` — Gemini providers
- `claude-kiro-oauth`, `claude-custom` — Claude providers
- `openai-custom`, `openai-qwen-oauth`, `openai-iflow`, `openai-codex` — OpenAI-protocol providers
- `forward` — Generic HTTP forward proxy

### Adding a New Provider

See `PROVIDER_ADAPTER_GUIDE.md` for the full process:
1. Add constant to `src/utils/common.js` `MODEL_PROVIDER`
2. Implement service class in `src/providers/`
3. Register adapter in `src/providers/adapter.js`
4. Add models to `provider-models.js` and health check defaults
5. Update frontend UI
6. Add system-level mappings

## Configuration

- `configs/config.json` — Main settings (port, API key, default provider, proxy, Redis config)
- `configs/pwd` — Web UI password
- **Provider pools** — Stored in Redis only
- CLI args override config: `--host`, `--port`, `--api-key`, `--model-provider`, etc.

### Redis Configuration (Required)

| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_ENABLED` | `true` | Redis storage (required) |
| `REDIS_URL` | - | Full Redis URL |
| `REDIS_HOST` | `localhost` | Redis host |
| `REDIS_PORT` | `6379` | Redis port |
| `REDIS_PASSWORD` | - | Redis password |
| `REDIS_DB` | `0` | Redis database number |
| `REDIS_KEY_PREFIX` | `aiclient:` | Key prefix |

### Go Service Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GO_KIRO_PORT` | `8081` | Server port |
| `GO_KIRO_HOST` | `0.0.0.0` | Server host |
| `GO_KIRO_MAX_CONNS` | `100` | Max HTTP connections to Kiro API |
| `GO_KIRO_LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `GO_KIRO_MAX_RETRIES` | `3` | Max retry attempts on 429/403 |

## API Endpoints

### Node.js Service (port 8080)
- `/v1/chat/completions` — OpenAI-compatible
- `/v1beta/generateContent` — Gemini-native
- `/ollama/api/chat`, `/ollama/api/tags` — Ollama protocol
- `/api/*` — Web UI backend APIs
- `/` — Web UI frontend

### Go Service (port 8081)
- `POST /v1/messages` — Claude Messages API (Kiro provider only)
- `GET /health` — Health check

## Development Notes

### Go Service
- Module path: `github.com/anthropics/AIClient-2-API`
- Package structure: `internal/` (private), `pkg/` (public), `cmd/` (executables)
- Must share Redis data structures with Node.js (same key prefixes, same data formats)
- Token distribution: 1:2:25 ratio (input:cache_creation:cache_read)
- Use `slog` for structured logging
- Tests use table-driven pattern with testify assertions

### Node.js Service
- ES modules only (`"type": "module"`)
- No framework dependencies (raw Node.js HTTP server)
- All persistence must use Redis
- Protocol converters must maintain backward compatibility

## Technologies

### Node.js Service
- Node.js ES Modules (ES2022+)
- ioredis 5.x (Redis client)
- axios, undici (HTTP clients)
- ws (WebSocket)
- Jest + babel-jest (testing)

### Go Service
- Go 1.24+
- github.com/redis/go-redis/v9
- net/http (standard library)
- log/slog (structured logging)
- github.com/stretchr/testify (testing)

### Infrastructure
- Redis 7.x Alpine (required)
- Docker + Docker Compose
