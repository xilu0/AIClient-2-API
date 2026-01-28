# Tasks: Go-based High-Concurrency Kiro Messages Endpoint

**Input**: Design documents from `/specs/001-go-kiro-messages/`
**Prerequisites**: plan.md ✅, spec.md ✅, research.md ✅, data-model.md ✅, contracts/ ✅

**Tests**: Unit tests, integration tests, and benchmark tests are included as per constitution requirement (Testing Discipline principle) and spec.md success criteria (SC-001 through SC-009).

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

Go code resides at repository root (alongside Node.js in `src/`):
- **Source**: `cmd/`, `internal/`, `pkg/`
- **Tests**: `tests/unit/`, `tests/integration/`, `tests/benchmark/`
- **Deployment**: `docker/` (docker-compose, nginx)
- **Module**: `go.mod` at repo root for IDE support

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and Go module structure

- [x] T001 Create directory structure per plan.md (`cmd/`, `internal/`, `pkg/`, `tests/`)
- [x] T002 Initialize Go module with `go mod init` in `go.mod`
- [x] T003 [P] Add core dependencies (go-redis/redis/v9, singleflight, testify) in `go.mod`
- [x] T004 [P] Configure golangci-lint with `.golangci.yml` in `.golangci.yml`
- [x] T005 [P] Create Dockerfile for Go service in `Dockerfile.go`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

### Configuration & Entry Point

- [x] T006 Implement configuration loading from env/flags in `internal/config/config.go`
- [x] T007 Create server entry point with graceful shutdown in `cmd/kiro-server/main.go`

### Redis Infrastructure

- [x] T008 Implement Redis client with connection pooling in `internal/redis/client.go`
- [x] T009 Define Account and Token structs matching Node.js JSON in `internal/redis/types.go`
- [x] T010 Implement provider pool operations (HGETALL, HGET, HSET) in `internal/redis/pool.go`
- [x] T011 Implement token operations (GET, SET) in `internal/redis/tokens.go`

### Middleware Infrastructure

- [x] T012 [P] Implement API key authentication middleware in `pkg/middleware/auth.go`
- [x] T013 [P] Implement structured JSON logging middleware in `pkg/middleware/logging.go`

### Data Types

- [x] T014 Define Claude API request/response types (MessageRequest, MessageResponse, Usage) in `internal/claude/types.go`
- [x] T015 Define Kiro API types (KiroChunk, AWSEventMessage) in `internal/kiro/types.go`
- [x] T016 Define error response types matching Claude API format in `internal/claude/errors.go`

### Unit Tests for Foundational

- [x] T017 [P] Unit test for config loading in `tests/unit/config_test.go`
- [x] T018 [P] Unit test for Redis client connection in `tests/unit/redis_client_test.go`
- [x] T018a [P] [US4] Implement Redis disconnect detection with in-memory cache fallback in `internal/redis/client.go`

**Checkpoint**: Foundation ready - user story implementation can now begin in parallel

---

## Phase 3: User Story 1 - Streaming Messages Endpoint (Priority: P1) 🎯 MVP

**Goal**: API consumers can send streaming requests and receive SSE-formatted responses with content deltas and usage information

**Independent Test**: Send POST to `/v1/messages` with `stream: true`, verify SSE events (message_start, content_block_delta, message_stop)

### Unit Tests for User Story 1

- [x] T019 [P] [US1] Unit test for AWS event stream parser in `tests/unit/eventstream_test.go`
- [x] T020 [P] [US1] Unit test for token distribution (1:2:25 ratio) in `tests/unit/usage_test.go`
- [x] T021 [P] [US1] Unit test for SSE event writer in `tests/unit/sse_test.go`
- [x] T022 [P] [US1] Unit test for Kiro→Claude format conversion in `tests/unit/converter_test.go`

### Implementation for User Story 1

- [x] T023 [P] [US1] Implement AWS event stream binary parser in `internal/kiro/eventstream.go`
- [x] T024 [P] [US1] Implement 1:2:25 token distribution algorithm in `internal/claude/usage.go`
- [x] T025 [P] [US1] Implement SSE event writer with flush in `internal/claude/sse.go`
- [x] T026 [US1] Implement Kiro→Claude format converter in `internal/claude/converter.go`
- [x] T027 [US1] Implement HTTP client for Kiro API with connection pooling in `internal/kiro/client.go`
- [x] T028 [US1] Implement lock-free round-robin account selector in `internal/account/selector.go`
- [x] T029 [US1] Implement streaming messages handler in `internal/handler/messages.go`
- [x] T030 [US1] Wire up routes and middleware in `cmd/kiro-server/main.go`

### Integration Test for User Story 1

- [x] T031 [US1] Integration test for streaming messages endpoint in `tests/integration/messages_stream_test.go`

**Checkpoint**: Streaming endpoint works - can send requests and receive SSE responses

---

## Phase 4: User Story 2 - Transparent Failover (Priority: P1)

**Goal**: System automatically switches to healthy accounts when one becomes rate-limited (429) or returns auth errors (403)

**Independent Test**: Mark one account unhealthy, verify requests route to healthy accounts; verify 3-retry failover logic

### Unit Tests for User Story 2

- [x] T032 [P] [US2] Unit test for health eligibility check (60s cooldown) in `tests/unit/health_test.go`
- [x] T033 [P] [US2] Unit test for account selector with unhealthy filtering in `tests/unit/selector_test.go`

### Implementation for User Story 2

- [x] T034 [P] [US2] Implement health tracking and cooldown logic in `internal/account/health.go`
- [x] T035 [US2] Update account selector to filter unhealthy accounts (respecting cooldown) in `internal/account/selector.go`
- [x] T036 [US2] Implement retry loop with up to 3 credential switches in `internal/handler/messages.go`
- [x] T037 [US2] Update Redis pool operations for health status updates in `internal/redis/pool.go`
- [x] T038 [US2] Add "no healthy accounts" error response (503) in `internal/handler/messages.go`

### Integration Test for User Story 2

- [x] T039 [US2] Integration test for failover scenarios in `tests/integration/failover_test.go`

**Checkpoint**: Failover works - requests automatically retry with different accounts on 429/403

---

## Phase 5: User Story 3 - Non-Streaming Support (Priority: P2)

**Goal**: API consumers can send non-streaming requests and receive complete JSON responses

**Independent Test**: Send POST to `/v1/messages` with `stream: false`, verify complete JSON response with all content and usage

### Unit Tests for User Story 3

- [x] T040 [P] [US3] Unit test for response aggregation from Kiro chunks in `tests/unit/aggregator_test.go`

### Implementation for User Story 3

- [x] T041 [US3] Implement response aggregator to collect all chunks in `internal/claude/aggregator.go`
- [x] T042 [US3] Add non-streaming branch to messages handler in `internal/handler/messages.go`
- [x] T043 [US3] Ensure proper Content-Type header for JSON response in `internal/handler/messages.go`

### Integration Test for User Story 3

- [x] T044 [US3] Integration test for non-streaming endpoint in `tests/integration/messages_json_test.go`

**Checkpoint**: Non-streaming works - can receive complete JSON responses

---

## Phase 6: User Story 4 - Node.js Coexistence (Priority: P2)

**Goal**: Go and Node.js services share Redis state; changes in one are visible to the other

**Independent Test**: Run both services, mark account unhealthy in Go, verify Node.js sees updated status

### Implementation for User Story 4

- [x] T045 [P] [US4] Update docker-compose.yml to include Go service in `docker/docker-compose.yml`
- [x] T046 [P] [US4] Create nginx reverse proxy config routing `/claude-kiro-oauth/*` to Go in `docker/nginx.conf`
- [x] T047 [US4] Verify Redis key format compatibility (RFC3339 timestamps) in `internal/redis/pool.go`
- [x] T048 [US4] Add health endpoint returning Redis and account status in `internal/handler/health.go`

### Integration Test for User Story 4

- [x] T049 [US4] Integration test for Redis data sharing with Node.js format in `tests/integration/redis_compat_test.go`

**Checkpoint**: Coexistence works - both services share and update Redis state correctly

---

## Phase 7: User Story 5 - Background Token Refresh (Priority: P3)

**Goal**: Tokens refresh asynchronously without blocking requests; concurrent refresh requests are deduplicated

**Independent Test**: Set token near expiry, verify requests proceed while refresh happens; verify singleflight deduplication

### Unit Tests for User Story 5

- [x] T050 [P] [US5] Unit test for token expiry detection in `tests/unit/refresh_test.go`
- [x] T051 [P] [US5] Unit test for singleflight deduplication in `tests/unit/refresh_test.go`

### Implementation for User Story 5

- [x] T052 [US5] Implement token expiry check (5-minute threshold) in `internal/account/refresh.go`
- [x] T053 [US5] Implement background refresh with singleflight pattern in `internal/account/refresh.go`
- [x] T054 [US5] Integrate refresh trigger into account selection flow in `internal/account/selector.go`
- [x] T055 [US5] Add atomic token update in Redis with optimistic locking in `internal/redis/tokens.go`

### Integration Test for User Story 5

- [x] T056 [US5] Integration test for background refresh behavior in `tests/integration/refresh_test.go`

**Checkpoint**: Token refresh works - refreshes happen in background without blocking requests

---

## Phase 8: Performance Validation & Polish

**Purpose**: Verify performance targets and cross-cutting improvements

### Benchmark Tests

- [x] T057 [P] Benchmark test for 500+ concurrent connections in `tests/benchmark/concurrent_test.go`
- [x] T058 [P] Benchmark test for Redis operation latency (<10ms) in `tests/benchmark/redis_test.go`
- [x] T059 [P] Memory stability test under sustained load in `tests/benchmark/memory_test.go`

### Polish & Documentation

- [x] T060 [P] Add request validation with descriptive error messages in `internal/handler/messages.go`
- [x] T061 [P] Add context-based timeout for upstream Kiro API calls in `internal/kiro/client.go`
- [x] T062 [P] Update README with Go service build/run instructions in `README-go.md`
- [x] T063 Run full test suite and verify all success criteria
- [x] T064 Validate quickstart.md instructions work end-to-end

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phases 3-7)**: All depend on Foundational phase completion
  - US1 & US2 are both P1 and can proceed in parallel
  - US3 & US4 are P2, can start after Foundational
  - US5 is P3, can start after Foundational
- **Polish (Phase 8)**: Depends on all user stories being complete

### User Story Dependencies

| Story | Priority | Dependencies | Can Parallelize With |
|-------|----------|--------------|----------------------|
| US1 - Streaming | P1 | Foundational only | US2 (different files) |
| US2 - Failover | P1 | Foundational only | US1 (different files initially) |
| US3 - Non-Stream | P2 | US1 (reuses converter) | US4 |
| US4 - Coexistence | P2 | Foundational only | US3, US5 |
| US5 - Token Refresh | P3 | Foundational only | US3, US4 |

### Within Each User Story

1. Unit tests FIRST (marked [P] within story)
2. Implementation tasks (some parallelizable)
3. Integration test LAST (validates story works)

### Parallel Opportunities by Phase

**Phase 1 (Setup)**: T003, T004, T005 can all run in parallel

**Phase 2 (Foundational)**:
- T012, T013 can run in parallel (middleware)
- T017, T018, T018a can run in parallel (unit tests and Redis resilience)

**Phase 3 (US1)**:
- T019-T022 can run in parallel (unit tests)
- T023-T025 can run in parallel (parsers/writers)

**Phase 4 (US2)**: T032, T033 can run in parallel (unit tests)

**Phase 8 (Polish)**: T057-T062 can all run in parallel

---

## Parallel Example: User Story 1

```bash
# Step 1: Launch all unit tests in parallel (all marked [P])
Task: T019 "Unit test for AWS event stream parser"
Task: T020 "Unit test for token distribution (1:2:25)"
Task: T021 "Unit test for SSE event writer"
Task: T022 "Unit test for Kiro→Claude format conversion"

# Step 2: Launch parallelizable implementation tasks
Task: T023 "Implement AWS event stream binary parser"
Task: T024 "Implement 1:2:25 token distribution"
Task: T025 "Implement SSE event writer with flush"

# Step 3: Sequential implementation (has dependencies)
Task: T026 "Implement Kiro→Claude format converter" # depends on T023
Task: T027 "Implement HTTP client for Kiro API"
Task: T028 "Implement lock-free round-robin selector"
Task: T029 "Implement streaming messages handler" # depends on T024-T028
Task: T030 "Wire up routes and middleware"

# Step 4: Integration test
Task: T031 "Integration test for streaming endpoint"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (T001-T005)
2. Complete Phase 2: Foundational (T006-T018)
3. Complete Phase 3: User Story 1 (T019-T031)
4. **STOP and VALIDATE**: Test streaming endpoint independently
5. Deploy/demo if ready - this delivers the core 500+ concurrent streaming capability

### Incremental Delivery

1. Setup + Foundational → Foundation ready
2. Add US1 (Streaming) → Test → Deploy (MVP!)
3. Add US2 (Failover) → Test → Deploy (Production-ready)
4. Add US3 (Non-Stream) → Test → Deploy (Full compatibility)
5. Add US4 (Coexistence) → Test → Deploy (Strangler Fig complete)
6. Add US5 (Token Refresh) → Test → Deploy (Long-running stability)
7. Polish (Benchmarks) → Validate performance targets

### Suggested MVP Scope

**Minimum Viable Product**: Complete through Phase 4 (US1 + US2)
- Streaming endpoint with 500+ concurrent connections ✅
- Automatic failover for high availability ✅
- This delivers the primary performance goal stated in the spec

---

## Notes

- All file paths are relative to repository root
- [P] tasks touch different files with no dependencies - safe to parallelize
- [USn] labels map tasks to specific user stories for traceability
- Constitution compliance verified: lock-free Redis (INCR), singleflight refresh, connection pooling
- Redis key format must match Node.js exactly (RFC3339 timestamps, same JSON field names)
- 1:2:25 token distribution MUST match Node.js implementation (merge-protected per CLAUDE.md)
