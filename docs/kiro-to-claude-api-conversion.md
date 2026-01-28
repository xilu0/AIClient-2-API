# Kiro 到 Claude API 转换指南

本文档详细讲解 Go 实现中 Kiro API 与 Claude API 之间的协议转换流程。

## 目录

1. [概述](#概述)
2. [架构设计](#架构设计)
3. [请求转换：Claude → Kiro](#请求转换claude--kiro)
4. [响应转换：Kiro → Claude](#响应转换kiro--claude)
5. [数据结构对比](#数据结构对比)
6. [Tool Use 处理](#tool-use-处理)
7. [Token 分配策略](#token-分配策略)
8. [常见问题与解决方案](#常见问题与解决方案)

---

## 概述

Kiro 是 AWS CodeWhisperer 的底层 API，其协议格式与 Claude API 有显著差异。本服务作为中间层，将 Claude API 请求转换为 Kiro 格式，并将 Kiro 响应转换回 Claude SSE 格式。

### 核心挑战

| 挑战 | 描述 |
|------|------|
| 消息格式差异 | Claude 使用 `messages` 数组，Kiro 使用 `conversationState` |
| 流式响应格式 | Claude 使用 SSE 事件，Kiro 使用 AWS Event Stream |
| Tool Use 协议 | 两者的工具调用格式完全不同 |
| Token 计算 | Kiro 只返回 `contextUsagePercentage`，需要反推 token 数 |

---

## 架构设计

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Client (Claude Code)                         │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼ Claude API Format
┌─────────────────────────────────────────────────────────────────────┐
│                    internal/handler/messages.go                      │
│                       (HTTP Handler Layer)                           │
│  - 解析 Claude 请求                                                   │
│  - 调用 BuildRequestBody() 转换请求                                   │
│  - 调用 streamResponse() 处理流式响应                                 │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                    ┌───────────────┴───────────────┐
                    ▼                               ▼
┌──────────────────────────────┐   ┌──────────────────────────────────┐
│   internal/kiro/client.go    │   │   internal/claude/converter.go   │
│      (请求转换层)             │   │       (响应转换层)                │
│  - BuildRequestBody()        │   │  - Convert() → []*SSEEvent       │
│  - parseMessageContent()     │   │  - convertToolUse()              │
│  - parseAssistantContent()   │   │  - convertKiroContent()          │
│  - convertToolsToKiroFormat()│   │                                  │
└──────────────────────────────┘   └──────────────────────────────────┘
                    │                               ▲
                    ▼ Kiro Format                   │ Kiro Format
┌─────────────────────────────────────────────────────────────────────┐
│                          Kiro API (AWS)                              │
└─────────────────────────────────────────────────────────────────────┘
```

### 核心文件

| 文件 | 职责 |
|------|------|
| `internal/handler/messages.go` | HTTP 请求处理，协调请求/响应转换 |
| `internal/kiro/client.go` | Kiro 客户端，请求构建与发送 |
| `internal/claude/converter.go` | 流式响应转换（Kiro → Claude SSE） |
| `internal/claude/aggregator.go` | 非流式响应聚合（收集所有 chunk 后返回） |
| `internal/claude/sse.go` | SSE 事件写入工具 |
| `internal/kiro/parser.go` | AWS Event Stream 解析器 |

---

## 请求转换：Claude → Kiro

### 整体流程

```
Claude Request                          Kiro Request
─────────────────                       ─────────────────
{                                       {
  "model": "claude-sonnet-4",    →        "conversationState": {
  "messages": [...],                        "chatTriggerType": "MANUAL",
  "max_tokens": 4096,                       "conversationId": "uuid",
  "system": "...",                          "history": [...],
  "tools": [...]                            "currentMessage": {
}                                             "userInputMessage": {...}
                                            }
                                          },
                                          "profileArn": "..."
                                        }
```

### 核心函数：`BuildRequestBody()`

位置：`internal/kiro/client.go:205`

```go
func BuildRequestBody(
    model string,           // Claude 模型名
    messages []byte,        // Claude messages JSON
    maxTokens int,          // 最大 token 数（未使用）
    stream bool,            // 流式标志（未使用）
    system string,          // 系统提示
    profileARN string,      // Kiro 身份标识
    tools []byte,           // 工具定义 JSON
) ([]byte, error)
```

### 消息转换规则

#### 1. System Prompt 处理

```
Claude:                              Kiro:
system: "You are helpful"    →      prepend to first user message content
```

系统提示被追加到第一条用户消息的 content 前面。

#### 2. 消息角色映射

| Claude Role | Kiro Structure |
|-------------|----------------|
| `user` | `userInputMessage` |
| `assistant` | `assistantResponseMessage` |

#### 3. History 构建规则

**关键约束**：Kiro API 要求 `history` 必须以 `assistantResponseMessage` 结尾（如果有 history）。

```go
// 如果 history 最后是 userInputMessage，需要补一个空的 assistantResponseMessage
if len(history) > 0 {
    lastHistoryItem := history[len(history)-1]
    if _, hasUser := lastHistoryItem["userInputMessage"]; hasUser {
        history = append(history, map[string]interface{}{
            "assistantResponseMessage": map[string]interface{}{
                "content": "Continue",
            },
        })
    }
}
```

#### 4. 最后一条消息处理

**约束**：`currentMessage` 必须是 `userInputMessage` 类型。

- 如果最后一条消息是 `user`：直接作为 `currentMessage`
- 如果最后一条消息是 `assistant`：移入 `history`，创建 `"Continue"` 作为 `currentMessage`

### Content Block 解析

#### User 消息内容类型

| Claude Type | 处理方式 |
|-------------|----------|
| `text` | 累加到 content 字符串 |
| `tool_result` | 转换为 `toolResults` 数组 |
| `image` | 转换为 `images` 数组 |

```go
// tool_result 转换
toolResult := map[string]interface{}{
    "content":   []map[string]interface{}{{"text": extractedText}},
    "status":    "success",  // 或 "error"
    "toolUseId": block.ToolUseID,
}
```

#### Assistant 消息内容类型

| Claude Type | 处理方式 |
|-------------|----------|
| `text` | 累加到 content 字符串 |
| `tool_use` | 转换为 `toolUses` 数组 |
| `thinking` | 包装为 `<kiro_thinking>...</kiro_thinking>` |

```go
// tool_use 转换（input 字段必须存在）
toolUse := map[string]interface{}{
    "toolUseId": block.ID,
    "name":      block.Name,
    "input":     input,  // 必须存在，空时为 {}
}
```

### Tools 转换

Claude 工具定义转换为 Kiro 格式：

```
Claude:                                  Kiro:
{                                        {
  "name": "get_weather",          →        "toolSpecification": {
  "description": "...",                      "name": "get_weather",
  "input_schema": {...}                      "description": "...",
}                                            "inputSchema": {
                                               "json": {...}
                                             }
                                           }
                                         }
```

**注意事项**：
- 过滤 `web_search`/`websearch` 工具（Kiro 不支持）
- 描述超过 9216 字符会被截断
- `input_schema` 为 `null` 时替换为 `{}`

---

## 响应转换：Kiro → Claude

### 流式响应处理

Kiro 使用 AWS Event Stream 格式返回响应，需要转换为 Claude SSE 格式。

```
Kiro Chunk                              Claude SSE Events
───────────                             ─────────────────
{content: "Hello"}              →       message_start
                                        content_block_start (type: text)
                                        content_block_delta (text_delta)

{name: "tool", toolUseId: "x"}  →       content_block_start (type: tool_use)
{input: "{\"a\":1}"}            →       content_block_delta (input_json_delta)
{stop: true}                    →       content_block_stop

{contextUsagePercentage: 1.5}   →       message_delta (usage)
                                        message_stop
```

### 核心函数：`Convert()`

位置：`internal/claude/converter.go:57`

```go
func (c *Converter) Convert(chunk *kiro.KiroChunk) ([]*SSEEvent, error)
```

**关键设计**：返回 `[]*SSEEvent` 切片，因为单个 Kiro chunk 可能需要生成多个 Claude 事件。

例如，第一个 content chunk 需要同时生成：
1. `message_start`
2. `content_block_start`
3. `content_block_delta`

### Kiro Chunk 类型处理

| Kiro 字段 | 处理函数 | 生成的 Claude 事件 |
|-----------|----------|-------------------|
| `content` (非空) | `convertKiroContent()` | message_start, content_block_start, content_block_delta |
| `name` + `toolUseId` | `convertToolUse()` | content_block_start (tool_use), content_block_delta (input_json_delta) |
| `input` (tool 续传) | `convertToolUse()` | content_block_delta (input_json_delta) |
| `stop: true` | `convertToolUse()` | content_block_stop |
| `contextUsagePercentage` | 内部记录 | 用于最终 usage 计算 |

### Tool Use 事件序列

```
┌─────────────────────────────────────────────────────────────────────┐
│ Kiro: {name: "edit", toolUseId: "toolu_xxx", input: "{\"path\":"}   │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│ Claude: message_start (如果还没发送)                                 │
│ Claude: content_block_start {type: "tool_use", id, name, input: {}} │
│ Claude: content_block_delta {type: "input_json_delta", partial_json}│
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│ Kiro: {input: "\"file.js\"}"}                                       │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│ Claude: content_block_delta {type: "input_json_delta", partial_json}│
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│ Kiro: {stop: true}                                                  │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│ Claude: content_block_stop                                          │
└─────────────────────────────────────────────────────────────────────┘
```

### 非流式响应聚合

`Aggregator` 类用于收集所有流式 chunk，最终构建完整的 `MessageResponse`。

位置：`internal/claude/aggregator.go`

```go
type Aggregator struct {
    content      []ContentBlock  // 聚合的内容块
    currentBlockInputStr string  // 当前 tool_use 的 input 累积
    // ...
}

func (a *Aggregator) Add(chunk *kiro.KiroChunk) error  // 添加 chunk
func (a *Aggregator) Build() *MessageResponse          // 构建最终响应
```

**关键处理**：`validateAndGetInput()` 验证 tool input JSON：
- 有效 JSON：直接使用
- 无效 JSON：包装为 `{"raw_arguments": "..."}`
- 空：返回 `{}`

---

## 数据结构对比

### 请求结构

```
Claude Request                          Kiro Request
═══════════════                         ════════════
{                                       {
  "model": "claude-sonnet-4",             "conversationState": {
  "max_tokens": 4096,                       "chatTriggerType": "MANUAL",
  "system": "...",                          "conversationId": "uuid-v4",
  "messages": [                             "history": [
    {                                         {
      "role": "user",                           "userInputMessage": {
      "content": "Hello"                          "content": "Hello",
    },                                            "modelId": "claude-sonnet-4-v2",
    {                                             "origin": "AI_EDITOR"
      "role": "assistant",                      }
      "content": "Hi!"                        },
    }                                         {
  ],                                            "assistantResponseMessage": {
  "tools": [                                      "content": "Hi!"
    {                                           }
      "name": "edit",                         }
      "description": "...",                 ],
      "input_schema": {...}                 "currentMessage": {
    }                                         "userInputMessage": {
  ]                                             "content": "...",
}                                               "modelId": "...",
                                                "origin": "AI_EDITOR",
                                                "userInputMessageContext": {
                                                  "toolResults": [...],
                                                  "tools": [...]
                                                }
                                              }
                                            }
                                          },
                                          "profileArn": "arn:aws:..."
                                        }
```

### 响应结构

```
Kiro Streaming Chunk                    Claude SSE Event
════════════════════                    ════════════════
// 文本内容
{                                       event: content_block_delta
  "content": "Hello"                    data: {
}                                         "type": "content_block_delta",
                                          "index": 0,
                                          "delta": {
                                            "type": "text_delta",
                                            "text": "Hello"
                                          }
                                        }

// Tool Use 开始
{                                       event: content_block_start
  "name": "edit",                       data: {
  "toolUseId": "toolu_xxx",               "type": "content_block_start",
  "input": "{\"path\":"                   "index": 1,
}                                         "content_block": {
                                            "type": "tool_use",
                                            "id": "toolu_xxx",
                                            "name": "edit",
                                            "input": {}
                                          }
                                        }
                                        event: content_block_delta
                                        data: {
                                          "type": "content_block_delta",
                                          "index": 1,
                                          "delta": {
                                            "type": "input_json_delta",
                                            "partial_json": "{\"path\":"
                                          }
                                        }
```

---

## Tool Use 处理

### 请求中的 Tool Use

当 Claude 客户端发送包含 `tool_use` 的 assistant 消息（表示之前的工具调用）：

```json
{
  "role": "assistant",
  "content": [
    {"type": "text", "text": "I'll edit the file"},
    {
      "type": "tool_use",
      "id": "toolu_xxx",
      "name": "edit",
      "input": {"path": "file.js", "content": "..."}
    }
  ]
}
```

转换为 Kiro 的 `assistantResponseMessage.toolUses`：

```json
{
  "assistantResponseMessage": {
    "content": "I'll edit the file",
    "toolUses": [
      {
        "toolUseId": "toolu_xxx",
        "name": "edit",
        "input": {"path": "file.js", "content": "..."}
      }
    ]
  }
}
```

**关键点**：`input` 字段必须存在，即使为空也要设为 `{}`。

### 请求中的 Tool Result

当 Claude 客户端发送工具执行结果：

```json
{
  "role": "user",
  "content": [
    {
      "type": "tool_result",
      "tool_use_id": "toolu_xxx",
      "content": "File edited successfully"
    }
  ]
}
```

转换为 Kiro 的 `userInputMessageContext.toolResults`：

```json
{
  "userInputMessage": {
    "content": "Tool results provided.",
    "userInputMessageContext": {
      "toolResults": [
        {
          "content": [{"text": "File edited successfully"}],
          "status": "success",
          "toolUseId": "toolu_xxx"
        }
      ]
    }
  }
}
```

### 响应中的 Tool Use

Kiro 返回工具调用时的事件序列：

1. **首个 chunk**：包含 `name`、`toolUseId`、可能的 `input`
2. **后续 chunk**：只包含 `input`（JSON 片段）
3. **结束 chunk**：包含 `stop: true`

转换器需要：
1. 追踪当前是否在 tool use 状态（`inToolUse`）
2. 追踪是否已发送 `content_block_start`（`toolUseStartSent`）
3. 正确累积和验证 input JSON

---

## Token 分配策略

### 背景

Kiro API 不直接返回 token 数，只返回 `contextUsagePercentage`（上下文使用百分比）。需要反推 token 数并按 1:2:25 比例分配。

### 计算公式

```
total_tokens = 172500 * contextUsagePercentage / 100
input_tokens = total_tokens - output_tokens

分配比例 (1:2:25)：
- input_tokens = total * 1/28
- cache_creation_input_tokens = total * 2/28
- cache_read_input_tokens = total * 25/28
```

### 实现

位置：`internal/claude/tokens.go`

```go
func DistributeTokens(inputTokens int) DistributedTokens {
    const (
        ratioInput         = 1
        ratioCacheCreation = 2
        ratioCacheRead     = 25
        totalRatio         = 28
        threshold          = 100
    )

    if inputTokens < threshold {
        return DistributedTokens{InputTokens: inputTokens}
    }

    return DistributedTokens{
        InputTokens:              inputTokens * ratioInput / totalRatio,
        CacheCreationInputTokens: inputTokens * ratioCacheCreation / totalRatio,
        CacheReadInputTokens:     inputTokens - above_two,
    }
}
```

---

## 常见问题与解决方案

### 1. 400 Improperly formed request

**可能原因**：
- `toolUses` 中 `input` 字段缺失
- `inputSchema` 为 `null`
- `history` 格式不正确

**解决方案**：
```go
// 确保 input 始终存在
if input == nil {
    input = map[string]interface{}{}
}

// 确保 inputSchema 不为 null
if string(inputSchema) == "null" {
    inputSchema = []byte("{}")
}
```

### 2. JSON serialization error for RawMessage

**原因**：累积的 tool input 不是有效的 JSON

**解决方案**：使用 `validateAndGetInput()` 验证并处理：
```go
func (a *Aggregator) validateAndGetInput() json.RawMessage {
    if err := json.Unmarshal([]byte(a.currentBlockInputStr), &js); err != nil {
        // 无效 JSON，包装为 raw_arguments
        wrapped := map[string]string{"raw_arguments": a.currentBlockInputStr}
        result, _ := json.Marshal(wrapped)
        return result
    }
    return js
}
```

### 3. null is not an object (evaluating 'fH.value.type')

**原因**：`content_block_start` 中 `input` 字段为 `null`

**解决方案**：始终设置为空对象：
```go
ContentBlock: ContentStart{
    Type:  "tool_use",
    ID:    chunk.ToolUseID,
    Name:  chunk.Name,
    Input: json.RawMessage("{}"),  // 不能是 null
}
```

### 4. 事件丢失（Dropped events）

**原因**：使用 `pendingInput` 缓存导致后续 chunk 数据丢失

**解决方案**：在第一个 chunk 中直接发送所有事件，不使用缓存：
```go
// 第一个 chunk 同时包含 name、toolUseId、input
events = append(events, &SSEEvent{Type: "content_block_start", Data: ...})
if chunk.Input != "" {
    events = append(events, &SSEEvent{Type: "content_block_delta", Data: ...})
}
return events, nil
```

---

## 调试技巧

### 启用请求日志

在 `internal/kiro/client.go` 中，400 错误时会输出请求体：

```go
c.logger.Warn("Kiro API error",
    "status", resp.StatusCode,
    "response_body", string(body),
    "request_body", string(req.Body),
)
```

### 对比 JS 实现

JS 实现位于 `src/providers/claude/claude-kiro.js`，可用于参考：
- `buildKiroRequest()` - 请求构建
- `parseEventStreamChunk()` - 响应解析

### 常用测试命令

```bash
# 运行单元测试
go test ./tests/unit/... -v

# 运行特定测试
go test ./tests/unit/... -v -run TestConvertToolUse

# 构建
go build ./cmd/kiro-server/
```

---

## 版本历史

| 日期 | 变更 |
|------|------|
| 2026-01-28 | 初始版本 |
