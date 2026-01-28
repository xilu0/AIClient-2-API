<!--
  Sync Impact Report
  ===================
  Version change: 0.0.0 → 1.0.0 (MAJOR: Initial constitution adoption)

  Modified principles: N/A (initial creation)

  Added sections:
  - Core Principles (5 principles)
  - Performance & Concurrency Standards
  - Development Workflow
  - Governance

  Removed sections: N/A

  Templates requiring updates:
  - .specify/templates/plan-template.md ✅ (compatible - Constitution Check section exists)
  - .specify/templates/spec-template.md ✅ (compatible - no changes needed)
  - .specify/templates/tasks-template.md ✅ (compatible - test discipline supported)

  Follow-up TODOs: None
-->

# AIClient-2-API Constitution

## Core Principles

### I. Multi-Protocol Compatibility

All features MUST maintain compatibility with OpenAI, Claude, and Gemini protocols. Protocol converters MUST be bidirectional and lossless for supported message types. Breaking protocol compatibility requires explicit justification and migration path.

**Rationale**: The project's core value is unified API access across multiple LLM providers. Protocol compatibility is non-negotiable.

### II. High Availability Architecture

Services MUST implement automatic failover, health checking, and graceful degradation. Account pool management MUST use round-robin selection with health-aware routing. Single points of failure MUST be eliminated through redundancy or fallback chains.

**Rationale**: The project targets 99.9% availability through intelligent account pooling and automatic recovery.

### III. Performance-First Concurrency

Concurrent operations MUST use lock-free patterns where possible (atomic Redis operations, singleflight). Mutex locks MUST NOT be used in hot paths serving user requests. Redis operations MUST use connection pooling with pipelining. HTTP clients MUST reuse connections via keep-alive.

**Rationale**: Supporting 500+ concurrent streaming connections requires avoiding bottlenecks in account selection, token refresh, and upstream calls.

### IV. Testing Discipline

Integration tests MUST cover all API endpoints and protocol conversions. Unit tests MUST cover business logic including token distribution, health checking, and failover. Test coverage target: 90%+ for core modules. Tests MAY be written after implementation for existing code, but new features SHOULD include tests.

**Rationale**: High test coverage ensures reliability across the complex multi-protocol conversion layer.

### V. Modular Extensibility

New providers MUST be added via the Adapter pattern without modifying core routing. Protocol conversion MUST use the Strategy pattern via ConverterFactory. Configuration MUST be injectable without code changes. Redis data structures MUST be backward-compatible.

**Rationale**: The project frequently adds new providers; modularity reduces integration effort and risk.

## Performance & Concurrency Standards

### Redis Operations

- MUST use atomic primitives (INCR, HSET, WATCH/MULTI) with optimistic retry
- MUST NOT use Lua scripts unless absolutely necessary for atomicity
- MUST use connection pooling with configurable pool size
- MUST implement pipelining for batch operations

### HTTP Connections

- Upstream API calls MUST use pooled connections with keep-alive
- Connection limits MUST be configurable per provider
- Timeouts MUST be enforced for all external calls

### Streaming

- SSE responses MUST NOT buffer entire payloads
- Goroutines/workers MUST have bounded concurrency limits
- Resource cleanup MUST occur on connection interruption

## Development Workflow

### Code Quality Gates

1. All tests MUST pass before merge
2. Linting MUST pass (ESLint for JS, golangci-lint for Go)
3. No new security vulnerabilities in dependencies
4. Documentation MUST be updated for API changes

### Breaking Change Policy

- Protocol-level changes require MAJOR version bump
- Redis schema changes MUST include migration path
- Configuration changes SHOULD be backward-compatible

### Review Requirements

- Core modules (converters, pool manager, Redis client): 1 approval minimum
- New providers: Can be self-merged with tests passing
- Security-related changes: Team lead approval required

## Governance

This constitution supersedes informal practices. Amendments require:

1. Written proposal with rationale
2. Impact assessment on existing features
3. Migration plan if breaking existing behavior
4. Update to this document with version increment

All implementation plans MUST verify compliance via Constitution Check before proceeding. Complexity beyond these principles MUST be justified in writing.

Use `CLAUDE.md` for runtime development guidance and project-specific technical details.

**Version**: 1.0.0 | **Ratified**: 2026-01-27 | **Last Amended**: 2026-01-27
