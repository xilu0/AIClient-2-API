# Feature Specification: Redis-Based Configuration Storage

**Feature Branch**: `001-redis-config`
**Created**: 2026-01-25
**Status**: Draft
**Input**: User description: "目前使用文件保存帐号配置性能较差，请改用redis保存配置，关注优化性能，优化并发数支持，业务场景是ai api。相关配置文件有：docker/configs/provider_pools.json，docker/configs/config.json，docker/configs/kiro目录下的token文件，此配置为示例，不同部署位置目录需对应调整。"

## Clarifications

### Session 2026-01-25

- Q: What should happen when Redis becomes unavailable during operation? → A: Continue operating with in-memory cached state; queue writes for retry when Redis recovers.
- Q: How is migration from files to Redis triggered? → A: Manual CLI command only — operator must explicitly run a migration script before starting the service with Redis.
- Q: Should there be a way to export Redis data back to config files? → A: Yes, include a CLI export command that dumps Redis config state back to standard config files for backup/recovery.

## User Scenarios & Testing

### User Story 1 - Provider Pool Configuration Stored in Redis (Priority: P1)

As an API service operator, I want provider pool configurations (account lists, health status, usage counters, error tracking) stored in Redis instead of `provider_pools.json` files, so that concurrent API requests can safely read and update provider state without data loss or file corruption.

**Why this priority**: Provider pool state is the most frequently read and written configuration. Every API request reads provider selection data, and every completed request updates usage counters. File-based storage causes race conditions under high concurrency, leading to lost usage counts, corrupted health status, and provider selection failures. This is the highest-impact change.

**Independent Test**: Can be fully tested by starting the service with Redis enabled, sending concurrent API requests to multiple providers, and verifying that all usage counts are accurately tracked and no provider state is lost.

**Acceptance Scenarios**:

1. **Given** the service is configured to use Redis, **When** provider pool configuration is loaded at startup, **Then** the service reads provider pools from Redis and operates normally.
2. **Given** multiple concurrent API requests are being processed, **When** each request updates provider usage counts, **Then** all updates are applied atomically and no counts are lost.
3. **Given** a provider encounters an error and is marked unhealthy, **When** another request simultaneously updates usage on a different provider, **Then** both updates are preserved correctly.
4. **Given** the UI adds a new provider account, **When** an API request is simultaneously updating provider health status, **Then** both operations succeed without conflict.

---

### User Story 2 - Token Credential Storage in Redis (Priority: P2)

As an API service operator, I want OAuth token credentials (Kiro, Gemini, Qwen, iFlow, Codex) stored in Redis instead of individual JSON files, so that token refresh operations don't lose data due to concurrent read-modify-write conflicts.

**Why this priority**: Token files are modified during OAuth refresh flows. When multiple requests trigger token refreshes simultaneously, the current file-based approach can overwrite one refresh result with stale data. This causes authentication failures and service disruptions. Fixing this is critical for reliability but affects fewer operations than provider pool state.

**Independent Test**: Can be tested by triggering multiple simultaneous token refresh operations for the same provider and verifying that all token updates are preserved and subsequent API calls authenticate successfully.

**Acceptance Scenarios**:

1. **Given** a Kiro provider token is stored in Redis, **When** the token expires and needs refresh, **Then** the new token is atomically updated in Redis without affecting other token fields.
2. **Given** two concurrent requests both trigger a token refresh for the same provider, **When** both refresh operations complete, **Then** the most recent valid token is stored and no data is corrupted.
3. **Given** token credentials for multiple provider types exist, **When** refreshing tokens for different providers simultaneously, **Then** each provider's tokens are updated independently without interference.

---

### User Story 3 - Main Configuration in Redis (Priority: P3)

As an API service operator, I want the main service configuration (`config.json` settings) stored in Redis, so that configuration changes from the web UI take effect immediately without file writes that block the event loop.

**Why this priority**: Main configuration changes are infrequent compared to provider pool and token updates, but synchronous file writes (`writeFileSync`) block the Node.js event loop, causing all in-flight requests to stall. Moving to Redis eliminates this blocking behavior.

**Independent Test**: Can be tested by modifying service configuration through the web UI while API requests are being processed, and verifying that configuration changes take effect without any request processing interruption.

**Acceptance Scenarios**:

1. **Given** a configuration change is made via the web UI, **When** the change is saved, **Then** it is stored in Redis without blocking concurrent API request processing.
2. **Given** the service is running with Redis-based configuration, **When** a configuration value is updated, **Then** subsequent API requests use the updated configuration.

---

### User Story 4 - Data Migration from Files to Redis (Priority: P2)

As an API service operator, I want a CLI migration command that imports my existing file-based configurations into Redis, so that I can transition to Redis storage without manually recreating all my provider accounts and settings. The migration is a deliberate manual step that the operator runs before starting the service with Redis enabled.

**Why this priority**: Without migration support, operators would need to manually re-enter all provider accounts, tokens, and settings. This is a critical adoption blocker that must be available when Redis storage is first deployed.

**Independent Test**: Can be tested by preparing a set of existing configuration files, running the CLI migration command, and verifying all data is correctly represented in Redis.

**Acceptance Scenarios**:

1. **Given** existing `provider_pools.json` with multiple provider accounts, **When** the migration tool is run, **Then** all provider accounts with their configurations are imported into Redis.
2. **Given** existing token files in provider directories (kiro, gemini, etc.), **When** the migration tool is run, **Then** all token credentials are imported into Redis.
3. **Given** an existing `config.json` with custom settings, **When** the migration tool is run, **Then** all configuration values are imported into Redis.
4. **Given** migration has been completed, **When** the service starts, **Then** it operates identically to before migration using the Redis-stored data.
5. **Given** Redis contains active configuration data, **When** the operator runs the CLI export command, **Then** all provider pools, tokens, and config settings are written back to standard config files for backup.
6. **Given** provider accounts have token file paths configured (e.g., `KIRO_OAUTH_CREDS_FILE_PATH`), **When** the migration tool imports tokens, **Then** each token is stored in Redis using the provider's UUID as the key (not the file name), enabling correct token lookup at runtime.

---

### User Story 5 - File-Based Fallback (Priority: P3)

As an API service operator, I want the service to fall back to file-based storage when Redis is unavailable, so that I can run the service in environments where Redis is not deployed or during Redis maintenance.

**Why this priority**: Not all deployments will have Redis available. Backward compatibility ensures the service can still run in simple single-instance setups using file-based storage.

**Independent Test**: Can be tested by starting the service without Redis configured and verifying it operates using file-based storage exactly as before.

**Acceptance Scenarios**:

1. **Given** Redis is not configured, **When** the service starts, **Then** it operates using file-based storage with the same behavior as the current system.
2. **Given** Redis was configured but becomes unavailable during operation, **When** the service detects Redis connection loss, **Then** it continues operating using in-memory cached state from the last successful Redis read, queues pending writes, and logs warnings.
3. **Given** Redis was unavailable and writes were queued, **When** Redis connectivity is restored, **Then** queued writes are replayed and the service resumes normal Redis-backed operation.

---

### User Story 6 - Session Token Storage in Redis (Priority: P2)

As an API service operator, I want web UI session tokens stored in Redis instead of `token-store.json`, so that session management works correctly across multiple service instances and eliminates file I/O for authentication.

**Why this priority**: Session tokens are checked on every authenticated UI request. File-based storage causes race conditions when multiple UI sessions exist and prevents horizontal scaling.

**Independent Test**: Log into the web UI on one service instance, verify the session is valid when routed to a different instance sharing the same Redis.

**Acceptance Scenarios**:

1. **Given** the service is configured with Redis, **When** a user logs into the web UI, **Then** the session token is stored in Redis with automatic TTL-based expiry.
2. **Given** multiple service instances share Redis, **When** a user authenticates on one instance, **Then** the session is valid on all instances.
3. **Given** a session token exists in Redis, **When** it expires, **Then** Redis automatically removes it (TTL-based expiry).

---

### User Story 7 - Usage Cache Storage in Redis (Priority: P3)

As an API service operator, I want provider usage cache stored in Redis instead of `usage-cache.json`, so that usage data is consistent across service instances and updated atomically.

**Why this priority**: Usage cache is read frequently by the UI to display provider status. File-based storage causes stale reads across instances.

**Independent Test**: Refresh usage data on one instance, verify the updated cache is visible from another instance.

**Acceptance Scenarios**:

1. **Given** Redis is configured, **When** provider usage is refreshed, **Then** the cache is stored in Redis.
2. **Given** multiple instances share Redis, **When** usage is updated, **Then** all instances see the same cached data.

---

### User Story 8 - Plugin Configuration Storage in Redis (Priority: P3)

As an API service operator, I want plugin configuration stored in Redis instead of `plugins.json`, so that plugin enable/disable state is consistent across instances.

**Why this priority**: Plugin state changes are infrequent but must be consistent across all service instances.

**Independent Test**: Enable/disable a plugin via UI, verify the state change is reflected on all instances.

**Acceptance Scenarios**:

1. **Given** Redis is configured, **When** a plugin is enabled/disabled via UI, **Then** the change is stored in Redis.
2. **Given** multiple instances share Redis, **When** plugin state changes, **Then** all instances reflect the change.

---

### User Story 9 - Redis Initialization CLI (Priority: P2)

As an API service operator, I want a CLI command to initialize Redis with default configuration, so that I can start fresh without needing existing config files (enabling removal of the configs volume mount).

**Why this priority**: Without initialization support, operators must maintain config files even when using Redis as primary storage.

**Independent Test**: Run init command against empty Redis, verify service starts and operates with default configuration.

**Acceptance Scenarios**:

1. **Given** an empty Redis, **When** the init command is run, **Then** default config, empty provider pools, and default plugin settings are created.
2. **Given** Redis already has data, **When** the init command is run without --force, **Then** it refuses to overwrite and warns the operator.
3. **Given** Redis is initialized, **When** the service starts, **Then** it operates normally without any local config files.

---

### Edge Cases

- What happens when Redis runs out of memory during a configuration write? The system should handle the error gracefully, log the failure, and retry or report the error to the caller.
- How does the system handle Redis connection timeout during a token refresh? The token refresh should fail with an appropriate error, and the system should attempt reconnection on the next operation.
- What happens if the migration tool is run twice on the same data? Migration must be idempotent — running it again should not duplicate or corrupt data.
- How does the system behave if Redis data is manually deleted while the service is running? The system should detect missing data and either re-initialize from defaults or report the error.
- What happens during a Redis failover in a replicated setup? The system continues serving from in-memory cache during the interruption, queues writes, and replays them after reconnection.
- How are configuration paths adjusted for different deployment environments (Docker vs bare metal)? Redis connection settings should be configurable via environment variables or the main configuration file, independent of file paths.
- What happens if the operator starts the service with Redis configured but hasn't run migration? The system should detect empty Redis and log a clear warning advising the operator to run the migration command, then fall back to file-based storage.

## Requirements

### Functional Requirements

- **FR-001**: System MUST store provider pool configurations (account lists, health status, usage counters, error tracking) in Redis when Redis is configured.
- **FR-002**: System MUST store OAuth token credentials for all provider types (Kiro, Gemini, Qwen, iFlow, Codex) in Redis when Redis is configured.
- **FR-003**: System MUST store main service configuration settings in Redis when Redis is configured.
- **FR-004**: System MUST support atomic updates to provider usage counters (increment operations must not lose counts under concurrent access).
- **FR-005**: System MUST support atomic updates to provider health status (marking providers healthy/unhealthy must not conflict with other state changes).
- **FR-006**: System MUST support atomic token credential updates (refreshing a token must not overwrite other credential fields).
- **FR-007**: System MUST provide a CLI migration command to import existing file-based configurations into Redis. Migration is not automatic; the operator must explicitly invoke it before starting the service with Redis enabled.
- **FR-007a**: System MUST provide a CLI export command to dump current Redis configuration state back to standard config files (provider_pools.json, config.json, token files) for backup and recovery purposes.
- **FR-007b**: Migration tool MUST resolve token file paths from provider pool configurations (e.g., `KIRO_OAUTH_CREDS_FILE_PATH`) and store tokens using the provider's UUID as the Redis key, ensuring runtime token lookup works correctly.
- **FR-008**: System MUST fall back to file-based storage when Redis is not configured.
- **FR-009**: System MUST allow Redis connection settings to be configured (host, port, password, database number).
- **FR-010**: System MUST log Redis connection status and errors for operational monitoring.
- **FR-011**: System MUST support configuration path adjustments for different deployment environments via environment variables or configuration settings.
- **FR-012**: Web UI provider management operations (add, update, delete providers) MUST use the same storage backend as runtime operations to prevent conflicts.
- **FR-013**: Migration tool MUST be idempotent (running it multiple times produces the same result without data duplication).
- **FR-014**: System MUST use a consistent key naming scheme in Redis to organize data by type (provider pools, tokens, configuration).
- **FR-015**: When Redis becomes unavailable during operation, the system MUST continue serving requests using in-memory cached state and queue pending writes for replay upon Redis recovery.
- **FR-016**: When Redis connectivity is restored after an outage, the system MUST replay queued writes and resume normal Redis-backed operation.
- **FR-017**: System MUST store web UI session tokens in Redis when Redis is configured, with automatic TTL-based expiry.
- **FR-018**: System MUST store provider usage cache in Redis when Redis is configured.
- **FR-019**: System MUST store plugin configuration in Redis when Redis is configured.
- **FR-020**: System MUST provide a CLI initialization command to create default configuration in Redis for fresh deployments without config files.

### Key Entities

- **Provider Pool**: A collection of provider accounts for a given provider type, containing account configurations, health status, usage counts, and error history.
- **Provider Account**: An individual provider account within a pool, identified by a unique ID, containing credentials, endpoint configuration, and runtime state.
- **Token Credential**: OAuth token data for a provider account, including access tokens, refresh tokens, and expiration metadata.
- **Service Configuration**: Global service settings including port, API key, default provider, proxy settings, and fallback chains.
- **Redis Connection**: The connection configuration for the Redis backend, including host, port, authentication, and database selection.

## Success Criteria

### Measurable Outcomes

- **SC-001**: The service handles at least 5x more concurrent API requests without configuration data loss compared to file-based storage.
- **SC-002**: Provider usage counter accuracy is 100% under concurrent load (no lost increments).
- **SC-003**: Configuration updates from the web UI do not cause any measurable delay to concurrent API request processing.
- **SC-004**: Token refresh operations complete without data corruption when multiple refreshes occur simultaneously for the same provider.
- **SC-005**: Migration from file-based to Redis storage completes with 100% data fidelity (all accounts, tokens, and settings preserved).
- **SC-006**: The service starts and operates correctly in file-only mode when Redis is not configured, maintaining full backward compatibility.
- **SC-007**: The system supports horizontal scaling with multiple service instances sharing the same Redis backend without data conflicts.

## Assumptions

- Redis 6.0 or later is available in the deployment environment when Redis storage is desired.
- The existing configuration file formats (`provider_pools.json`, `config.json`, token JSON files) remain the source of truth for migration.
- A single Redis instance (non-clustered) is sufficient for the expected configuration data volume.
- Redis persistence (RDB or AOF) is configured by the operator to prevent data loss on Redis restart.
- The Docker deployment environment will include a Redis service in the docker-compose configuration.
- Configuration data volume is small enough (typically under 1MB total) that Redis memory usage is negligible.
- The existing web UI frontend does not need changes; only backend storage operations are modified.
