# Implementation Plan: Redis-Based Configuration Storage

**Branch**: `001-redis-config` | **Date**: 2026-01-25 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/001-redis-config/spec.md`

## Summary

Replace file-based configuration storage with Redis to improve concurrency handling and eliminate race conditions in provider pool state management. The system will use Redis for atomic updates to usage counters, health status, and token credentials, while maintaining backward compatibility with file-based storage when Redis is unavailable.

## Technical Context

**Language/Version**: Node.js ES Modules (ES2022+, `"type": "module"`)
**Primary Dependencies**: ioredis (Redis client), existing: axios, uuid, google-auth-library
**Storage**: Redis 6.0+ (primary), file-based fallback (configs/*.json)
**Testing**: Jest with babel-jest for ESM transform, 30s timeout
**Target Platform**: Linux server (Docker), bare metal Node.js
**Project Type**: Single server application with web UI backend
**Performance Goals**: 5x concurrent request improvement, 100% counter accuracy, <10ms Redis operations
**Constraints**: Graceful degradation when Redis unavailable, no data loss during failover, idempotent migration
**Scale/Scope**: Single Redis instance, <1MB config data, multiple service instances sharing Redis

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

**Status**: The project constitution (`constitution.md`) is a template without specific project constraints defined. No gates require enforcement.

**General Principles Applied**:
- Maintain backward compatibility (file-based fallback)
- Test-first approach for critical atomic operations
- Minimal changes to existing interfaces (adapter pattern)

## Project Structure

### Documentation (this feature)

```text
specs/001-redis-config/
├── plan.md              # This file
├── research.md          # Phase 0 output - Redis patterns research
├── data-model.md        # Phase 1 output - Redis key schema
├── quickstart.md        # Phase 1 output - Developer guide
├── contracts/           # Phase 1 output - API contracts
│   └── redis-storage.yaml
└── tasks.md             # Phase 2 output (created by /speckit.tasks)
```

### Source Code (repository root)

```text
src/
├── core/
│   ├── config-manager.js         # MODIFY: Add Redis storage option
│   ├── redis-client.js           # NEW: Redis connection manager
│   └── redis-config-manager.js   # NEW: Redis config storage adapter
├── providers/
│   └── provider-pool-manager.js  # MODIFY: Use storage adapter interface
├── services/
│   └── service-manager.js        # MODIFY: Initialize Redis on startup
└── cli/
    ├── migrate-to-redis.js       # NEW: CLI migration tool
    └── export-from-redis.js      # NEW: CLI export tool

tests/
├── unit/
│   ├── redis-config-manager.test.js  # NEW: Unit tests with mocked Redis
│   └── redis-client.test.js          # NEW: Connection handling tests
└── integration/
    └── redis-storage.test.js         # NEW: Integration with real Redis

docker/
├── docker-compose.yml            # MODIFY: Add Redis service
└── docker-compose.build.yml      # MODIFY: Add Redis service
```

**Structure Decision**: Single project structure maintained. New files added to `src/core/` for Redis management, `src/cli/` for migration tools. Existing files modified via adapter pattern to support both storage backends.

## Complexity Tracking

> No constitution violations requiring justification. The feature adds complexity proportional to the problem:
> - Redis client: Required for the core feature
> - Fallback logic: Required by FR-008, FR-015, FR-016
> - Migration CLI: Required by FR-007, FR-007a

| Component | Justification |
|-----------|---------------|
| Storage adapter interface | Enables backend switching without changing consumers |
| Write queue with retry | Handles Redis unavailability per FR-015/FR-016 |
| CLI tools | Operator-controlled migration per clarifications |
