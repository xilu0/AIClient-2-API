# Count Tokens 接口风险分析

本文档分析本服务缺少或不完整实现 `count_tokens` 接口可能带来的风险。

## 1. 接口概述

### 1.1 Claude API 官方接口

**端点**：`POST /v1/messages/count_tokens`

**作用**：在不实际创建消息的情况下，预先计算消息会消耗多少 token。

**官方响应格式**：
```json
{
  "input_tokens": 14,
  "cache_creation_input_tokens": 0,
  "cache_read_input_tokens": 0
}
```

**特点**：
- 完全免费（不产生计费）
- 独立的速率限制
- 支持所有内容类型（文本、图像、PDF、工具等）
- 提供精确的 token 计数

### 1.2 客户端使用场景

Claude Code 等客户端在以下场景调用此接口：

1. **精确上下文管理** - 当本地 tokenizer 估算不够准确时
2. **Autocompact 决策** - 判断是否需要触发压缩
3. **会话恢复** - 计算整个对话历史的 token 数
4. **成本估算** - 预估 API 调用费用

---

## 2. 当前实现状态

| 服务 | `count_tokens` 支持 | 端口 | 实现方式 |
|-----|-------------------|------|---------|
| **Node.js 服务** | ✅ 已实现 | 8080 | 本地 tokenizer 估算 |
| **Go 服务** | ❌ 未实现 | 8081 | 无 |

### 2.1 Node.js 服务实现

Node.js 服务在 `src/providers/claude/claude-kiro.js` 中实现了 `countTokens` 方法：

```javascript
countTokens(requestBody) {
    let totalTokens = 0;

    // 文本：使用本地 tokenizer
    totalTokens += this.countTextTokens(text);

    // 图像：固定估算 1600 tokens
    if (block.type === 'image') {
        totalTokens += 1600;
    }

    // 文档：基于 base64 长度估算
    if (block.type === 'document') {
        const estimatedChars = block.source.data.length * 0.75;
        totalTokens += Math.ceil(estimatedChars / 4);
    }

    return { input_tokens: totalTokens };
}
```

**局限性**：
- 图像固定 1600 tokens（实际可能 500-5000+ tokens）
- 文档基于字节数估算，不考虑内容复杂度
- 不返回 `cache_creation_input_tokens` 和 `cache_read_input_tokens`

### 2.2 Go 服务状态

Go 服务（端口 8081）**完全没有实现** `count_tokens` 端点。

---

## 3. 风险场景分析

### 3.1 请求流程

```
Claude Code 客户端
    │
    ├─ 本地 tokenizer 估算: 180k tokens
    │
    ├─ 调用 count_tokens API 确认
    │       │
    │       ├─ Node.js 服务 → 返回本地估算值
    │       │
    │       └─ Go 服务 → 返回 404 错误
    │                         ↓
    │                 客户端回退到本地估算
    │
    └─ 发送实际请求 ──→ Kiro API
                            ↓
                    实际 token 数可能超限
                            ↓
                    请求失败或被截断
```

### 3.2 具体风险场景

#### 场景 1：包含图像的请求

```
本地估算：文本 50k + 图像估算 1.6k = 51.6k tokens
实际情况：文本 50k + 图像实际 15k = 65k tokens

误差：13.4k tokens（26%）
```

**风险**：
- 客户端认为有足够空间，实际可能接近或超过限制
- Autocompact 可能延迟触发

#### 场景 2：包含 PDF 文档的请求

```
本地估算：基于 base64 字节数 / 4
实际情况：PDF 内容复杂度、图表、格式等影响 token 数

误差：可能高达 50%+
```

#### 场景 3：Go 服务无 count_tokens

```
客户端请求 → Go 服务 /v1/messages/count_tokens
                  ↓
             404 Not Found
                  ↓
         客户端完全依赖本地估算
                  ↓
         所有估算误差风险叠加
```

### 3.3 风险等级评估

| 风险场景 | Go 服务 | Node.js 服务 | 严重程度 |
|---------|--------|-------------|---------|
| 纯文本请求 | 中（回退本地估算） | 低（本地 tokenizer 准确） | **低** |
| 包含图像 | 高（无法估算） | 中（固定 1600 估算不准） | **高** |
| 包含 PDF | 高（无法估算） | 中（字节估算不准） | **高** |
| 大量工具定义 | 高 | 低（可以计算） | **中** |
| Prompt Caching | 高（无缓存 token 信息） | 高（不返回缓存字段） | **中** |
| Autocompact 决策 | 高（无精确数据） | 中（估算可能不准） | **中** |

---

## 4. 潜在影响

### 4.1 用户体验影响

| 影响 | 描述 | 严重程度 |
|-----|------|---------|
| **请求失败** | 上下文超限导致 400 错误 | 高 |
| **响应截断** | 模型输出不完整 | 中 |
| **Autocompact 失效** | 压缩触发时机不准确 | 中 |
| **成本估算不准** | 用户无法准确预估费用 | 低 |

### 4.2 最坏情况

```
用户使用 Claude Code 通过 Go 服务
    ↓
发送包含多张图像的请求
    ↓
本地估算：100k tokens（图像按 1.6k 估算）
实际情况：150k tokens（图像实际 10k+ 每张）
    ↓
Kiro 上下文窗口：172.5k tokens
    ↓
用户继续添加内容，认为还有 72.5k 空间
    ↓
实际只有 22.5k 空间
    ↓
请求突然失败，用户困惑
```

---

## 5. 缓解因素

### 5.1 Claude Code 的容错机制

1. **Autocompact Buffer**
   - 预留 ~33k tokens（16.5%）作为缓冲
   - 可以吸收部分估算误差

2. **回退到本地估算**
   - 如果 API 不可用，使用本地 tokenizer
   - 对纯文本内容仍然有效

3. **错误重试**
   - 遇到 400 错误时可能重试其他账号
   - 但不能解决根本问题

### 5.2 局限性

- Buffer 可能不足以覆盖大的估算误差（如多图像场景）
- 本地估算对复杂内容（图像、PDF）误差大
- 重试不能解决上下文超限问题

---

## 6. 本地 Tokenizer vs API Token Counting

| 维度 | 本地 Tokenizer | API Token Counting |
|------|----------------|-------------------|
| **文本准确性** | 高（使用官方 tokenizer） | 精确 |
| **图像准确性** | 低（固定估算） | 精确 |
| **PDF 准确性** | 低（字节估算） | 精确 |
| **工具定义** | 中（可计算） | 精确 |
| **Prompt Caching** | 无法计算 | 精确 |
| **系统优化 token** | 无法预测 | 包含在内 |
| **延迟** | 毫秒级 | 100-500ms |
| **成本** | 无 | 免费 |

---

## 7. 建议措施

### 7.1 短期措施（Go 服务）

**优先级：高**

1. 实现基本的 `/v1/messages/count_tokens` 端点
2. 使用与 Node.js 一致的本地估算逻辑
3. 返回标准响应格式：
   ```json
   {
     "input_tokens": 1234,
     "cache_creation_input_tokens": 0,
     "cache_read_input_tokens": 0
   }
   ```

### 7.2 中期措施（改进估算）

**优先级：中**

1. 改进图像 token 估算算法
   - 基于图像尺寸和格式估算
   - 参考 Claude 官方文档的图像 token 计算规则

2. 改进 PDF token 估算
   - 考虑页数、内容复杂度
   - 提取文本后使用 tokenizer 计算

### 7.3 长期措施（精确计算）

**优先级：低**

1. 检查 Kiro API 是否支持 token counting
2. 如果支持，转发请求获取精确值
3. 实现缓存机制，减少重复计算

---

## 8. 实现建议

### 8.1 Go 服务 count_tokens 端点

```go
// POST /v1/messages/count_tokens
func (h *MessagesHandler) HandleCountTokens(w http.ResponseWriter, r *http.Request) {
    var req claude.MessageRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        // 返回错误
    }

    // 使用本地估算
    inputTokens := claude.EstimateInputTokens(&req)

    // 返回响应
    response := map[string]int{
        "input_tokens": inputTokens,
        "cache_creation_input_tokens": 0,
        "cache_read_input_tokens": 0,
    }
    json.NewEncoder(w).Encode(response)
}
```

### 8.2 图像 Token 估算改进

参考 Claude 官方文档，图像 token 计算大致为：

```
tokens = (width * height) / 750
```

建议实现：
```go
func EstimateImageTokens(width, height int) int {
    // 基于图像尺寸估算
    tokens := (width * height) / 750
    // 最小 100，最大 5000
    if tokens < 100 {
        tokens = 100
    }
    if tokens > 5000 {
        tokens = 5000
    }
    return tokens
}
```

---

## 9. 总结

### 9.1 当前风险

| 问题 | 风险等级 | 影响范围 |
|-----|---------|---------|
| Go 服务无 count_tokens | **高** | 所有 Go 服务用户 |
| 图像 token 估算不准 | **高** | 包含图像的请求 |
| PDF token 估算不准 | **中** | 包含 PDF 的请求 |
| 无 Prompt Caching 信息 | **低** | 使用缓存的请求 |

### 9.2 建议优先级

1. **立即**：为 Go 服务实现 count_tokens 端点（本地估算）
2. **短期**：改进图像 token 估算算法
3. **中期**：改进 PDF token 估算算法
4. **长期**：探索 Kiro API 的 token counting 支持

---

## 参考资料

- [Claude API - Token Counting](https://docs.anthropic.com/en/api/counting-tokens)
- [Claude API - Vision](https://docs.anthropic.com/en/docs/build-with-claude/vision)
- [Claude Code - How It Works](https://docs.anthropic.com/en/docs/claude-code/how-claude-code-works)
