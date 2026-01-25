# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

AIClient-2-API is a proxy service that converts client-only LLM APIs (Gemini, Claude/Kiro, Qwen, etc.) into OpenAI-compatible interfaces. It enables tools like Cherry-Studio, NextChat, and Cline to access multiple AI providers through a unified API.

- **Language**: Node.js with ES modules (`"type": "module"`)
- **License**: GPL v3
- **No framework**: Uses raw Node.js HTTP server (no Express)

## Commands

```bash
# Start
npm start                        # Production via process manager (master.js → api-server.js)
npm run start:standalone         # Direct single-process start
npm run start:dev                # Development mode

# Test
npm run test:unit                # Unit tests (basic, gemini-converter, openai-responses-converter)
npm run test:integration         # Docker integration tests (requires running container)
npm run test:integration:full    # Full API integration tests
npm run test:coverage            # Jest coverage report
npx jest tests/basic.test.js    # Run a single test file

# Build & Deploy
make update                      # Docker build + compose up
```

**Test environment**: Jest with babel-jest for ESM transform. Test timeout: 30s. Test API key for integration: `AI_club2026`.

## Architecture

### Request Flow

```
HTTP Request → master.js (child process manager)
             → api-server.js (HTTP server, ~3000 lines)
             → request-handler.js (route dispatch)
             → {UI API | Static files | Plugin routes | API routes | Ollama protocol}
```

API requests go through: `request-handler.js` → `api-manager.js` → `adapter.js` (factory) → Provider service → `ConverterFactory` (protocol conversion) → Response

### Core Patterns

**Adapter Pattern** — `src/providers/adapter.js` defines `ApiServiceAdapter` base class. Each provider implements `generateContent()`, `generateContentStream()`, `listModels()`. Factory function `getServiceAdapter()` dispatches by provider type string.

**Strategy Pattern** — `src/converters/` converts between protocols. `ConverterFactory` selects converter (Gemini, OpenAI, Claude, Ollama, Codex) based on model name prefix. Internal protocol is Gemini-native; other protocols are converted to/from it.

**Account Pooling** — `src/providers/provider-pool-manager.js` manages multi-account round-robin, automatic failover on 429/errors, health checks, usage/error tracking, and provider/model fallback chains.

### Directory Layout

| Directory | Purpose |
|-----------|---------|
| `src/providers/` | Provider implementations (claude/, gemini/, openai/, forward/) and adapter/pool manager |
| `src/converters/` | Protocol converters and token usage normalization |
| `src/services/` | Server entry point (`api-server.js`), service init (`service-manager.js`), UI (`ui-manager.js`) |
| `src/handlers/` | Request routing (`request-handler.js`, `ollama-handler.js`) |
| `src/core/` | Process manager (`master.js`), config loading (`config-manager.js`), plugin system |
| `src/ui-modules/` | Web UI backend API endpoints (13 modules: oauth, provider, config, usage, etc.) |
| `src/utils/` | Shared utilities — `common.js` has constants including `MODEL_PROVIDER` enum |
| `configs/` | Runtime config (`config.json`, `provider_pools.json`, `pwd`) |
| `static/` | Web UI frontend |
| `tests/` | Jest test suite |

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

- `configs/config.json` — Main settings (port, API key, default provider, proxy, fallback chains)
- `configs/provider_pools.json` — Multi-account pool definitions per provider type
- `configs/pwd` — Web UI password
- CLI args override config: `--host`, `--port`, `--api-key`, `--model-provider`, etc.

### Redis Configuration

Redis provides atomic operations for concurrent access, enabling accurate usage tracking across multiple instances.

**Environment Variables**:
| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_ENABLED` | `false` | Enable Redis storage |
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

**Graceful Degradation**: When Redis disconnects, the service continues using in-memory cache and queues writes for replay on reconnection.

### Protocol Routing

Model names can be prefixed to force provider routing: `[Kiro]`, `[Gemini]`, `[Claude]`, etc. Endpoints:
- `/v1/chat/completions` — OpenAI-compatible
- `/v1beta/generateContent` — Gemini-native
- `/ollama/api/chat`, `/ollama/api/tags` — Ollama protocol

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
- 2026-01-25: Added Redis configuration storage with graceful fallback, CLI migration tools, and /api/redis/status endpoint
- 2026-01-25: Expanded CLAUDE.md with architecture, commands, and development guidance
- 2026-01-24: Created document with Kiro cache token distribution protection notes

## Active Technologies
- Node.js ES Modules (ES2022+, `"type": "module"`)
- ioredis 5.x (Redis client for atomic operations)
- Redis 7.x Alpine (via Docker, with AOF persistence)
- File-based fallback (configs/*.json) when Redis unavailable
