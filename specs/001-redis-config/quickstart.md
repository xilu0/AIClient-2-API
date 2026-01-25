# Quickstart: Redis Configuration Storage

**Feature**: 001-redis-config | **Date**: 2026-01-25

## Prerequisites

- Node.js 18+ with ES Modules support
- Redis 6.0+ (Docker or native installation)
- Existing AIClient-2-API installation with file-based config

## Quick Setup

### 1. Start Redis (Docker)

```bash
# Start Redis with persistence
docker run -d --name aiclient-redis \
  -p 6379:6379 \
  -v redis-data:/data \
  redis:7-alpine redis-server --appendonly yes

# Verify Redis is running
docker exec aiclient-redis redis-cli ping
# Expected: PONG
```

### 2. Configure AIClient-2-API

**Option A: Environment Variables**
```bash
export REDIS_ENABLED=true
export REDIS_URL=redis://localhost:6379
```

**Option B: config.json**
```json
{
  "redis": {
    "enabled": true,
    "url": "redis://localhost:6379"
  }
}
```

### 3. Migrate Existing Configuration

```bash
# Run migration CLI tool
node src/cli/migrate-to-redis.js --config-dir ./configs

# Expected output:
# Migration complete:
#   - Providers imported: 12
#   - Tokens imported: 8
#   - Config imported: yes
```

### 4. Start the Service

```bash
npm start
# Or with Docker:
docker-compose up -d
```

### 5. Verify Redis Storage

```bash
# Check Redis keys
docker exec aiclient-redis redis-cli KEYS "aiclient:*"

# Expected:
# aiclient:config
# aiclient:pools:gemini-cli-oauth
# aiclient:pools:claude-kiro-oauth
# aiclient:tokens:claude-kiro-oauth:550e8400-...
# ...
```

## Docker Compose Setup

Use this docker-compose.yml for production:

```yaml
version: '3.8'

services:
  redis:
    image: redis:7-alpine
    container_name: aiclient-redis
    ports:
      - "6379:6379"
    volumes:
      - redis-data:/data
    command: redis-server --appendonly yes
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 5s
      retries: 3
    restart: unless-stopped

  aiclient-api:
    build: .
    container_name: aiclient-api
    ports:
      - "8080:3000"
    environment:
      - REDIS_ENABLED=true
      - REDIS_URL=redis://redis:6379
      - ARGS=--api-key YOUR_API_KEY
    depends_on:
      redis:
        condition: service_healthy
    restart: unless-stopped

volumes:
  redis-data:
```

## Configuration Reference

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_ENABLED` | `false` | Enable Redis storage |
| `REDIS_URL` | - | Full Redis URL (overrides host/port) |
| `REDIS_HOST` | `localhost` | Redis host |
| `REDIS_PORT` | `6379` | Redis port |
| `REDIS_PASSWORD` | - | Redis password (optional) |
| `REDIS_DB` | `0` | Redis database number |

### config.json Options

```json
{
  "redis": {
    "enabled": true,
    "url": "redis://localhost:6379",
    "host": "localhost",
    "port": 6379,
    "password": null,
    "db": 0,
    "keyPrefix": "aiclient:",
    "connectTimeout": 5000,
    "commandTimeout": 1000
  }
}
```

### DEFAULT_MODEL_PROVIDERS Configuration

When using Redis storage, ensure the `DEFAULT_MODEL_PROVIDERS` configuration includes all provider types you want to initialize at startup:

```json
{
  "DEFAULT_MODEL_PROVIDERS": ["claude-kiro-oauth", "gemini-cli-oauth"]
}
```

This setting controls which provider types are initialized during startup. Providers not in this list will be skipped during initialization (though their data remains in Redis).

**To update via Redis CLI**:
```bash
# Get current config, add providers, and save back
redis-cli get "aiclient:config" | \
  python3 -c "import sys,json; d=json.loads(sys.stdin.read()); d['DEFAULT_MODEL_PROVIDERS'] = ['claude-kiro-oauth', 'gemini-cli-oauth']; print(json.dumps(d))" | \
  redis-cli -x set "aiclient:config"
```

**Note**: After updating this configuration, restart the service for changes to take effect.

## CLI Commands

### Migrate to Redis

```bash
node src/cli/migrate-to-redis.js [options]

Options:
  --config-dir <path>   Source config directory (default: ./configs)
  --redis-url <url>     Redis URL (default: from env/config)
  --dry-run             Preview migration without writing
  --force               Overwrite existing Redis data
```

### Export from Redis

```bash
node src/cli/export-from-redis.js [options]

Options:
  --output-dir <path>   Output directory (default: ./configs-backup)
  --redis-url <url>     Redis URL (default: from env/config)
```

## Fallback Behavior

### Redis Unavailable at Startup

If Redis is configured but unavailable:
1. Service logs a warning
2. Falls back to file-based storage
3. Continues normal operation

### Redis Disconnects During Operation

1. Service continues using in-memory cached state
2. Write operations are queued
3. On reconnection, queued writes are replayed
4. Log warnings indicate degraded mode

### Monitoring Redis Status

```bash
# Check Redis connection status
curl http://localhost:3000/api/redis/status

# Response:
{
  "connected": true,
  "lastConnectedAt": "2026-01-25T10:00:00Z",
  "queuedWrites": 0
}
```

## Troubleshooting

### "Redis connection refused"

```bash
# Check Redis is running
docker ps | grep redis

# Check connectivity
redis-cli -h localhost -p 6379 ping

# Check firewall/network
telnet localhost 6379
```

### "Migration failed: empty provider pools"

```bash
# Verify source files exist
ls -la configs/provider_pools.json

# Check file permissions
cat configs/provider_pools.json | head
```

### "Atomic counter mismatch"

```bash
# Verify counter keys
redis-cli KEYS "aiclient:counters:*"

# Check specific counter
redis-cli GET "aiclient:counters:gemini-cli-oauth:UUID:usageCount"
```

## Performance Comparison

| Operation | File I/O | Redis |
|-----------|----------|-------|
| Read config | 1-5ms | <1ms |
| Update counter | 50-200ms | <1ms |
| 100 concurrent updates | Race conditions | Atomic |
| Service restart | Re-read all files | Instant from Redis |

## Next Steps

1. Review [Data Model](./data-model.md) for Redis key schema
2. Review [Contracts](./contracts/redis-storage.yaml) for interface details
3. Run integration tests: `npm run test:integration`
