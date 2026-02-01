# Go Kiro 服务 400 "Improperly formed request" 排查报告

> 日期: 2026-02-01
> 状态: **已修复**
> 分支: `001-go-kiro-messages`

## 根因（TL;DR）

Kiro API（AWS Q generateAssistantResponse 端点）拒绝工具 `inputSchema` 中包含 **`$` 前缀属性名**的请求。

例如 MCP Excel 工具 `mcp__excel__list_workbook_permissions` 的 schema 中包含 OData 查询参数
`$expand`、`$select`、`$skip`、`$top` 作为 `properties` 的键名，Kiro API 将这些视为保留的
JSON Schema 关键字而返回 400。

**修复方案：** 在 `convertToolsToKiroFormat()` 中添加 `sanitizeToolSchema()` 函数，递归移除
`properties` 对象中以 `$` 开头的属性名。Go 和 Node.js 服务均已修复。

```bash
make update-go   # 部署 Go 修复
make update       # 部署两个服务
```

## 1. 问题描述

相同的 Claude API 请求（88 tools，model `claude-haiku-4-5-20251001`），Go 服务返回 400：

```
{"message":"Improperly formed request.","reason":null}
```

Go 服务在 `handleStreaming()` 中尝试多个账号均失败，所有账号都返回 400。

### 样本请求特征

```
model: claude-haiku-4-5-20251001
messages: 1 条用户消息
system: 3565 字符
tools: 88 个（含 WebSearch，转换后 87 个）
max_tokens: 32000
stream: true
```

## 2. 排查过程

### 2.1 已排除的假设

| 假设 | 排除方法 |
|------|----------|
| 模型 ID 映射（大写/小写） | 改为 sonnet 模型 ID 后仍失败；简化 tool #76 后 haiku 模型正常工作 |
| JSON pretty-print / compact | `json.Compact` 保留原始 key 顺序，仍失败 |
| Go `json.Marshal` HTML 转义 | `MarshalWithoutHTMLEscape` 也失败 |
| JSON key 排序 | 使用 `json.Compact`（保留原序）和 `json.Marshal`（字母序）都失败 |
| HTTP 协议 / Headers | 简单请求成功，证明 headers 和协议无问题 |
| `profileArn` 缺失 | dump 无 profileArn（IDC 认证），不影响结果 |
| Go HTTP 客户端问题 | Node.js 直接发送同一 dump 也返回 400 |
| 请求体大小限制 | 移除特定工具后请求变大但成功 |
| `conversationId` 过期 | 使用新 UUID 仍失败 |
| 账号差异 | 使用不同账号、相同账号均失败 |
| `$schema` 顶层字段 | 单独添加 `$schema` 不影响，请求通过 |
| `anyOf` JSON Schema 构造 | 单独添加 `anyOf` 不影响，请求通过 |

### 2.2 工具二分搜索定位

对 dump 文件中的 87 个工具进行裁剪测试：

| 工具数量 | 结果 | 说明 |
|---------|------|------|
| 76（前 76 个） | ✅ | 最后一个成功的数量 |
| 77（前 77 个） | ❌ | 新增的第 77 个工具触发失败 |
| 86（移除 #76） | ✅ | 仅移除 `mcp__excel__list_workbook_permissions` |
| 86（保留 #76，移除其他） | ❌ | 确认是 #76 的内容，非总大小 |

**定位到 tool #76：`mcp__excel__list_workbook_permissions`**

### 2.3 Schema 特征对比

问题工具 schema 与正常 Excel 工具对比：

```
Tool [74] mcp__excel__list_table_rows      → ✅ 正常
  properties: item_id, session_id, table_id
  无 $-prefixed 属性名

Tool [75] mcp__excel__list_tables           → ✅ 正常
  properties: item_id, session_id, worksheet
  无 $-prefixed 属性名

Tool [76] mcp__excel__list_workbook_permissions → ❌ 失败
  properties: $expand, $select, $skip, $top, drive_id, item_id
  有 $-prefixed 属性名 ← 唯一差异
```

### 2.4 控制变量实验

| 测试内容 | 结果 | 结论 |
|---------|------|------|
| 简化 #76 schema（仅保留 drive_id, item_id） | ✅ | 移除 `$` 属性后通过 |
| 仅添加 `$schema` 顶层字段 | ✅ | `$schema` 不是原因 |
| 仅添加 `$expand`, `$select` 属性名 | ❌ | **`$`-prefixed 属性名是根因** |
| 仅添加 `anyOf` 构造（无 `$` 属性名） | ✅ | `anyOf` 不是原因 |

## 3. 根因分析

### Kiro API 的 JSON Schema 校验规则

Kiro API（`q.{region}.amazonaws.com/generateAssistantResponse`）对工具的 `inputSchema.json`
执行 JSON Schema 校验。`$` 前缀在 JSON Schema 规范中是保留前缀（用于 `$ref`、`$schema`、
`$id`、`$defs` 等关键字）。

当 `properties` 对象中出现 `$` 前缀的键名（如 `$expand`、`$select`），Kiro API 的校验器
将其视为非法的 JSON Schema 关键字（而非属性定义），导致校验失败返回 400。

这些 `$` 前缀属性来源于 MCP（Model Context Protocol）工具，特别是 Microsoft Graph API 的
OData 查询参数（`$expand`、`$select`、`$skip`、`$top`、`$filter` 等）。

### 问题工具 Schema 示例

```json
{
  "type": "object",
  "properties": {
    "$expand": {                          ← Kiro API 拒绝
      "type": ["string", "null"],
      "default": null,
      "description": "Comma-separated list of related entities to expand."
    },
    "$select": {                          ← Kiro API 拒绝
      "type": ["string", "null"],
      "default": null
    },
    "$skip": {                            ← Kiro API 拒绝
      "anyOf": [
        {"type": "integer", "minimum": 0},
        {"type": "null"}
      ],
      "default": null
    },
    "drive_id": {"type": "string"},       ← 正常
    "item_id": {"type": "string"}         ← 正常
  },
  "required": ["drive_id", "item_id"],
  "$schema": "http://json-schema.org/draft-07/schema#"    ← 顶层 $schema 不受影响
}
```

### Node.js 服务为何曾经"成功"

TestReplayDump 测试通过 Node.js 服务回放时显示成功，但经调查发现：

1. 原始请求 model 为 `claude-haiku-4-5-20251001`
2. Node.js 的 `KIRO_MODELS` 列表不含 `claude-haiku-4-5-20251001`（仅有 `claude-haiku-4-5`）
3. `generateContentStream()` 中的模型回退逻辑：
   ```javascript
   const finalModel = MODEL_MAPPING[model] ? model : this.modelName;
   // "claude-haiku-4-5-20251001" 不在 MODEL_MAPPING → 回退到 this.modelName (sonnet)
   ```
4. Node.js 实际使用 **sonnet 模型**处理了该请求，且成功的 dump 来自**不同时间的请求**
5. Node.js 同样未清理 `$` 前缀属性名，理论上也会失败（但由于该特定请求碰巧未触发问题或 API 行为有时间差异）

**关键：Node.js 和 Go 都受此问题影响，只是测试条件差异导致表面上 Node.js "成功"。**

## 4. 修复方案

### 4.1 Go 服务修复

**文件：`internal/kiro/client.go`**

新增 `sanitizeToolSchema()` 和 `sanitizeSchemaProperties()` 函数：

```go
// sanitizeToolSchema removes $-prefixed property names from JSON Schema properties.
// The Kiro API rejects schemas with $-prefixed property names (e.g., $expand, $select)
func sanitizeToolSchema(schemaJSON json.RawMessage) json.RawMessage {
    var schema map[string]interface{}
    if err := json.Unmarshal(schemaJSON, &schema); err != nil {
        return schemaJSON
    }
    if !sanitizeSchemaProperties(schema) {
        return schemaJSON // No changes needed
    }
    result, err := json.Marshal(schema)
    if err != nil {
        return schemaJSON
    }
    return result
}
```

在 `convertToolsToKiroFormat()` 中调用：

```go
inputSchema = sanitizeToolSchema(inputSchema)
```

处理逻辑：
- 递归扫描 `properties` 对象，删除以 `$` 开头的键
- 同步清理 `required` 数组中对应的引用
- 递归处理 `items`、`additionalProperties`、`anyOf`/`allOf`/`oneOf` 中的嵌套 schema
- 无 `$` 前缀属性时返回原始 bytes（零开销）

### 4.2 Node.js 服务修复

**文件：`src/providers/claude/claude-kiro.js`**

新增 `sanitizeToolSchema()` 函数（JavaScript 实现），逻辑与 Go 版本一致。
在 `buildCodewhispererRequest()` 的工具转换中调用：

```javascript
const schema = sanitizeToolSchema(tool.input_schema || {});
```

### 4.3 单元测试

**文件：`tests/unit/kiro_request_test.go`**

- `TestBuildRequestBody_SanitizesDollarPrefixedProperties`：验证 `$expand`、`$select`、`$skip` 被移除，`drive_id` 和 `$schema` 被保留
- `TestBuildRequestBody_NoSanitizationWhenNoDollarProps`：验证无 `$` 前缀时 schema 不变

## 5. 验证结果

### 修复前

```bash
# 原始请求（88 tools 含 $-prefixed properties）通过 Go BuildRequestBody → 发送
KIRO_ORIGINAL_REQUEST=.../request.json go test ... -run TestKiroRebuildAndSend
# → ❌ FAIL: 400 "Improperly formed request"
```

### 修复后

```bash
# 同一请求，sanitizeToolSchema 移除 $-prefixed 属性名后
KIRO_ORIGINAL_REQUEST=.../request.json go test ... -run TestKiroRebuildAndSend
# → ✅ PASS: 请求成功，haiku 模型正常工作

go test ./tests/unit/... -count=1
# → ok (0.203s) 全部通过
```

## 6. 受影响范围

### 触发条件

1. 客户端使用 MCP 工具（特别是 Microsoft Graph API 相关的 Excel/SharePoint 工具）
2. 工具的 `input_schema.properties` 中包含 OData 查询参数（`$expand`、`$select`、`$skip`、`$top`、`$filter` 等）
3. 请求经过 Go 或 Node.js 服务转发到 Kiro API

### 影响

- 包含此类工具的请求始终返回 400，无法通过重试解决
- Go 服务的重试逻辑会尝试不同账号，但所有账号都会失败
- 用户看到 "Improperly formed request" 错误

### 修复后的行为

- `$` 前缀属性名从工具 schema 中移除（这些通常是可选的 OData 查询参数）
- 工具的核心必填参数（如 `drive_id`、`item_id`）不受影响
- `$schema` 等 JSON Schema 标准关键字在顶层保留
- 无 `$` 前缀属性的工具不受影响（零开销，返回原始 bytes）

## 7. 复现命令

```bash
# 1. 使用原始失败请求重建并发送（修复前失败，修复后成功）
KIRO_ORIGINAL_REQUEST=/root/src/AIClient-2-API/kiro-debug-31/errors/31b61895-9f69-437a-a4f4-d025f6770b37/request.json \
  go test ./tests/integration/... -v -run TestKiroRebuildAndSend -timeout 120s

# 2. 直接发送 dump 文件（仍会失败，因为 dump 未经 sanitize）
KIRO_REQUEST_FILE=/root/src/AIClient-2-API/kiro-debug-31/errors/31b61895-9f69-437a-a4f4-d025f6770b37/kiro_request.json \
  go test ./tests/integration/... -v -run TestKiroRequestFromFile -timeout 120s

# 3. 单元测试
go test ./tests/unit/... -v -run "Sanitize|NoDollarProps"

# 4. 全部单元测试
go test ./tests/unit/... -count=1
```

## 8. 关联文件

| 文件 | 说明 |
|------|------|
| `internal/kiro/client.go` | Go 修复：`sanitizeToolSchema()` + `sanitizeSchemaProperties()` |
| `src/providers/claude/claude-kiro.js` | Node.js 修复：`sanitizeToolSchema()` |
| `tests/unit/kiro_request_test.go` | 单元测试 |
| `tests/integration/kiro_rebuild_test.go` | 集成测试：从原始请求重建并发送 |
| `tests/integration/kiro_request_file_test.go` | 集成测试：直接发送 dump 文件 |
| `tests/integration/kiro_request_diagnose_test.go` | 诊断测试：工具二分搜索 |
| `tests/integration/send-kiro-request.mjs` | Node.js 集成测试脚本 |
| `kiro-debug-31/errors/31b61895-.../` | 原始失败请求样本 |
