# 问题排查：/api/config 接口仍返回 62 个账户

## 问题描述

修改完所有代码后，`/api/config` 和 `/api/providers` 接口仍然返回 62 个 claude-kiro-oauth 账户，而 Redis 是空的。

## 根本原因

**服务启动时没有启用 Redis 存储模式**，导致服务使用了文件存储适配器（FileStorageAdapter），从 `configs/provider_pools.json` 文件加载了 62 个账户。

### 详细分析

1. **环境变量缺失**：
   - 服务启动时没有设置 `REDIS_ENABLED=true` 环境变量
   - `configs/config.json` 中也没有 Redis 配置
   - 导致 `storage-factory.js` 判断 Redis 未启用

2. **日志证据**：
   ```
   [Storage] Redis storage disabled, using file storage
   [Storage] File storage adapter initialized
   [Storage] Initialized storage adapter: file
   ```

3. **数据流**：
   ```
   storage-factory.js (Redis disabled)
     ↓
   FileStorageAdapter 创建
     ↓
   service-manager.js: adapter.getProviderPools()
     ↓
   FileStorageAdapter 从 provider_pools.json 读取
     ↓
   返回 62 个账户
     ↓
   config.providerPools = 62 个账户
     ↓
   /api/config 返回 62 个账户
   ```

## 解决方案

### 方法 1：使用环境变量启动（推荐）

```bash
REDIS_ENABLED=true REDIS_URL=redis://localhost:6379 npm start
```

### 方法 2：修改 config.json

在 `configs/config.json` 中添加：

```json
{
  "redis": {
    "enabled": true,
    "url": "redis://localhost:6379"
  }
}
```

### 方法 3：Docker Compose（生产环境推荐）

```yaml
services:
  redis:
    image: redis:8-alpine
    volumes:
      - redis-data:/data

  aiclient-api:
    image: heishui/aiclient-2-api:latest
    environment:
      - REDIS_ENABLED=true
      - REDIS_URL=redis://redis:6379
    depends_on:
      - redis
```

## 验证

### 启动日志检查

正确启用 Redis 时，应看到：

```
[Config] Redis storage enabled: redis://localhost:6379
[Storage] Redis storage enabled, attempting connection...
[Redis] Connection established
[Storage] Redis connection established successfully
[Storage] Initialized storage adapter: redis
[Initialization] Storage adapter returned empty provider pools (redis mode)
```

### API 接口检查

```bash
# 1. 登录获取 token
TOKEN=$(curl -s -X POST http://localhost:3001/api/login \
  -H "Content-Type: application/json" \
  -d '{"password":"admin123"}' | jq -r '.token')

# 2. 检查 /api/providers
curl -s -H "Authorization: Bearer $TOKEN" \
  http://localhost:3001/api/providers | jq

# 预期结果：{}（空对象）

# 3. 检查 /api/config
curl -s -H "Authorization: Bearer $TOKEN" \
  http://localhost:3001/api/config | jq '.providerPools'

# 预期结果：{}（空对象）
```

## 关键教训

1. **存储模式切换**：
   - 文件模式 → Redis 模式需要明确启用 Redis
   - 不能只依赖代码修改，还需要正确配置运行环境

2. **日志的重要性**：
   - 启动日志中的 "Storage adapter type" 是关键信息
   - 如果看到 `type: file` 而不是 `type: redis`，说明 Redis 未启用

3. **环境变量持久化**：
   - 通过 `npm start` 启动时，环境变量只在当前 session 有效
   - 服务重启后需要重新设置
   - 使用 Docker Compose 或 systemd 可以持久化环境变量

## 后续建议

### 1. 添加启动脚本

创建 `start-redis.sh`：

```bash
#!/bin/bash
export REDIS_ENABLED=true
export REDIS_URL=redis://localhost:6379
npm start
```

### 2. 添加配置验证

在启动时检查存储模式并打印警告：

```javascript
// api-server.js 启动时
const storageType = getStorageType();
console.log(`\n=== Storage Configuration ===`);
console.log(`Storage type: ${storageType}`);
if (storageType === 'file') {
  console.warn(`⚠️  WARNING: Using file storage mode`);
  console.warn(`⚠️  To enable Redis, set REDIS_ENABLED=true`);
}
console.log(`=============================\n`);
```

### 3. 更新文档

在 `README.md` 和 `CLAUDE.md` 中明确说明：
- 如何启用 Redis 模式
- 不同启动方式的环境变量配置
- 如何验证存储模式是否正确

## 时间线

- **2026-01-26 19:58**: 发现问题 - /api/config 返回 62 个账户
- **2026-01-26 20:15**: 添加调试日志追踪数据流
- **2026-01-26 20:25**: 发现 storage adapter type 是 "file" 而不是 "redis"
- **2026-01-26 20:30**: 确认环境变量未设置，使用环境变量重启
- **2026-01-26 20:35**: 验证通过 - 所有接口返回空数据

## 相关文件

- `src/core/storage-factory.js` - 存储适配器工厂，根据配置决定使用哪种存储
- `src/services/service-manager.js` - 初始化时从存储加载 provider pools
- `specs/002-fix-concurrent-file-io/final-fix.md` - handleGetProviderType 函数的修复
- `specs/002-fix-concurrent-file-io/implementation.md` - 完整的实施记录
