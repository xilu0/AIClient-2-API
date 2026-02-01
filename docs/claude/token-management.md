# Claude Code CLI Token 计算与限制机制

本文档详细说明 Claude Code CLI 工具如何计算 token 数量，以及如何确保请求不超过上下文窗口限制。

## 1. Token 计算机制

### 1.1 官方 Tokenizer

Claude Code 使用 **Anthropic 官方 tokenizer**（`@anthropic-ai/tokenizer`）进行精确计算：

```javascript
import { countTokens } from '@anthropic-ai/tokenizer';
```

**关键特性**：
- 使用 `countTokens()` 函数进行精确计算
- 不是估算，而是基于 Claude 模型实际使用的 tokenizer
- 与 Claude API 服务端使用的 tokenizer 完全一致

### 1.2 缓存优化策略

为避免高并发下重复调用 tokenizer 阻塞事件循环，Claude Code 实现了 **LRU 缓存机制**：

**缓存特性**：
- **LRU Cache**：最近最少使用缓存，默认容量 1000-2000 条记录
- **智能键生成**：
  - 短文本（≤200 字符）直接使用文本作为键
  - 长文本使用采样策略生成键（首尾各 50 字符 + 中间采样）
- **缓存统计**：追踪命中率、命中数、未命中数

**代码示例**：
```javascript
export class TokenCache {
    constructor(options = {}) {
        this.maxSize = options.maxSize || 2000;
        this.maxKeyLength = options.maxKeyLength || 200;
        this.cache = new LRUCache(this.maxSize);
        this.hits = 0;
        this.misses = 0;
    }

    count(text) {
        if (!text) return 0;
        const key = this._getCacheKey(text);
        const cached = this.cache.get(key);

        if (cached !== undefined) {
            this.hits++;
            return cached;
        }

        this.misses++;
        try {
            const count = countTokens(text);
            this.cache.set(key, count);
            return count;
        } catch (error) {
            // 降级到估算：约 1 token ≈ 4 字符
            const estimate = Math.ceil(text.length / 4);
            this.cache.set(key, estimate);
            return estimate;
        }
    }
}
```

### 1.3 批量 Token 计数

Claude Code 提供了高效的批量计数函数，用于处理多个文本：

```javascript
export function countTokensBatch(texts) {
    // 去重处理：相同文本只计算一次
    const uniqueTexts = new Map();

    // 收集唯一文本并追踪位置
    for (let i = 0; i < texts.length; i++) {
        const text = texts[i];
        if (uniqueTexts.has(text)) {
            uniqueTexts.get(text).push(i);
        } else {
            uniqueTexts.set(text, [i]);
        }
    }

    // 每个唯一文本只计算一次，然后填充结果
    for (const [text, indices] of uniqueTexts) {
        const count = globalCache.count(text);
        for (const idx of indices) {
            results[idx] = count;
        }
    }

    return results;
}
```

**优势**：自动去重，避免重复计算相同文本的 token 数

---

## 2. 上下文窗口管理

### 2.1 上下文窗口限制

不同模型有不同的上下文窗口大小：

| 模型 | 上下文窗口 |
|-----|----------|
| Claude Haiku | 200K tokens |
| Claude Sonnet | 200K tokens |
| Claude Opus | 200K tokens |
| 扩展上下文 (beta) | 1M tokens |

**API Beta 版本**：
```
- "token-counting-2024-11-01"          # Token 计数 API
- "model-context-window-exceeded-2025-08-26"  # 上下文窗口超限处理
- "context-1m-2025-08-07"              # 1M token 上下文支持
- "context-management-2025-06-27"      # 上下文管理功能
```

### 2.2 自动上下文压缩机制

当上下文接近限制时，Claude Code 会自动执行压缩：

**压缩策略**（按优先级）：
1. **清除旧工具输出**：首先删除较早的命令执行结果
2. **对话摘要**：如果仍需空间，对早期对话进行摘要
3. **保留关键内容**：用户请求和关键代码片段被保留
4. **丢弃详细指令**：早期对话中的详细指令可能被丢弃

> "Claude Code manages context automatically as you approach the limit. It clears older tool outputs first, then summarizes the conversation if needed. Your requests and key code snippets are preserved; detailed instructions from early in the conversation may be lost."

### 2.3 上下文监控命令

Claude Code 提供了实时上下文监控工具：

```bash
# 查看当前上下文使用情况
/context

# 查看 MCP 服务器的上下文成本
/mcp

# 自定义压缩指令
/compact focus on code samples and API usage
```

**上下文成本分析**：

| 功能 | 加载时机 | 上下文成本 |
|------|--------|---------|
| CLAUDE.md | 会话启动 | 每个请求都包含 |
| Skills | 启动时加载描述，使用时加载完整内容 | 低（仅描述） |
| MCP 服务器 | 会话启动 | 每个请求都包含所有工具定义 |
| Subagents | 按需生成 | 隔离上下文，不影响主会话 |
| Hooks | 触发时运行 | 零成本（外部运行） |

---

## 3. 请求大小限制

### 3.1 消息限制

**API 限制**（来自 Claude Messages API 文档）：
- **单个请求中的消息数**：最多 100,000 条消息
- **单个消息内容**：可以是字符串或内容块数组
- **max_tokens 参数**：指定生成前停止的最大 token 数

```javascript
// API 请求示例
{
    "max_tokens": 4096,  // 最大输出 token 数
    "messages": [
        {"role": "user", "content": "..."},
        {"role": "assistant", "content": "..."}
    ]
}
```

### 3.2 Extended Thinking Token 预算

当启用 Extended Thinking（扩展思考）时：

```javascript
{
    "thinking": {
        "type": "enabled",
        "budget_tokens": 31999  // 默认预算
    }
}
```

**限制**：
- 最小预算：1,024 tokens
- 最大预算：必须小于 `max_tokens`
- 计费：thinking tokens 作为输出 token 计费

---

## 4. 成本管理和监控

### 4.1 成本追踪命令

```bash
# 查看当前会话的 token 使用和成本
/cost

# 输出示例
Total cost:            $0.55
Total duration (API):  6m 19.7s
Total duration (wall): 6h 33m 10.2s
Total code changes:    0 lines added, 0 lines removed
```

### 4.2 成本优化策略

**官方推荐的 token 使用优化方法**：

1. **主动管理上下文**
   - 使用 `/clear` 在任务间清除过期上下文
   - 使用 `/rename` 和 `/resume` 管理会话

2. **选择合适的模型**
   - Sonnet：成本低，适合大多数任务
   - Opus：复杂推理任务
   - Haiku：简单子任务

3. **减少 MCP 服务器开销**
   - 每个 MCP 服务器在每个请求中都添加工具定义
   - 禁用未使用的服务器
   - 使用 CLI 工具替代 MCP（更高效）

4. **使用 Skills 和 Subagents**
   - Skills 按需加载，减少基础上下文
   - Subagents 隔离上下文，避免主会话膨胀

5. **Prompt Caching**
   - Claude Code 自动使用 prompt caching 减少重复内容成本
   - 系统提示等重复内容被缓存

### 4.3 团队级别的速率限制建议

| 团队规模 | 每用户 TPM | 每用户 RPM |
|---------|-----------|-----------|
| 1-5 用户 | 200k-300k | 5-7 |
| 5-20 用户 | 100k-150k | 2.5-3.5 |
| 20-50 用户 | 50k-75k | 1.25-1.75 |
| 50-100 用户 | 25k-35k | 0.62-0.87 |
| 100-500 用户 | 15k-20k | 0.37-0.47 |
| 500+ 用户 | 10k-15k | 0.25-0.35 |

---

## 5. 本项目 Go 服务的 Token 计算

### 5.1 Kiro API 返回的 Token 数据

Kiro API 返回两种 token 相关数据：

```go
// Usage information (can be a number or object)
Usage json.RawMessage `json:"usage,omitempty"`

// Context usage percentage - sent at end of stream
ContextUsagePercentage *float64 `json:"contextUsagePercentage,omitempty"`
```

- **`usage`**：可能是数字或对象，包含 `input_tokens` 和 `output_tokens`
- **`contextUsagePercentage`**：上下文使用百分比（在流结束时发送）

### 5.2 输入 Token 计算（三种来源，按优先级）

**优先级 1：基于 `contextUsagePercentage` 计算**（最准确）
```go
func CalculateInputTokensFromPercentage(percentage float64, outputTokens int) int {
    totalTokens := int(float64(TotalContextTokens) * percentage / 100)  // TotalContextTokens = 172500
    inputTokens := totalTokens - outputTokens
    return inputTokens
}
```
公式：`inputTokens = (172500 * percentage / 100) - outputTokens`

**优先级 2：使用 Kiro 返回的 `usage.input_tokens`**（如果有）

**优先级 3：基于字符估算**（兜底方案）
```go
func EstimateInputTokens(req *MessageRequest) int {
    // 统计 system prompt + messages 的字符数
    // 转换：tokens = totalChars / 4  (CharsPerToken = 4)
}
```

### 5.3 输出 Token 计算

**优先级 1：使用 Kiro 返回的 `usage.output_tokens`**

**优先级 2：基于累积输出文本估算**
```go
func CountTextTokens(text string) int {
    tokens := len(strings.TrimSpace(text)) / 4  // 4 字符 ≈ 1 token
    return tokens
}
```

### 5.4 Token 分配策略（1:2:25 比例）

本项目实现了特殊的 token 分配策略，用于模拟 Claude API 的缓存 token 计费：

```go
func DistributeTokens(inputTokens int) TokenUsage {
    if inputTokens < 100 {
        return TokenUsage{InputTokens: inputTokens}
    }

    const totalParts = 28 // 1 + 2 + 25

    input := inputTokens * 1 / totalParts
    creation := inputTokens * 2 / totalParts
    read := inputTokens - input - creation

    return TokenUsage{
        InputTokens:              input,
        CacheCreationInputTokens: creation,
        CacheReadInputTokens:     read,
    }
}
```

**分配示例**（1000 tokens）：
- `input_tokens`：1000 × 1/28 = 35 tokens
- `cache_creation_input_tokens`：1000 × 2/28 = 71 tokens
- `cache_read_input_tokens`：1000 × 25/28 = 894 tokens

**阈值**：低于 100 tokens 时不进行分配

---

## 6. 总结

### Claude Code CLI Token 管理机制

| 机制 | 作用 |
|-----|-----|
| 官方 Tokenizer | 精确计算，与服务端一致 |
| LRU 缓存 | 避免重复计算，提高性能 |
| 自动压缩 | 接近限制时自动清理/摘要 |
| 实时监控 | `/context` 命令查看使用情况 |
| 分层加载 | Skills/MCP 按需加载，减少基础开销 |

### 本项目 Go 服务 Token 计算

| 数据来源 | 输入 Token | 输出 Token |
|---------|-----------|-----------|
| Kiro API 直接返回 | `usage.input_tokens` | `usage.output_tokens` |
| Kiro API 间接返回 | `contextUsagePercentage` 计算 | - |
| 本地估算（兜底） | 字符数 / 4 | 字符数 / 4 |

---

## 参考资料

- [Claude Code 官方文档 - Costs](https://docs.anthropic.com/en/docs/claude-code/costs)
- [Claude Code 官方文档 - How Claude Code Works](https://docs.anthropic.com/en/docs/claude-code/how-claude-code-works)
- [Claude API - Messages](https://docs.anthropic.com/en/api/messages)
- [Anthropic Tokenizer NPM](https://www.npmjs.com/package/@anthropic-ai/tokenizer)
