# Data Model: Redis Key Schema

**Feature**: 001-redis-config | **Date**: 2026-01-25

## Overview

This document defines the Redis key structure for storing AIClient-2-API configuration data. All keys use the `aiclient:` prefix by default (configurable via `keyPrefix` setting).

## Key Naming Convention

```
{prefix}:{category}:{type}:{identifier}
```

- **prefix**: `aiclient` (configurable)
- **category**: `config`, `pools`, `tokens`, `meta`
- **type**: Provider type (e.g., `claude-kiro-oauth`, `gemini-cli-oauth`)
- **identifier**: UUID or specific identifier

## Key Definitions

### 1. Service Configuration

**Key**: `aiclient:config`
**Type**: String (JSON)
**TTL**: None (persistent)

```json
{
  "REQUIRED_API_KEY": "string",
  "SERVER_PORT": 3000,
  "HOST": "0.0.0.0",
  "MODEL_PROVIDER": "gemini-cli-oauth",
  "SYSTEM_PROMPT_FILE_PATH": "system_prompt.txt",
  "PROMPT_LOG_MODE": "none",
  "REQUEST_MAX_RETRIES": 3,
  "providerFallbackChain": {
    "claude-kiro-oauth": ["gemini-cli-oauth"]
  },
  "modelFallbackMapping": {
    "claude-3.5-sonnet": {
      "targetProvider": "gemini-cli-oauth",
      "targetModel": "gemini-2.0-flash"
    }
  }
}
```

**Operations**:
- `GET aiclient:config` - Read full config
- `SET aiclient:config {json}` - Write full config

---

### 2. Provider Pools

**Key Pattern**: `aiclient:pools:{providerType}`
**Type**: Hash
**TTL**: None (persistent)

**Hash Structure**:
| Field | Value |
|-------|-------|
| `{uuid}` | JSON string of provider object |

**Provider Object Schema**:
```json
{
  "uuid": "550e8400-e29b-41d4-a716-446655440000",
  "customName": "Primary Kiro Account",
  "isHealthy": true,
  "isDisabled": false,
  "usageCount": 1234,
  "errorCount": 5,
  "lastUsed": "2026-01-25T10:30:00Z",
  "lastErrorTime": "2026-01-24T15:00:00Z",
  "lastHealthCheckTime": "2026-01-25T10:35:00Z",
  "refreshCount": 42,
  "needsRefresh": false,
  "credsFilePath": "/configs/kiro/account1.json"
}
```

**Provider-Specific Fields** (included in same object):
- Kiro: `KIRO_OAUTH_CREDS_FILE_PATH`
- Gemini: `GEMINI_OAUTH_CREDS_FILE_PATH`, `GEMINI_API_KEY`
- OpenAI: `OPENAI_API_KEY`, `OPENAI_BASE_URL`
- Forward: `FORWARD_URL`, `FORWARD_API_KEY`

**Operations**:
- `HGET aiclient:pools:{type} {uuid}` - Get single provider
- `HSET aiclient:pools:{type} {uuid} {json}` - Set single provider
- `HGETALL aiclient:pools:{type}` - Get all providers for type
- `HDEL aiclient:pools:{type} {uuid}` - Delete provider
- `HKEYS aiclient:pools:{type}` - List provider UUIDs

---

### 3. Atomic Counter Keys

**Key Pattern**: `aiclient:counters:{providerType}:{uuid}:{counter}`
**Type**: String (integer)
**TTL**: None (persistent)

**Counter Types**:
| Counter | Description |
|---------|-------------|
| `usageCount` | Total successful API calls |
| `errorCount` | Total errors encountered |
| `refreshCount` | Token refresh operations |

**Operations**:
- `INCR aiclient:counters:{type}:{uuid}:usageCount` - Atomic increment
- `GET aiclient:counters:{type}:{uuid}:usageCount` - Read counter

**Note**: Counters stored separately for atomic `INCR`. On pool read, counters are merged into provider object.

---

### 4. Token Credentials

**Key Pattern**: `aiclient:tokens:{providerType}:{uuid}`
**Type**: String (JSON)
**TTL**: Based on token expiry (optional)

**Token Object Schema** (Kiro/Claude example):
```json
{
  "accessToken": "eyJhbGciOiJSUzI1NiIsInR5cCI6...",
  "refreshToken": "dGhpcyBpcyBhIHJlZnJlc2ggdG9rZW4...",
  "expiresAt": "2026-01-25T12:00:00Z",
  "tokenType": "Bearer",
  "scope": "openid profile email"
}
```

**Token Object Schema** (Gemini OAuth example):
```json
{
  "access_token": "ya29.a0AfH6SMB...",
  "refresh_token": "1//0gYx...",
  "expiry_date": 1706180400000,
  "token_type": "Bearer"
}
```

**Operations**:
- `GET aiclient:tokens:{type}:{uuid}` - Read token
- `SET aiclient:tokens:{type}:{uuid} {json}` - Write token
- `SETEX aiclient:tokens:{type}:{uuid} {ttl} {json}` - Write with expiry

---

### 5. UI Password

**Key**: `aiclient:pwd`
**Type**: String
**TTL**: None (persistent)

**Operations**:
- `GET aiclient:pwd` - Read password
- `SET aiclient:pwd {password}` - Set password

---

### 6. UI Session Tokens

**Key Pattern**: `aiclient:sessions:{tokenHash}`
**Type**: String (JSON)
**TTL**: Based on session expiry

```json
{
  "token": "abc123def456...",
  "expiresAt": "2026-01-26T10:00:00Z"
}
```

**Operations**:
- `GET aiclient:sessions:{hash}` - Read session
- `SETEX aiclient:sessions:{hash} {ttl} {json}` - Create session with expiry
- `DEL aiclient:sessions:{hash}` - Delete session

---

### 7. Usage Cache

**Key**: `aiclient:usage:cache`
**Type**: String (JSON)
**TTL**: None (persistent, updated on refresh)

```json
{
  "timestamp": "2026-01-25T10:00:00Z",
  "providers": {
    "claude-kiro-oauth": {
      "providerType": "claude-kiro-oauth",
      "instances": [...]
    }
  }
}
```

**Per-Provider Key**: `aiclient:usage:{providerType}`
**Type**: String (JSON)
**TTL**: None (persistent)

**Operations**:
- `GET aiclient:usage:cache` - Read full usage cache
- `SET aiclient:usage:cache {json}` - Write full usage cache
- `GET aiclient:usage:{type}` - Read provider-specific cache
- `SET aiclient:usage:{type} {json}` - Write provider-specific cache

---

### 8. Plugin Configuration

**Key**: `aiclient:plugins`
**Type**: String (JSON)
**TTL**: None (persistent)

```json
{
  "plugins": {
    "api-potluck": {
      "enabled": true,
      "description": "API 大锅饭 - Key 管理和用量统计插件"
    },
    "default-auth": {
      "enabled": true,
      "description": "默认 API Key 认证插件"
    }
  }
}
```

**Operations**:
- `GET aiclient:plugins` - Read plugin configuration
- `SET aiclient:plugins {json}` - Write plugin configuration

---

### 9. Metadata

**Key**: `aiclient:meta`
**Type**: Hash
**TTL**: None (persistent)

| Field | Value |
|-------|-------|
| `version` | Schema version (e.g., "1.0") |
| `migratedAt` | ISO timestamp of last migration |
| `migratedFrom` | Source (e.g., "files") |

**Operations**:
- `HGET aiclient:meta version` - Check schema version
- `HSET aiclient:meta migratedAt {timestamp}` - Update migration time

---

## Entity Relationships

```
aiclient:config (1)
    └── references → providerFallbackChain → aiclient:pools:{type} (many)

aiclient:pools:{type} (per provider type)
    └── contains → {uuid} → provider object (many)
        └── references → aiclient:tokens:{type}:{uuid} (1)
        └── references → aiclient:counters:{type}:{uuid}:* (3)

aiclient:meta (1)
    └── describes schema version for all keys
```

## Validation Rules

| Entity | Field | Rule |
|--------|-------|------|
| Provider | uuid | Required, UUID v4 format |
| Provider | isHealthy | Boolean, default true |
| Provider | isDisabled | Boolean, default false |
| Provider | usageCount | Integer >= 0 |
| Provider | errorCount | Integer >= 0 |
| Token | accessToken | Required, non-empty string |
| Token | expiresAt | ISO 8601 timestamp or Unix ms |
| Config | SERVER_PORT | Integer 1-65535 |
| Config | REQUIRED_API_KEY | Non-empty string |

## State Transitions

### Provider Health State

```
[healthy] ──(API error)──> [unhealthy]
    ^                           │
    └───(health check pass)─────┘
```

### Token Refresh State

```
[valid] ──(expiry approaching)──> [needsRefresh=true]
                                        │
                ┌───(refresh success)───┘
                v
           [valid, needsRefresh=false]
```

## Migration Mapping

| File Location | Redis Key |
|---------------|-----------|
| `configs/config.json` | `aiclient:config` |
| `configs/provider_pools.json[type]` | `aiclient:pools:{type}` |
| `configs/kiro/*.json` | `aiclient:tokens:claude-kiro-oauth:{uuid}` |
| `configs/gemini/*.json` | `aiclient:tokens:gemini-cli-oauth:{uuid}` |
| `configs/pwd` | `aiclient:pwd` |
| `configs/token-store.json` | `aiclient:sessions:{hash}` |
| `configs/usage-cache.json` | `aiclient:usage:cache` |
| `configs/plugins.json` | `aiclient:plugins` |
