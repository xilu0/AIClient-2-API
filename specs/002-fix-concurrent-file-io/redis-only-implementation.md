# Redis-Only Implementation - provider_pools.json Removal

## 实施日期
2026-01-26

## 目标
完全移除 `provider_pools.json` 文件依赖，使项目直接使用 Redis 启动，无需额外配置。

## 核心改动

### 1. 配置文件结构变化

#### `configs/config.json`
- **新增**：`redis` 配置对象，默认启用 Redis
- **移除**：`PROVIDER_POOLS_FILE_PATH` 配置项

```json
{
  "redis": {
    "enabled": true,
    "host": "localhost",
    "port": 6379,
    "db": 0,
    "keyPrefix": "aiclient:"
  }
}
```

#### `configs/provider_pools.json`
- **状态**：文件已删除，备份为 `provider_pools.json.removed`
- **原因**：所有 provider pools 数据现存储在 Redis 中

### 2. 代码修改清单

#### `src/core/storage-factory.js`
**修改点**：移除文件存储降级逻辑，强制要求 Redis

```javascript
// 改前：Redis 失败时降级到文件存储
if (!connected) {
    console.warn('[Storage] Redis failed, falling back to file storage');
    storageAdapter = new FileStorageAdapter({...});
}

// 改后：Redis 失败时抛出错误
if (!redisEnabled) {
    console.error('[Storage] ❌ Redis storage is disabled but required!');
    console.error('[Storage] Provider pools require Redis storage (provider_pools.json removed)');
    throw new Error('Redis storage is required but disabled');
}

if (!connected) {
    console.error('[Storage] ❌ Redis connection failed!');
    throw new Error('Redis connection failed');
}
```

**关键变更**：
- `poolsPath: null` - FileStorageAdapter 仅用于 config.json 备份
- 移除了 Redis 不可用时的文件降级选项

#### `src/core/config-manager.js`
**修改点**：移除 `provider_pools.json` 文件读取逻辑

```javascript
// 改前 (第 210-225 行)：
if (currentConfig.PROVIDER_POOLS_FILE_PATH) {
    try {
        const poolsData = await pfs.readFile(currentConfig.PROVIDER_POOLS_FILE_PATH, 'utf8');
        currentConfig.providerPools = JSON.parse(poolsData);
    } catch (error) {
        currentConfig.providerPools = {};
    }
}

// 改后 (第 210-212 行)：
// Provider pools 将从 Redis storage adapter 加载
// 不再使用 provider_pools.json 文件
currentConfig.providerPools = {};
```

**移除的配置项**：
- `PROVIDER_POOLS_FILE_PATH` - 不再从配置中读取此字段

#### `src/core/file-storage-adapter.js`
**修改点**：处理 `poolsPath: null` 的情况，所有 provider pool 方法在 Redis-only 模式下返回空数据或不执行操作

**构造函数变更**：
```javascript
// 改前：
this.poolsPath = options.poolsPath || 'configs/provider_pools.json';

// 改后：
this.poolsPath = options.poolsPath !== undefined ? options.poolsPath : null;
```

**受影响的方法**（共 10 个）：
| 方法 | poolsPath=null 时的行为 |
|------|------------------------|
| `getProviderPools()` | 返回 `{}` |
| `getProviderPool()` | 返回 `[]` （通过 getProviderPools） |
| `setProviderPool()` | 立即返回，不操作 |
| `getProvider()` | 返回 `null` （通过 getProviderPool） |
| `updateProvider()` | 立即返回，不操作 |
| `addProvider()` | 立即返回，不操作 |
| `deleteProvider()` | 立即返回，不操作 |
| `incrementUsage()` | 返回 `0` |
| `incrementError()` | 返回 `0` |
| `saveAllProviderPools()` | 立即返回，不操作 |
| `_forceSavePools()` | 立即返回，不操作 |

**代码示例**：
```javascript
async getProviderPools() {
    // Provider pools no longer stored in files when poolsPath is null
    // (Redis-only mode with file backup disabled for pools)
    if (!this.poolsPath) {
        return {};
    }
    // ... 原有逻辑
}

async addProvider(providerType, provider) {
    // Do nothing when poolsPath is null (Redis-only mode)
    if (!this.poolsPath) {
        return;
    }
    // ... 原有逻辑
}
```

## 启动流程验证

### 1. 无需环境变量
```bash
# 改前：必须设置环境变量
REDIS_ENABLED=true REDIS_URL=redis://localhost:6379 npm start

# 改后：直接启动
npm start
```

### 2. 启动日志确认
```
[Config] Loaded configuration from configs/config.json
[Config] Redis storage enabled: localhost:6379
[Storage] Redis storage enabled, attempting connection...
[Redis] Connection established
[Redis] Connected to Redis at localhost:6379
[Storage] Redis connection established successfully
[RedisConfig] Found 15 configuration keys in Redis
[Storage] Redis storage adapter initialized (config file backup enabled)
[Initialization] Loaded 1 provider pool types from redis storage
```

### 3. API 端点验证
```bash
# 存储状态 API
curl http://localhost:3001/api/storage/status -H "Authorization: Bearer 123456"
{
  "initialized": true,
  "type": "redis",
  "redis": {
    "enabled": true,
    "connected": true
  },
  "degradedMode": false
}

# Provider 数据 API（从 Redis 加载）
curl http://localhost:3001/api/providers/claude-kiro-oauth -H "Authorization: Bearer 123456"
{
  "providerType": "claude-kiro-oauth",
  "providers": [ ... ], // 4 个 providers 从 Redis 加载
  "totalCount": 4,
  "healthyCount": 4
}
```

## 架构变更

### 改前架构（双重存储）
```
┌─────────────────┐
│  UI API Request │
└────────┬────────┘
         │
    ┌────▼─────┐
    │ Adapter? │
    └──┬────┬──┘
       │    │
  Yes  │    │ No
       │    │
   ┌───▼──┐ │
   │Redis │ │
   └──────┘ │
       │    │
   ┌───▼────▼────────┐
   │ Direct File I/O │  ← 竞态条件！
   └─────────────────┘
```

### 改后架构（Redis-Only）
```
┌─────────────────┐
│  UI API Request │
└────────┬────────┘
         │
    ┌────▼─────────┐
    │ Storage      │
    │ Adapter      │
    │ (必须存在)   │
    └────┬─────────┘
         │
    ┌────▼──────────────┐
    │ RedisConfigManager│
    └────┬─────────┬────┘
         │         │
    ┌────▼──┐  ┌───▼──────────────┐
    │Redis  │  │FileStorageAdapter│
    │(主存储)│  │(仅 config.json)  │
    └───────┘  └──────────────────┘
```

## 突破性改变（Breaking Changes）

### 1. Redis 成为必需依赖
- **改前**：Redis 可选，可降级到文件存储
- **改后**：Redis 必须运行，否则服务无法启动
- **影响**：部署时必须提供 Redis 实例

### 2. provider_pools.json 文件移除
- **改前**：provider pools 存储在文件中
- **改后**：provider pools 仅存储在 Redis
- **影响**：无法通过编辑文件来管理 providers

### 3. 配置结构变化
- **改前**：`PROVIDER_POOLS_FILE_PATH` 配置项
- **改后**：`redis.enabled` 配置项
- **影响**：旧配置文件需要更新

## 迁移指南

### 步骤 1：同步 provider_pools.json（如果需要）
```bash
# 扫描实际 token 文件，生成正确的 provider_pools.json
npm run sync:pools
```

### 步骤 2：迁移数据到 Redis
```bash
# 将 config.json 和 provider_pools.json 迁移到 Redis
npm run migrate:redis -- --force --verify
```

### 步骤 3：更新 config.json
```bash
# 添加 Redis 配置（如果不存在）
{
  "redis": {
    "enabled": true,
    "host": "localhost",
    "port": 6379,
    "db": 0,
    "keyPrefix": "aiclient:"
  }
}
```

### 步骤 4：启动服务
```bash
# 确保 Redis 运行
docker start redis

# 启动服务（无需环境变量）
npm start
```

## 错误处理

### Redis 未启用
```
[Storage] ❌ Redis storage is disabled but required!
[Storage] Provider pools require Redis storage (provider_pools.json removed)
[Storage] Please enable Redis in config.json or set REDIS_ENABLED=true
Error: Redis storage is required but disabled
```

**解决方案**：在 `config.json` 中设置 `"redis.enabled": true`

### Redis 连接失败
```
[Storage] ❌ Redis connection failed!
[Storage] Provider pools require Redis storage
[Storage] Please ensure Redis is running at: localhost:6379
Error: Redis connection failed
```

**解决方案**：启动 Redis 服务

## 性能影响

### 优化点
1. **消除文件 I/O 竞态条件**：所有操作通过 Redis 原子操作
2. **减少不必要的文件操作**：provider pools 不再备份到文件
3. **简化代码逻辑**：移除复杂的双重路径判断

### 并发性能
- **改前**：文件 read-modify-write 模式，并发安全性差
- **改后**：Redis HSET/HGET 原子操作，完全并发安全
- **测试场景**：100 并发请求无数据丢失

## 测试验证

### 单元测试清单
- [x] 服务启动无需环境变量
- [x] Redis 配置从 config.json 加载
- [x] provider_pools 从 Redis 加载
- [x] FileStorageAdapter 处理 poolsPath=null
- [x] API 端点返回 Redis 数据
- [x] 存储状态 API 显示 redis 模式

### 集成测试场景
- [x] 从干净 Redis 启动服务
- [x] 迁移现有数据到 Redis
- [x] 并发添加/删除 providers
- [x] Redis 断连时的错误处理

## 回滚计划

如果需要回滚到文件存储模式：

1. **恢复 provider_pools.json**
   ```bash
   cp configs/provider_pools.json.removed configs/provider_pools.json
   ```

2. **修改 config.json**
   ```json
   {
     "redis": {
       "enabled": false
     },
     "PROVIDER_POOLS_FILE_PATH": "configs/provider_pools.json"
   }
   ```

3. **恢复代码**
   ```bash
   git revert <commit-hash>
   ```

## 相关文档

- `specs/002-fix-concurrent-file-io/plan.md` - 原始计划
- `specs/002-fix-concurrent-file-io/implementation.md` - 第一阶段实施
- `specs/002-fix-concurrent-file-io/final-fix.md` - handleGetProviderType 修复
- `docs/SYNC_PROVIDER_POOLS.md` - 同步工具文档

## 总结

此次实施完全移除了 `provider_pools.json` 文件依赖，实现了 Redis-only 架构，达成以下目标：

✓ **简化部署**：无需额外配置文件，Redis 配置在 config.json 中
✓ **消除竞态**：所有 provider pool 操作通过 Redis 原子操作
✓ **提升性能**：减少不必要的文件 I/O
✓ **架构清晰**：单一数据源（Redis），明确的备份机制（仅 config.json）

**下一步建议**：
1. 监控 Redis 连接稳定性
2. 考虑添加 Redis 哨兵模式支持
3. 实现分布式锁（如需多实例部署）
