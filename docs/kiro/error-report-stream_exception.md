# Kiro Debug Dump 错误分析报告

**Session ID**: `7e57aa73-825b-477a-9748-936ff49deb24`
**日期**: 2026-02-02
**分析结论**: 幽灵异常（Ghost Exception） — 客户端已收到完整响应，异常为误报

---

## 概要

| 字段 | 值 |
|------|-----|
| Status | FAILED (实际内容完整) |
| Error Type | `stream_exception` |
| Model | `claude-haiku-4-5-20251001` |
| Account | `9422398a-dfe2-4b66-a28f-637a1a68227e` |
| Duration | 29.42s |
| Kiro Chunks | 1,188 (1,187 content + 1 exception) |
| Claude Chunks | 1,192 (完整流，含 `message_stop`) |
| Input Tokens | 2,184 + 4,368 cache_creation + 54,611 cache_read = ~61,163 total |
| Output Tokens | 4,771 |
| Request Size | ~197KB (kiro format), 38 history messages |

**对应错误日志**:
```json
{"time":"2026-02-02T02:00:19.093095088Z","level":"ERROR","msg":"received exception","payload":"{\"message\":\"Encountered an unexpected error when processing the request, please try again.\"}"}
```

---

## Claude SSE 事件序列

| 事件类型 | 数量 |
|----------|------|
| `message_start` | 1 |
| `content_block_start` | 1 |
| `content_block_delta` | 1,187 |
| `content_block_stop` | 1 |
| `message_delta` (stop_reason: `end_turn`) | 1 |
| `message_stop` | 1 |

流正常完成了全部 Claude SSE 事件序列。客户端收到完整响应。

---

## 异常时序还原

```
[01:59:49.673] 请求开始 → Kiro API
                ↓
[01:59:49 ~ 02:00:19] 接收 1,187 个 content chunks
                      全部正常转换为 Claude SSE events 并转发给客户端
                ↓
[02:00:19.xxx] Kiro API 在流的最末尾发送 exception 帧:
               AWS Event Stream Header: {:message-type: "exception"}
               Payload: {"message":"Encountered an unexpected error when processing the request, please try again."}
                ↓
[02:00:19.093] Go 服务记录异常 → session 标记为 FAILED
```

---

## 根因分析

### Kiro API 端行为

Kiro API 使用 AWS event stream 二进制协议传输数据。在本次请求中，API **成功传输了所有内容**后，在 TCP 流末尾追加了一个 exception 帧。这表明 Kiro 服务端在响应生成完成后的某个阶段（连接清理、后处理、或超时机制）触发了一个内部错误，但此时内容已完整发送。

### Go 服务处理逻辑

`internal/handler/messages.go:477-489` 中的流处理：

```
1. 收到 content chunk → IsEvent()=true → 正常处理，转换为 Claude event，转发给客户端
2. 收到 exception chunk → IsException()=true → 设置 exceptionReceived=true, continue 跳过
3. 下一次 body.Read() 返回 io.EOF → 调用 sendFinalStreamEvents() → 发送 message_stop
4. 函数返回 exceptionReceived=true → debug dumper 将 session 标记为 FAILED
```

### 问题本质

当前 Go 服务将所有 `exceptionReceived=true` 的 session 一律标记为 `FAILED`，**没有区分异常发生的时机**：

- **流中异常**：内容尚未完成时收到 exception → 真正的失败，需要重试
- **流尾异常**（本次情况）：内容已完整传输后收到 exception → 客户端不受影响，属于误报

---

## 影响评估

| 维度 | 评估 |
|------|------|
| 客户端影响 | **无** — 收到完整响应（含 `end_turn` + `message_stop`） |
| 数据丢失 | **无** — 4,771 output tokens 全部传输 |
| 账号健康 | **正常** — 非 429/403 错误，不需要禁用账号 |
| 误报风险 | **高** — 当前逻辑会将此类正常完成的请求计入错误统计 |

---

## 建议

### 1. 区分流尾异常和流中异常（推荐）

在 `internal/handler/messages.go` 的 `streamResponse()` 中，跟踪是否已收到完整内容（`content_block_stop` 事件）。如果异常发生在内容完成之后，将 session 标记为 `success`（带 warning）而非 `FAILED`：

```go
// 伪代码
if exceptionReceived && contentComplete {
    // 流尾异常：内容已完整，降级为 warning
    debugSession.SetStatus("success_with_warning")
} else if exceptionReceived {
    // 流中异常：真正的失败
    debugSession.SetStatus("failed")
}
```

### 2. 错误统计修正

当前所有 `stream_exception` 都会被计入账号的错误次数，可能导致健康的账号被误判为不可用。建议在健康检查逻辑中排除流尾异常。

### 3. 不需要重试

对于此类流尾异常，不应触发自动重试，因为客户端已经收到完整响应，重试反而浪费资源和配额。
