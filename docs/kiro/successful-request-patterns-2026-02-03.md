# Kiro API 成功请求特征分析

**日期**: 2026-02-03
**数据来源**:
- Node.js 成功样本: `/root/src/AIClient-2-API/kiro-debug-js/success/` (19 个)
- Go 成功样本: `/root/kiro-debug/success/` (27 个)
- Go 失败样本: `/root/src/AIClient-2-API/kiro-debug/errors/` (799 个)

---

## 0. 根因分析结论

### 主要根因：History 中的空 tool input (71%)

| 样本类型 | 有空 tool input | 比例 |
|----------|-----------------|------|
| **Go 成功** | 0/27 | **0%** |
| **Go 失败** | 567/799 | **71%** |

**位置**: `history[].assistantResponseMessage.toolUses[].input`

**问题示例**:
```json
{
  "assistantResponseMessage": {
    "content": "...",
    "toolUses": [
      {
        "name": "Task",
        "toolUseId": "tooluse_xxx",
        "input": {}  // ← 空对象，导致 Kiro 拒绝
      }
    ]
  }
}
```

**根本原因**: Go 服务在构建 history 时，没有过滤掉空 input 的 toolUses。之前的修复 (commit `9fd1b6c`) 只处理了 currentMessage 中的 toolUse，没有处理 history。

### 次要根因：待进一步分析 (29%)

剩余 232 个失败样本没有空 tool input，可能原因：
- 请求过大（部分超过 3MB）
- 其他未知校验规则

---

## 1. 核心发现

### 成功 vs 失败对比

| 指标 | 成功 (Node.js) | 失败 (Go) | 差异倍数 |
|------|----------------|-----------|----------|
| **文件大小** | 0.2KB - 121KB | 74KB - 3216KB | 失败大 26x |
| **History 长度** | 0 - 12 条 | 6 - 162 条 | 失败长 13x |
| **有 ToolResults** | 5.3% (1/19) | 85% (170/200) | 失败复杂 16x |
| **History > 20** | 0% | 90% | 长对话全部失败 |

### 关键结论

> **成功的请求都是简单、短小的；失败的请求都是复杂、长大的。**

---

## 2. 成功请求特征

### 2.1 请求规模

```
File size:    0.2KB - 120.8KB (最大 121KB)
History:      0 - 12 条消息 (最大 12 条)
Tools:        0 - 19 个
ToolResults:  0 - 1 个
Content:      8 - 19762 字符
```

### 2.2 结构特征

- **chatTriggerType**: `MANUAL`
- **origin**: `AI_EDITOR`
- **modelId 格式**: 混合 (大写 `CLAUDE_OPUS_4_5_20251101_V1_0` 或小写 `claude-opus-4.5`)
- **assistantResponseMessage.content**: 字符串类型
- **assistantResponseMessage.toolUses**: 存在，包含 `input`, `name`, `toolUseId`

### 2.3 所有成功样本详情

| 样本 ID | 大小 | 模型 | History | Tools | ToolResults |
|---------|------|------|---------|-------|-------------|
| f8b248da | 121KB | opus-4-5 | 4 | 19 | 0 |
| 8b459e59 | 106KB | opus-4-5 | 2 | 19 | 0 |
| 2b637df5 | 88KB | opus-4-5 | 12 | 19 | 0 |
| ef183f0d | 87KB | opus-4-5 | 10 | 19 | 0 |
| ad48e5e3 | 87KB | opus-4-5 | 4 | 19 | 0 |
| 35e9e9f2 | 86KB | opus-4-5 | 8 | 19 | 0 |
| 6b441b4f | 86KB | opus-4-5 | 6 | 19 | 0 |
| ea8402a3 | 86KB | opus-4-5 | 2 | 19 | 0 |
| a6a87814 | 85KB | opus-4-5 | 2 | 19 | **1** |
| 31d4bc22 | 85KB | opus-4-5 | 2 | 19 | 0 |
| cf51b5ba | 1.3KB | haiku-4-5 | 2 | 0 | 0 |
| f6b53be4 | 1.0KB | sonnet-4-5 | 2 | 0 | 0 |
| 606d321a | 0.8KB | haiku-4-5 | 2 | 0 | 0 |
| 其他 6 个 | 0.2KB | 各种 | 0 | 0 | 0 |

### 2.4 唯一带 ToolResults 的成功样本 (a6a87814)

```json
{
  "toolResults": [
    {
      "content": [{"text": "No tasks found"}],
      "status": "success",
      "toolUseId": "tooluse_urB6TC8ETRS8DbP07m7nNg"
    }
  ]
}
```

**特点**:
- 只有 1 个 toolResult
- text 内容非空 ("No tasks found")
- History 只有 2 条

---

## 3. 失败请求特征

### 3.1 请求规模

```
File size:    74KB - 3216KB (最大 3.2MB)
History:      6 - 162 条消息
Tools:        16 - 61 个
ToolResults:  0 - 4 个
Content:      1 - 3716 字符
```

### 3.2 已排除的问题

| 检查项 | 结果 | 说明 |
|--------|------|------|
| 空 tool input (`{}`) | 0/100 | 无此问题 |
| Unicode 转义 | 0/100 | 无此问题 |
| `$` 前缀属性 | 0/100 | 无此问题 |
| null inputSchema | 0/100 | 无此问题 |
| 空 toolResult text | 26/799 (3.3%) | 少数存在 |

### 3.3 典型失败样本结构

```json
{
  "conversationState": {
    "chatTriggerType": "MANUAL",
    "conversationId": "uuid",
    "currentMessage": {
      "userInputMessage": {
        "content": "...",
        "modelId": "CLAUDE_SONNET_4_5_20250929_V1_0",
        "origin": "AI_EDITOR",
        "userInputMessageContext": {
          "toolResults": [...],  // 1-4 个
          "tools": [...]         // 16-61 个
        }
      }
    },
    "history": [...]  // 6-162 条
  }
}
```

---

## 4. 结构对比

### 4.1 请求顶层结构 (相同)

```
conversationState
├── chatTriggerType: "MANUAL"
├── conversationId: uuid
├── currentMessage
│   └── userInputMessage
│       ├── content
│       ├── modelId
│       ├── origin
│       └── userInputMessageContext
│           ├── tools
│           └── toolResults
└── history[]
```

### 4.2 History 消息结构 (相同)

```
history[i]
├── userInputMessage
│   ├── content
│   ├── modelId
│   ├── origin
│   └── userInputMessageContext (可选)
└── assistantResponseMessage
    ├── content (字符串)
    └── toolUses (可选)
```

### 4.3 toolUses 结构 (相同)

```json
{
  "toolUses": [
    {
      "input": {...},
      "name": "ToolName",
      "toolUseId": "tooluse_xxx"
    }
  ]
}
```

---

## 5. Go 服务成功样本分析

### 关键发现：Go 服务可以成功处理复杂请求

| 样本 | 大小 | History | Tools | ToolResults | ToolUse 历史 |
|------|------|---------|-------|-------------|--------------|
| 43c637f2 | 554KB | 268 | 21 | 0 | 123 |
| 04a860a8 | 553KB | 268 | 21 | 0 | 123 |
| 380b0d9a | 551KB | 266 | 21 | **3** | 123 |
| 4d2061b7 | 518KB | 264 | 21 | 1 | 120 |
| 49ab42c7 | 475KB | 270 | 1 | 1 | 124 |

**结论**: 大请求、长历史、多 toolResults 都可以成功，**关键是 history 中不能有空 tool input**。

### 成功样本共性

- **空 tool input**: 0 个（全部成功样本都没有）
- **Unicode 转义**: 存在（不影响成功）
- **最大请求**: 554KB
- **最长历史**: 270 条
- **最多 ToolResults**: 3 个
- **最多 ToolUse 历史**: 124 个

---

## 6. 修复建议

### 已实现修复 v2：智能过滤 history 中的空 tool input（保护 toolResult 引用）

**文件**: `internal/kiro/client.go`

**实现方案**: 根据工具定义和 toolResult 引用智能过滤

```go
// 1. 收集所有被 toolResult 引用的 toolUseId（必须保留）
referencedToolUseIds := make(map[string]bool)
for _, tr := range msg.UserContent.ToolResults {
    if id, ok := tr["toolUseId"].(string); ok && id != "" {
        referencedToolUseIds[id] = true
    }
}

// 2. 获取不需要参数的工具列表
func getToolsWithNoRequiredParams(toolsJSON []byte) map[string]bool

// 3. 过滤空 input（保留无参数要求的工具，保留被 toolResult 引用的工具）
func filterToolUsesWithEmptyInput(
    toolUses []map[string]interface{},
    noRequiredParams map[string]bool,
    referencedByToolResult map[string]bool  // 新增参数
) []map[string]interface{}

// 4. 在构建 history 时应用过滤
filteredToolUses := filterToolUsesWithEmptyInput(msg.AssistantContent.ToolUses, toolsNoRequiredParams, referencedToolUseIds)
```

**逻辑**（按优先级）:
1. **被 toolResult 引用**: 必须保留（避免孤儿 toolResult 导致请求失败）
2. **无 `required` 字段的工具**: 空 input 被保留（如 TaskList）
3. **有 `required` 字段且未被引用**: 空 input 被过滤（如 Read, Task）

### v1 修复导致的问题（已解决）

v1 版本只考虑了工具是否需要参数，没有考虑 toolResult 引用关系，导致：
- 过滤掉被 toolResult 引用的 toolUse
- 创建孤儿 toolResult（toolResult 引用不存在的 toolUseId）
- Kiro API 拒绝：`"Improperly formed request"`

**问题样本分析** (34 个新错误):
- 7 个 (25%) 有孤儿 toolResult（v1 bug 导致）
- 15 个 (44%) 无空 input，原因待分析
- 6 个 (18%) 无 toolResults
- 6 个 (18%) session 超时

### 验证方法

1. 单元测试已通过
2. 部署后重放失败样本，预期孤儿 toolResult 错误消失
3. 继续分析剩余 ~29% 的预存错误

---

## 7. 相关文件

- **Go 成功样本**: `/root/kiro-debug/success/` (27 个)
- **Node.js 成功样本**: `/root/src/AIClient-2-API/kiro-debug-js/success/` (19 个)
- **Go 失败样本**: `/root/src/AIClient-2-API/kiro-debug/errors/` (799 个)
- **错误日志**: `/root/aiclient-go-kiro-errors.log`
- **Go 客户端**: `internal/kiro/client.go`
- **Node.js 客户端**: `src/providers/claude/claude-kiro.js`
- **历史修复文档**: `docs/fixes/improperly-formed-request-fix.md`

---

## 8. 附录：错误分布统计

### 空 tool input 分布

```
总失败样本:     799
有空 tool input: 567 (71%)
无空 tool input: 232 (29%)

空 input 数量分布:
  最小: 1
  最大: 75
  平均: 6.1
```

### 请求大小分布

```
成功样本: 0.2KB - 554KB
失败样本: 73KB - 3216KB
```

### 模型分布

```
claude-sonnet-4-5: 74% 的失败
claude-opus-4-5:   21% 的失败
claude-haiku-4-5:  5% 的失败
```
