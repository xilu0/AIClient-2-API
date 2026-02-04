# 修复：Go json.Marshal HTML 转义导致 Kiro API 400 错误

**日期**: 2026-02-02
**提交**: `9bc99fa` (最终修复), `de683c0` (首次不完整修复)
**影响**: 包含 `<`/`>`/`&` 字符的请求（如带工具定义的 Claude Code 请求）全部失败

## 问题现象

Go Kiro 服务处理包含工具定义的请求时，Kiro API 返回 400 错误：

```json
{"message": "Improperly formed request", "reason": "INVALID_INPUT"}
```

同样的请求通过 Node.js 服务（端口 8080）正常成功。

## 根因分析

### Go `json.Marshal` 的默认行为

Go 标准库 `json.Marshal` 默认对 HTML 敏感字符进行 Unicode 转义：

| 原始字符 | 转义后 |
|---------|--------|
| `<` | `\u003c` |
| `>` | `\u003e` |
| `&` | `\u0026` |

Claude Code 的工具定义中大量使用 `<` 和 `>` 字符（如 XML 标签 `<example>`、HTML 描述等）。经 `json.Marshal` 序列化后，这些字符被替换为 `\u003c`/`\u003e`，Kiro API 无法识别这种格式，返回 400。

### 对比验证

通过 debug dump 对比 Go 失败请求与 Node.js 成功请求：

| 维度 | Go (失败) | Node.js (成功) |
|------|----------|---------------|
| `\u003c` 出现次数 | 221 | 0 |
| 字面 `<` 出现次数 | 0 | 55 |
| 请求结果 | 400 Improperly formed | 200 Success |

两个请求的结构和内容完全一致，唯一差异就是 HTML 转义。

### 排除的错误假设

调查过程中排除了两个错误方向：

1. **`$schema` 字段假设**: 最初怀疑工具 inputSchema 中的 `$schema` 字段导致问题。但所有请求（包括成功的）都包含 `$schema`，且 Kiro API 文档允许该字段。
2. **toolResults + tools 共存假设**: 发现失败请求中 currentMessage 同时包含 toolResults 和 tools，但 Node.js 成功请求中也存在相同结构。

## 修复方案

### 使用 `MarshalWithoutHTMLEscape` 替代 `json.Marshal`

```go
func MarshalWithoutHTMLEscape(v interface{}) ([]byte, error) {
    var buf bytes.Buffer
    enc := json.NewEncoder(&buf)
    enc.SetEscapeHTML(false) // 关键：禁用 HTML 转义
    if err := enc.Encode(v); err != nil {
        return nil, err
    }
    b := buf.Bytes()
    if len(b) > 0 && b[len(b)-1] == '\n' {
        b = b[:len(b)-1]
    }
    return b, nil
}
```

### 修复的三个位置（`internal/kiro/client.go`）

| 位置 | 函数 | 作用 |
|------|------|------|
| 行 581 | `BuildRequestBody` | 最终请求序列化，所有请求必经 |
| 行 915 | `InjectToolsFromHistory` | 重试路径，注入工具后重新序列化 |
| 行 1055 | `sanitizeToolSchema` | 工具 schema 清理后序列化为 `json.RawMessage` |

### 为什么首次修复（`de683c0`）不完整

首次修复在 handler 层（`messages.go:271-272`）对输入调用了 `MarshalWithoutHTMLEscape`，但 `BuildRequestBody` 内部执行了 unmarshal → 重建 → marshal 的流程，使用标准 `json.Marshal` 重新序列化，HTML 转义被重新引入。

```
Handler层 (MarshalWithoutHTMLEscape)  →  正确的 bytes
    ↓
BuildRequestBody (json.Unmarshal)     →  Go 对象（转义丢失）
    ↓
BuildRequestBody (json.Marshal)       →  HTML 转义重新出现 ← 问题所在
```

正确修复直接在 `BuildRequestBody` 的输出点使用 `MarshalWithoutHTMLEscape`。

### `json.RawMessage` 透传问题

`sanitizeToolSchema` 返回 `json.RawMessage`（本质是 `[]byte`）。当外层 marshal 整个请求时，`json.RawMessage` 的内容被原样嵌入，不会被重新编码。因此如果内层使用 `json.Marshal` 产生了 `\u003c`，即使外层使用 `MarshalWithoutHTMLEscape` 也无法修正。这就是 `sanitizeToolSchema` 也必须修复的原因。

## 验证

```bash
# 构建
go build ./cmd/kiro-server

# 单元测试
go test ./tests/unit/...

# 确认 client.go 中无残留 json.Marshal
grep 'json\.Marshal(' internal/kiro/client.go  # 应无输出
```

## 经验总结

1. **Go `json.Marshal` HTML 转义是常见陷阱**: 任何向外部 API 发送 JSON 的场景都应考虑是否需要禁用 HTML 转义。
2. **修复要覆盖所有序列化路径**: 仅修复入口点不够，必须追踪数据经过的所有 marshal 点。
3. **`json.RawMessage` 不受外层 marshal 影响**: 嵌套的 `json.RawMessage` 在外层序列化时原样透传，内层的转义问题必须在内层解决。
4. **对比调试法有效**: 将 Go 和 Node.js 的实际请求 dump 进行字节级对比，快速定位到唯一差异。
