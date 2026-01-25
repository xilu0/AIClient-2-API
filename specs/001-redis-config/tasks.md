# Tasks: Redis-Based Configuration Storage

**Input**: Design documents from `/specs/001-redis-config/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/redis-storage.yaml

**Tests**: Not explicitly requested - implementation tasks only.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and Redis client infrastructure

- [x] T001 Add ioredis dependency to package.json
- [x] T002 Create src/cli/ directory for CLI migration tools
- [x] T003 [P] Add Redis service to docker/docker-compose.yml with health check and persistence
- [x] T004 [P] Add Redis service to docker/docker-compose.build.yml with health check and persistence

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core Redis client and storage adapter infrastructure that MUST be complete before ANY user story

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [x] T005 Create Redis client manager with connection handling in src/core/redis-client.js (auto-reconnect, connection events, health check)
- [x] T006 Define StorageAdapter base interface with all methods in src/core/storage-adapter.js (getConfig, setConfig, getProviderPools, updateProvider, incrementUsage, getToken, setToken, etc.)
- [x] T007 Implement FileStorageAdapter wrapping existing file I/O logic in src/core/file-storage-adapter.js
- [x] T008 Create storage adapter factory function (createStorageAdapter) in src/core/storage-factory.js
- [x] T009 Add Redis configuration schema to src/core/config-manager.js (redis.enabled, redis.url, redis.keyPrefix, etc.)
- [x] T010 Add Redis environment variable support (REDIS_ENABLED, REDIS_URL, REDIS_HOST, REDIS_PORT, REDIS_PASSWORD, REDIS_DB) to src/core/config-manager.js
- [x] T011 Initialize storage adapter in src/services/api-server.js and src/services/service-manager.js startup sequence (adapter init BEFORE initApiService, load pools from Redis if file empty)
- [x] T012 Create WriteQueue class for queueing writes during Redis unavailability in src/core/write-queue.js

**Checkpoint**: Foundation ready - storage adapter infrastructure in place, user story implementation can now begin

---

## Phase 3: User Story 1 - Provider Pool Configuration Stored in Redis (Priority: P1) 🎯 MVP

**Goal**: Store provider pool configurations in Redis with atomic counter updates for usage tracking

**Independent Test**: Start service with Redis, send concurrent API requests, verify all usage counts are accurately tracked

### Implementation for User Story 1

- [x] T013 [US1] Implement Redis provider pool storage methods in src/core/redis-config-manager.js: getProviderPools(), getProviderPool(type), getProvider(type, uuid)
- [x] T014 [US1] Implement Redis provider pool write methods in src/core/redis-config-manager.js: setProviderPool(type, providers), updateProvider(type, uuid, updates), deleteProvider(type, uuid)
- [x] T015 [US1] Implement atomic counter operations in src/core/redis-config-manager.js: incrementUsage(type, uuid), incrementError(type, uuid) using Redis INCR
- [x] T016 [US1] Implement Lua script for compound atomic updates (increment + timestamp) in src/core/redis-config-manager.js
- [x] T017 [US1] Add in-memory cache layer with Redis sync in src/core/redis-config-manager.js for read performance
- [x] T018 [US1] Modify src/providers/provider-pool-manager.js to use storage adapter instead of direct file I/O for pool loading
- [x] T019 [US1] Modify src/providers/provider-pool-manager.js to use storage adapter for usage count updates (replace debounced file writes)
- [x] T020 [US1] Modify src/providers/provider-pool-manager.js to use storage adapter for health status updates
- [x] T021 [US1] Modify src/providers/provider-pool-manager.js to use storage adapter for error tracking updates
- [x] T022 [US1] Update src/ui-modules/provider-api.js to use storage adapter for add/update/delete provider operations
- [x] T023 [US1] Implement write queue integration in src/core/redis-config-manager.js for Redis unavailability handling
- [x] T024 [US1] Add connection status logging to src/core/redis-client.js for operational monitoring (FR-010)

**Checkpoint**: Provider pools stored in Redis with atomic updates, concurrent API requests work without data loss

---

## Phase 4: User Story 2 - Token Credential Storage in Redis (Priority: P2)

**Goal**: Store OAuth token credentials in Redis for atomic refresh operations

**Independent Test**: Trigger multiple simultaneous token refresh operations, verify all updates preserved

### Implementation for User Story 2

- [x] T025 [US2] Implement Redis token storage methods in src/core/redis-config-manager.js: getToken(type, uuid), setToken(type, uuid, token)
- [x] T026 [US2] Add token TTL support based on expiry in src/core/redis-config-manager.js using SETEX
- [x] T027 [US2] Modify src/providers/claude/claude-kiro.js to use storage adapter for token read/write operations
- [x] T028 [US2] Modify src/providers/gemini/gemini-core.js to use storage adapter for token read/write operations
- [x] T029 [P] [US2] Modify src/providers/openai/qwen-core.js to use storage adapter for token operations
- [x] T030 [P] [US2] Modify src/providers/openai/iflow-core.js to use storage adapter for token operations
- [x] T031 [P] [US2] Modify src/providers/openai/codex-core.js to use storage adapter for token operations
- [x] T032 [US2] Implement atomic token update pattern in storage adapter to prevent concurrent refresh conflicts

**Checkpoint**: Token credentials stored in Redis, concurrent refresh operations work without data corruption

---

## Phase 5: User Story 3 - Main Configuration in Redis (Priority: P3)

**Goal**: Store main service configuration in Redis for non-blocking UI updates

**Independent Test**: Modify configuration via web UI while processing API requests, verify no request stalls

### Implementation for User Story 3

- [x] T033 [US3] Implement Redis config storage methods in src/core/redis-config-manager.js: getConfig(), setConfig(config)
- [x] T034 [US3] Modify src/core/config-manager.js to use storage adapter for reading config on startup (added loadConfigFromRedis, saveConfigToRedis, syncConfigWithRedis)
- [x] T035 [US3] Modify src/ui-modules/config-api.js to use storage adapter for config updates (non-blocking writes)
- [x] T036 [US3] Implement in-memory config cache with change notification in src/core/redis-config-manager.js
- [x] T037 [US3] Add UI password storage methods in src/core/redis-config-manager.js: getPassword(), setPassword()
- [x] T038 [US3] Modify src/ui-modules/auth.js to use storage adapter for password operations

**Checkpoint**: Main configuration in Redis, UI updates don't block API request processing

---

## Phase 6: User Story 4 - Data Migration from Files to Redis (Priority: P2)

**Goal**: CLI tools for migrating file-based config to Redis and exporting Redis back to files

**Independent Test**: Prepare config files, run migration CLI, verify all data correctly represented in Redis

### Implementation for User Story 4

- [x] T039 [US4] Create CLI migration tool in src/cli/migrate-to-redis.js with --config-dir, --redis-url, --dry-run, --force options
- [x] T040 [US4] Implement provider_pools.json import logic in src/cli/migrate-to-redis.js (read file, parse, HSET each provider)
- [x] T041 [US4] Implement token files import logic in src/cli/migrate-to-redis.js (scan provider directories, import each token)
- [x] T042 [US4] Implement config.json import logic in src/cli/migrate-to-redis.js
- [x] T043 [US4] Implement pwd file import logic in src/cli/migrate-to-redis.js
- [x] T044 [US4] Add idempotency handling in src/cli/migrate-to-redis.js (overwrite mode, no duplication)
- [x] T045 [US4] Add migration verification step in src/cli/migrate-to-redis.js (compare Redis state with file state)
- [x] T046 [US4] Add migration metadata recording in src/cli/migrate-to-redis.js (set aiclient:meta version, migratedAt)
- [x] T047 [US4] Create CLI export tool in src/cli/export-from-redis.js with --output-dir, --redis-url options
- [x] T048 [US4] Implement Redis to provider_pools.json export in src/cli/export-from-redis.js
- [x] T049 [US4] Implement Redis to token files export in src/cli/export-from-redis.js
- [x] T050 [US4] Implement Redis to config.json export in src/cli/export-from-redis.js
- [x] T051 [US4] Add npm scripts for migration tools: npm run migrate:redis, npm run export:redis

### Bug Fixes (2026-01-25)

- [x] T041a [US4] Fix token migration to use provider UUID as Redis key instead of file-derived ID (added `migrateProviderTokens()` function in src/cli/migrate-to-redis.js)
- [x] T011a [Foundational] Fix storage adapter initialization order in src/services/api-server.js - must initialize before `initApiService()` to enable Redis-only mode
- [x] T011b [Foundational] Add Redis provider pools loading in src/services/service-manager.js `initApiService()` when file-based pools are empty

**Checkpoint**: CLI migration and export tools complete, file-to-Redis transition works with 100% data fidelity

---

## Phase 7: User Story 5 - File-Based Fallback (Priority: P3)

**Goal**: Service operates with file-based storage when Redis unavailable

**Independent Test**: Start service without Redis, verify it operates exactly as before with file storage

### Implementation for User Story 5

- [x] T052 [US5] Implement fallback detection in src/core/storage-factory.js (check Redis connection, fall back to FileStorageAdapter if unavailable or connection fails)
- [x] T053 [US5] Add empty Redis detection in src/core/redis-config-manager.js (log warning, suggest migration command)
- [x] T054 [US5] Implement runtime Redis disconnect handling in src/core/redis-config-manager.js (switch to in-memory cache, queue writes)
- [x] T055 [US5] Implement write queue replay on Redis reconnection in src/core/redis-config-manager.js
- [x] T056 [US5] Add graceful degradation logging in src/core/redis-client.js (connection lost, operating from cache, reconnected)
- [x] T057 [US5] Verify FileStorageAdapter uses existing file I/O patterns unchanged in src/core/file-storage-adapter.js

**Checkpoint**: Service runs correctly in file-only mode, handles Redis disconnection gracefully

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Final integration, documentation, and operational improvements

- [x] T058 [P] Update docker/docker-compose.yml with Redis volume and network configuration
- [x] T059 [P] Update docker/docker-compose.build.yml with Redis volume and network configuration
- [x] T060 Add Redis status endpoint to src/ui-modules/health-api.js (GET /api/redis/status)
- [x] T061 Update CLAUDE.md with Redis configuration documentation
- [x] T062 Validate quickstart.md scenarios work end-to-end
- [x] T063 Run concurrency stress test: 100 concurrent requests, verify 100% counter accuracy (SC-002)
- [x] T064 Run performance benchmark: verify <10ms Redis operations (Performance Goals from plan.md)

---

## Phase 9: Extended Redis Storage (User Request Extension)

**Purpose**: Migrate remaining config files to Redis, add initialization CLI to enable volume-free deployment

### User Story 6 - Session Token Storage

- [x] T065 [US6] Add session token storage methods to src/core/redis-config-manager.js: getSessionToken(hash), setSessionToken(hash, data, ttl), deleteSessionToken(hash), cleanExpiredSessions()
- [x] T066 [US6] Modify src/ui-modules/auth.js to use storage adapter for session token operations (replace TOKEN_STORE_FILE)
- [x] T067 [US6] Modify src/ui-modules/event-broadcast.js to use storage adapter for session tokens
- [x] T068 [US6] Add session token migration to src/cli/migrate-to-redis.js (import token-store.json)
- [ ] T069 [US6] Add session token export to src/cli/export-from-redis.js *(deferred - low priority)*

### User Story 7 - Usage Cache Storage

- [x] T070 [US7] Add usage cache storage methods to src/core/redis-config-manager.js: getUsageCache(), setUsageCache(data), getProviderUsageCache(type), updateProviderUsageCache(type, data)
- [x] T071 [US7] Modify src/ui-modules/usage-cache.js to use storage adapter (replace USAGE_CACHE_FILE)
- [x] T072 [US7] Add usage cache migration to src/cli/migrate-to-redis.js (import usage-cache.json)
- [ ] T073 [US7] Add usage cache export to src/cli/export-from-redis.js *(deferred - low priority)*

### User Story 8 - Plugin Configuration Storage

- [x] T074 [US8] Add plugin storage methods to src/core/redis-config-manager.js: getPlugins(), setPlugins(config), getPlugin(name), updatePlugin(name, config)
- [x] T075 [US8] Modify src/core/plugin-manager.js to use storage adapter (replace PLUGINS_CONFIG_FILE)
- [x] T076 [US8] Add plugins.json migration to src/cli/migrate-to-redis.js
- [ ] T077 [US8] Add plugins export to src/cli/export-from-redis.js *(deferred - low priority)*

### User Story 9 - Redis Initialization CLI

- [x] T078 [US9] Create src/cli/init-redis.js with --redis-url, --force, --defaults options
- [x] T079 [US9] Implement default config initialization in init-redis.js (API key, port, provider settings)
- [x] T080 [US9] Implement default plugin config initialization in init-redis.js
- [x] T081 [US9] Implement empty provider pools initialization in init-redis.js
- [x] T082 [US9] Add npm script: npm run init:redis
- [x] T083 [US9] Update CLAUDE.md with init:redis documentation and volume-free deployment guide

**Checkpoint**: All config files migrated to Redis, volume mount can be removed for pure Redis deployment

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phase 3-7)**: All depend on Foundational phase completion
  - US1 (P1): Can start first, provides core Redis storage
  - US2 (P2) and US4 (P2): Can start after Foundational, or wait for US1
  - US3 (P3) and US5 (P3): Can start after Foundational
- **Polish (Phase 8)**: Depends on all user stories being complete

### User Story Dependencies

| Story | Priority | Can Start After | Dependencies on Other Stories |
|-------|----------|-----------------|-------------------------------|
| US1 - Provider Pools | P1 | Foundational | None - start first |
| US2 - Token Storage | P2 | Foundational | None - uses same adapter pattern |
| US3 - Main Config | P3 | Foundational | None - uses same adapter pattern |
| US4 - Migration CLI | P2 | Foundational | Best after US1+US2+US3 for complete migration |
| US5 - File Fallback | P3 | Foundational | None - fallback is independent |

### Within Each User Story

- Core storage methods before provider modifications
- Provider modifications before UI module modifications
- All story tasks complete before moving to next priority

### Parallel Opportunities

- T003, T004: Docker compose files can be updated in parallel
- T029, T030, T031: OAuth provider modifications can run in parallel
- T058, T059: Docker compose polish can run in parallel

---

## Parallel Example: User Story 1

```bash
# After T016 (Lua script) completes, these can run in parallel:
Task: "Modify src/providers/provider-pool-manager.js to use storage adapter for usage count updates"
Task: "Modify src/providers/provider-pool-manager.js to use storage adapter for health status updates"
Task: "Modify src/providers/provider-pool-manager.js to use storage adapter for error tracking updates"

# After all pool manager modifications, UI module can be updated:
Task: "Update src/ui-modules/provider-api.js to use storage adapter for add/update/delete provider operations"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL - blocks all stories)
3. Complete Phase 3: User Story 1 - Provider Pools in Redis
4. **STOP and VALIDATE**: Test concurrent API requests, verify atomic counter accuracy
5. Deploy/demo if ready - this alone solves the core concurrency problem

### Incremental Delivery

1. Setup + Foundational → Foundation ready
2. Add User Story 1 (Provider Pools) → Test independently → Deploy MVP
3. Add User Story 4 (Migration CLI) → Enable easy adoption
4. Add User Story 2 (Tokens) → Complete data migration path
5. Add User Story 3 (Config) → Full Redis storage
6. Add User Story 5 (Fallback) → Production resilience
7. Polish → Documentation and validation

### Suggested MVP Scope

**MVP = Phase 1 + Phase 2 + Phase 3 (24 tasks)**

This delivers:
- Redis client infrastructure
- Storage adapter pattern
- Provider pool storage in Redis with atomic counters
- 100% counter accuracy under concurrent load

---

## Summary

| Phase | Tasks | Purpose |
|-------|-------|---------|
| Phase 1: Setup | 4 | Project initialization |
| Phase 2: Foundational | 8 | Core infrastructure (BLOCKING) |
| Phase 3: US1 - Provider Pools | 12 | MVP - atomic pool updates |
| Phase 4: US2 - Token Storage | 8 | Atomic token refresh |
| Phase 5: US3 - Main Config | 6 | Non-blocking UI updates |
| Phase 6: US4 - Migration CLI | 13 | File-to-Redis migration |
| Phase 7: US5 - File Fallback | 6 | Backward compatibility |
| Phase 8: Polish | 7 | Integration and validation |
| Phase 9: Extended Storage | 19 | Session, usage cache, plugins, init CLI |
| **Total** | **83** | |

### Tasks Per User Story

| User Story | Priority | Task Count |
|------------|----------|------------|
| US1 - Provider Pools | P1 | 12 |
| US2 - Token Storage | P2 | 8 |
| US3 - Main Config | P3 | 6 |
| US4 - Migration CLI | P2 | 13 |
| US5 - File Fallback | P3 | 6 |
| US6 - Session Tokens | P2 | 5 |
| US7 - Usage Cache | P3 | 4 |
| US8 - Plugin Config | P3 | 4 |
| US9 - Init CLI | P2 | 6 |

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- The plan.md specifies test-first for critical atomic operations - tests should be added if concurrency issues arise during implementation
