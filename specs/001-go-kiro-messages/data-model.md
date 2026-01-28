# Data Model: Go-based High-Concurrency Kiro Messages Endpoint

**Feature**: 001-go-kiro-messages | **Date**: 2026-01-27 | **Phase**: 1

## Entity Definitions

### 1. Account (Provider Entry)

Represents a Kiro OAuth credential in the provider pool.

**Source**: Redis HSET at `aiclient:pools:claude-kiro-oauth`

```go
type Account struct {
    // Identity
    UUID         string `json:"uuid"`
    ProviderType string `json:"providerType"` // always "claude-kiro-oauth"

    // Kiro-specific
    Region     string `json:"region"`     // AWS region (e.g., "us-east-1")
    ProfileARN string `json:"profileArn"` // AWS profile ARN for API calls

    // Health & Usage
    IsHealthy        bool   `json:"isHealthy"`
    UsageCount       int64  `json:"usageCount"`
    ErrorCount       int64  `json:"errorCount"`
    LastUsed         string `json:"lastUsed"`         // ISO 8601 timestamp
    LastErrorTime    string `json:"lastErrorTime"`    // ISO 8601 timestamp
    LastHealthCheck  string `json:"lastHealthCheckTime"` // ISO 8601 timestamp

    // Metadata
    Description string `json:"description,omitempty"`
    AddedAt     string `json:"addedAt"`
}
```

**Validation Rules**:
- `UUID` must be non-empty UUID v4 format
- `Region` must be valid AWS region
- `ProfileARN` must match AWS ARN pattern
- `UsageCount` and `ErrorCount` must be >= 0

**State Transitions**:
```
┌─────────────────────────────────────────────────────────────┐
│                        HEALTHY                               │
│                    (isHealthy: true)                        │
└─────────────────┬───────────────────────────────────────────┘
                  │
                  │ 429/403 error received
                  ▼
┌─────────────────────────────────────────────────────────────┐
│                       UNHEALTHY                              │
│    (isHealthy: false, lastErrorTime: <now>)                 │
│                                                              │
│    Eligible for retry after:                                │
│    lastErrorTime + 60 seconds                               │
└─────────────────┬───────────────────────────────────────────┘
                  │
                  │ Selected for retry AND request succeeds
                  ▼
┌─────────────────────────────────────────────────────────────┐
│                        HEALTHY                               │
│                    (isHealthy: true)                        │
└─────────────────────────────────────────────────────────────┘
```

---

### 2. Token

OAuth credentials for a Kiro account.

**Source**: Redis key at `aiclient:tokens:claude-kiro-oauth:{uuid}`

```go
type Token struct {
    // Credentials
    AccessToken  string `json:"accessToken"`
    RefreshToken string `json:"refreshToken"`
    ExpiresAt    int64  `json:"expiresAt"` // Unix timestamp (milliseconds)

    // Auth metadata
    AuthMethod string `json:"authMethod"` // "social" or "builder_id"
    TokenType  string `json:"tokenType,omitempty"` // typically "Bearer"

    // Refresh tracking
    LastRefreshed string `json:"lastRefreshed,omitempty"` // ISO 8601
}
```

**Validation Rules**:
- `AccessToken` must be non-empty
- `RefreshToken` must be non-empty
- `ExpiresAt` must be valid Unix timestamp
- `AuthMethod` must be "social" or "builder_id"

**Refresh Threshold**: Token refresh triggered when `ExpiresAt - now < 5 minutes`

---

### 3. MessageRequest

Claude-compatible request payload for `/v1/messages` endpoint.

```go
type MessageRequest struct {
    // Required
    Model    string    `json:"model"`
    Messages []Message `json:"messages"`
    MaxTokens int      `json:"max_tokens"`

    // Optional
    Stream      bool              `json:"stream,omitempty"`
    System      string            `json:"system,omitempty"`
    Temperature *float64          `json:"temperature,omitempty"`
    TopP        *float64          `json:"top_p,omitempty"`
    TopK        *int              `json:"top_k,omitempty"`
    StopSequences []string        `json:"stop_sequences,omitempty"`
    Metadata    map[string]string `json:"metadata,omitempty"`

    // Extended thinking
    Thinking *ThinkingConfig `json:"thinking,omitempty"`

    // Tools (optional)
    Tools     []Tool         `json:"tools,omitempty"`
    ToolChoice *ToolChoice   `json:"tool_choice,omitempty"`
}

type Message struct {
    Role    string          `json:"role"`    // "user" or "assistant"
    Content json.RawMessage `json:"content"` // string or []ContentBlock
}

type ContentBlock struct {
    Type string `json:"type"` // "text", "image", "tool_use", "tool_result"
    // Type-specific fields...
}

type ThinkingConfig struct {
    Type         string `json:"type"` // "enabled"
    BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type Tool struct {
    Name        string          `json:"name"`
    Description string          `json:"description,omitempty"`
    InputSchema json.RawMessage `json:"input_schema"`
}

type ToolChoice struct {
    Type string `json:"type"` // "auto", "any", "tool"
    Name string `json:"name,omitempty"` // when type="tool"
}
```

**Validation Rules**:
- `Model` must be non-empty (validated against allowed models)
- `Messages` must have at least one message
- `MaxTokens` must be > 0 and <= model limit
- `Messages[].Role` must be "user" or "assistant"
- First message role must be "user"
- Roles must alternate (user → assistant → user → ...)

---

### 4. SSE Event

Server-Sent Event for streaming responses.

```go
type SSEEvent struct {
    Type string      `json:"type"`
    Data interface{} `json:"data,omitempty"`
}

// Event types
const (
    EventMessageStart       = "message_start"
    EventContentBlockStart  = "content_block_start"
    EventContentBlockDelta  = "content_block_delta"
    EventContentBlockStop   = "content_block_stop"
    EventMessageDelta       = "message_delta"
    EventMessageStop        = "message_stop"
    EventPing               = "ping"
    EventError              = "error"
)
```

**SSE Event Sequence** (streaming):
```
message_start
content_block_start (index: 0)
content_block_delta (index: 0, delta: {text: "..."})
...
content_block_stop (index: 0)
[optional: more content blocks]
message_delta (stop_reason, usage)
message_stop
```

---

### 5. MessageResponse

Complete response for non-streaming requests.

```go
type MessageResponse struct {
    ID           string         `json:"id"`
    Type         string         `json:"type"` // "message"
    Role         string         `json:"role"` // "assistant"
    Content      []ContentBlock `json:"content"`
    Model        string         `json:"model"`
    StopReason   string         `json:"stop_reason"`
    StopSequence *string        `json:"stop_sequence,omitempty"`
    Usage        Usage          `json:"usage"`
}

type Usage struct {
    InputTokens              int `json:"input_tokens"`
    OutputTokens             int `json:"output_tokens"`
    CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
    CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}
```

---

### 6. TokenUsage (Internal)

Token distribution calculation result.

```go
type TokenUsage struct {
    InputTokens              int
    CacheCreationInputTokens int
    CacheReadInputTokens     int
}

// Distribution algorithm: 1:2:25 ratio
// Threshold: 100 tokens (below = no distribution)
// Total parts: 28
func DistributeTokens(totalInputTokens int) TokenUsage {
    if totalInputTokens < 100 {
        return TokenUsage{InputTokens: totalInputTokens}
    }
    input := totalInputTokens * 1 / 28
    creation := totalInputTokens * 2 / 28
    read := totalInputTokens - input - creation // remainder
    return TokenUsage{
        InputTokens:              input,
        CacheCreationInputTokens: creation,
        CacheReadInputTokens:     read,
    }
}
```

---

### 7. AWS Event Stream Message

Binary format from Kiro API.

```go
type AWSEventMessage struct {
    // Prelude (12 bytes)
    TotalLength   uint32
    HeadersLength uint32
    PreludeCRC    uint32

    // Headers (variable)
    Headers map[string]HeaderValue

    // Payload (variable)
    Payload []byte

    // CRC (4 bytes)
    MessageCRC uint32
}

type HeaderValue struct {
    Type  byte   // 7 = string
    Value string
}
```

**Important Headers**:
- `:message-type` - "event" or "exception"
- `:event-type` - "chunk", "messageStart", "contentBlockDelta", etc.
- `:content-type` - "application/json"

---

### 8. KiroChunk (Internal)

Parsed Kiro API response chunk.

```go
type KiroChunk struct {
    Type string `json:"type"`

    // For messageStart
    Message *KiroMessage `json:"message,omitempty"`

    // For contentBlockStart
    Index        *int          `json:"index,omitempty"`
    ContentBlock *ContentBlock `json:"content_block,omitempty"`

    // For contentBlockDelta
    Delta *Delta `json:"delta,omitempty"`

    // For messageComplete
    StopReason string `json:"stopReason,omitempty"`
    Usage      *struct {
        InputTokens  int `json:"inputTokens"`
        OutputTokens int `json:"outputTokens"`
    } `json:"usage,omitempty"`

    // For thinking blocks
    Thinking *string `json:"thinking,omitempty"`
}
```

---

### 9. ErrorResponse

Standard error response format.

```go
type ErrorResponse struct {
    Type  string     `json:"type"` // "error"
    Error ErrorBody  `json:"error"`
}

type ErrorBody struct {
    Type    string `json:"type"`    // "invalid_request_error", "authentication_error", etc.
    Message string `json:"message"`
}
```

**Error Types**:
| Type | HTTP Status | Description |
|------|-------------|-------------|
| `invalid_request_error` | 400 | Malformed request |
| `authentication_error` | 401 | Invalid API key |
| `permission_denied_error` | 403 | Not authorized |
| `not_found_error` | 404 | Resource not found |
| `rate_limit_error` | 429 | Too many requests |
| `api_error` | 500 | Internal error |
| `overloaded_error` | 529 | Service overloaded |

---

## Relationships

```
┌──────────────┐         ┌──────────────┐
│   Account    │ 1 ── 1  │    Token     │
│   (pool)     ├─────────┤  (credentials)│
└──────────────┘         └──────────────┘
       │
       │ selected for
       ▼
┌──────────────┐
│MessageRequest│
└──────────────┘
       │
       │ produces
       ▼
┌──────────────┐         ┌──────────────┐
│KiroChunk     │ ─────▶  │  SSEEvent    │
│(from Kiro)   │ convert │(to client)   │
└──────────────┘         └──────────────┘
       │
       │ aggregates to
       ▼
┌──────────────┐
│MessageResponse│
│(non-stream)  │
└──────────────┘
```

## Index/Cache Structures

### Round-Robin Counter

**Key**: `aiclient:kiro:round-robin-counter` (new, Go-only)
**Type**: Redis string (integer)
**Purpose**: Lock-free account selection via atomic INCR

### In-Memory Cache (Go service)

```go
type AccountCache struct {
    Accounts     []Account
    HealthyUUIDs []string
    UpdatedAt    time.Time
    TTL          time.Duration // 5 seconds
}
```

- Cached provider pool data
- Refreshed on cache miss or TTL expiry
- Health filtering done locally after fetch
