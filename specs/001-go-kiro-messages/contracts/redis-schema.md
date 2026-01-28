# Redis Schema: Kiro OAuth Provider

**Feature**: 001-go-kiro-messages | **Date**: 2026-01-27

This document defines the Redis key structures used by both Node.js and Go services for Kiro OAuth provider management. **Compatibility is critical** for Strangler Fig coexistence.

## Key Prefix

All keys use the configurable prefix (default: `aiclient:`).

## Provider Pool Keys

### `aiclient:pools:claude-kiro-oauth`

**Type**: HASH
**Purpose**: Store all Kiro OAuth accounts
**Field**: UUID → JSON account data

**Account JSON Structure**:
```json
{
  "uuid": "550e8400-e29b-41d4-a716-446655440000",
  "providerType": "claude-kiro-oauth",
  "region": "us-east-1",
  "profileArn": "arn:aws:codewhisperer:us-east-1:123456789:profile/xyz",
  "isHealthy": true,
  "usageCount": 1234,
  "errorCount": 5,
  "lastUsed": "2026-01-27T10:30:00.000Z",
  "lastErrorTime": "2026-01-27T09:00:00.000Z",
  "lastHealthCheckTime": "2026-01-27T10:30:00.000Z",
  "description": "Primary account",
  "addedAt": "2026-01-01T00:00:00.000Z"
}
```

**Operations**:
| Operation | Node.js | Go | Notes |
|-----------|---------|-----|-------|
| Get all | `HGETALL` | `HGetAll` | Decode each value as JSON |
| Get one | `HGET` | `HGet` | By UUID |
| Add/Update | `HSET` | `HSet` | Serialize to JSON |
| Delete | `HDEL` | `HDel` | By UUID |

---

## Token Keys

### `aiclient:tokens:claude-kiro-oauth:{uuid}`

**Type**: STRING
**Purpose**: Store OAuth tokens for each account
**TTL**: None (tokens include expiry in payload)

**Token JSON Structure**:
```json
{
  "accessToken": "eyJhbGciOiJSUzI1NiIsInR5cCI6...",
  "refreshToken": "eyJjdHkiOiJKV1QiLCJlbmMiOi...",
  "expiresAt": 1706356200000,
  "authMethod": "social",
  "tokenType": "Bearer",
  "lastRefreshed": "2026-01-27T10:00:00.000Z"
}
```

**Operations**:
| Operation | Node.js | Go | Notes |
|-----------|---------|-----|-------|
| Get | `GET` | `Get` | Parse JSON |
| Set | `SET` | `Set` | Serialize JSON |
| Atomic Update | `atomicTokenUpdate` (Lua) | `Watch`/`TxPipelined` | CAS pattern |
| Delete | `DEL` | `Del` | On account removal |

---

## Refresh Token Index

### `aiclient:kiro:refresh-index:{hash}`

**Type**: STRING
**Purpose**: Deduplication index for refresh tokens
**Value**: UUID of account owning this refresh token

**Hash Algorithm**: SHA256 of refresh token, first 32 characters

**Operations**:
| Operation | Node.js | Go | Notes |
|-----------|---------|-----|-------|
| Check duplicate | `GET` | `Get` | Returns existing UUID or nil |
| Create index | `SET` | - | Node.js only (account creation) |
| Delete index | `DEL` | - | Node.js only (account deletion) |

> **Go Note**: Go service only reads this index; account management remains in Node.js.

---

## Round-Robin Counter (NEW - Go Only)

### `aiclient:kiro:round-robin-counter`

**Type**: STRING (integer)
**Purpose**: Atomic counter for lock-free account selection
**Initial Value**: 0

**Operations**:
| Operation | Node.js | Go | Notes |
|-----------|---------|-----|-------|
| Increment | - | `INCR` | Returns new value |
| Read | - | `GET` | For debugging only |

> **Note**: This is a new key created by the Go service. Node.js does not use it (Node.js uses its own mutex-based selection in `provider-pool-manager.js`).

---

## Configuration Keys

### `aiclient:config`

**Type**: STRING
**Purpose**: Shared configuration (API key, default provider, etc.)

**Go Reads**:
- `apiKey` - For request authentication validation
- Other fields as needed

**Operations**:
| Operation | Node.js | Go | Notes |
|-----------|---------|-----|-------|
| Get | `GET` | `Get` | Parse full JSON |
| Set | `SET` | - | Node.js only (admin UI) |

---

## Key Patterns Summary

```
aiclient:
├── config                               # Shared config (JSON string)
├── pools:
│   └── claude-kiro-oauth               # Account pool (HASH)
├── tokens:
│   └── claude-kiro-oauth:
│       └── {uuid}                      # OAuth token (JSON string)
├── kiro:
│   ├── refresh-index:
│   │   └── {hash}                      # Dedup index (string)
│   └── round-robin-counter             # NEW: Go selection counter (int)
└── meta                                 # Metadata (HASH)
```

---

## Atomic Operations

### Usage Increment (Lua Script in Node.js)

```lua
-- Key: aiclient:pools:claude-kiro-oauth
-- ARGV[1]: uuid, ARGV[2]: timestamp
local provider = redis.call('HGET', KEYS[1], ARGV[1])
if not provider then return nil end
local data = cjson.decode(provider)
data.usageCount = (data.usageCount or 0) + 1
data.lastUsed = ARGV[2]
redis.call('HSET', KEYS[1], ARGV[1], cjson.encode(data))
return data.usageCount
```

**Go Equivalent** (without Lua):
```go
// Use WATCH/MULTI/EXEC for optimistic locking
ctx := context.Background()
key := "aiclient:pools:claude-kiro-oauth"

err := rdb.Watch(ctx, func(tx *redis.Tx) error {
    data, err := tx.HGet(ctx, key, uuid).Result()
    if err != nil { return err }

    var account Account
    json.Unmarshal([]byte(data), &account)
    account.UsageCount++
    account.LastUsed = time.Now().Format(time.RFC3339)

    updated, _ := json.Marshal(account)
    _, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
        pipe.HSet(ctx, key, uuid, updated)
        return nil
    })
    return err
}, key)
```

---

## Health Update Pattern

**Mark Unhealthy** (after 429/403):
```go
account.IsHealthy = false
account.ErrorCount++
account.LastErrorTime = time.Now().Format(time.RFC3339)
// Update in Redis
```

**Check Eligibility for Retry**:
```go
func isEligibleForRetry(account Account) bool {
    if account.IsHealthy {
        return true
    }
    if account.LastErrorTime == "" {
        return true
    }
    lastError, _ := time.Parse(time.RFC3339, account.LastErrorTime)
    return time.Since(lastError) >= 60*time.Second
}
```

**Mark Healthy** (after successful request):
```go
account.IsHealthy = true
account.LastHealthCheckTime = time.Now().Format(time.RFC3339)
// Update in Redis
```

---

## Compatibility Checklist

- [ ] Go uses same key prefix (`aiclient:`)
- [ ] Go parses account JSON with same field names
- [ ] Go parses token JSON with same field names
- [ ] Go updates use RFC3339 timestamp format (matches Node.js ISO 8601)
- [ ] Go does not modify refresh-index keys
- [ ] Go does not modify config key
- [ ] Round-robin counter is isolated (won't affect Node.js)

---

## AWS Event Stream Binary Format Reference

The Kiro API returns responses in AWS event stream binary encoding. Each message has:

| Field | Size | Description |
|-------|------|-------------|
| Total Length | 4 bytes | Big-endian uint32 |
| Headers Length | 4 bytes | Big-endian uint32 |
| Prelude CRC | 4 bytes | CRC32 of first 8 bytes |
| Headers | Variable | Key-value pairs |
| Payload | Variable | JSON content |
| Message CRC | 4 bytes | CRC32 of entire message |

**Header Format** (repeated):

| Field | Size | Description |
|-------|------|-------------|
| Name Length | 1 byte | |
| Name | Variable | UTF-8 string |
| Type | 1 byte | 7 = string |
| Value Length | 2 bytes | Big-endian uint16 |
| Value | Variable | UTF-8 string |

**Key Headers**:
- `:message-type`: "event" or "exception"
- `:event-type`: "chunk", "messageStart", "contentBlockDelta", "messageComplete"
- `:content-type`: "application/json"

**Reference**: [AWS Event Stream Encoding](https://docs.aws.amazon.com/transcribe/latest/dg/streaming-format.html)
