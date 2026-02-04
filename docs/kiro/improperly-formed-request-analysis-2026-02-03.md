# "Improperly formed request" 错误分析报告

**日期**: 2026-02-03
**分析样本**: 799 个失败请求 (从 651MB debug dump 中筛选)

---

## 1. 错误概览

### 今日错误统计 (来自 aiclient-go-kiro-errors.log)

| 指标 | 数值 |
|------|------|
| 总错误数 | 1038 条 |
| "Improperly formed request" | 800 条 (77%) |
| CONTENT_LENGTH_EXCEEDS_THRESHOLD | 210 条 (20%) |
| 其他 (429/5xx/403) | 28 条 (3%) |

### 按模型分布

| 模型 | Improperly formed | Content too long |
|------|-------------------|------------------|
| claude-sonnet-4-5 | 597 | 141 |
| claude-opus-4-5 | 169 | 22 |
| claude-haiku-4-5 | 34 | 47 |

---

## 2. 失败请求特征分析

### 2.1 请求结构 (100 样本统计)

| 特征 | 数量 | 说明 |
|------|------|------|
| `has_history` | 100/100 | 全部有历史消息 |
| `has_tools` | 100/100 | 全部有工具定义 |
| `total_with_tool_results` | 84/100 | 84% 包含 toolResults |
| `assistant_last_in_history` | 100/100 | 历史最后一条都是 assistant |
| `adjacent_same_role` | 0/100 | 没有相邻同角色消息 |

### 2.2 已排除的已知问题

| 已知问题 | 检测结果 | 说明 |
|----------|----------|------|
| 空 tool input (`{}` 或 `null`) | 0/100 | 无此问题 |
| Unicode 转义 (`\u003c` 等) | 0/100 | 无此问题 |
| `$` 前缀属性名 | 0/100 | 无此问题 |
| null inputSchema | 0/100 | 无此问题 |
| thinking blocks | 0/100 | 历史中无 thinking |
| 空 thinking | 0/100 | 无此问题 |
| 编码问题 (BOM/null bytes) | 0/100 | 无此问题 |

### 2.3 潜在问题点

| 问题 | 数量 | 影响 |
|------|------|------|
| 空 tool_result text (`{'text': ''}`) | 6/100 | 可能相关，需进一步验证 |

---

## 3. 请求结构详情

### 3.1 Kiro 请求顶层结构

```json
{
  "conversationState": {
    "chatTriggerType": "MANUAL",
    "conversationId": "uuid",
    "currentMessage": {
      "userInputMessage": {
        "content": "string",
        "modelId": "CLAUDE_SONNET_4_5_20250929_V1_0",
        "origin": "AI_EDITOR",
        "userInputMessageContext": {
          "tools": [...],
          "toolResults": [...]
        }
      }
    },
    "history": [...]
  }
}
```

### 3.2 典型请求规模

| 字段 | 典型值 |
|------|--------|
| history 长度 | 66-156 条消息 |
| tools 数量 | 16-55 个工具 |
| 请求大小 | 0.8MB - 3.2MB |
| 原始 messages | 17-135 条 |

### 3.3 toolResults 结构

```json
{
  "toolResults": [
    {
      "content": [{"text": "..."}],
      "status": "success",
      "toolUseId": "tooluse_xxx"
    }
  ]
}
```

**发现**: 6% 的样本存在空 text 内容 `{"text": ""}`

---

## 4. 历史修复回顾

### 已修复的根因 (按时间倒序)

| 提交 | 根因 | 解决方案 |
|------|------|----------|
| `9bc99fa` | JSON HTML 转义 | `MarshalWithoutHTMLEscape()` |
| `d577ea2` | 内容重复 | 单消息+system 不再重复 |
| `de683c0` | $前缀属性 | `sanitizeToolSchema()` |
| `8dbb1cb` | 孤立 tool_result | 过滤对应的 tool_result |
| `9fd1b6c` | 空 tool input | 过滤 `{}` 和 `null` |
| `c649d10` | null inputSchema | 默认空对象 |
| `7942254` | 缺失 input 字段 | 默认 `{}` |
| `d4e8cb5` | 相邻消息未合并 | 合并同角色消息 |
| `a0ee0fc` | 缺少 tool_result 支持 | 添加完整支持 |
| `2af6932` | 协议不对齐 | 对齐 headers、profileArn |

---

## 5. 待排查方向

### 5.1 高优先级

1. **空 tool_result text**
   - 6% 样本存在 `{"text": ""}`
   - 需验证 Kiro API 是否拒绝空 text

2. **请求大小边界**
   - 部分请求达到 3.2MB
   - 可能存在未报告的 CONTENT_LENGTH 问题

3. **Go vs Node.js 对比**
   - 同一请求发送到 Node.js 是否成功
   - 需要 A/B 测试验证

### 5.2 中优先级

4. **history 结构差异**
   - 全部以 assistant 结尾
   - 检查是否需要额外的 user 消息

5. **toolResults 位置**
   - 当前在 `userInputMessageContext` 中
   - 验证是否应该在其他位置

### 5.3 低优先级

6. **modelId 格式**
   - 当前使用大写格式 `CLAUDE_SONNET_4_5_20250929_V1_0`
   - 验证是否有新的格式要求

---

## 6. 下一步行动

1. **添加空 text 过滤** — 尝试过滤 `{"text": ""}` 的 tool_result content
2. **A/B 测试** — 将失败请求发送到 Node.js 服务对比
3. **启用详细日志** — 在 Kiro API 错误时记录完整请求
4. **联系 Kiro API** — 获取更详细的错误原因

---

## 7. 相关文件

- 错误日志: `/root/aiclient-go-kiro-errors.log`
- Debug dumps: `/root/src/AIClient-2-API/kiro-debug/errors/`
- Go 客户端: `internal/kiro/client.go`
- 历史修复文档: `docs/fixes/improperly-formed-request-fix.md`
