# Claude Code 自动压缩机制

本文档详细说明 Claude Code CLI 的自动压缩（autocompact）机制的工作原理。

## 核心结论

**自动压缩完全是客户端行为，Claude API 不参与压缩决策，也没有返回字段来控制压缩。**

---

## 1. 触发机制

### 1.1 自动触发

当上下文窗口接近容量限制时自动触发（默认约 95% 容量）。

| 触发方式 | 条件 | 控制方式 |
|---------|------|---------|
| 自动触发 | 上下文 ≥ 95% 容量 | `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE` 环境变量 |
| 手动触发 | 用户执行 `/compact` | 命令行 |

### 1.2 环境变量配置

```bash
# 在 50% 容量时触发压缩（更激进）
CLAUDE_AUTOCOMPACT_PCT_OVERRIDE=50 claude

# 在 80% 容量时触发压缩
CLAUDE_AUTOCOMPACT_PCT_OVERRIDE=80 claude
```

### 1.3 手动触发

用户可以通过 `/compact` 命令手动触发压缩：

```bash
/compact                           # 标准压缩
/compact focus on API changes      # 带自定义指令的压缩
```

---

## 2. 压缩原理

### 2.1 执行位置

**完全在客户端本地处理，不调用 API。**

- **位置**：Claude Code CLI 客户端
- **不调用 API**：压缩过程不需要 API 参与
- **本地优化**：基于本地上下文窗口管理进行

### 2.2 分层清理策略

Claude Code 采用分层清理策略，按优先级清理上下文：

```
上下文接近满载
    ↓
[第一层] 清除旧工具输出（命令执行结果）
    ↓ (如果还不够)
[第二层] 摘要化早期对话
    ↓ (如果还不够)
[第三层] 应用自定义指令优先级
    ↓
压缩完成，继续会话
```

### 2.3 保留优先级

| 优先级 | 内容 | 处理方式 |
|-------|------|---------|
| 最高 | 用户请求和关键代码 | ✅ 保留 |
| 高 | 最近的对话上下文 | ✅ 保留 |
| 中 | 工具执行结果 | ⚠️ 可能被清理 |
| 低 | 早期的详细指令 | ❌ 可能被摘要 |

### 2.4 自定义指令的作用

```bash
/compact focus on code samples and API usage
```

这会告诉压缩算法：
- 优先保留代码示例
- 优先保留 API 使用说明
- 可以删除其他详细信息

---

## 3. Claude API 与压缩的关系

### 3.1 API 响应中没有压缩控制字段

**Claude API 本身没有返回字段来控制或触发压缩。**

API 只返回 token 使用量：

```json
{
  "usage": {
    "input_tokens": 36000,
    "output_tokens": 1500
  }
}
```

### 3.2 压缩完全由客户端负责

1. 客户端使用 tokenizer 计算当前上下文大小
2. 客户端判断是否达到阈值
3. 客户端执行压缩逻辑
4. 压缩后的上下文发送给 API

### 3.3 Subagent 压缩事件日志

在 subagent 的压缩事件日志中会记录压缩元数据：

```json
{
  "type": "system",
  "subtype": "compact_boundary",
  "compactMetadata": {
    "trigger": "auto",           // "auto" 或 "manual"
    "preTokens": 167189          // 压缩前的 token 数
  }
}
```

---

## 4. Autocompact Buffer 的作用

### 4.1 Buffer 的核心功能

从 `/context` 输出可以看到：

```
Autocompact buffer: 33.0k tokens (16.5%)
```

这是**预留的缓冲区**，用于：

1. **容量监控**：持续跟踪当前上下文使用量
2. **阈值管理**：当达到阈值时触发压缩
3. **缓冲作用**：
   - 防止上下文突然溢出
   - 给压缩过程留出时间
   - 确保有足够空间生成回复

### 4.2 监控命令

```bash
# 查看当前上下文使用
/context

# 查看成本和 token 使用
/cost

# 手动触发压缩
/compact focus on recent changes
```

---

## 5. `/compact` 命令工作原理

### 5.1 命令语法

```bash
/compact [instructions]
```

### 5.2 执行流程

1. **触发 PreCompact 钩子**
   - 钩子事件：`PreCompact`
   - 可以通过 matcher 区分触发源：
     - `manual` - 用户手动调用 `/compact`
     - `auto` - 自动压缩触发

2. **接收自定义指令**
   - 用户可以提供压缩焦点
   - 指令被编码到 `PreCompact` 钩子输入的 `custom_instructions` 字段

3. **执行压缩**
   - 应用分层清理策略
   - 根据自定义指令优先保留特定内容
   - 生成压缩摘要

4. **会话恢复**
   - 压缩后会话继续
   - 新消息追加到压缩后的上下文
   - 完整的对话历史保存在本地会话文件中

### 5.3 PreCompact 钩子输入示例

```json
{
  "session_id": "abc123",
  "transcript_path": "~/.claude/projects/.../session.jsonl",
  "permission_mode": "default",
  "hook_event_name": "PreCompact",
  "trigger": "manual",
  "custom_instructions": "focus on API changes"
}
```

---

## 6. 压缩触发条件总结

| 条件 | 触发方式 | 控制方式 |
|------|---------|---------|
| **自动触发** | 上下文 ≥ 95% 容量 | `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE` |
| **手动触发** | 用户执行 `/compact` | 命令行 |
| **Subagent 自动压缩** | Subagent 上下文满 | 同样的环境变量 |
| **SessionStart 后** | 恢复会话时 | 自动应用 |

---

## 7. 对代理服务的启示

### 7.1 代理无法控制压缩

本项目（Go 服务）作为代理，**无法控制客户端的压缩行为**，因为：

- 代理只转发请求，不管理对话历史
- 每个请求都是独立的，代理不知道完整上下文
- 压缩决策应该由调用方（如 Claude Code）做出

### 7.2 如果要实现类似功能

如果想在客户端实现类似的压缩功能，需要：

1. **客户端实现**：压缩逻辑必须在客户端（调用方）实现
2. **Token 计算**：使用 tokenizer 精确计算上下文大小
3. **阈值监控**：在发送请求前检查是否接近限制
4. **摘要策略**：实现对话历史的摘要算法

---

## 8. 实际应用建议

### 8.1 优化压缩效果

在 `CLAUDE.md` 中添加压缩指令：

```markdown
# Compact instructions
When you are using compact, please focus on test output and code changes
```

### 8.2 预防压缩丢失信息

- 将持久规则放在 `CLAUDE.md` 中（不会被压缩）
- 使用 subagent 隔离高输出量操作
- 定期使用 `/clear` 清理无关上下文
- 为重要信息添加自定义压缩指令

---

## 9. 总结

| 问题 | 答案 |
|-----|-----|
| 压缩是谁触发的？ | 客户端（Claude Code CLI） |
| API 能控制压缩吗？ | 不能，API 不参与 |
| 压缩在哪里执行？ | 客户端本地 |
| 代理能实现压缩吗？ | 不能，代理不管理对话历史 |
| Buffer 的作用？ | 容量监控和阈值管理 |
| 压缩算法？ | 分层清理 + 摘要化 |

---

## 参考资料

- [Claude Code 官方文档 - How Claude Code Works](https://docs.anthropic.com/en/docs/claude-code/how-claude-code-works)
- [Claude Code 官方文档 - Hooks](https://docs.anthropic.com/en/docs/claude-code/hooks)
- [Claude Code 官方文档 - Costs](https://docs.anthropic.com/en/docs/claude-code/costs)
