# Redis Key Reference

本文档描述 AIClient-2-API 使用的所有 Redis 键。

## 概述

| 键前缀 | 默认值 | 配置方式 |
|--------|--------|----------|
| `aiclient:` | 默认前缀 | `REDIS_KEY_PREFIX` 环境变量 |

## 键模式总览

| 键模式 | 数据类型 | 用途 |
|--------|----------|------|
| `aiclient:config` | String (JSON) | 主配置 |
| `aiclient:pools:{providerType}` | Hash | 多账号提供者池 |
| `aiclient:tokens:{providerType}:{uuid}` | String (JSON) | OAuth 令牌 |
| `aiclient:token-lock:{providerType}:{uuid}` | String | 令牌刷新分布式锁 |
| `aiclient:pwd` | String | Web UI 密码 |
| `aiclient:sessions:{tokenHash}` | String (JSON) | 会话令牌 |
| `aiclient:meta` | Hash | 元数据 |
| `aiclient:usage:cache` | String (JSON) | 使用量缓存 |
| `aiclient:plugins` | String (JSON) | 插件配置 |

---

## 详细说明

### 1. aiclient:config

**用途**: 主服务配置（端口、API Key、默认提供者、代理设置、故障转移链等）

**数据类型**: String (JSON 序列化)

**Redis 操作**:
```bash
GET aiclient:config
SET aiclient:config <json>
```

**数据结构**:
```javascript
{
  port: number,              // 服务端口
  apiKey: string,            // API 密钥
  defaultProvider: string,   // 默认提供者类型
  proxy: {                   // 代理设置
    enabled: boolean,
    host: string,
    port: number
  },
  fallbackChains: {          // 故障转移链
    [modelPrefix]: [providerType, ...]
  },
  redis: {                   // Redis 配置
    enabled: boolean,
    host: string,
    port: number
  }
}
```

**缓存**: 内存缓存，5 秒 TTL

---

### 2. aiclient:pools:{providerType}

**用途**: 多账号提供者池（轮询账号、健康检查、使用量追踪）

**数据类型**: Redis Hash (field=uuid, value=JSON)

**提供者类型示例**:
- `aiclient:pools:claude-kiro-oauth`
- `aiclient:pools:gemini-cli-oauth`
- `aiclient:pools:openai-custom`
- `aiclient:pools:openai-qwen-oauth`

**Redis 操作**:
```bash
# 获取所有池
KEYS aiclient:pools:*

# 获取某个池的所有提供者
HGETALL aiclient:pools:{providerType}

# 获取单个提供者
HGET aiclient:pools:{providerType} {uuid}

# 添加/更新提供者
HSET aiclient:pools:{providerType} {uuid} <json>

# 删除提供者
HDEL aiclient:pools:{providerType} {uuid}

# 清空整个池
DEL aiclient:pools:{providerType}
```

**单个提供者数据结构**:
```javascript
{
  uuid: string,                      // 唯一标识符
  customName: string|undefined,      // 显示名称
  isHealthy: boolean,                // 健康状态
  isDisabled: boolean,               // 是否禁用
  usageCount: number,                // 总使用次数
  errorCount: number,                // 总错误次数
  lastUsed: "ISO string",            // 最后使用时间
  lastErrorTime: "ISO string",       // 最后错误时间
  lastHealthCheckTime: "ISO string", // 最后健康检查时间
  refreshCount: number|undefined,    // 令牌刷新次数
  needsRefresh: boolean|undefined    // 是否需要刷新令牌
}
```

**原子操作** (Lua 脚本):
| 操作 | 说明 |
|------|------|
| `atomicUsageUpdate` | 递增 `usageCount`，更新 `lastUsed` |
| `atomicErrorUpdate` | 递增 `errorCount`，更新 `lastErrorTime`，可选标记不健康 |
| `atomicHealthUpdate` | 更新 `isHealthy`，设置 `lastHealthCheckTime` |
| `atomicProviderUpdate` | 合并字段更新到提供者对象 |

**缓存**: 内存缓存，5 秒 TTL

---

### 3. aiclient:tokens:{providerType}:{uuid}

**用途**: 各提供者账号的 OAuth 令牌凭据

**数据类型**: String (JSON 序列化)，可选 TTL

**示例**:
- `aiclient:tokens:claude-kiro-oauth:uuid-123`
- `aiclient:tokens:gemini-cli-oauth:oauth_creds`

**Redis 操作**:
```bash
# 获取令牌
GET aiclient:tokens:{providerType}:{uuid}

# 设置令牌（永久）
SET aiclient:tokens:{providerType}:{uuid} <json>

# 设置令牌（带过期）
SETEX aiclient:tokens:{providerType}:{uuid} <ttl> <json>

# 删除令牌
DEL aiclient:tokens:{providerType}:{uuid}
```

**数据结构** (OAuth2 格式):
```javascript
// Claude/Kiro 风格
{
  accessToken: string,        // 访问令牌
  refreshToken: string,       // 刷新令牌
  expiresAt: "ISO string"     // 过期时间
}

// Gemini 风格
{
  access_token: string,       // 访问令牌
  refresh_token: string,      // 刷新令牌
  expiry_date: number         // 过期时间（Unix 毫秒）
}
```

**原子操作** (CAS 模式):
| 操作 | 说明 |
|------|------|
| `atomicTokenUpdate` | 比较并交换更新，检测刷新令牌冲突 |
| `atomicTokenUpdateWithTTL` | 带 TTL 的 CAS 更新 |

---

### 4. aiclient:token-lock:{providerType}:{uuid}

**用途**: 令牌刷新操作的分布式锁（防止并发刷新冲突）

**数据类型**: String (锁 ID)，带 TTL

**默认 TTL**: 30 秒

**Redis 操作**:
```bash
# 获取锁（NX = 仅当不存在时设置，EX = 设置过期）
SET aiclient:token-lock:{providerType}:{uuid} <lockId> NX EX <ttl>

# 验证锁所有权
GET aiclient:token-lock:{providerType}:{uuid}

# 释放锁
DEL aiclient:token-lock:{providerType}:{uuid}
```

**锁 ID 格式**: `{timestamp}-{randomString}`

---

### 5. aiclient:pwd

**用途**: Web UI 仪表板认证密码

**数据类型**: String（明文密码）

**Redis 操作**:
```bash
GET aiclient:pwd
SET aiclient:pwd <password>
```

---

### 6. aiclient:sessions:{tokenHash}

**用途**: Web UI 会话令牌（带过期）

**数据类型**: String (JSON 序列化)，使用 SETEX 设置 TTL

**示例**:
- `aiclient:sessions:3972587d4ab86878e537bd6fc4083f6aee1edec3e22d49cecbe3a7df2073a219`

**Redis 操作**:
```bash
# 获取会话
GET aiclient:sessions:{tokenHash}

# 设置会话（带过期）
SETEX aiclient:sessions:{tokenHash} <ttl> <json>

# 删除会话
DEL aiclient:sessions:{tokenHash}

# 列出所有会话
KEYS aiclient:sessions:*
```

**数据结构**:
```javascript
{
  username: string,          // 用户名
  loginTime: "ISO string",   // 登录时间
  expiryTime: number         // 过期时间（Unix 毫秒）
}
```

**默认 TTL**: 3600 秒（1 小时）

---

### 7. aiclient:meta

**用途**: Redis 存储状态元数据（迁移信息、版本）

**数据类型**: Redis Hash（多个字段）

**Redis 操作**:
```bash
# 获取所有元数据
HGETALL aiclient:meta

# 设置单个字段
HSET aiclient:meta <field> <value>
```

**字段**:
```javascript
{
  version: "1.0",              // Schema 版本
  migratedAt: "ISO string",    // 迁移时间
  migratedFrom: "file|redis"   // 之前的存储类型
}
```

**设置方**: 迁移工具（`migrate-to-redis.js`、`init-redis.js`）

---

### 8. aiclient:usage:cache

**用途**: 仪表板使用量统计缓存

**数据类型**: String (JSON 序列化)

**Redis 操作**:
```bash
GET aiclient:usage:cache
SET aiclient:usage:cache <json>
```

**数据结构**:
```javascript
{
  timestamp: "ISO string",
  providers: {
    [providerType]: {
      // 提供者特定的使用量数据
    }
  }
}
```

**缓存**: 内存缓存，30 秒 TTL

---

### 9. aiclient:plugins

**用途**: 插件配置和启用状态

**数据类型**: String (JSON 序列化)

**Redis 操作**:
```bash
GET aiclient:plugins
SET aiclient:plugins <json>
```

**数据结构**:
```javascript
{
  plugins: {
    [pluginName]: {
      enabled: boolean,
      description: string,
      // 插件特定字段
    }
  }
}
```

**缓存**: 内存缓存，60 秒 TTL

---

## 降级模式

当 Redis 断开连接时：

### 写入队列
- 失败的写操作队列最多 1000 个操作
- 重连后自动重放队列操作
- 每个操作最多重试 3 次

### 缓存行为
- 继续使用内存缓存（不过期）
- 缓存保持有效直到 Redis 重连
- 队列状态可通过 `/api/redis/status` 查看

### 回退机制
非池操作（令牌、密码、会话）在 Redis 不可用时可使用文件适配器回退。

---

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `REDIS_ENABLED` | `true` | 启用/禁用 Redis（池必需） |
| `REDIS_URL` | - | 完整 Redis URL（覆盖 host/port） |
| `REDIS_HOST` | `localhost` | Redis 服务器主机 |
| `REDIS_PORT` | `6379` | Redis 服务器端口 |
| `REDIS_PASSWORD` | - | Redis 认证密码 |
| `REDIS_DB` | `0` | Redis 数据库编号 |
| `REDIS_KEY_PREFIX` | `aiclient:` | 所有键的前缀 |

---

## CLI 工具

### 迁移到 Redis
```bash
npm run migrate:redis -- --config-dir ./configs --redis-url redis://localhost:6379
```

### 从 Redis 导出
```bash
npm run export:redis -- --output-dir ./backup --redis-url redis://localhost:6379
```

### 初始化 Redis
```bash
npm run init:redis -- --redis-url redis://localhost:6379 --api-key YOUR_API_KEY
```

---

## API 端点

| 端点 | 说明 |
|------|------|
| `GET /api/redis/status` | Redis 连接状态、健康度、指标 |
| `GET /api/storage/status` | 存储适配器类型和状态 |

---

## 源文件

| 文件 | 说明 |
|------|------|
| `src/core/redis-client.js` | Redis 连接管理器 |
| `src/core/redis-config-manager.js` | Redis 操作（所有键模式） |
| `src/core/storage-adapter.js` | 存储接口定义 |
| `src/core/storage-factory.js` | 适配器初始化 |
| `src/core/file-storage-adapter.js` | 文件回退 |
| `src/core/write-queue.js` | 不可用期间的写入队列 |
| `src/cli/migrate-to-redis.js` | 文件到 Redis 迁移 |
| `src/cli/export-from-redis.js` | Redis 到文件导出 |
| `src/cli/init-redis.js` | Redis 初始化 |
