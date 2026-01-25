# Research: Redis-Based Configuration Storage

**Feature**: 001-redis-config | **Date**: 2026-01-25

## Research Tasks

### 1. Redis Client Library Selection

**Decision**: `ioredis`

**Rationale**:
- Most popular Node.js Redis client with 12k+ GitHub stars
- Full Redis 6.0+ feature support including Lua scripting
- Built-in connection pooling and auto-reconnect
- Native Promise support for async/await
- Cluster and Sentinel support for future scaling
- Active maintenance and TypeScript definitions

**Alternatives Considered**:
| Library | Rejected Because |
|---------|------------------|
| `redis` (node-redis) | Requires explicit promisify, less ergonomic API |
| `tedis` | Smaller community, fewer features |
| `handy-redis` | Wrapper over node-redis, adds dependency layer |

### 2. Redis Data Structure Patterns

**Decision**: Hash-based storage with atomic operations

**Rationale**:
- **Hashes** for provider pools: Each provider type is a hash, each provider UUID is a field
  - Enables `HGET`/`HSET` for single provider access
  - Enables `HGETALL` for full pool retrieval
  - Enables `HINCRBY` for atomic counter updates
- **Strings** for config.json: Single JSON blob (small, rarely updated)
- **Strings** for tokens: Individual token credentials per provider

**Key Schema**:
```
aiclient:config                    # String: JSON of config.json
aiclient:pools:{providerType}      # Hash: uuid → JSON provider object
aiclient:tokens:{providerType}:{uuid}  # String: JSON token credentials
aiclient:pwd                       # String: UI password
```

**Alternatives Considered**:
| Pattern | Rejected Because |
|---------|------------------|
| Single JSON blob for pools | No atomic field updates, full rewrite on each change |
| Redis Streams | Overkill for config state, designed for event logs |
| RedisJSON module | Requires Redis Stack, not available in standard Redis |

### 3. Atomic Counter Updates

**Decision**: Use `HINCRBY` with Lua script for compound updates

**Rationale**:
- `HINCRBY` provides atomic increment for `usageCount`, `errorCount`
- Lua script for compound updates (e.g., increment + set timestamp):
  ```lua
  -- Atomic usage update
  redis.call('HINCRBY', KEYS[1], ARGV[1]..':usageCount', 1)
  redis.call('HSET', KEYS[1], ARGV[1]..':lastUsed', ARGV[2])
  return redis.call('HGET', KEYS[1], ARGV[1])
  ```
- ioredis supports `defineCommand` for reusable Lua scripts

**Performance**: Lua scripts execute atomically on Redis server, eliminating round-trip race conditions.

### 4. Connection Resilience Pattern

**Decision**: Lazy reconnect with in-memory cache and write queue

**Rationale** (per FR-015, FR-016):
1. On startup: Load full state from Redis into memory
2. On Redis disconnect:
   - Continue serving from in-memory cache
   - Queue write operations with timestamps
   - Log warnings for monitoring
3. On Redis reconnect:
   - Replay queued writes in order
   - Resume normal operation
4. On persistent failure:
   - Fall back to file-based storage after configurable timeout

**Implementation**:
```javascript
class WriteQueue {
  queue = [];
  maxSize = 1000;

  push(operation) {
    if (this.queue.length >= this.maxSize) {
      this.queue.shift(); // Drop oldest if queue full
      logger.warn('Write queue overflow, dropping oldest operation');
    }
    this.queue.push({ operation, timestamp: Date.now() });
  }

  async replay(redisClient) {
    while (this.queue.length > 0) {
      const { operation } = this.queue.shift();
      await operation(redisClient);
    }
  }
}
```

### 5. Migration Strategy

**Decision**: CLI-based import/export with idempotency

**Rationale** (per clarifications):
- Migration is operator-initiated, not automatic
- Idempotent: Re-running migration overwrites without duplication
- Export capability for backup/recovery

**Migration Algorithm**:
```
1. Read provider_pools.json → Parse JSON
2. For each providerType:
   a. For each provider in pool:
      - Generate Redis key: aiclient:pools:{providerType}
      - HSET field: {uuid} → JSON(provider)
3. Read config.json → SET aiclient:config
4. Read token files → SET aiclient:tokens:{type}:{uuid}
5. Read pwd → SET aiclient:pwd
6. Verify: Compare Redis state with file state
7. Log summary: "Migrated X providers, Y tokens, config"
```

**Export Algorithm** (reverse):
```
1. HGETALL aiclient:pools:* → Write provider_pools.json
2. GET aiclient:config → Write config.json
3. GET aiclient:tokens:* → Write token files
4. GET aiclient:pwd → Write pwd file
```

### 6. Docker Integration

**Decision**: Sidecar Redis with health check

**docker-compose.yml addition**:
```yaml
services:
  redis:
    image: redis:7-alpine
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

  aiclient-api:
    depends_on:
      redis:
        condition: service_healthy
    environment:
      - REDIS_URL=redis://redis:6379

volumes:
  redis-data:
```

### 7. Configuration Options

**Decision**: Environment variable and config.json support

**Environment Variables**:
```
REDIS_URL=redis://localhost:6379       # Full Redis URL
REDIS_HOST=localhost                   # Alternative: host only
REDIS_PORT=6379                        # Alternative: port only
REDIS_PASSWORD=secret                  # Optional: password
REDIS_DB=0                             # Optional: database number
REDIS_ENABLED=true                     # Enable/disable Redis storage
```

**config.json addition**:
```json
{
  "redis": {
    "enabled": true,
    "url": "redis://localhost:6379",
    "keyPrefix": "aiclient:",
    "connectTimeout": 5000,
    "commandTimeout": 1000
  }
}
```

**Priority**: Environment variables override config.json values.

### 8. Performance Benchmarks (Expected)

Based on Redis benchmarks and similar implementations:

| Operation | File I/O (current) | Redis (expected) |
|-----------|-------------------|------------------|
| Read config | 1-5ms (sync) | <1ms |
| Read provider pool | 2-10ms (async) | <1ms |
| Update usage counter | 50-200ms (debounced write) | <1ms (HINCRBY) |
| Concurrent updates | Race conditions | Atomic |
| 100 concurrent requests | Data loss risk | 100% accuracy |

### 9. Backward Compatibility

**Decision**: Storage adapter interface with factory pattern

```javascript
// StorageAdapter interface
class StorageAdapter {
  async getConfig() {}
  async setConfig(config) {}
  async getProviderPools() {}
  async updateProvider(type, uuid, updates) {}
  async incrementUsage(type, uuid) {}
  // ... etc
}

// Factory
function createStorageAdapter(config) {
  if (config.redis?.enabled) {
    return new RedisStorageAdapter(config.redis);
  }
  return new FileStorageAdapter(config);
}
```

Existing code changes minimally: replace direct file I/O calls with adapter method calls.

## Unresolved Items

None. All NEEDS CLARIFICATION items from spec resolved:
- Redis unavailable behavior: In-memory cache + write queue (FR-015, FR-016)
- Migration trigger: Manual CLI command (clarification session)
- Export capability: CLI export command (FR-007a)
