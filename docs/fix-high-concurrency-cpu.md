# 高并发 CPU 100% 问题修复方案

## 问题描述

服务在多人使用时，并发一上来就 CPU 100%，导致服务不稳定。

## 根因分析

问题是**多个因素叠加**造成的，按影响程度排序：

### 第一层：Redis 缓存失效风暴（最严重）

**位置**：`src/core/redis-config-manager.js:514, 618, 634, 651`

```javascript
async incrementUsage(providerType, uuid) {
    this._poolsCache = null;  // 每次请求都失效整个缓存
    // ...
}
```

**连锁反应**：

```
请求 1: incrementUsage() → 缓存失效
请求 2: getProviderPools() → 缓存未命中 → client.keys('pools:*') O(N) 阻塞
请求 3: incrementUsage() → 缓存再次失效
请求 4: getProviderPools() → 又一次 keys 扫描
...
```

**影响**：
- `client.keys()` 是 Redis 单线程阻塞操作
- 500 个键扫描耗时 10-50ms
- 高并发下形成缓存击穿，每秒可能触发 20-50 次全量扫描
- 单独这一项就能打满 CPU

### 第二层：Provider 选择的串行化瓶颈

**位置**：`src/providers/provider-pool-manager.js:662-664`

```javascript
async selectProvider(providerType, requestedModel = null, options = {}) {
    return this._selectionMutex.withLock(providerType, () => {
        return this._doSelectProvider(providerType, requestedModel, options);
    });
}
```

**问题**：
- 每个 `providerType` 使用全局互斥锁
- 100 个并发请求 → 99 个在队列中等待
- 串行化本身就是瓶颈

### 第三层：每请求的同步 CPU 操作叠加

#### 3.1 排序选择 O(n log n)

**位置**：`src/providers/provider-pool-manager.js:711-717`

```javascript
const selected = availableAndHealthyProviders.sort((a, b) => {
    const scoreA = this._calculateNodeScore(a, now);
    const scoreB = this._calculateNodeScore(b, now);
    // ...
})[0];
```

- 50 个账号 → 每次请求约 300 次比较

#### 3.2 协议转换 + JSON 序列化

**位置**：`src/utils/common.js:339-362`

```javascript
for await (const nativeChunk of nativeStream) {
    const chunkToSend = needsConversion
        ? convertData(nativeChunk, 'streamChunk', toProvider, fromProvider, model)
        : nativeChunk;

    const serialized = JSON.stringify(chunk);  // 每个 chunk 都序列化
    res.write(`data: ${serialized}\n\n`);
}
```

- 流式响应可能有 100+ chunks
- 每个 chunk 都做 `convertData` + `JSON.stringify`

#### 3.3 日志广播

**位置**：`src/ui-modules/event-broadcast.js:96-137`

- 虽然用了 `setImmediate` 异步化，但每条日志仍需序列化
- 高并发下日志量巨大

### 第四层：重试放大效应

**位置**：`src/utils/common.js:438-442`

- 错误时触发凭证切换重试
- 每次重试都要重新 `selectProvider` → 再次触发锁竞争和排序

## CPU 占用估算（100 并发/秒）

| 操作 | 频率 | 单次耗时 | CPU 占用 |
|------|------|----------|----------|
| Redis `keys()` 扫描 | 20-50/s | 10-50ms | **20-250%** |
| Provider 排序 | 100/s | 0.5-2ms | 5-20% |
| JSON 序列化（流式） | 5000/s | 0.1ms | 50% |
| 日志序列化 | 500/s | 0.2ms | 10% |
| 锁等待开销 | 100/s | 0.1ms | 1% |
| **总计** | - | - | **86-331%** |

**结论**：Redis 缓存失效是主要元凶，其他因素是放大器。

---

## 修复方案

### P0（立即修复）：消除缓存失效风暴

**修改文件**：`src/core/redis-config-manager.js`

**修改内容**：

1. `incrementUsage` 方法（约 Line 616-630）：
```javascript
async incrementUsage(providerType, uuid) {
    // 删除：this._poolsCache = null;

    // 改为：更新内存缓存中的对应字段
    if (this._poolsCache && this._poolsCache[providerType]) {
        const provider = this._poolsCache[providerType].find(p => p.uuid === uuid);
        if (provider) {
            provider.usageCount = (provider.usageCount || 0) + 1;
            provider.lastUsed = new Date().toISOString();
        }
    }

    // Redis 更新继续执行（fire-and-forget）
    const timestamp = new Date().toISOString();
    const executeResult = await this._execute(async (client) => {
        return await client.atomicUsageUpdate(
            this._key(`pools:${providerType}`),
            uuid,
            timestamp
        );
    }, `incrementUsage:${providerType}:${uuid}`);

    return executeResult.result || 0;
}
```

2. `incrementError` 方法（约 Line 632-647）：
```javascript
async incrementError(providerType, uuid, markUnhealthy = false) {
    // 删除：this._poolsCache = null;

    // 改为：更新内存缓存中的对应字段
    if (this._poolsCache && this._poolsCache[providerType]) {
        const provider = this._poolsCache[providerType].find(p => p.uuid === uuid);
        if (provider) {
            provider.errorCount = (provider.errorCount || 0) + 1;
            provider.lastErrorTime = new Date().toISOString();
            if (markUnhealthy) {
                provider.isHealthy = false;
            }
        }
    }

    // Redis 更新继续执行
    const timestamp = new Date().toISOString();
    const executeResult = await this._execute(async (client) => {
        return await client.atomicErrorUpdate(
            this._key(`pools:${providerType}`),
            uuid,
            timestamp,
            markUnhealthy.toString()
        );
    }, `incrementError:${providerType}:${uuid}`);

    return executeResult.result || 0;
}
```

3. `updateHealthStatus` 方法（约 Line 649-662）：
```javascript
async updateHealthStatus(providerType, uuid, isHealthy) {
    // 删除：this._poolsCache = null;

    // 改为：更新内存缓存中的对应字段
    if (this._poolsCache && this._poolsCache[providerType]) {
        const provider = this._poolsCache[providerType].find(p => p.uuid === uuid);
        if (provider) {
            provider.isHealthy = isHealthy;
            provider.lastHealthCheckTime = new Date().toISOString();
        }
    }

    // Redis 更新继续执行
    const timestamp = new Date().toISOString();
    await this._execute(async (client) => {
        await client.atomicHealthUpdate(
            this._key(`pools:${providerType}`),
            uuid,
            isHealthy.toString(),
            timestamp
        );
    }, `updateHealthStatus:${providerType}:${uuid}:${isHealthy}`);
}
```

4. `updateProvider` 方法（约 Line 512-541）：
```javascript
async updateProvider(providerType, uuid, updates) {
    // 删除：this._poolsCache = null;

    // 改为：更新内存缓存中的对应字段
    if (this._poolsCache && this._poolsCache[providerType]) {
        const provider = this._poolsCache[providerType].find(p => p.uuid === uuid);
        if (provider) {
            Object.assign(provider, updates);
        }
    }

    // 其余代码保持不变...
}
```

**预期效果**：CPU 从 100% 降到 20-30%

---

### P1（短期优化）：移除 `keys()` 命令

**修改文件**：`src/core/redis-config-manager.js`

**修改内容**：

1. 在 `setProviderPool` 方法中维护 provider 类型集合：
```javascript
async setProviderPool(providerType, providers) {
    // 更新缓存
    if (this._poolsCache) {
        this._poolsCache[providerType] = providers;
        this._poolsCacheTime = Date.now();
    }

    await this._execute(async (client) => {
        const key = this._key(`pools:${providerType}`);
        // 清除现有池
        await client.del(key);
        // 添加所有 providers
        if (providers.length > 0) {
            const multi = client.multi();
            for (const provider of providers) {
                multi.hset(key, provider.uuid, JSON.stringify(provider));
            }
            // 维护 provider 类型集合
            multi.sadd(this._key('pool-types'), providerType);
            await multi.exec();
        } else {
            // 如果池为空，从类型集合中移除
            await client.srem(this._key('pool-types'), providerType);
        }
    }, `setProviderPool:${providerType}`);
}
```

2. 修改 `getProviderPools` 方法：
```javascript
async getProviderPools() {
    // 缓存检查逻辑保持不变...

    const client = this.redisManager.getClient();
    if (!client || !this.redisManager.isConnected()) {
        console.warn('[RedisConfig] Redis not connected, returning empty pools');
        return {};
    }

    try {
        // 使用 SMEMBERS 获取所有 provider 类型（O(M)，M 为类型数量）
        const types = await client.smembers(this._key('pool-types'));
        const pools = {};

        for (const providerType of types) {
            const providers = await client.hgetall(this._key(`pools:${providerType}`));
            pools[providerType] = Object.values(providers).map(p => JSON.parse(p));
        }

        this._poolsCache = pools;
        this._poolsCacheTime = Date.now();
        return pools;
    } catch (error) {
        console.error('[RedisConfig] Failed to get provider pools:', error.message);
        return {};
    }
}
```

3. 在 `saveAllProviderPools` 方法中同步更新类型集合：
```javascript
async saveAllProviderPools(pools) {
    // 更新缓存
    this._poolsCache = pools;
    this._poolsCacheTime = Date.now();

    await this._execute(async (client) => {
        const multi = client.multi();

        // 清除旧的类型集合
        multi.del(this._key('pool-types'));

        for (const [providerType, providers] of Object.entries(pools)) {
            const key = this._key(`pools:${providerType}`);
            multi.del(key);
            if (providers.length > 0) {
                for (const provider of providers) {
                    multi.hset(key, provider.uuid, JSON.stringify(provider));
                }
                // 添加到类型集合
                multi.sadd(this._key('pool-types'), providerType);
            }
        }

        await multi.exec();
    }, 'saveAllProviderPools');
}
```

**预期效果**：消除 Redis 阻塞，`getProviderPools` 响应时间从 50ms 降到 5ms

---

### P2（中期优化）：移除 Provider 选择锁

**修改文件**：`src/providers/provider-pool-manager.js`

**修改内容**：

```javascript
async selectProvider(providerType, requestedModel = null, options = {}) {
    // 参数校验
    if (!providerType || typeof providerType !== 'string') {
        this._log('error', `Invalid providerType: ${providerType}`);
        return null;
    }

    // 移除互斥锁，直接执行选择
    // 由于 Redis 原子操作保证了数据一致性，不需要应用层锁
    return this._doSelectProvider(providerType, requestedModel, options);
}
```

**注意事项**：
- 需要确保 `_doSelectProvider` 内部操作是幂等的
- Redis 原子操作（Lua 脚本）已经保证了并发安全
- 移除锁后，多个请求可能选择同一个 provider，但 `_lastSelectionSeq` 机制会在下一轮排序中区分

**预期效果**：吞吐量提升 5-10 倍

---

### P3（长期优化）：批量更新 + 减少日志

#### 3.1 批量 Redis 更新

累积 100ms 内的更新，用 Pipeline 批量提交：

```javascript
class BatchUpdater {
    constructor(redisClient, flushInterval = 100) {
        this.client = redisClient;
        this.pendingUpdates = new Map();
        this.flushInterval = flushInterval;
        this.timer = null;
    }

    queueUpdate(key, field, value) {
        if (!this.pendingUpdates.has(key)) {
            this.pendingUpdates.set(key, new Map());
        }
        this.pendingUpdates.get(key).set(field, value);
        this._scheduleFlush();
    }

    _scheduleFlush() {
        if (this.timer) return;
        this.timer = setTimeout(() => this._flush(), this.flushInterval);
    }

    async _flush() {
        this.timer = null;
        if (this.pendingUpdates.size === 0) return;

        const pipeline = this.client.pipeline();
        for (const [key, fields] of this.pendingUpdates) {
            for (const [field, value] of fields) {
                pipeline.hset(key, field, JSON.stringify(value));
            }
        }
        this.pendingUpdates.clear();
        await pipeline.exec();
    }
}
```

#### 3.2 减少日志输出

高并发时自动降低日志级别：

```javascript
// 在 event-broadcast.js 中添加
let requestCount = 0;
let lastResetTime = Date.now();

function shouldLog() {
    const now = Date.now();
    if (now - lastResetTime > 1000) {
        requestCount = 0;
        lastResetTime = now;
    }
    requestCount++;

    // 超过 50 请求/秒时，只记录 10% 的日志
    if (requestCount > 50) {
        return Math.random() < 0.1;
    }
    return true;
}
```

#### 3.3 预计算排序

维护已排序的 provider 列表，增量更新：

```javascript
class SortedProviderCache {
    constructor() {
        this.sortedLists = new Map();
    }

    updateProvider(providerType, uuid, score) {
        // 使用二分查找更新位置，O(log n) 而不是 O(n log n)
    }

    getTop(providerType) {
        return this.sortedLists.get(providerType)?.[0];
    }
}
```

---

## 实施顺序

1. **P0**：立即实施，预计 CPU 降到 20-30%
2. **P1**：P0 验证后实施，彻底消除 Redis 瓶颈
3. **P2**：根据实际负载决定是否需要
4. **P3**：长期优化，按需实施

## 验证方法

```bash
# 使用 Node.js 内置 profiler
node --prof src/services/api-server.js

# 压测
ab -n 1000 -c 100 -p request.json -T application/json \
  -H "X-API-Key: YOUR_API_KEY" \
  http://localhost:3000/claude-kiro-oauth/v1/messages

# 分析 profiler 输出
node --prof-process isolate-*.log > profile.txt
```

## 回滚方案

如果修复后出现问题，可以通过以下方式回滚：

```bash
git checkout main -- src/core/redis-config-manager.js
git checkout main -- src/providers/provider-pool-manager.js
```

---

## 更新日志

- 2026-01-27: 创建文档，完成根因分析和修复方案设计
