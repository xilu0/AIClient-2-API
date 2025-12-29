# Provider Pool 调度算法详解

本文档详细介绍 AIClient-2-API 中账号池（Provider Pool）的调度算法实现。

## 概述

`ProviderPoolManager` 类（位于 `src/provider-pool-manager.js`）负责管理多账号池的调度，核心目标是：

1. **负载均衡** - 将请求均匀分配到各个账号
2. **高可用性** - 自动检测并隔离故障账号
3. **智能降级** - 当主要 Provider 不可用时自动切换到备用 Provider

---

## 核心调度算法

### 1. LRU (Least Recently Used) 选择策略

与传统的取模轮询（Round-Robin）不同，本系统采用 **"最久未使用"策略 (LRU)**：

```javascript
// src/provider-pool-manager.js:153-160
const selected = availableAndHealthyProviders.sort((a, b) => {
    const timeA = a.config.lastUsed ? new Date(a.config.lastUsed).getTime() : 0;
    const timeB = b.config.lastUsed ? new Date(b.config.lastUsed).getTime() : 0;
    // 优先选择从未用过的，或者最久没用的
    if (timeA !== timeB) return timeA - timeB;
    // 如果时间相同，使用使用次数辅助判断
    return (a.config.usageCount || 0) - (b.config.usageCount || 0);
})[0];
```

**算法逻辑：**

```
┌─────────────────────────────────────────────────────────────┐
│                     选择 Provider 流程                        │
├─────────────────────────────────────────────────────────────┤
│  1. 获取指定类型的所有 Provider                               │
│                    ↓                                        │
│  2. 过滤：isHealthy=true AND isDisabled=false               │
│                    ↓                                        │
│  3. (可选) 按 requestedModel 过滤不支持该模型的 Provider       │
│                    ↓                                        │
│  4. 按 lastUsed 升序排序（时间最早 = 最久未用 = 优先选择）       │
│                    ↓                                        │
│  5. 若 lastUsed 相同，按 usageCount 升序排序                   │
│                    ↓                                        │
│  6. 选择排序后的第一个 Provider                               │
│                    ↓                                        │
│  7. 更新 lastUsed 和 usageCount                              │
└─────────────────────────────────────────────────────────────┘
```

**为什么选择 LRU 而不是 Round-Robin？**

| 方案 | 优点 | 缺点 |
|------|------|------|
| Round-Robin (取模轮询) | 实现简单 | 当可用列表长度动态变化时，会导致分配不均 |
| **LRU (最久未使用)** | 即使列表变化也能保证均匀分配 | 需要额外存储 lastUsed 时间戳 |

---

### 2. 模型过滤机制

当请求指定了特定模型时，会排除不支持该模型的 Provider：

```javascript
// src/provider-pool-manager.js:128-143
if (requestedModel) {
    const modelFilteredProviders = availableAndHealthyProviders.filter(p => {
        // 如果提供商没有配置 notSupportedModels，则认为它支持所有模型
        if (!p.config.notSupportedModels || !Array.isArray(p.config.notSupportedModels)) {
            return true;
        }
        // 检查 notSupportedModels 数组中是否包含请求的模型
        return !p.config.notSupportedModels.includes(requestedModel);
    });
    // ...
}
```

**配置示例** (`configs/provider_pools.json`)：

```json
{
  "gemini-cli-oauth": [
    {
      "uuid": "provider-1",
      "notSupportedModels": ["gemini-3.0-pro", "gemini-3.5-flash"],
      "checkHealth": true
    }
  ]
}
```

---

### 3. 跨类型降级 (Fallback Chain)

当某个 Provider 类型的所有账号都不可用时，系统可以自动降级到备用类型。

**配置** (`configs/config.json`)：

```json
{
  "providerFallbackChain": {
    "gemini-cli-oauth": ["gemini-antigravity"],
    "gemini-antigravity": ["gemini-cli-oauth"],
    "claude-kiro-oauth": ["claude-custom"],
    "claude-custom": ["claude-kiro-oauth"]
  }
}
```

**降级流程：**

```
┌─────────────────────────────────────────────────────────────┐
│              selectProviderWithFallback() 流程               │
├─────────────────────────────────────────────────────────────┤
│  请求: providerType = "gemini-cli-oauth"                     │
│                    ↓                                        │
│  1. 尝试从 gemini-cli-oauth 池选择                            │
│     └─→ 成功? 返回 { config, actualProviderType, isFallback: false }
│                    ↓ 失败                                    │
│  2. 查找 fallbackChain: ["gemini-antigravity"]               │
│                    ↓                                        │
│  3. 检查协议兼容性 (gemini == gemini) ✓                       │
│                    ↓                                        │
│  4. 检查模型支持性                                            │
│                    ↓                                        │
│  5. 尝试从 gemini-antigravity 池选择                          │
│     └─→ 成功? 返回 { config, actualProviderType, isFallback: true }
│                    ↓ 失败                                    │
│  6. 返回 null (所有备选都不可用)                              │
└─────────────────────────────────────────────────────────────┘
```

**协议兼容性检查：**

降级只发生在协议兼容的类型之间（如 `gemini-*` 之间，`claude-*` 之间）：

```javascript
// src/provider-pool-manager.js:226-234
const primaryProtocol = getProtocolPrefix(providerType);
const fallbackProtocol = getProtocolPrefix(currentType);

if (primaryProtocol !== fallbackProtocol) {
    // 协议不兼容，跳过此 fallback 类型
    continue;
}
```

---

## 429 限流处理机制

系统采用**两层防护**处理 429 (Too Many Requests) 错误：

### 第一层：Provider 内部重试

每个 Provider 核心类都内置了指数退避重试机制：

```javascript
// src/gemini/gemini-core.js:433-437 (其他 Provider 类似)
if (error.response?.status === 429 && retryCount < maxRetries) {
    const delay = baseDelay * Math.pow(2, retryCount);
    console.log(`[API] Received 429. Retrying in ${delay}ms... (attempt ${retryCount + 1}/${maxRetries})`);
    await new Promise(resolve => setTimeout(resolve, delay));
    // 重试...
}
```

**重试参数**（可在 `config.json` 配置）：

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `REQUEST_MAX_RETRIES` | 3 | 最大重试次数 |
| `REQUEST_BASE_DELAY` | 1000 | 基础延迟时间 (ms) |

**延迟计算**：`delay = baseDelay * 2^retryCount`
- 第 1 次重试：1000ms
- 第 2 次重试：2000ms
- 第 3 次重试：4000ms

### 第二层：Provider Pool 熔断与降级

如果所有重试都失败，错误冒泡到 `handleContentGenerationRequest()`：

```javascript
// src/common.js:248-250
} catch (error) {
    if (providerPoolManager && pooluuid) {
        providerPoolManager.markProviderUnhealthy(toProvider, { uuid: pooluuid });
    }
}
```

**熔断流程**：

```
┌─────────────────────────────────────────────────────────────┐
│                    429 处理完整流程                           │
├─────────────────────────────────────────────────────────────┤
│  API 返回 429                                               │
│         ↓                                                   │
│  ┌─────────────────────────────────┐                        │
│  │ 第一层: Provider 内部重试        │                        │
│  │  - 指数退避: 1s → 2s → 4s       │                        │
│  │  - 最多重试 3 次                │                        │
│  └───────────┬─────────────────────┘                        │
│              ↓                                              │
│         所有重试成功?                                        │
│    ├─→ 是: 返回正常响应                                      │
│    ↓ 否                                                     │
│  ┌─────────────────────────────────┐                        │
│  │ 第二层: Pool Manager 熔断       │                        │
│  │  - errorCount++                 │                        │
│  │  - errorCount >= 3? → 熔断      │                        │
│  └───────────┬─────────────────────┘                        │
│              ↓                                              │
│         当前类型还有健康 Provider?                           │
│    ├─→ 是: 下次请求使用其他 Provider                         │
│    ↓ 否                                                     │
│  ┌─────────────────────────────────┐                        │
│  │ 第三层: Fallback 降级           │                        │
│  │  gemini-cli-oauth               │                        │
│  │       ↓ 全部不可用              │                        │
│  │  gemini-antigravity             │                        │
│  └─────────────────────────────────┘                        │
└─────────────────────────────────────────────────────────────┘
```

### Antigravity 特殊处理：单次请求内账号切换

Antigravity Provider 支持**单次请求内立即切换账号**，429 时按以下顺序尝试：

1. 切换 Base URL (daily → autopush)
2. 切换到账号池中的下一个账号
3. 所有账号都失败后直接返回错误

**核心实现** (`src/gemini/antigravity-core.js`)：

```javascript
// callApi() 中的 429 处理逻辑 (约 line 647-665)
if (error.response?.status === 429) {
    // 1. 先尝试切换 Base URL
    if (baseURLIndex + 1 < this.baseURLs.length) {
        return this.callApi(method, body, isRetry, retryCount, baseURLIndex + 1);
    }

    // 2. 所有 Base URL 都失败了，尝试切换到池中的下一个账号
    const nextAccount = await this._tryNextAccount();
    if (nextAccount) {
        // 重置 base URL index，用新账号从头开始
        return this.callApi(method, body, false, 0, 0);
    }

    // 3. 所有账号都试过了，直接失败（不再指数退避重试）
}
```

**账号切换辅助方法**：

```javascript
// _tryNextAccount() - 选择下一个可用账号
async _tryNextAccount() {
    // 标记当前账号为已尝试
    this.triedUuids.add(this.currentUuid);

    // 获取所有可用账号，排除已尝试的
    const availableProviders = allProviders.filter(p =>
        p.config.isHealthy &&
        !p.config.isDisabled &&
        !this.triedUuids.has(p.config.uuid)
    );

    // 按 LRU 选择下一个账号
    const nextProvider = availableProviders.sort((a, b) => {
        const timeA = a.config.lastUsed ? new Date(a.config.lastUsed).getTime() : 0;
        const timeB = b.config.lastUsed ? new Date(b.config.lastUsed).getTime() : 0;
        return timeA - timeB;
    })[0];

    // 重新加载新账号的凭证
    await this._reinitializeWithNewCredentials(nextProvider.config);
    return nextProvider.config;
}
```

**数据流**：

```
request-handler.js (providerPoolManager)
    ↓
service-manager.js (getApiServiceWithFallback)
    ↓ 传递 { providerPoolManager, providerType, currentUuid }
adapter.js (AntigravityApiServiceAdapter)
    ↓
antigravity-core.js (AntigravityApiService)
    └─ 可访问 providerPoolManager 进行账号切换
```

### 配置示例

**启用跨类型降级** (`configs/config.json`)：

```json
{
  "REQUEST_MAX_RETRIES": 3,
  "REQUEST_BASE_DELAY": 1000,
  "MAX_ERROR_COUNT": 3,
  "providerFallbackChain": {
    "gemini-cli-oauth": ["gemini-antigravity"],
    "claude-kiro-oauth": ["claude-custom"]
  }
}
```

**多账号池配置** (`configs/provider_pools.json`)：

```json
{
  "gemini-cli-oauth": [
    { "uuid": "account-1", "GEMINI_OAUTH_CREDS_FILE_PATH": "./creds1.json" },
    { "uuid": "account-2", "GEMINI_OAUTH_CREDS_FILE_PATH": "./creds2.json" },
    { "uuid": "account-3", "GEMINI_OAUTH_CREDS_FILE_PATH": "./creds3.json" }
  ]
}
```

### 单次请求 vs 多次请求（按 Provider 类型区分）

| Provider 类型 | 单次请求内 429 | 行为说明 |
|--------------|---------------|----------|
| **gemini-antigravity** | **立即切换账号** | 先切换 Base URL，再切换账号池中的下一个账号 |
| 其他同类型账号池 | 不切换账号 | 同账号重试 3 次后失败，下次请求换账号 |

**gemini-antigravity 特殊处理（已实现）：**

```
┌─────────────────────────────────────────────────────────────┐
│ 请求 #1: account-1                                          │
│   429 (daily) → 切换 Base URL → 429 (autopush)              │
│   → 立即切换账号 → account-2                                 │
│   429 (daily) → 切换 Base URL → 429 (autopush)              │
│   → 立即切换账号 → account-3                                 │
│   成功 ✓                                                    │
├─────────────────────────────────────────────────────────────┤
│ 如果所有账号都 429：                                         │
│   → 直接返回错误（不再指数退避重试）                          │
└─────────────────────────────────────────────────────────────┘
```

**其他 Provider 类型（如 gemini-cli-oauth）：**

```
┌─────────────────────────────────────────────────────────────┐
│ 请求 #1: account-1                                          │
│   429 → 重试(account-1) → 429 → 重试(account-1) → 失败       │
│   → markProviderUnhealthy(account-1)                        │
│   → 返回错误给客户端                                         │
├─────────────────────────────────────────────────────────────┤
│ 请求 #2: selectProvider() → account-2 (LRU 选择)            │
│   成功 ✓                                                    │
└─────────────────────────────────────────────────────────────┘
```

**相关文件修改清单**（实现单次请求内账号切换）：

| 文件 | 修改内容 |
|------|----------|
| `src/adapter.js` | `AntigravityApiServiceAdapter` 接收 `options` 参数传递 pool manager |
| `src/adapter.js` | `getServiceAdapter()` 接收 `options` 参数 |
| `src/service-manager.js` | `getApiServiceWithFallback()` 传递 pool manager 信息 |
| `src/gemini/antigravity-core.js` | 构造函数接收 pool manager 相关参数 |
| `src/gemini/antigravity-core.js` | 新增 `_resetTriedAccounts()`、`_tryNextAccount()`、`_reinitializeWithNewCredentials()` |
| `src/gemini/antigravity-core.js` | 修改 `callApi()` 和 `streamApi()` 的 429 处理逻辑 |

**设计考量：**

- **gemini-antigravity** 支持单次请求内切换是因为其架构允许动态重新加载凭证
- 其他 Provider 需要更复杂的重构才能支持此功能
- 如需为其他 Provider 类型实现类似功能，可参考 `antigravity-core.js` 的实现模式

---

## 健康管理机制

### 健康状态字段

每个 Provider 维护以下状态字段：

| 字段 | 类型 | 说明 |
|------|------|------|
| `isHealthy` | boolean | 是否健康 |
| `isDisabled` | boolean | 是否被管理员禁用 |
| `errorCount` | number | 连续错误计数 |
| `lastErrorTime` | ISO string | 最后一次错误时间 |
| `lastErrorMessage` | string | 最后一次错误信息 |
| `lastUsed` | ISO string | 最后使用时间 |
| `usageCount` | number | 累计使用次数 |
| `lastHealthCheckTime` | ISO string | 最后健康检查时间 |
| `lastHealthCheckModel` | string | 健康检查使用的模型 |

### 错误计数与自动熔断

```
┌─────────────────────────────────────────────────────────────┐
│                    错误处理流程                               │
├─────────────────────────────────────────────────────────────┤
│  API 调用失败                                                │
│         ↓                                                   │
│  markProviderUnhealthy(providerType, config, errorMessage)  │
│         ↓                                                   │
│  errorCount++                                               │
│  lastErrorTime = now                                        │
│  lastUsed = now  // 避免 LRU 策略重复选中失败节点              │
│         ↓                                                   │
│  errorCount >= maxErrorCount (默认 3)?                       │
│    ├─→ 是: isHealthy = false (标记为不健康)                   │
│    └─→ 否: 保持 isHealthy = true (仍可被选中)                 │
└─────────────────────────────────────────────────────────────┘
```

### 健康检查与自动恢复

系统定期执行健康检查（默认间隔 10 分钟），尝试恢复不健康的 Provider：

```javascript
// src/provider-pool-manager.js:463-519
async performHealthChecks(isInit = false) {
    for (const providerType in this.providerStatus) {
        for (const providerStatus of this.providerStatus[providerType]) {
            // 跳过最近刚出错的 Provider（避免频繁检查）
            if (!providerStatus.config.isHealthy &&
                now - lastErrorTime < this.healthCheckInterval) {
                continue;
            }

            // 执行实际健康检查
            const healthResult = await this._checkProviderHealth(providerType, providerConfig);

            if (healthResult.success) {
                // 恢复健康
                this.markProviderHealthy(providerType, providerConfig);
            } else {
                // 仍然不健康，增加错误计数
                this.markProviderUnhealthy(providerType, providerConfig);
            }
        }
    }
}
```

**健康检查请求格式：**

不同 Provider 类型使用不同的请求格式：

| Provider 类型 | 请求格式 |
|--------------|----------|
| `gemini-*` | `{ contents: [{ role: 'user', parts: [{ text: 'Hi' }] }] }` |
| `claude-kiro-*` | `{ messages: [{ role: 'user', content: 'Hi' }], model, max_tokens: 1 }` |
| `openaiResponses-*` | `{ input: [{ role: 'user', content: 'Hi' }], model }` |
| 其他 | `{ messages: [{ role: 'user', content: 'Hi' }], model }` |

---

## 状态持久化

### 防抖保存机制

为避免频繁的文件 I/O 操作，系统采用防抖（debounce）机制：

```
┌─────────────────────────────────────────────────────────────┐
│                    防抖保存流程                               │
├─────────────────────────────────────────────────────────────┤
│  状态变更 (选择/错误/健康检查)                                 │
│         ↓                                                   │
│  _debouncedSave(providerType)                               │
│         ↓                                                   │
│  pendingSaves.add(providerType)                             │
│  重置定时器 (默认 1 秒)                                       │
│         ↓                                                   │
│  1 秒内无新变更?                                             │
│    └─→ 是: _flushPendingSaves()                             │
│              ├─ 读取 provider_pools.json                     │
│              ├─ 合并所有待保存的 providerType                 │
│              └─ 一次性写入文件                                │
└─────────────────────────────────────────────────────────────┘
```

---

## 配置参数

### ProviderPoolManager 构造函数选项

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `maxErrorCount` | 3 | 连续错误达到此值后标记为不健康 |
| `healthCheckInterval` | 600000 (10分钟) | 健康检查最小间隔 (毫秒) |
| `logLevel` | 'info' | 日志级别: debug/info/warn/error |
| `saveDebounceTime` | 1000 (1秒) | 状态保存防抖时间 (毫秒) |

### Provider 配置字段

| 字段 | 必填 | 说明 |
|------|------|------|
| `uuid` | 是 | 唯一标识符 |
| `customName` | 否 | 自定义名称 |
| `checkHealth` | 否 | 是否启用健康检查 |
| `checkModelName` | 否 | 健康检查使用的模型（覆盖默认值）|
| `notSupportedModels` | 否 | 不支持的模型列表 |
| `isDisabled` | 否 | 是否禁用 |

---

## API 方法速查

| 方法 | 说明 |
|------|------|
| `selectProvider(type, model?, options?)` | 选择一个 Provider |
| `selectProviderWithFallback(type, model?, options?)` | 选择 Provider，支持降级 |
| `markProviderHealthy(type, config)` | 标记为健康 |
| `markProviderUnhealthy(type, config, errorMsg?)` | 标记为不健康 |
| `disableProvider(type, config)` | 禁用 Provider |
| `enableProvider(type, config)` | 启用 Provider |
| `performHealthChecks()` | 执行健康检查 |
| `getProviderStats(type)` | 获取统计信息 |
| `isAllProvidersUnhealthy(type)` | 检查是否全部不健康 |
| `getFallbackChain(type)` | 获取降级链配置 |
| `setFallbackChain(type, fallbacks)` | 设置降级链配置 |

---

## 流程图总结

### 通用 Provider 调度流程

```
                              ┌──────────────────┐
                              │   API 请求到达    │
                              └────────┬─────────┘
                                       ↓
                    ┌──────────────────────────────────────┐
                    │ selectProviderWithFallback(type, model) │
                    └────────┬─────────────────────────────┘
                             ↓
              ┌──────────────────────────────┐
              │ 1. 从主要类型池中选择 Provider │
              │    - 过滤 isHealthy=true     │
              │    - 过滤 isDisabled=false   │
              │    - 按模型过滤              │
              │    - LRU 排序选择            │
              └──────────┬───────────────────┘
                         ↓
                    找到可用 Provider?
                    ├─→ 是: 返回并更新 lastUsed
                    ↓ 否
              ┌──────────────────────────────┐
              │ 2. 尝试 Fallback 类型        │
              │    - 检查协议兼容性          │
              │    - 检查模型支持性          │
              │    - LRU 排序选择            │
              └──────────┬───────────────────┘
                         ↓
                    找到可用 Provider?
                    ├─→ 是: 返回 (isFallback: true)
                    ↓ 否
              ┌──────────────────────────────┐
              │ 3. 返回 null (无可用 Provider) │
              └──────────────────────────────┘
```

### gemini-antigravity 429 处理流程

```
                              ┌──────────────────┐
                              │  API 调用 429     │
                              └────────┬─────────┘
                                       ↓
              ┌──────────────────────────────┐
              │ 1. 切换 Base URL             │
              │    daily → autopush          │
              └──────────┬───────────────────┘
                         ↓
                    成功?
                    ├─→ 是: 返回响应
                    ↓ 否 (仍然 429)
              ┌──────────────────────────────┐
              │ 2. _tryNextAccount()         │
              │    - 标记当前账号已尝试       │
              │    - 过滤可用账号             │
              │    - LRU 选择下一个账号       │
              │    - 重新加载凭证             │
              └──────────┬───────────────────┘
                         ↓
                    还有可用账号?
                    ├─→ 是: 回到步骤 1 (新账号)
                    ↓ 否
              ┌──────────────────────────────┐
              │ 3. 直接返回 429 错误          │
              │    (不再指数退避重试)         │
              └──────────────────────────────┘
```

### 错误处理流程

```
                              ┌──────────────────┐
                              │   API 调用完成    │
                              └────────┬─────────┘
                                       ↓
                                   调用成功?
                    ├─→ 是: markProviderHealthy() → errorCount=0
                    ↓ 否
                         markProviderUnhealthy()
                                   ↓
                         errorCount >= maxErrorCount?
                    ├─→ 是: isHealthy=false (熔断)
                    ↓ 否
                         继续使用，等待后续请求或健康检查
```
