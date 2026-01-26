# Storage Adapter 严格模式

## 概述

从提交 `fce6b27` 开始，系统支持通过环境变量强制要求 storage adapter，并在降级到直接文件操作时打印醒目的警告。

## 功能

### 1. 严格模式（Strict Mode）

通过环境变量 `REQUIRE_STORAGE_ADAPTER=true` 启用严格模式，强制要求 storage adapter 必须可用。

**启用方式**：
```bash
# 通过环境变量
export REQUIRE_STORAGE_ADAPTER=true
npm start

# 或通过 Docker Compose
environment:
  - REQUIRE_STORAGE_ADAPTER=true
  - REDIS_ENABLED=true
  - REDIS_URL=redis://redis:6379
```

**行为**：
- ✅ 如果 storage adapter 正常初始化：系统正常运行
- ❌ 如果 storage adapter 初始化失败：立即抛出错误，阻止请求处理

**错误信息**：
```
Error: [UI API] Storage adapter is required but not initialized.
Set REQUIRE_STORAGE_ADAPTER=false to allow file fallback.
```

### 2. 降级警告（Fallback Warnings）

当系统降级到直接文件操作时（通常不应该发生），会在所有 10 个 UI API 函数中打印醒目的警告。

**警告格式**：
```
⚠️  [UI API] FALLBACK MODE: No storage adapter available, using direct file I/O.
This should not happen in production!
```

**触发场景**：
- Storage adapter 完全未初始化（极少见，只在测试或特殊配置下）
- 应该立即调查为什么 adapter 未初始化

## 使用场景

### 场景 1：生产环境强制使用 Redis

**目标**：确保生产环境必须使用 Redis，防止意外降级到文件模式导致并发问题。

**配置**：
```yaml
# docker-compose.yml
services:
  aiclient-api:
    environment:
      - REQUIRE_STORAGE_ADAPTER=true  # 启用严格模式
      - REDIS_ENABLED=true
      - REDIS_URL=redis://redis:6379
    depends_on:
      redis:
        condition: service_healthy
```

**效果**：
- ✅ Redis 正常：系统使用 Redis 原子操作，高并发安全
- ❌ Redis 宕机：请求失败，不会降级到文件模式（避免数据不一致）

### 场景 2：开发环境允许降级

**目标**：开发环境下允许没有 Redis 也能运行，但要能看到警告。

**配置**：
```bash
# 不设置 REQUIRE_STORAGE_ADAPTER（默认允许降级）
npm start
```

**效果**：
- 如果没有 Redis：使用 FileStorageAdapter（也是 adapter）
- 如果 adapter 真的未初始化：降级到直接文件操作 + 打印警告

### 场景 3：CI/CD 测试环境

**目标**：单元测试可以不依赖 Redis，但集成测试必须使用 Redis。

**配置**：
```bash
# 单元测试（允许降级）
npm run test:unit

# 集成测试（强制 Redis）
export REQUIRE_STORAGE_ADAPTER=true
docker-compose up -d redis
npm run test:integration
```

## 架构说明

### 正常情况下的 Adapter 初始化

系统在启动时会自动创建 storage adapter：
1. **优先选择**：Redis adapter（如果 `REDIS_ENABLED=true` 且连接成功）
2. **自动降级**：File adapter（如果 Redis 未启用或连接失败）

因此，在正常情况下 `getAdapter()` 总是会返回一个 adapter（Redis 或 File），**永远不会返回 `null`**。

### 何时会触发 Fallback？

降级到直接文件操作（`if (!adapter)`）只在以下极端情况下发生：
- ❌ Storage adapter 完全未初始化（启动配置错误）
- ❌ 在 adapter 初始化前就调用了 UI API（时序问题）
- ❌ 测试环境故意不初始化 adapter

**这些情况在生产环境中不应该出现**，因此会打印醒目的警告。

### 代码实现

```javascript
function getAdapter() {
    if (isStorageInitialized()) {
        return getStorageAdapter();  // 返回 RedisConfigManager 或 FileStorageAdapter
    }

    // 严格模式检查
    if (process.env.REQUIRE_STORAGE_ADAPTER === 'true') {
        throw new Error('[UI API] Storage adapter is required...');
    }

    return null;  // 允许降级
}

// 在所有 10 个 UI API 函数中
if (!adapter) {
    console.warn('⚠️  [UI API] FALLBACK MODE: ...');
    // 直接文件操作（最后的安全网）
}
```

## 监控建议

### 生产环境监控

**应该监控的日志**：
```
⚠️  [UI API] FALLBACK MODE
```

**如果出现此日志**：
1. 🔴 **立即告警** - 这表示系统架构出现问题
2. 🔍 **检查原因**：
   - Redis 是否正常连接？
   - Storage adapter 是否初始化失败？
   - 是否有配置错误？
3. 🛠️ **修复并重启**：
   - 修复 Redis 连接问题
   - 或启用 `REQUIRE_STORAGE_ADAPTER=true` 让系统快速失败而不是降级

### 日志示例

**正常运行（有 adapter）**：
```
[Storage] Redis storage adapter initialized with file fallback
[UI API] Added new provider via redis: abc-123
```

**异常降级（无 adapter）**：
```
⚠️  [UI API] FALLBACK MODE: No storage adapter available, using direct file I/O.
This should not happen in production!
[UI API] Added new provider to claude-kiro-oauth: abc-123
```

## 配置对比

| 场景 | REQUIRE_STORAGE_ADAPTER | REDIS_ENABLED | 行为 |
|------|------------------------|---------------|------|
| 生产（严格） | `true` | `true` | 必须有 Redis，否则失败 ❌ |
| 生产（宽松） | `false`（默认） | `true` | Redis 或 File，都可以 ✅ |
| 开发（本地） | `false`（默认） | `false` | 使用 File adapter ✅ |
| 测试（单元） | `false`（默认） | `false` | 允许直接文件操作 ✅ |
| 测试（集成） | `true` | `true` | 强制 Redis 测试 ✅ |

## 迁移指南

### 从旧版本升级

**无需任何配置更改**，系统默认行为不变：
- ✅ 有 adapter 时使用 adapter
- ✅ 无 adapter 时降级到文件操作（现在会打印警告）

### 启用严格模式（推荐）

**生产环境**建议启用严格模式：
```bash
# 修改 docker-compose.yml 或启动脚本
environment:
  - REQUIRE_STORAGE_ADAPTER=true
```

**效果**：
- 🔒 强制使用 storage adapter
- 🚫 禁止降级到直接文件操作
- ⚡ 快速失败而不是静默降级

## 常见问题

### Q1: 为什么不直接移除降级逻辑？

**A**: 保留降级逻辑有几个原因：
1. **向后兼容**：不破坏现有部署
2. **开发灵活性**：本地开发可以不依赖 Redis
3. **测试友好**：单元测试不需要 mock adapter
4. **最后安全网**：即使在极端情况下也能继续运行

通过 `REQUIRE_STORAGE_ADAPTER=true`，你可以选择性地禁用降级。

### Q2: 生产环境应该设置 `REQUIRE_STORAGE_ADAPTER=true` 吗？

**A**: **强烈推荐**，理由：
- ✅ 确保使用 Redis 原子操作（高并发安全）
- ✅ 快速失败，而不是静默降级导致数据不一致
- ✅ 明确的失败信号，容易定位问题

**例外**：如果你的生产环境是单实例且并发量很小，可以不启用。

### Q3: 如果看到 `⚠️  FALLBACK MODE` 警告怎么办？

**A**: 立即调查：
1. 检查 Redis 是否正常运行
2. 检查 storage adapter 初始化日志
3. 考虑启用 `REQUIRE_STORAGE_ADAPTER=true` 让问题更明显

### Q4: 开发环境需要 Redis 吗？

**A**: **不需要**。系统会自动使用 `FileStorageAdapter`，它也是一个 adapter，只是基于文件而不是 Redis。只有在 adapter 完全未初始化时才会触发 fallback。

## 相关文档

- `specs/002-fix-concurrent-file-io/` - 并发文件操作修复设计
- `specs/001-redis-config/` - Redis 存储架构设计
- `src/core/storage-factory.js` - Storage adapter 工厂
- `src/core/redis-config-manager.js` - Redis adapter 实现
- `src/core/file-storage-adapter.js` - File adapter 实现

## 版本历史

- **v2.8.3** (提交 fce6b27): 添加严格模式和降级警告
- **v2.8.2** (提交 928cf26): 修复并发文件操作问题
- **v2.8.1**: 引入 Redis 存储支持
