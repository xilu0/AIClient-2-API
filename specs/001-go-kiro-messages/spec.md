# Feature Specification: Go-based High-Concurrency Kiro Messages Endpoint

**Feature Branch**: `001-go-kiro-messages`
**Created**: 2026-01-27
**Status**: Draft
**Input**: User description: "目前本项目性能问题严重，我想使用绞杀者模式配合go重构本服务，先重构/claude-kiro-oauth/v1/messages接口功能，前端和帐号管理功能仍然使用nodejs版本，新的go程序就放在本项目中和js不冲突，基于redis共享帐号，使用量，认证信息，先把/claude-kiro-oauth/v1/messages接口优化，希望达到500以上并发，场景是大模型api接口，see模式的请求。"

## Overview

This feature implements a Strangler Fig pattern migration to gradually replace the Node.js `/claude-kiro-oauth/v1/messages` endpoint with a high-performance Go implementation. The Go service will handle only the messages endpoint while sharing account pools, usage tracking, and authentication data with the existing Node.js application via Redis.

## Clarifications

### Session 2026-01-27

- Q: How should traffic be routed between Go and Node.js services? → A: Reverse proxy (nginx/traefik) routes `/claude-kiro-oauth/*` to Go service
- Q: How should unhealthy accounts recover? → A: Passive recovery - unhealthy accounts retried after cooldown period on next request
- Q: What is the health recovery cooldown duration? → A: 60 seconds
- Q: What logging format should be used? → A: Structured JSON for log aggregation tool compatibility
- Q: How many credential switch retries before failing? → A: 3 attempts maximum
- Q: What concurrency strategy for account selection? → A: Atomic Redis counter (INCR) for lock-free round-robin, avoiding mutex bottlenecks
- Q: How to prevent token refresh races? → A: In-process singleflight pattern to coalesce concurrent refresh requests per account
- Q: What Redis connection strategy for high concurrency? → A: Connection pool with pipelining for parallel connections and batched commands
- Q: How to handle atomic Redis operations? → A: Redis primitives (INCR, HSET, WATCH/MULTI) with optimistic retry, avoiding Lua script complexity
- Q: What HTTP client strategy for upstream Kiro API? → A: Pooled connections with keep-alive and configurable limits to maximize reuse

## User Scenarios & Testing *(mandatory)*

### User Story 1 - API Consumer Uses Streaming Messages Endpoint (Priority: P1)

An API consumer (application like Cherry-Studio, NextChat, or Cline) sends a streaming request to `/claude-kiro-oauth/v1/messages` to interact with Claude models via Kiro OAuth. The request should be handled with low latency and high concurrency support.

**Why this priority**: This is the core functionality - without this working, the feature delivers no value. The primary use case is streaming responses for LLM interactions (SSE mode).

**Independent Test**: Can be tested by sending a streaming request to the endpoint and receiving proper SSE-formatted responses with content deltas and usage information.

**Acceptance Scenarios**:

1. **Given** a valid API key and properly formatted Claude messages request, **When** the consumer sends a POST to `/claude-kiro-oauth/v1/messages` with `stream: true`, **Then** the system returns SSE events including `message_start`, `content_block_delta`, and `message_stop` events with proper formatting.

2. **Given** a valid request with "thinking" mode enabled, **When** the consumer sends the request, **Then** the system extracts thinking blocks and streams them as separate `thinking_delta` events before content deltas.

3. **Given** 500 concurrent streaming requests, **When** all requests are sent simultaneously, **Then** all requests receive responses without timeouts or errors within acceptable latency thresholds.

---

### User Story 2 - Transparent Failover Between Accounts (Priority: P1)

When a Kiro OAuth account becomes rate-limited or unhealthy, the system automatically switches to another healthy account from the pool without the API consumer noticing any interruption.

**Why this priority**: Multi-account failover is essential for achieving the 500+ concurrency target - without it, a single account's rate limits would bottleneck the entire system.

**Independent Test**: Can be tested by marking one account unhealthy and verifying requests are routed to healthy accounts.

**Acceptance Scenarios**:

1. **Given** multiple Kiro OAuth accounts in the pool with one becoming rate-limited (429), **When** a request is assigned to the rate-limited account, **Then** the system marks it unhealthy and retries with a different healthy account transparently.

2. **Given** all accounts in the pool are unhealthy, **When** a request arrives, **Then** the system returns a clear error indicating no healthy accounts are available.

3. **Given** an account was marked unhealthy and the cooldown period has elapsed, **When** the next request arrives and this account is selected for retry, **Then** if the request succeeds, the account is marked healthy again and included in normal rotation.

---

### User Story 3 - Non-Streaming Request Support (Priority: P2)

An API consumer sends a non-streaming request and receives the complete response in a single JSON payload.

**Why this priority**: While streaming is the primary use case, non-streaming support is needed for backward compatibility with some clients.

**Independent Test**: Can be tested by sending a request with `stream: false` and receiving a complete JSON response.

**Acceptance Scenarios**:

1. **Given** a valid request with `stream: false`, **When** the consumer sends the request, **Then** the system returns a complete JSON response with all content and usage information.

2. **Given** a non-streaming request exceeding token limits, **When** the request is processed, **Then** the system returns an appropriate error response with error type and message.

---

### User Story 4 - Seamless Coexistence with Node.js Service (Priority: P2)

The Go service runs alongside the existing Node.js application, sharing Redis for account pools and usage data, with traffic routing configurable between the two implementations.

**Why this priority**: The Strangler Fig pattern requires both services to coexist during migration. This enables gradual rollout and easy rollback.

**Independent Test**: Can be tested by running both services and verifying they share the same account pool state via Redis.

**Acceptance Scenarios**:

1. **Given** both Go and Node.js services are running, **When** the Go service marks an account unhealthy, **Then** the Node.js service sees the updated health status in Redis.

2. **Given** the Go service is handling traffic, **When** an account's usage count is incremented, **Then** the Node.js admin UI displays the updated usage statistics.

3. **Given** a need to rollback, **When** traffic is redirected back to Node.js, **Then** all requests continue to work without data loss or inconsistency.

---

### User Story 5 - Token Refresh Without Request Blocking (Priority: P3)

When an OAuth token is about to expire, the system refreshes it asynchronously without blocking or delaying in-flight requests.

**Why this priority**: Token refresh is important for long-running availability but is not part of the core request path.

**Independent Test**: Can be tested by setting a token near expiration and verifying requests continue while refresh happens in background.

**Acceptance Scenarios**:

1. **Given** a token expiring within the refresh threshold (e.g., 5 minutes), **When** a request uses that account, **Then** the token is refreshed in the background while the current request proceeds with the existing valid token.

2. **Given** multiple concurrent requests for an account with an expiring token, **Then** only one refresh operation is triggered (deduplication), not one per request.

---

### Edge Cases

- What happens when Redis is temporarily unavailable? System should use cached account data and queue writes for replay.
- How does the system handle malformed requests? Return appropriate 4xx error responses with descriptive error messages.
- What happens when the upstream Kiro API returns unexpected response formats? Log the anomaly and return a graceful error to the client.
- How does the system handle requests with extremely large context windows? Respect upstream limits and return appropriate error responses.
- What happens when a streaming connection is interrupted mid-response? Clean up resources and log the interruption without affecting other connections.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST accept POST requests at `/claude-kiro-oauth/v1/messages` with Claude-compatible message format
- **FR-002**: System MUST authenticate requests using the same API key validation as the Node.js service (via shared config/Redis)
- **FR-003**: System MUST support streaming responses using Server-Sent Events (SSE) format compatible with Claude API
- **FR-004**: System MUST support non-streaming responses returning complete JSON payloads
- **FR-005**: System MUST select accounts from the provider pool using lock-free round-robin via atomic Redis counter (INCR), avoiding mutex bottlenecks under high concurrency
- **FR-006**: System MUST track account usage counts and error counts using Redis primitives (INCR, HSET) with optimistic retry on conflicts, avoiding Lua script complexity
- **FR-006a**: System MUST use Redis connection pooling with pipelining for high-throughput concurrent access
- **FR-007**: System MUST mark accounts as unhealthy after receiving rate-limit (429) or authorization (403) errors
- **FR-007a**: System MUST use passive health recovery - unhealthy accounts become eligible for retry after 60 seconds cooldown, and are marked healthy upon successful request
- **FR-008**: System MUST retry failed requests with different healthy accounts, up to 3 credential switch attempts before returning an error
- **FR-009**: System MUST refresh OAuth tokens before expiration without blocking requests, using singleflight pattern to coalesce concurrent refresh attempts for the same account
- **FR-010**: System MUST use HTTP connection pooling with keep-alive for upstream Kiro API calls, with configurable connection limits to prevent exhaustion
- **FR-010a**: System MUST parse AWS event stream binary format from Kiro API responses (see [AWS Event Stream Encoding](https://docs.aws.amazon.com/transcribe/latest/dg/streaming-format.html) and `data-model.md` section 7 for struct definition)
- **FR-011**: System MUST convert Kiro API responses to Claude-compatible SSE event format
- **FR-012**: System MUST extract and stream "thinking" blocks as separate events when thinking mode is enabled
- **FR-013**: System MUST calculate and report token usage with the 1:2:25 distribution ratio for cache tokens
- **FR-014**: System MUST coexist with the Node.js service, sharing Redis data without conflicts
- **FR-015**: System MUST run as a separate process with its own port, with traffic routed via reverse proxy (nginx/traefik) for `/claude-kiro-oauth/*` paths
- **FR-016**: System MUST support graceful shutdown, completing in-flight requests before terminating
- **FR-017**: System MUST log request/response metadata in structured JSON format for debugging and log aggregation tool compatibility

### Key Entities

- **Account**: Represents a Kiro OAuth credential with access token, refresh token, expiration time, region, profile ARN, health status, usage count, and error count
- **Provider Pool**: Collection of accounts for a provider type with round-robin selection and health tracking
- **Message Request**: Claude-compatible request with messages array, model selection, streaming flag, and optional parameters
- **SSE Event**: Server-sent event with type (message_start, content_block_delta, etc.) and data payload
- **Usage Metrics**: Token counts including input tokens, output tokens, cache creation tokens, and cache read tokens

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: System handles 500+ concurrent streaming connections without request failures or timeouts
- **SC-002**: Median time-to-first-byte (TTFB) is under 500ms for streaming requests, measured from TCP connection established to first SSE `message_start` event received by client
- **SC-003**: 99th percentile TTFB is under 2 seconds for streaming requests, using the same measurement as SC-002
- **SC-004**: Memory usage remains stable under sustained high load (no memory leaks)
- **SC-005**: Account selection and health updates complete within 10ms (Redis operations)
- **SC-006**: Zero data inconsistency between Go and Node.js services when accessing shared Redis data
- **SC-007**: System recovers gracefully within 30 seconds when Redis connection is restored after outage
- **SC-008**: Token refresh completes without blocking any user requests
- **SC-009**: 100% compatibility with existing Claude API client libraries (Cherry-Studio, NextChat, Cline)

## Assumptions

- Redis 7.x is available and configured as the primary data store for account pools (as per current architecture)
- The existing Node.js service continues to handle admin UI, account management, and other endpoints
- Traffic routing uses a reverse proxy (nginx/traefik) to route `/claude-kiro-oauth/*` requests to the Go service while other paths continue to Node.js
- Kiro API endpoint format and AWS event stream protocol remain stable
- OAuth token refresh endpoint and format remain unchanged
- The existing Redis key structure (`aiclient:provider_pools:*`, `aiclient:kiro:tokens:*`) is preserved

## Out of Scope

- Rewriting the admin UI or account management endpoints
- Migrating other provider types (Gemini, OpenAI, etc.) - only Kiro OAuth in this phase
- Changes to the existing Node.js codebase beyond minimal integration hooks
- Database migration or schema changes beyond Redis key compatibility
- Load balancing or auto-scaling infrastructure - assumed to be handled externally
- Metrics/monitoring infrastructure - basic logging only in initial implementation
