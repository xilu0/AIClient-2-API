import * as fs from 'fs'; // Import fs module
import * as crypto from 'crypto'; // Import crypto module for UUID generation
import { getServiceAdapter } from './adapter.js';
import { MODEL_PROVIDER, getProtocolPrefix } from '../utils/common.js';
import { getProviderModels } from './provider-models.js';
import axios from 'axios';
import { getStorageAdapter, isStorageInitialized } from '../core/storage-factory.js';
import { KeyedMutex } from '../utils/async-mutex.js';

/**
 * Manages a pool of API service providers, handling their health and selection.
 */
export class ProviderPoolManager {
    // 默认健康检查模型配置
    // 键名必须与 MODEL_PROVIDER 常量值一致
    static DEFAULT_HEALTH_CHECK_MODELS = {
        'gemini-cli-oauth': 'gemini-2.5-flash',
        'gemini-antigravity': 'gemini-2.5-flash',
        'openai-custom': 'gpt-4o-mini',
        'claude-custom': 'claude-3-7-sonnet-20250219',
        'claude-kiro-oauth': 'claude-haiku-4-5',
        'openai-qwen-oauth': 'qwen3-coder-flash',
        'openai-iflow': 'qwen3-coder-plus',
        'openai-codex-oauth': 'gpt-5-codex-mini',
        'openaiResponses-custom': 'gpt-4o-mini',
        'forward-api': 'gpt-4o-mini',
    };

    constructor(providerPools, options = {}) {
        this.providerPools = providerPools;
        this.globalConfig = options.globalConfig || {}; // 存储全局配置
        this.providerStatus = {}; // Tracks health and usage for each provider instance
        this.roundRobinIndex = {}; // Tracks the current index for round-robin selection for each provider type
        // 使用 ?? 运算符确保 0 也能被正确设置，而不是被 || 替换为默认值
        this.maxErrorCount = options.maxErrorCount ?? 10; // Default to 10 errors before marking unhealthy
        this.healthCheckInterval = options.healthCheckInterval ?? 10 * 60 * 1000; // Default to 10 minutes

            // 日志级别控制
        this.logLevel = options.logLevel || 'info'; // 'debug', 'info', 'warn', 'error'
        
        // 添加防抖机制，避免频繁的文件 I/O 操作
        this.saveDebounceTime = options.saveDebounceTime || 1000; // 默认1秒防抖
        this.saveTimer = null;
        this.pendingSaves = new Set(); // 记录待保存的 providerType
        
        // Fallback 链配置
        this.fallbackChain = options.globalConfig?.providerFallbackChain || {};
        
        // Model Fallback 映射配置
        this.modelFallbackMapping = options.globalConfig?.modelFallbackMapping || {};

        // 并发控制：每个 providerType 的选择锁
        // 用于确保 selectProvider 的排序 and 更新操作是原子的
        this._selectionLocks = {};
        this._isSelecting = {}; // Legacy: kept for compatibility
        // Promise-based mutex for selection (avoids busy-wait polling)
        this._selectionMutex = new KeyedMutex();
        
        // 文件写入锁，防止并发读写冲突
        this._fileLock = false;

        // 异步写入队列，避免阻塞API请求
        this._writeQueue = [];
        this._isProcessingQueue = false;

        // Storage adapter reference (lazy initialized)
        this._storageAdapter = null;

        // --- V2: 读写分离 and 异步刷新队列 ---
        // 刷新并发控制配置
        this.refreshConcurrency = {
            global: options.globalConfig?.REFRESH_CONCURRENCY_GLOBAL ?? 1, // 降低全局并发数
            perProvider: options.globalConfig?.REFRESH_CONCURRENCY_PER_PROVIDER ?? 1 // 每个提供商内部最大并行数
        };
        
        this.activeProviderRefreshes = 0; // 当前正在刷新的提供商类型数量
        this.globalRefreshWaiters = []; // 等待全局并发槽位的任务
        
        this.warmupTarget = options.globalConfig?.WARMUP_TARGET || 0; // 默认预热0个节点
        this.refreshingUuids = new Set(); // 正在刷新的节点 UUID 集合
        
        this.refreshQueues = {}; // 按 providerType 分组的队列
        // 缓冲队列机制：延迟5秒，去重后再执行刷新
        this.refreshBufferQueues = {}; // 按 providerType 分组的缓冲队列
        this.refreshBufferTimers = {}; // 按 providerType 分组的定时器
        this.bufferDelay = options.globalConfig?.REFRESH_BUFFER_DELAY ?? 5000; // 默认5秒缓冲延迟
        
        // 用于并发选点时的原子排序辅助（自增序列）
        this._selectionSequence = 0;
        // P5 Fix (方案A): 基于时间戳的序列号基准，避免进程重启后序列号倒退
        this._sequenceBase = Date.now() * 1000;

        // P3 Fix: Batch Redis update queue for usage increments
        // Accumulates updates and flushes them periodically to reduce Redis round-trips
        this._usageBatchQueue = new Map(); // Map<providerType:uuid, {count, lastUsed}>
        this._usageBatchTimer = null;
        // P5 Fix (方案C): 自适应批处理延迟配置
        this._usageBatchIntervalMin = options.globalConfig?.USAGE_BATCH_INTERVAL_MIN ?? 10;
        this._usageBatchIntervalMax = options.globalConfig?.USAGE_BATCH_INTERVAL_MAX ?? 100;
        this._usageBatchInterval = options.globalConfig?.USAGE_BATCH_INTERVAL ?? 50; // 初始值改为50ms

        // P4 Fix: Throttle recovery check to reduce CPU overhead
        // Only check scheduled recoveries once per second instead of every request
        this._lastRecoveryCheckTime = 0;
        this._recoveryCheckThrottleMs = options.globalConfig?.RECOVERY_CHECK_THROTTLE_MS ?? 1000;

        // P5 Fix (方案B): 防止100ms内重复选择同一节点
        this._recentSelections = new Map(); // Map<providerType:uuid, timestamp>
        this._recentSelectionWindow = options.globalConfig?.RECENT_SELECTION_WINDOW ?? 100;
        this._recentSelectionCleanupCounter = 0;

        // P1-2: Provider UUID 索引，O(1) 查询
        this._providerIndex = new Map(); // Map<uuid, { providerType, statusEntry }>

        this.initializeProviderStatus();
    }

    /**
     * P3 Fix: Queue a usage increment for batch processing
     * @private
     */
    _queueUsageIncrement(providerType, uuid) {
        const key = `${providerType}:${uuid}`;
        const existing = this._usageBatchQueue.get(key);

        if (existing) {
            existing.count++;
            existing.lastUsed = new Date().toISOString();
        } else {
            this._usageBatchQueue.set(key, {
                providerType,
                uuid,
                count: 1,
                lastUsed: new Date().toISOString()
            });
        }

        // Schedule batch flush if not already scheduled
        if (!this._usageBatchTimer) {
            this._usageBatchTimer = setTimeout(() => {
                this._flushUsageBatch();
            }, this._usageBatchInterval);
        }
    }

    /**
     * P3 Fix: Flush accumulated usage increments to Redis
     * @private
     */
    async _flushUsageBatch() {
        this._usageBatchTimer = null;

        // P5 Fix (方案C): 记录当前队列大小用于自适应调整
        const currentQueueSize = this._usageBatchQueue.size;

        if (currentQueueSize === 0) {
            return;
        }

        const adapter = this._getStorageAdapter();
        if (!adapter || adapter.getType() !== 'redis') {
            // Fall back to individual updates for non-Redis storage
            this._usageBatchQueue.clear();
            return;
        }

        // Copy and clear the queue
        const updates = Array.from(this._usageBatchQueue.values());
        this._usageBatchQueue.clear();

        // Fire and forget - don't await to avoid blocking
        Promise.resolve().then(async () => {
            try {
                // Process updates - could be optimized with Redis pipeline in the future
                for (const update of updates) {
                    await adapter.incrementUsage(update.providerType, update.uuid);
                }
                this._log('debug', `[P3 Batch] Flushed ${updates.length} usage updates to Redis`);
            } catch (err) {
                this._log('error', `[P3 Batch] Failed to flush usage updates: ${err.message}`);
            }
        });

        // P5 Fix (方案C): 自适应调整下次批处理间隔
        if (currentQueueSize > 50) {
            // 高并发：队列积压多，缩短间隔加快处理
            this._usageBatchInterval = Math.max(
                this._usageBatchIntervalMin,
                this._usageBatchInterval - 10
            );
            this._log('debug', `[P5 Adaptive] Batch interval decreased to ${this._usageBatchInterval}ms (queue: ${currentQueueSize})`);
        } else if (currentQueueSize < 10 && this._usageBatchInterval < this._usageBatchIntervalMax) {
            // 低并发：队列较空，延长间隔减少CPU开销
            this._usageBatchInterval = Math.min(
                this._usageBatchIntervalMax,
                this._usageBatchInterval + 10
            );
            this._log('debug', `[P5 Adaptive] Batch interval increased to ${this._usageBatchInterval}ms (queue: ${currentQueueSize})`);
        }

        // 重新调度下次刷新（使用新的间隔）
        if (this._usageBatchQueue.size > 0 || currentQueueSize > 0) {
            this._usageBatchTimer = setTimeout(() => {
                this._flushUsageBatch();
            }, this._usageBatchInterval);
        }
    }

    /**
     * 检查所有节点的配置文件，如果发现即将过期则触发刷新
     */
    async checkAndRefreshExpiringNodes() {
        this._log('info', 'Checking nodes for approaching expiration dates using provider adapters...');
        
        for (const providerType in this.providerStatus) {
            const providers = this.providerStatus[providerType];
            for (const providerStatus of providers) {
                const config = providerStatus.config;
                
                // 根据 providerType 确定配置文件路径字段名
                let configPath = null;
                if (providerType.startsWith('claude-kiro')) {
                    configPath = config.KIRO_OAUTH_CREDS_FILE_PATH;
                } else if (providerType.startsWith('gemini-cli')) {
                    configPath = config.GEMINI_OAUTH_CREDS_FILE_PATH;
                } else if (providerType.startsWith('gemini-antigravity')) {
                    configPath = config.ANTIGRAVITY_OAUTH_CREDS_FILE_PATH;
                } else if (providerType.startsWith('openai-qwen')) {
                    configPath = config.QWEN_OAUTH_CREDS_FILE_PATH;
                } else if (providerType.startsWith('openai-iflow')) {
                    configPath = config.IFLOW_OAUTH_CREDS_FILE_PATH;
                } else if (providerType.startsWith('openai-codex')) {
                    configPath = config.CODEX_OAUTH_CREDS_FILE_PATH;
                }
                
                // console.log(`Checking node ${providerStatus.uuid} (${providerType}) expiry date... configPath: ${configPath}`);
                // 排除不健康和禁用的节点
                if (!config.isHealthy || config.isDisabled) continue;

                if (configPath && fs.existsSync(configPath)) {
                    try {
                        // 检查配置文件是否接近过期（这里应该有具体的过期检查逻辑）
                        // 暂时禁用自动刷新，避免CPU占用过高
                        const shouldRefresh = false; // TODO: 实现真正的过期检查逻辑
                        if (shouldRefresh) {
                            this._log('warn', `Node ${providerStatus.uuid} (${providerType}) is near expiration. Enqueuing refresh...`);
                            this._enqueueRefresh(providerType, providerStatus);
                        }
                    } catch (err) {
                        this._log('error', `Failed to check expiry for node ${providerStatus.uuid}: ${err.message}`);
                    }
                } else {
                    this._log('debug', `Node ${providerStatus.uuid} (${providerType}) has no valid config file path or file does not exist.`);
                }
            }
        }
    }

    /**
     * 系统预热逻辑：按提供商分组，每组预热 warmupTarget 个节点
     * @returns {Promise<void>}
     */
    async warmupNodes() {
        if (this.warmupTarget <= 0) return;
        this._log('info', `Starting system warmup (Group Target: ${this.warmupTarget} nodes per provider)...`);

        const nodesToWarmup = [];

        for (const type in this.providerStatus) {
            const pool = this.providerStatus[type];
            
            // 挑选当前提供商下需要预热的节点
            const candidates = pool
                .filter(p => p.config.isHealthy && !p.config.isDisabled && !this.refreshingUuids.has(p.uuid))
                .sort((a, b) => {
                    // 优先级 A: 明确标记需要刷新的
                    if (a.config.needsRefresh && !b.config.needsRefresh) return -1;
                    if (!a.config.needsRefresh && b.config.needsRefresh) return 1;

                    // 优先级 B: 按照正常的选择权重排序（最久没用过的优先补）
                    const scoreA = this._calculateNodeScore(a);
                    const scoreB = this._calculateNodeScore(b);
                    return scoreA - scoreB;
                })
                .slice(0, this.warmupTarget);

            candidates.forEach(p => nodesToWarmup.push({ type, status: p }));
        }

        this._log('info', `Warmup: Selected total ${nodesToWarmup.length} nodes across all providers to refresh.`);

        for (const node of nodesToWarmup) {
            this._enqueueRefresh(node.type, node.status, true);
        }

        // 注意：warmupNodes 不等待队列结束，它是异步后台执行的
    }

    /**
     * 将节点放入缓冲队列，延迟5秒后去重并执行刷新
     * @param {string} providerType 
     * @param {object} providerStatus 
     * @param {boolean} force - 是否强制刷新（跳过缓冲队列）
     * @private
     */
    _enqueueRefresh(providerType, providerStatus, force = false) {
        const uuid = providerStatus.uuid;
        
        // 如果已经在刷新中，直接返回
        if (this.refreshingUuids.has(uuid)) {
            this._log('debug', `Node ${uuid} is already in refresh queue.`);
            return;
        }

        // 判断提供商池内的总可用节点数，小于5个时，不等待缓冲，直接加入刷新队列
        const healthyCount = this.getHealthyCount(providerType);
        if (healthyCount < 5) {
            this._log('info', `Provider ${providerType} has only ${healthyCount} healthy nodes. Bypassing buffer and enqueuing refresh for ${uuid} immediately.`);
            this._enqueueRefreshImmediate(providerType, providerStatus, force);
            return;
        }

        // 初始化缓冲队列
        if (!this.refreshBufferQueues[providerType]) {
            this.refreshBufferQueues[providerType] = new Map(); // 使用 Map 自动去重
        }

        const bufferQueue = this.refreshBufferQueues[providerType];
        
        // 检查是否已在缓冲队列中
        const existing = bufferQueue.get(uuid);
        const isNewEntry = !existing;
        
        // 更新或添加节点（保留 force: true 状态）
        bufferQueue.set(uuid, {
            providerStatus,
            force: existing ? (existing.force || force) : force
        });
        
        if (isNewEntry) {
            this._log('debug', `Node ${uuid} added to buffer queue for ${providerType}. Buffer size: ${bufferQueue.size}`);
        } else {
            this._log('debug', `Node ${uuid} already in buffer queue, updated force flag. Buffer size: ${bufferQueue.size}`);
        }

        // 只在新增节点或缓冲队列为空时重置定时器
        // 避免频繁重置导致刷新被无限延迟
        if (isNewEntry || !this.refreshBufferTimers[providerType]) {
            // 清除之前的定时器
            if (this.refreshBufferTimers[providerType]) {
                clearTimeout(this.refreshBufferTimers[providerType]);
            }

            // 设置新的定时器，延迟5秒后处理缓冲队列
            this.refreshBufferTimers[providerType] = setTimeout(() => {
                this._flushRefreshBuffer(providerType);
            }, this.bufferDelay);
        }
    }

    /**
     * 处理缓冲队列，将去重后的节点放入实际刷新队列
     * @param {string} providerType 
     * @private
     */
    _flushRefreshBuffer(providerType) {
        const bufferQueue = this.refreshBufferQueues[providerType];
        if (!bufferQueue || bufferQueue.size === 0) {
            return;
        }

        this._log('info', `Flushing refresh buffer for ${providerType}. Processing ${bufferQueue.size} unique nodes.`);

        // 将缓冲队列中的所有节点放入实际刷新队列
        for (const [uuid, { providerStatus, force }] of bufferQueue.entries()) {
            this._enqueueRefreshImmediate(providerType, providerStatus, force);
        }

        // 清空缓冲队列和定时器
        bufferQueue.clear();
        delete this.refreshBufferTimers[providerType];
    }

    /**
     * 立即将节点放入刷新队列（内部方法，由缓冲队列调用）
     * @param {string} providerType 
     * @param {object} providerStatus 
     * @param {boolean} force 
     * @private
     */
    _enqueueRefreshImmediate(providerType, providerStatus, force = false) {
        const uuid = providerStatus.uuid;
        
        // 再次检查是否已经在刷新中（防止并发问题）
        if (this.refreshingUuids.has(uuid)) {
            this._log('debug', `Node ${uuid} is already in refresh queue (immediate check).`);
            return;
        }

        this.refreshingUuids.add(uuid);

        // 初始化提供商队列
        if (!this.refreshQueues[providerType]) {
            this.refreshQueues[providerType] = {
                activeCount: 0,
                waitingTasks: []
            };
        }

        const queue = this.refreshQueues[providerType];

        const runTask = async () => {
            try {
                await this._refreshNodeToken(providerType, providerStatus, force);
            } catch (err) {
                this._log('error', `Failed to process refresh for node ${uuid}: ${err.message}`);
            } finally {
                this.refreshingUuids.delete(uuid);
                
                // 再次获取当前队列引用
                const currentQueue = this.refreshQueues[providerType];
                if (!currentQueue) return;

                currentQueue.activeCount--;
                
                // 1. 尝试从当前提供商队列中取下一个任务
                if (currentQueue.waitingTasks.length > 0) {
                    const nextTask = currentQueue.waitingTasks.shift();
                    currentQueue.activeCount++;
                    // 使用 Promise.resolve().then 避免过深的递归
                    Promise.resolve().then(nextTask);
                } else if (currentQueue.activeCount === 0) {
                    // 2. 如果当前提供商的所有任务都完成了，释放全局槽位
                    // 只有在确定队列为空且没有新任务时才清理
                    if (currentQueue.waitingTasks.length === 0 &&
                        this.refreshQueues[providerType] === currentQueue) {
                        this.activeProviderRefreshes--;
                        delete this.refreshQueues[providerType]; // 清理空队列
                    }
                    
                    // 3. 尝试启动下一个等待中的提供商队列
                    if (this.globalRefreshWaiters.length > 0) {
                        const nextProviderStart = this.globalRefreshWaiters.shift();
                        Promise.resolve().then(nextProviderStart);
                    }
                }
            }
        };

        const tryStartProviderQueue = () => {
            if (queue.activeCount < this.refreshConcurrency.perProvider) {
                queue.activeCount++;
                runTask();
            } else {
                queue.waitingTasks.push(runTask);
            }
        };

        // 检查全局并发限制（按提供商分组）
        // 情况1: 该提供商已经在运行，直接加入其队列（不占用新的全局槽位）
        if (this.refreshQueues[providerType].activeCount > 0) {
            tryStartProviderQueue();
        }
        // 情况2: 该提供商未运行，需要检查全局槽位
        else if (this.activeProviderRefreshes < this.refreshConcurrency.global) {
            this.activeProviderRefreshes++;
            tryStartProviderQueue();
        }
        // 情况3: 全局槽位已满，进入等待队列
        else {
            this.globalRefreshWaiters.push(() => {
                // 重新获取最新的队列引用
                if (!this.refreshQueues[providerType]) {
                    this.refreshQueues[providerType] = {
                        activeCount: 0,
                        waitingTasks: []
                    };
                }
                // 重要：从等待队列启动时需要增加全局计数
                this.activeProviderRefreshes++;
                tryStartProviderQueue();
            });
        }
    }

    /**
     * 实际执行节点刷新逻辑
     * @private
     */
    async _refreshNodeToken(providerType, providerStatus, force = false) {
        const config = providerStatus.config;
        
        // 检查刷新次数是否已达上限（最大3次）
        const currentRefreshCount = config.refreshCount || 0;
        if (currentRefreshCount >= 3 && !force) {
            this._log('warn', `Node ${providerStatus.uuid} has reached maximum refresh count (3), marking as unhealthy`);
            // 标记为不健康
            this.markProviderUnhealthyImmediately(providerType, config, 'Maximum refresh count (3) reached');
            return;
        }
        
        // 添加5秒内的随机等待时间，避免并发刷新时的冲突
        // const randomDelay = Math.floor(Math.random() * 5000);
        // this._log('info', `Starting token refresh for node ${providerStatus.uuid} (${providerType}) with ${randomDelay}ms delay`);
        // await new Promise(resolve => setTimeout(resolve, randomDelay));

        try {
            // 增加刷新计数
            config.refreshCount = currentRefreshCount + 1;

            // 使用适配器进行刷新
            const tempConfig = {
                ...config,
                MODEL_PROVIDER: providerType
            };
            const serviceAdapter = getServiceAdapter(tempConfig);
            
            // 调用适配器的 refreshToken 方法（内部封装了具体的刷新逻辑）
            if (typeof serviceAdapter.refreshToken === 'function') {
                const startTime = Date.now();
                force ? await serviceAdapter.forceRefreshToken() : await serviceAdapter.refreshToken() 
                const duration = Date.now() - startTime;
                this._log('info', `Token refresh successful for node ${providerStatus.uuid} (Duration: ${duration}ms)`);
            } else {
                throw new Error(`refreshToken method not implemented for ${providerType}`);
            }

        } catch (error) {
            this._log('error', `Token refresh failed for node ${providerStatus.uuid}: ${error.message}`);
            this.markProviderUnhealthyImmediately(providerType, config, `Refresh failed: ${error.message}`);
            throw error;
        }
    }

    /**
     * 解析时间戳，支持数字（毫秒）或 ISO 字符串格式
     * 使用内存缓存避免重复解析
     * @private
     */
    _parseTimestamp(value, cacheKey, config) {
        if (!value) return 0;
        // 如果是数字，直接返回
        if (typeof value === 'number') return value;
        // 检查缓存
        const cacheField = `_${cacheKey}Ts`;
        if (config[cacheField] !== undefined && config[`_${cacheKey}Src`] === value) {
            return config[cacheField];
        }
        // 解析并缓存
        const ts = new Date(value).getTime();
        config[cacheField] = ts;
        config[`_${cacheKey}Src`] = value;
        return ts;
    }

    /**
     * 计算节点的权重/评分，用于排序
     * 分数越低，优先级越高
     * @private
     */
    _calculateNodeScore(providerStatus, now = Date.now()) {
        const config = providerStatus.config;

        // 1. 基础健康分：不健康的排最后
        if (!config.isHealthy || config.isDisabled) return 1e18;

        // 2. 预热/刷新分：2分钟内刷新过且使用次数极少的节点视为"新鲜"，分数极低（最高优）
        // 使用缓存的时间戳解析，避免重复创建 Date 对象
        const lastHealthCheckTs = this._parseTimestamp(config.lastHealthCheckTime, 'lastHealthCheck', config);
        const isFresh = lastHealthCheckTs &&
                        (now - lastHealthCheckTs < 120000) &&
                        (config.usageCount === 0);
        if (isFresh) return -2e18; // 极其优先

        // 3. 权重计算逻辑：
        // 改进点：使用 lastUsedTime + usageCount 惩罚 + selectionSequence 惩罚
        // selectionSequence 用于在同一毫秒内彻底打破平局

        // 使用缓存的时间戳解析
        const lastUsedTime = config.lastUsed
            ? this._parseTimestamp(config.lastUsed, 'lastUsed', config)
            : (now - 86400000); // 没用过的视为 24 小时前用过（更旧）
        const usageCount = config.usageCount || 0;
        const lastSelectionSeq = config._lastSelectionSeq || 0;

        // 核心目标：选分最小的。
        // - lastUsedTime 越久，分越小。
        // - usageCount 越多，分越大。
        // - lastSelectionSeq 越大（最近选过），分越大。

        // usageCount * 10000: 每多用一次，权重增加 10 秒
        // lastSelectionSeq * 1000: 即使毫秒时间相同，序列号也会让分数产生差异（增加 1 秒权重）
        // 这样可以确保在毫秒级并发下，刚被选中的节点会立刻排到队列末尾
        const baseScore = lastUsedTime + (usageCount * 10000);
        const sequenceScore = lastSelectionSeq * 1000;

        return baseScore + sequenceScore;
    }

    /**
     * 获取指定类型的健康节点数量
     */
    getHealthyCount(providerType) {
        return (this.providerStatus[providerType] || []).filter(p => p.config.isHealthy && !p.config.isDisabled).length;
    }

    /**
     * 日志输出方法，支持日志级别控制
     * P4 优化：支持延迟求值，message 可以是字符串或返回字符串的函数
     * 使用函数时，只有在日志级别匹配时才会执行，避免不必要的 JSON.stringify 等开销
     * @param {string} level - 日志级别
     * @param {string|function} message - 日志消息或返回消息的函数
     * @private
     */
    _log(level, message) {
        const levels = { debug: 0, info: 1, warn: 2, error: 3 };
        if (levels[level] >= levels[this.logLevel]) {
            const logMethod = level === 'debug' ? 'log' : level;
            // P4 优化：支持延迟求值
            const msg = typeof message === 'function' ? message() : message;
            console[logMethod](`[ProviderPoolManager] ${msg}`);
        }
    }

    /**
     * Get the storage adapter (lazy initialization)
     * @returns {import('../core/storage-adapter.js').StorageAdapter|null}
     * @private
     */
    _getStorageAdapter() {
        if (this._storageAdapter) {
            return this._storageAdapter;
        }
        if (isStorageInitialized()) {
            this._storageAdapter = getStorageAdapter();
            return this._storageAdapter;
        }
        return null;
    }

    /**
     * Check if Redis storage is being used
     * @returns {boolean}
     */
    isUsingRedis() {
        const adapter = this._getStorageAdapter();
        return adapter && adapter.getType() === 'redis';
    }

    /**
     * Set the storage adapter (for dependency injection)
     * @param {import('../core/storage-adapter.js').StorageAdapter} adapter
     */
    setStorageAdapter(adapter) {
        this._storageAdapter = adapter;
        this._log('info', `Storage adapter set: ${adapter.getType()}`);
    }

    /**
     * 查找指定的 provider
     * P1-2: 使用索引实现 O(1) 查询
     * @private
     */
    _findProvider(providerType, uuid) {
        if (!providerType || !uuid) {
            this._log('error', `Invalid parameters: providerType=${providerType}, uuid=${uuid}`);
            return null;
        }
        // P1-2: 优先使用索引查找
        const indexed = this._providerIndex.get(uuid);
        if (indexed && indexed.providerType === providerType) {
            return indexed.statusEntry;
        }
        // 回退到线性搜索（索引可能不同步时）
        const pool = this.providerStatus[providerType];
        return pool?.find(p => p.uuid === uuid) || null;
    }

    /**
     * Load provider pools from storage adapter
     * @returns {Promise<Object>} Provider pools data
     */
    async loadProviderPoolsFromStorage() {
        const adapter = this._getStorageAdapter();
        if (adapter) {
            try {
                const pools = await adapter.getProviderPools();
                if (pools && Object.keys(pools).length > 0) {
                    this._log('info', `Loaded provider pools from ${adapter.getType()} storage`);
                    return pools;
                }
            } catch (error) {
                this._log('error', `Failed to load from storage adapter: ${error.message}`);
            }
        }
        return null;
    }

    /**
     * Reload provider pools from storage and reinitialize
     * @returns {Promise<boolean>} Whether reload was successful
     */
    async reloadFromStorage() {
        const pools = await this.loadProviderPoolsFromStorage();
        if (pools) {
            this.providerPools = pools;
            this.initializeProviderStatus();
            return true;
        }
        return false;
    }

    /**
     * Sync current state to storage adapter (useful for initial migration)
     * @returns {Promise<void>}
     */
    async syncToStorage() {
        const adapter = this._getStorageAdapter();
        if (!adapter) {
            this._log('warn', 'No storage adapter available for sync');
            return;
        }

        try {
            // Build pools from current status
            const pools = {};
            for (const providerType in this.providerStatus) {
                pools[providerType] = this.providerStatus[providerType].map(p => {
                    const config = { ...p.config };
                    // Convert Date objects to ISOString
                    if (config.lastUsed instanceof Date) {
                        config.lastUsed = config.lastUsed.toISOString();
                    }
                    if (config.lastErrorTime instanceof Date) {
                        config.lastErrorTime = config.lastErrorTime.toISOString();
                    }
                    if (config.lastHealthCheckTime instanceof Date) {
                        config.lastHealthCheckTime = config.lastHealthCheckTime.toISOString();
                    }
                    return config;
                });
            }

            await adapter.saveAllProviderPools(pools);
            this._log('info', `Synced all provider pools to ${adapter.getType()} storage`);
        } catch (error) {
            this._log('error', `Failed to sync to storage: ${error.message}`);
            throw error;
        }
    }

    /**
     * Initializes the status for each provider in the pools.
     * Initially, all providers are considered healthy and have zero usage.
     */
    initializeProviderStatus() {
        // P1-2: 重建索引前先清空
        this._providerIndex.clear();

        for (const providerType in this.providerPools) {
            this.providerStatus[providerType] = [];
            this.roundRobinIndex[providerType] = 0; // Initialize round-robin index for each type
            // 只有在锁不存在时才初始化，避免在运行中被重置导致并发问题
            if (!this._selectionLocks[providerType]) {
                this._selectionLocks[providerType] = Promise.resolve();
            }
            this.providerPools[providerType].forEach((providerConfig) => {
                // Ensure initial health and usage stats are present in the config
                providerConfig.isHealthy = providerConfig.isHealthy !== undefined ? providerConfig.isHealthy : true;
                providerConfig.isDisabled = providerConfig.isDisabled !== undefined ? providerConfig.isDisabled : false;
                providerConfig.lastUsed = providerConfig.lastUsed !== undefined ? providerConfig.lastUsed : null;
                providerConfig.usageCount = providerConfig.usageCount !== undefined ? providerConfig.usageCount : 0;
                providerConfig.errorCount = providerConfig.errorCount !== undefined ? providerConfig.errorCount : 0;

                // --- V2: 刷新监控字段 ---
                providerConfig.needsRefresh = providerConfig.needsRefresh !== undefined ? providerConfig.needsRefresh : false;
                providerConfig.refreshCount = providerConfig.refreshCount !== undefined ? providerConfig.refreshCount : 0;

                // 优化2: 简化 lastErrorTime 处理逻辑
                providerConfig.lastErrorTime = providerConfig.lastErrorTime instanceof Date
                    ? providerConfig.lastErrorTime.toISOString()
                    : (providerConfig.lastErrorTime || null);

                // 健康检测相关字段
                providerConfig.lastHealthCheckTime = providerConfig.lastHealthCheckTime || null;
                providerConfig.lastHealthCheckModel = providerConfig.lastHealthCheckModel || null;
                providerConfig.lastErrorMessage = providerConfig.lastErrorMessage || null;
                providerConfig.customName = providerConfig.customName || null;

                const statusEntry = {
                    config: providerConfig,
                    uuid: providerConfig.uuid, // Still keep uuid at the top level for easy access
                };
                this.providerStatus[providerType].push(statusEntry);

                // P1-2: 添加到索引
                this._providerIndex.set(providerConfig.uuid, { providerType, statusEntry });
            });
        }
        this._log('info', `Initialized provider statuses: ok (maxErrorCount: ${this.maxErrorCount})`);
    }

    /**
     * Selects a provider from the pool for a given provider type.
     * Currently uses a simple round-robin for healthy providers.
     * If requestedModel is provided, providers that don't support the model will be excluded.
     *
     * 注意：此方法现在返回 Promise，使用互斥锁确保并发安全。
     *
     * @param {string} providerType - The type of provider to select (e.g., 'gemini-cli', 'openai-custom').
     * @param {string} [requestedModel] - Optional. The model name to filter providers by.
     * @returns {Promise<object|null>} The selected provider's configuration, or null if no healthy provider is found.
     */
    async selectProvider(providerType, requestedModel = null, options = {}) {
        // 参数校验
        if (!providerType || typeof providerType !== 'string') {
            this._log('error', `Invalid providerType: ${providerType}`);
            return null;
        }

        // P2 Fix: Remove mutex lock to allow concurrent provider selection
        // The lock was causing serialization of all requests under high concurrency.
        //
        // Why this is safe:
        // 1. Redis atomic operations (Lua scripts) guarantee data consistency
        // 2. _lastSelectionSeq ensures different requests get different providers
        // 3. In-memory cache updates are atomic (single-threaded JS)
        // 4. Worst case: two requests select same provider, which is acceptable
        //
        // Trade-off: Slightly less perfect load balancing for much higher throughput
        return this._doSelectProvider(providerType, requestedModel, options);
    }

    /**
     * 实际执行 provider 选择的内部方法
     * @private
     */
    _doSelectProvider(providerType, requestedModel, options) {
        const availableProviders = this.providerStatus[providerType] || [];

        // 检查并恢复已到恢复时间的提供商（节流执行）
        this._checkAndRecoverScheduledProviders(providerType);

        // 获取固定时间戳，确保排序过程中一致
        const now = Date.now();

        // 健康 provider 过滤函数
        const filterHealthy = (providers) => providers.filter(p =>
            p.config.isHealthy && !p.config.isDisabled && !p.config.needsRefresh
        );

        let availableAndHealthyProviders = filterHealthy(availableProviders);

        // P4 安全检查：如果没有健康 provider，强制执行恢复检查后重试
        // 这确保节流不会导致请求在有可恢复 provider 时失败
        if (availableAndHealthyProviders.length === 0) {
            this._checkAndRecoverScheduledProviders(providerType, true); // 强制执行
            availableAndHealthyProviders = filterHealthy(availableProviders);
        }

        // 如果指定了模型，则排除不支持该模型的提供商
        if (requestedModel) {
            const modelFilteredProviders = availableAndHealthyProviders.filter(p => {
                // 如果提供商没有配置 notSupportedModels，则认为它支持所有模型
                if (!p.config.notSupportedModels || !Array.isArray(p.config.notSupportedModels)) {
                    return true;
                }
                // 检查 notSupportedModels 数组中是否包含请求的模型，如果包含则排除
                return !p.config.notSupportedModels.includes(requestedModel);
            });

            if (modelFilteredProviders.length === 0) {
                this._log('warn', `No available providers for type: ${providerType} that support model: ${requestedModel}`);
                return null;
            }

            availableAndHealthyProviders = modelFilteredProviders;
            this._log('debug', `Filtered ${modelFilteredProviders.length} providers supporting model: ${requestedModel}`);
        }

        if (availableAndHealthyProviders.length === 0) {
            this._log('warn', `No available and healthy providers for type: ${providerType}`);
            return null;
        }

        // P4 优化：使用 O(n) 线性扫描找最小值，而不是 O(n log n) 排序
        // 只需要找到分数最低的 provider，不需要完整排序
        let selected = availableAndHealthyProviders[0];
        let minScore = this._calculateNodeScore(selected, now);

        for (let i = 1; i < availableAndHealthyProviders.length; i++) {
            const provider = availableAndHealthyProviders[i];
            const score = this._calculateNodeScore(provider, now);
            if (score < minScore || (score === minScore && provider.uuid < selected.uuid)) {
                selected = provider;
                minScore = score;
            }
        }

        // P5 Fix (方案B): 防止100ms内重复选择同一节点
        const nowTs = Date.now();
        const recentKey = `${providerType}:${selected.uuid}`;
        const lastSelectTime = this._recentSelections.get(recentKey) || 0;

        // 如果100ms内刚选过此节点，且有其他可选节点
        if ((nowTs - lastSelectTime) < this._recentSelectionWindow &&
            availableAndHealthyProviders.length > 1) {

            // 找到第二优选项（排除刚选的）
            let secondBest = null;
            let secondMinScore = Infinity;

            for (const provider of availableAndHealthyProviders) {
                if (provider.uuid === selected.uuid) continue;
                const score = this._calculateNodeScore(provider, now);
                if (score < secondMinScore) {
                    secondBest = provider;
                    secondMinScore = score;
                }
            }

            if (secondBest) {
                this._log('debug', `[P5 Anti-Repeat] Avoided repeat selection of ${selected.uuid.substring(0, 8)}..., using ${secondBest.uuid.substring(0, 8)}... instead`);
                selected = secondBest;
                minScore = secondMinScore;
            }
        }

        // 记录本次选择时间
        this._recentSelections.set(recentKey, nowTs);

        // 定期清理过期记录（每100次选择清理一次）
        if ((++this._recentSelectionCleanupCounter) % 100 === 0) {
            for (const [key, time] of this._recentSelections.entries()) {
                if ((nowTs - time) > 1000) {  // 1秒后清理
                    this._recentSelections.delete(key);
                }
            }
            if (this._recentSelections.size < 50) {
                this._log('debug', `[P5 Anti-Repeat] Cleanup: ${this._recentSelections.size} recent selections tracked`);
            }
        }

        // 始终更新 lastUsed（确保 LRU 策略生效，避免并发请求选到同一个 provider）
        // usageCount 只在请求成功后才增加（由 skipUsageCount 控制）
        // P4 优化：使用时间戳数字存储，并清除缓存
        selected.config.lastUsed = new Date(nowTs).toISOString();
        selected.config._lastUsedTs = nowTs;
        selected.config._lastUsedSrc = selected.config.lastUsed;

        // P5 Fix (方案A): 使用时间戳基序列号，避免冲突和进程重启后序列号倒退
        const uniqueSeq = this._sequenceBase + (++this._selectionSequence);
        selected.config._lastSelectionSeq = uniqueSeq;

        // P5 Fix (方案D): 增强日志，显示诊断信息
        const totalProviders = availableProviders.length;
        const healthyProviders = availableAndHealthyProviders.length;
        const scoreInfo = `Score:${minScore.toFixed(0)}`;
        const poolInfo = `Pool:${healthyProviders}/${totalProviders}`;

        if (this._selectionSequence % 10 === 0) {
            this._log('info', `[Selection] ${selected.config.uuid.substring(0, 8)}... (Seq:${uniqueSeq}) | ${poolInfo} | ${scoreInfo}`);
        } else {
            this._log('debug', `[Selection] ${selected.config.uuid.substring(0, 8)}... (Seq:${uniqueSeq}) | ${poolInfo} | ${scoreInfo}`);
        }

        // P5 Fix (方案D): 警告只有1个健康provider
        if (healthyProviders === 1 && totalProviders > 1) {
            this._log('warn', `Only 1/${totalProviders} providers healthy for ${providerType}. Load balancing unavailable.`);
        }

        if (!options.skipUsageCount) {
            selected.config.usageCount++;
            // P3 Fix: Use batch queue for Redis updates to reduce round-trips
            if (this.isUsingRedis()) {
                // Queue for batch processing instead of immediate Redis call
                this._queueUsageIncrement(providerType, selected.config.uuid);
            } else {
                // 只有在使用次数达到一定阈值时才保存，减少I/O频率
                if (selected.config.usageCount % 50 === 0) {
                    this._debouncedSave(providerType);
                }
            }
        }

        this._log('debug', `Selected provider for ${providerType} (LRU): ${selected.config.uuid}${requestedModel ? ` for model: ${requestedModel}` : ''}${options.skipUsageCount ? ' (skip usage count)' : ''}`);
        
        return selected.config;
    }

    /**
     * Selects a provider from the pool with fallback support.
     * When the primary provider type has no healthy providers, it will try fallback types.
     * @param {string} providerType - The primary type of provider to select.
     * @param {string} [requestedModel] - Optional. The model name to filter providers by.
     * @param {Object} [options] - Optional. Additional options.
     * @param {boolean} [options.skipUsageCount] - Optional. If true, skip incrementing usage count.
     * @returns {object|null} An object containing the selected provider's configuration and the actual provider type used, or null if no healthy provider is found.
     */
    /**
     * Selects a provider from the pool with fallback support.
     * When the primary provider type has no healthy providers, it will try fallback types.
     *
     * 注意：此方法现在返回 Promise，因为内部调用的 selectProvider 是异步的。
     *
     * @param {string} providerType - The primary type of provider to select.
     * @param {string} [requestedModel] - Optional. The model name to filter providers by.
     * @param {Object} [options] - Optional. Additional options.
     * @param {boolean} [options.skipUsageCount] - Optional. If true, skip incrementing usage count.
     * @returns {Promise<object|null>} An object containing the selected provider's configuration and the actual provider type used, or null if no healthy provider is found.
     */
    async selectProviderWithFallback(providerType, requestedModel = null, options = {}) {
        // 参数校验
        if (!providerType || typeof providerType !== 'string') {
            this._log('error', `Invalid providerType: ${providerType}`);
            return null;
        }

        // ==========================
        // 优先级 1: Provider Fallback Chain (同协议/兼容协议的回退)
        // ==========================
        
        // 记录尝试过的类型，避免循环
        const triedTypes = new Set();
        const typesToTry = [providerType];
        
        const fallbackTypes = this.fallbackChain[providerType] || [];
        if (Array.isArray(fallbackTypes)) {
            typesToTry.push(...fallbackTypes);
        }

        for (const currentType of typesToTry) {
            // 避免重复尝试
            if (triedTypes.has(currentType)) {
                continue;
            }
            triedTypes.add(currentType);

            // 检查该类型是否有配置的池
            if (!this.providerStatus[currentType] || this.providerStatus[currentType].length === 0) {
                this._log('debug', `No provider pool configured for type: ${currentType}`);
                continue;
            }

            // 如果是 fallback 类型，需要检查模型兼容性
            if (currentType !== providerType && requestedModel) {
                // 检查协议前缀是否兼容
                const primaryProtocol = getProtocolPrefix(providerType);
                const fallbackProtocol = getProtocolPrefix(currentType);
                
                if (primaryProtocol !== fallbackProtocol) {
                    this._log('debug', `Skipping fallback type ${currentType}: protocol mismatch (${primaryProtocol} vs ${fallbackProtocol})`);
                    continue;
                }

                // 检查 fallback 类型是否支持请求的模型
                const supportedModels = getProviderModels(currentType);
                if (supportedModels.length > 0 && !supportedModels.includes(requestedModel)) {
                    this._log('debug', `Skipping fallback type ${currentType}: model ${requestedModel} not supported`);
                    continue;
                }
            }

            // 尝试从当前类型选择提供商（现在是异步的）
            const selectedConfig = await this.selectProvider(currentType, requestedModel, options);
            
            if (selectedConfig) {
                if (currentType !== providerType) {
                    this._log('info', `Fallback activated (Chain): ${providerType} -> ${currentType} (uuid: ${selectedConfig.uuid})`);
                }
                return {
                    config: selectedConfig,
                    actualProviderType: currentType,
                    isFallback: currentType !== providerType
                };
            }
        }

        // ==========================
        // 优先级 2: Model Fallback Mapping (跨协议/特定模型的回退)
        // ==========================

        if (requestedModel && this.modelFallbackMapping && this.modelFallbackMapping[requestedModel]) {
            const mapping = this.modelFallbackMapping[requestedModel];
            const targetProviderType = mapping.targetProviderType;
            const targetModel = mapping.targetModel;

            if (targetProviderType && targetModel) {
                this._log('info', `Trying Model Fallback Mapping for ${requestedModel}: -> ${targetProviderType} (${targetModel})`);
                
                // 递归调用 selectProviderWithFallback，但这次针对目标提供商类型
                // 注意：这里我们直接尝试从目标提供商池中选择，因为如果再次递归可能会导致死循环或逻辑复杂化
                // 简单起见，我们直接尝试选择目标提供商
                
                // 检查目标类型是否有配置的池
                if (this.providerStatus[targetProviderType] && this.providerStatus[targetProviderType].length > 0) {
                    // 尝试从目标类型选择提供商（使用转换后的模型名，现在是异步的）
                    const selectedConfig = await this.selectProvider(targetProviderType, targetModel, options);
                    
                    if (selectedConfig) {
                        this._log('info', `Fallback activated (Model Mapping): ${providerType} (${requestedModel}) -> ${targetProviderType} (${targetModel}) (uuid: ${selectedConfig.uuid})`);
                        return {
                            config: selectedConfig,
                            actualProviderType: targetProviderType,
                            isFallback: true,
                            actualModel: targetModel // 返回实际使用的模型名，供上层进行请求转换
                        };
                    } else {
                        // 如果目标类型的主池也不可用，尝试目标类型的 fallback chain
                        // 例如 claude-kiro-oauth (mapped) -> claude-custom (chain)
                        // 这需要我们小心处理，避免无限递归。
                        // 我们可以手动检查目标类型的 fallback chain
                        
                        const targetFallbackTypes = this.fallbackChain[targetProviderType] || [];
                        for (const fallbackType of targetFallbackTypes) {
                             // 检查协议兼容性 (目标类型 vs 它的 fallback)
                             const targetProtocol = getProtocolPrefix(targetProviderType);
                             const fallbackProtocol = getProtocolPrefix(fallbackType);
                             
                             if (targetProtocol !== fallbackProtocol) continue;
                             
                             // 检查模型支持
                             const supportedModels = getProviderModels(fallbackType);
                             if (supportedModels.length > 0 && !supportedModels.includes(targetModel)) continue;
                             
                             const fallbackSelectedConfig = await this.selectProvider(fallbackType, targetModel, options);
                             if (fallbackSelectedConfig) {
                                 this._log('info', `Fallback activated (Model Mapping -> Chain): ${providerType} (${requestedModel}) -> ${targetProviderType} -> ${fallbackType} (${targetModel}) (uuid: ${fallbackSelectedConfig.uuid})`);
                                 return {
                                     config: fallbackSelectedConfig,
                                     actualProviderType: fallbackType,
                                     isFallback: true,
                                     actualModel: targetModel
                                 };
                             }
                        }
                    }
                } else {
                    this._log('warn', `Model Fallback target provider ${targetProviderType} not configured or empty.`);
                }
            }
        }

        this._log('warn', `None available provider found for ${providerType} (Model: ${requestedModel}) after checking fallback chain and model mapping.`);
        return null;
    }

    /**
     * Gets the fallback chain for a given provider type.
     * @param {string} providerType - The provider type to get fallback chain for.
     * @returns {Array<string>} The fallback chain array, or empty array if not configured.
     */
    getFallbackChain(providerType) {
        return this.fallbackChain[providerType] || [];
    }

    /**
     * Sets or updates the fallback chain for a provider type.
     * @param {string} providerType - The provider type to set fallback chain for.
     * @param {Array<string>} fallbackTypes - Array of fallback provider types.
     */
    setFallbackChain(providerType, fallbackTypes) {
        if (!Array.isArray(fallbackTypes)) {
            this._log('error', `Invalid fallbackTypes: must be an array`);
            return;
        }
        this.fallbackChain[providerType] = fallbackTypes;
        this._log('info', `Updated fallback chain for ${providerType}: ${fallbackTypes.join(' -> ')}`);
    }

    /**
     * Checks if all providers of a given type are unhealthy.
     * @param {string} providerType - The provider type to check.
     * @returns {boolean} True if all providers are unhealthy or disabled.
     */
    isAllProvidersUnhealthy(providerType) {
        const providers = this.providerStatus[providerType] || [];
        if (providers.length === 0) {
            return true;
        }
        return providers.every(p => !p.config.isHealthy || p.config.isDisabled);
    }

    /**
     * Gets statistics about provider health for a given type.
     * @param {string} providerType - The provider type to get stats for.
     * @returns {Object} Statistics object with total, healthy, unhealthy, and disabled counts.
     */
    getProviderStats(providerType) {
        const providers = this.providerStatus[providerType] || [];
        const stats = {
            total: providers.length,
            healthy: 0,
            unhealthy: 0,
            disabled: 0
        };
        
        for (const p of providers) {
            if (p.config.isDisabled) {
                stats.disabled++;
            } else if (p.config.isHealthy) {
                stats.healthy++;
            } else {
                stats.unhealthy++;
            }
        }
        
        return stats;
    }

    /**
     * 标记提供商需要刷新并推入刷新队列
     * Uses Redis atomic operations when available.
     * @param {string} providerType - 提供商类型
     * @param {object} providerConfig - 提供商配置（包含 uuid）
     */
    markProviderNeedRefresh(providerType, providerConfig) {
        if (!providerConfig?.uuid) {
            this._log('error', 'Invalid providerConfig in markProviderNeedRefresh');
            return;
        }

        const provider = this._findProvider(providerType, providerConfig.uuid);
        if (provider) {
            provider.config.needsRefresh = true;
            this._log('info', `Marked provider ${providerConfig.uuid} as needsRefresh. Enqueuing...`);

            // 推入异步刷新队列
            this._enqueueRefresh(providerType, provider, true);

            // Use Redis atomic operations if available
            if (this.isUsingRedis()) {
                this._persistProviderUpdate(providerType, providerConfig.uuid, {
                    needsRefresh: true
                }).catch(err => {
                    this._log('error', `Async provider update failed: ${err.message}`);
                });
            } else {
                this._debouncedSave(providerType);
            }
        }
    }

    /**
     * Marks a provider as unhealthy (e.g., after an API error).
     * Uses Redis atomic operations when available for concurrent safety.
     * @param {string} providerType - The type of the provider.
     * @param {object} providerConfig - The configuration of the provider to mark.
     * @param {string} [errorMessage] - Optional error message to store.
     */
    markProviderUnhealthy(providerType, providerConfig, errorMessage = null) {
        if (!providerConfig?.uuid) {
            this._log('error', 'Invalid providerConfig in markProviderUnhealthy');
            return;
        }

        const provider = this._findProvider(providerType, providerConfig.uuid);
        if (provider) {
            const now = Date.now();
            const lastErrorTime = provider.config.lastErrorTime ? new Date(provider.config.lastErrorTime).getTime() : 0;
            const errorWindowMs = 10000; // 10 秒窗口期

            // 如果距离上次错误超过窗口期，重置错误计数
            if (now - lastErrorTime > errorWindowMs) {
                provider.config.errorCount = 1;
            } else {
                provider.config.errorCount++;
            }

            provider.config.lastErrorTime = new Date().toISOString();
            // 更新 lastUsed 时间，避免因 LRU 策略导致失败节点被重复选中
            provider.config.lastUsed = new Date().toISOString();

            // 保存错误信息
            if (errorMessage) {
                provider.config.lastErrorMessage = errorMessage;
            }

            const shouldMarkUnhealthy = provider.config.errorCount >= this.maxErrorCount;
            if (shouldMarkUnhealthy) {
                provider.config.isHealthy = false;
                this._log('warn', `Marked provider as unhealthy: ${providerConfig.uuid} for type ${providerType}. Total errors: ${provider.config.errorCount}`);
            } else {
                this._log('warn', `Provider ${providerConfig.uuid} for type ${providerType} error count: ${provider.config.errorCount}/${this.maxErrorCount}. Still healthy.`);
            }

            // Use Redis atomic operations if available
            if (this.isUsingRedis()) {
                this._incrementErrorAtomic(providerType, providerConfig.uuid, shouldMarkUnhealthy).catch(err => {
                    this._log('error', `Async error increment failed: ${err.message}`);
                });
                // Also persist the error message if provided
                if (errorMessage) {
                    this._persistProviderUpdate(providerType, providerConfig.uuid, {
                        lastErrorMessage: errorMessage,
                        lastUsed: provider.config.lastUsed
                    }).catch(err => {
                        this._log('error', `Async error message update failed: ${err.message}`);
                    });
                }
            } else {
                this._debouncedSave(providerType);
            }
        }
    }

    /**
     * Marks a provider as unhealthy immediately (without accumulating error count).
     * Used for definitive authentication errors like 401/403.
     * Uses Redis atomic operations when available.
     * @param {string} providerType - The type of the provider.
     * @param {object} providerConfig - The configuration of the provider to mark.
     * @param {string} [errorMessage] - Optional error message to store.
     */
    markProviderUnhealthyImmediately(providerType, providerConfig, errorMessage = null) {
        if (!providerConfig?.uuid) {
            this._log('error', 'Invalid providerConfig in markProviderUnhealthyImmediately');
            return;
        }

        const provider = this._findProvider(providerType, providerConfig.uuid);
        if (provider) {
            provider.config.isHealthy = false;
            provider.config.errorCount = this.maxErrorCount; // Set to max to indicate definitive failure
            provider.config.lastErrorTime = new Date().toISOString();
            provider.config.lastUsed = new Date().toISOString();

            if (errorMessage) {
                provider.config.lastErrorMessage = errorMessage;
            }

            this._log('warn', `Immediately marked provider as unhealthy: ${providerConfig.uuid} for type ${providerType}. Reason: ${errorMessage || 'Authentication error'}`);

            // Use Redis atomic operations if available
            if (this.isUsingRedis()) {
                this._updateHealthStatusAtomic(providerType, providerConfig.uuid, false).catch(err => {
                    this._log('error', `Async health status update failed: ${err.message}`);
                });
                this._persistProviderUpdate(providerType, providerConfig.uuid, {
                    errorCount: this.maxErrorCount,
                    lastErrorTime: provider.config.lastErrorTime,
                    lastUsed: provider.config.lastUsed,
                    lastErrorMessage: errorMessage
                }).catch(err => {
                    this._log('error', `Async provider update failed: ${err.message}`);
                });
            } else {
                this._debouncedSave(providerType);
            }
        }
    }

    /**
     * Marks a provider as unhealthy with a scheduled recovery time.
     * Used for quota exhaustion errors (402) where the quota will reset at a specific time.
     * Uses Redis atomic operations when available.
     * @param {string} providerType - The type of the provider.
     * @param {object} providerConfig - The configuration of the provider to mark.
     * @param {string} [errorMessage] - Optional error message to store.
     * @param {Date|string} [recoveryTime] - Optional recovery time when the provider should be marked healthy again.
     */
    markProviderUnhealthyWithRecoveryTime(providerType, providerConfig, errorMessage = null, recoveryTime = null) {
        if (!providerConfig?.uuid) {
            this._log('error', 'Invalid providerConfig in markProviderUnhealthyWithRecoveryTime');
            return;
        }

        const provider = this._findProvider(providerType, providerConfig.uuid);
        if (provider) {
            provider.config.isHealthy = false;
            provider.config.errorCount = this.maxErrorCount; // Set to max to indicate definitive failure
            provider.config.lastErrorTime = new Date().toISOString();
            provider.config.lastUsed = new Date().toISOString();

            if (errorMessage) {
                provider.config.lastErrorMessage = errorMessage;
            }

            // Set recovery time if provided
            let recoveryDateStr = null;
            if (recoveryTime) {
                const recoveryDate = recoveryTime instanceof Date ? recoveryTime : new Date(recoveryTime);
                provider.config.scheduledRecoveryTime = recoveryDate.toISOString();
                recoveryDateStr = recoveryDate.toISOString();
                this._log('warn', `Marked provider as unhealthy with recovery time: ${providerConfig.uuid} for type ${providerType}. Recovery at: ${recoveryDateStr}. Reason: ${errorMessage || 'Quota exhausted'}`);
            } else {
                this._log('warn', `Marked provider as unhealthy: ${providerConfig.uuid} for type ${providerType}. Reason: ${errorMessage || 'Quota exhausted'}`);
            }

            // Use Redis atomic operations if available
            if (this.isUsingRedis()) {
                const updates = {
                    isHealthy: false,
                    errorCount: this.maxErrorCount,
                    lastErrorTime: provider.config.lastErrorTime,
                    lastUsed: provider.config.lastUsed,
                    lastErrorMessage: errorMessage
                };
                if (recoveryDateStr) {
                    updates.scheduledRecoveryTime = recoveryDateStr;
                }
                this._persistProviderUpdate(providerType, providerConfig.uuid, updates).catch(err => {
                    this._log('error', `Async provider update failed: ${err.message}`);
                });
            } else {
                this._debouncedSave(providerType);
            }
        }
    }

    /**
     * Marks a provider as healthy.
     * Uses Redis atomic operations when available.
     * @param {string} providerType - The type of the provider.
     * @param {object} providerConfig - The configuration of the provider to mark.
     * @param {boolean} resetUsageCount - Whether to reset usage count (optional, default: false).
     * @param {string} [healthCheckModel] - Optional model name used for health check.
     */
    markProviderHealthy(providerType, providerConfig, resetUsageCount = false, healthCheckModel = null) {
        if (!providerConfig?.uuid) {
            this._log('error', 'Invalid providerConfig in markProviderHealthy');
            return;
        }

        const provider = this._findProvider(providerType, providerConfig.uuid);
        if (provider) {
            provider.config.isHealthy = true;
            provider.config.errorCount = 0;
            provider.config.refreshCount = 0;
            provider.config.needsRefresh = false;
            provider.config.lastErrorTime = null;
            provider.config.lastErrorMessage = null;

            // 更新健康检测信息
            provider.config.lastHealthCheckTime = new Date().toISOString();
            if (healthCheckModel) {
                provider.config.lastHealthCheckModel = healthCheckModel;
            }

            // 只有在明确要求重置使用计数时才重置
            if (resetUsageCount) {
                provider.config.usageCount = 0;
            } else {
                provider.config.usageCount++;
                provider.config.lastUsed = new Date().toISOString();
            }
            this._log('info', `Marked provider as healthy: ${provider.config.uuid} for type ${providerType}${resetUsageCount ? ' (usage count reset)' : ''}`);

            // Use Redis atomic operations if available
            if (this.isUsingRedis()) {
                this._updateHealthStatusAtomic(providerType, providerConfig.uuid, true).catch(err => {
                    this._log('error', `Async health status update failed: ${err.message}`);
                });
                const updates = {
                    errorCount: 0,
                    refreshCount: 0,
                    needsRefresh: false,
                    lastErrorTime: null,
                    lastErrorMessage: null,
                    lastHealthCheckTime: provider.config.lastHealthCheckTime,
                    usageCount: provider.config.usageCount
                };
                if (healthCheckModel) {
                    updates.lastHealthCheckModel = healthCheckModel;
                }
                if (!resetUsageCount) {
                    updates.lastUsed = provider.config.lastUsed;
                }
                this._persistProviderUpdate(providerType, providerConfig.uuid, updates).catch(err => {
                    this._log('error', `Async provider update failed: ${err.message}`);
                });
            } else {
                this._debouncedSave(providerType);
            }
        }
    }

    /**
     * 重置提供商的刷新状态（needsRefresh 和 refreshCount）
     * 并将其标记为健康，以便立即投入使用
     * Uses Redis atomic operations when available.
     * @param {string} providerType - 提供商类型
     * @param {string} uuid - 提供商 UUID
     */
    resetProviderRefreshStatus(providerType, uuid) {
        if (!providerType || !uuid) {
            this._log('error', 'Invalid parameters in resetProviderRefreshStatus');
            return;
        }

        const provider = this._findProvider(providerType, uuid);
        if (provider) {
            provider.config.needsRefresh = false;
            provider.config.refreshCount = 0;
            // 更新为可用
            provider.config.lastHealthCheckTime = new Date().toISOString();
            // 标记为健康，以便立即投入使用
            this._log('info', `Reset refresh status and marked healthy for provider ${uuid} (${providerType})`);

            // Use Redis atomic operations if available
            if (this.isUsingRedis()) {
                this._persistProviderUpdate(providerType, uuid, {
                    needsRefresh: false,
                    refreshCount: 0,
                    lastHealthCheckTime: provider.config.lastHealthCheckTime
                }).catch(err => {
                    this._log('error', `Async provider update failed: ${err.message}`);
                });
            } else {
                this._debouncedSave(providerType);
            }
        }
    }

    /**
     * 重置提供商的计数器（错误计数和使用计数）
     * Uses Redis atomic operations when available.
     * @param {string} providerType - The type of the provider.
     * @param {object} providerConfig - The configuration of the provider to mark.
     */
    resetProviderCounters(providerType, providerConfig) {
        if (!providerConfig?.uuid) {
            this._log('error', 'Invalid providerConfig in resetProviderCounters');
            return;
        }

        const provider = this._findProvider(providerType, providerConfig.uuid);
        if (provider) {
            provider.config.errorCount = 0;
            provider.config.usageCount = 0;
            this._log('info', `Reset provider counters: ${provider.config.uuid} for type ${providerType}`);

            // Use Redis atomic operations if available
            if (this.isUsingRedis()) {
                this._persistProviderUpdate(providerType, providerConfig.uuid, {
                    errorCount: 0,
                    usageCount: 0
                }).catch(err => {
                    this._log('error', `Async provider update failed: ${err.message}`);
                });
            } else {
                this._debouncedSave(providerType);
            }
        }
    }

    /**
     * 禁用指定提供商
     * Uses Redis atomic operations when available.
     * @param {string} providerType - 提供商类型
     * @param {object} providerConfig - 提供商配置
     */
    disableProvider(providerType, providerConfig) {
        if (!providerConfig?.uuid) {
            this._log('error', 'Invalid providerConfig in disableProvider');
            return;
        }

        const provider = this._findProvider(providerType, providerConfig.uuid);
        if (provider) {
            provider.config.isDisabled = true;
            this._log('info', `Disabled provider: ${providerConfig.uuid} for type ${providerType}`);

            // Use Redis atomic operations if available
            if (this.isUsingRedis()) {
                this._persistProviderUpdate(providerType, providerConfig.uuid, {
                    isDisabled: true
                }).catch(err => {
                    this._log('error', `Async provider update failed: ${err.message}`);
                });
            } else {
                this._debouncedSave(providerType);
            }
        }
    }

    /**
     * 启用指定提供商
     * Uses Redis atomic operations when available.
     * @param {string} providerType - 提供商类型
     * @param {object} providerConfig - 提供商配置
     */
    enableProvider(providerType, providerConfig) {
        if (!providerConfig?.uuid) {
            this._log('error', 'Invalid providerConfig in enableProvider');
            return;
        }

        const provider = this._findProvider(providerType, providerConfig.uuid);
        if (provider) {
            provider.config.isDisabled = false;
            this._log('info', `Enabled provider: ${providerConfig.uuid} for type ${providerType}`);

            // Use Redis atomic operations if available
            if (this.isUsingRedis()) {
                this._persistProviderUpdate(providerType, providerConfig.uuid, {
                    isDisabled: false
                }).catch(err => {
                    this._log('error', `Async provider update failed: ${err.message}`);
                });
            } else {
                this._debouncedSave(providerType);
            }
        }
    }

    /**
     * 刷新指定提供商的 UUID
     * 用于在认证错误（如 401）时更换 UUID，以便重新尝试
     * Note: For Redis, this requires deleting the old key and adding a new one.
     * @param {string} providerType - 提供商类型
     * @param {object} providerConfig - 提供商配置（包含当前 uuid）
     * @returns {string|null} 新的 UUID，如果失败则返回 null
     */
    refreshProviderUuid(providerType, providerConfig) {
        if (!providerConfig?.uuid) {
            this._log('error', 'Invalid providerConfig in refreshProviderUuid');
            return null;
        }

        const provider = this._findProvider(providerType, providerConfig.uuid);
        if (provider) {
            const oldUuid = provider.config.uuid;
            // 生成新的 UUID
            const newUuid = 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
                const r = Math.random() * 16 | 0;
                const v = c === 'x' ? r : (r & 0x3 | 0x8);
                return v.toString(16);
            });

            // 更新 provider 的 UUID
            provider.uuid = newUuid;
            provider.config.uuid = newUuid;

            // 同时更新 providerPools 中的原始数据
            const poolArray = this.providerPools[providerType];
            if (poolArray) {
                const originalProvider = poolArray.find(p => p.uuid === oldUuid);
                if (originalProvider) {
                    originalProvider.uuid = newUuid;
                }
            }

            this._log('info', `Refreshed provider UUID: ${oldUuid} -> ${newUuid} for type ${providerType}`);

            // For Redis, we need to delete the old key and add a new one
            if (this.isUsingRedis()) {
                const adapter = this._getStorageAdapter();
                // Delete old entry and add new one atomically
                Promise.all([
                    adapter.deleteProvider(providerType, oldUuid),
                    adapter.addProvider(providerType, provider.config)
                ]).catch(err => {
                    this._log('error', `Async UUID refresh failed: ${err.message}`);
                });
            } else {
                this._debouncedSave(providerType);
            }

            return newUuid;
        }

        this._log('warn', `Provider not found for UUID refresh: ${providerConfig.uuid} in ${providerType}`);
        return null;
    }

    /**
     * 检查并恢复已到恢复时间的提供商
     * P4 优化：添加节流机制，每秒最多执行一次，减少 CPU 开销
     * @param {string} [providerType] - 可选，指定要检查的提供商类型。如果不提供，检查所有类型
     * @param {boolean} [force=false] - 是否强制执行，跳过节流检查
     * @private
     */
    _checkAndRecoverScheduledProviders(providerType = null, force = false) {
        const nowTs = Date.now();

        // P4 节流：除非强制执行，否则每秒最多检查一次
        if (!force && (nowTs - this._lastRecoveryCheckTime) < this._recoveryCheckThrottleMs) {
            return;
        }
        this._lastRecoveryCheckTime = nowTs;

        const typesToCheck = providerType ? [providerType] : Object.keys(this.providerStatus);

        for (const type of typesToCheck) {
            const providers = this.providerStatus[type] || [];
            for (const providerStatus of providers) {
                const config = providerStatus.config;

                // 检查是否有 scheduledRecoveryTime 且已到恢复时间
                if (config.scheduledRecoveryTime && !config.isHealthy) {
                    // P4 优化：使用时间戳比较，避免创建 Date 对象
                    const recoveryTimeTs = this._parseTimestamp(config.scheduledRecoveryTime, 'scheduledRecovery', config);
                    if (nowTs >= recoveryTimeTs) {
                        this._log('info', `Auto-recovering provider ${config.uuid} (${type}). Scheduled recovery time reached: ${config.scheduledRecoveryTime}`);

                        // 恢复健康状态
                        config.isHealthy = true;
                        config.errorCount = 0;
                        config.lastErrorTime = null;
                        config.lastErrorMessage = null;
                        config.scheduledRecoveryTime = null; // 清除恢复时间

                        // 保存更改
                        this._debouncedSave(type);
                    }
                }
            }
        }
    }

    /**
     * Performs health checks on all providers in the pool.
     * This method would typically be called periodically (e.g., via cron job).
     */
    async performHealthChecks(isInit = false) {
        this._log('info', 'Performing health checks on all providers...');
        // P1-3: 使用时间戳比较，避免在循环中创建 Date 对象
        const nowTs = Date.now();

        // 首先检查并恢复已到恢复时间的提供商
        // 强制执行，跳过节流检查（健康检查是周期性任务，频率不高）
        this._checkAndRecoverScheduledProviders(null, true);

        for (const providerType in this.providerStatus) {
            for (const providerStatus of this.providerStatus[providerType]) {
                const providerConfig = providerStatus.config;

                // 如果提供商有 scheduledRecoveryTime 且未到恢复时间，跳过健康检查
                if (providerConfig.scheduledRecoveryTime && !providerConfig.isHealthy) {
                    // P1-3: 使用 _parseTimestamp 替代 new Date()
                    const recoveryTimeTs = this._parseTimestamp(providerConfig.scheduledRecoveryTime, 'scheduledRecovery', providerConfig);
                    if (nowTs < recoveryTimeTs) {
                        this._log('debug', `Skipping health check for ${providerConfig.uuid} (${providerType}). Waiting for scheduled recovery at ${providerConfig.scheduledRecoveryTime}`);
                        continue;
                    }
                }

                // Only attempt to health check unhealthy providers after a certain interval
                // P1-3: 使用 _parseTimestamp 替代 new Date()
                if (!providerStatus.config.isHealthy && providerStatus.config.lastErrorTime) {
                    const lastErrorTs = this._parseTimestamp(providerStatus.config.lastErrorTime, 'lastError', providerStatus.config);
                    if (nowTs - lastErrorTs < this.healthCheckInterval) {
                        this._log('debug', `Skipping health check for ${providerConfig.uuid} (${providerType}). Last error too recent.`);
                        continue;
                    }
                }

                try {
                    // Perform actual health check based on provider type
                    const healthResult = await this._checkProviderHealth(providerType, providerConfig);
                    
                    if (healthResult === null) {
                        this._log('debug', `Health check for ${providerConfig.uuid} (${providerType}) skipped: Check not implemented.`);
                        this.resetProviderCounters(providerType, providerConfig);
                        continue;
                    }
                    
                    if (healthResult.success) {
                        if (!providerStatus.config.isHealthy) {
                            // Provider was unhealthy but is now healthy
                            // 恢复健康时不重置使用计数，保持原有值
                            this.markProviderHealthy(providerType, providerConfig, true, healthResult.modelName);
                            this._log('info', `Health check for ${providerConfig.uuid} (${providerType}): Marked Healthy (actual check)`);
                        } else {
                            // Provider was already healthy and still is
                            // 只在初始化时重置使用计数
                            this.markProviderHealthy(providerType, providerConfig, true, healthResult.modelName);
                            this._log('debug', `Health check for ${providerConfig.uuid} (${providerType}): Still Healthy`);
                        }
                    } else {
                        // Provider is not healthy
                        this._log('warn', `Health check for ${providerConfig.uuid} (${providerType}) failed: ${healthResult.errorMessage || 'Provider is not responding correctly.'}`);
                        this.markProviderUnhealthy(providerType, providerConfig, healthResult.errorMessage);
                        
                        // 更新健康检测时间和模型（即使失败也记录）
                        providerStatus.config.lastHealthCheckTime = new Date().toISOString();
                        if (healthResult.modelName) {
                            providerStatus.config.lastHealthCheckModel = healthResult.modelName;
                        }
                    }

                } catch (error) {
                    this._log('error', `Health check for ${providerConfig.uuid} (${providerType}) failed: ${error.message}`);
                    // If a health check fails, mark it unhealthy, which will update error count and lastErrorTime
                    this.markProviderUnhealthy(providerType, providerConfig, error.message);
                }
            }
        }
    }

    /**
     * 构建健康检查请求（返回多种格式用于重试）
     * @private
     * @returns {Array} 请求格式数组，按优先级排序
     */
    _buildHealthCheckRequests(providerType, modelName) {
        const baseMessage = { role: 'user', content: 'Hi' };
        const requests = [];
        
        // Gemini 使用 contents 格式
        if (providerType.startsWith('gemini')) {
            requests.push({
                contents: [{
                    role: 'user',
                    parts: [{ text: baseMessage.content }]
                }]
            });
            return requests;
        }
        
        // Kiro OAuth 只支持 messages 格式
        if (providerType.startsWith('claude-kiro')) {
            requests.push({
                messages: [baseMessage],
                model: modelName,
                max_tokens: 1
            });
            return requests;
        }
        
        // OpenAI Custom Responses 使用特殊格式
        if (providerType === MODEL_PROVIDER.OPENAI_CUSTOM_RESPONSES) {
            requests.push({
                input: [baseMessage],
                model: modelName
            });
            return requests;
        }
        
        // 其他提供商（OpenAI、Claude、Qwen）使用标准 messages 格式
        requests.push({
            messages: [baseMessage],
            model: modelName
        });
        
        return requests;
    }

    /**
     * Performs an actual health check for a specific provider.
     * @param {string} providerType - The type of the provider.
     * @param {object} providerConfig - The configuration of the provider to check.
     * @param {boolean} forceCheck - If true, ignore checkHealth config and force the check.
     * @returns {Promise<{success: boolean, modelName: string, errorMessage: string}|null>} - Health check result object or null if check not implemented.
     */
    async _checkProviderHealth(providerType, providerConfig, forceCheck = false) {
        // 如果未启用健康检查且不是强制检查，返回 null（提前返回，避免不必要的计算）
        if (!providerConfig.checkHealth && !forceCheck) {
            return null;
        }

        // 确定健康检查使用的模型名称
        const modelName = providerConfig.checkModelName ||
                        ProviderPoolManager.DEFAULT_HEALTH_CHECK_MODELS[providerType];

        if (!modelName) {
            this._log('warn', `Unknown provider type for health check: ${providerType}. Please check DEFAULT_HEALTH_CHECK_MODELS.`);
            return { 
                success: false, 
                modelName: null, 
                errorMessage: `Unknown provider type '${providerType}'. No default health check model configured.` 
            };
        }

        // ========== 实际 API 健康检查（带超时保护）==========
        const tempConfig = {
            ...providerConfig,
            MODEL_PROVIDER: providerType
        };
        const serviceAdapter = getServiceAdapter(tempConfig);

        // 获取所有可能的请求格式
        const healthCheckRequests = this._buildHealthCheckRequests(providerType, modelName);

        // 健康检查超时时间（15秒，避免长时间阻塞）
        const healthCheckTimeout = 15000;
        let lastError = null;

        // 重试机制：尝试不同的请求格式
        for (let i = 0; i < healthCheckRequests.length; i++) {
            const healthCheckRequest = healthCheckRequests[i];
            const abortController = new AbortController();
            const timeoutId = setTimeout(() => abortController.abort(), healthCheckTimeout);

            try {
                // P4 优化：使用延迟求值，避免在非 debug 模式下执行 JSON.stringify
                this._log('debug', () => `Health check attempt ${i + 1}/${healthCheckRequests.length} for ${modelName}: ${JSON.stringify(healthCheckRequest)}`);

                // 尝试将 signal 注入请求体，供支持的适配器使用
                const requestWithSignal = {
                    ...healthCheckRequest,
                    // signal: abortController.signal
                };

                await serviceAdapter.generateContent(modelName, requestWithSignal);
                
                clearTimeout(timeoutId);
                return { success: true, modelName, errorMessage: null };
            } catch (error) {
                clearTimeout(timeoutId);
                lastError = error;
                this._log('debug', `Health check attempt ${i + 1} failed for ${providerType}: ${error.message}`);
            }
        }

        // 所有尝试都失败
        this._log('error', `Health check failed for ${providerType} after ${healthCheckRequests.length} attempts: ${lastError?.message}`);
        return { success: false, modelName, errorMessage: lastError?.message || 'All health check attempts failed' };
    }

    /**
     * 优化1: 添加防抖保存方法
     * 延迟保存操作，避免频繁的文件 I/O
     * When using Redis, individual field updates are immediate (atomic),
     * but full pool syncs still use debouncing.
     * @private
     */
    _debouncedSave(providerType) {
        // 将待保存的 providerType 添加到集合中
        this.pendingSaves.add(providerType);

        // 清除之前的定时器
        if (this.saveTimer) {
            clearTimeout(this.saveTimer);
        }

        // 设置新的定时器
        this.saveTimer = setTimeout(() => {
            this._flushPendingSaves();
        }, this.saveDebounceTime);
    }

    /**
     * Persist a single provider update to Redis immediately (atomic operation)
     * Falls back to debounced file save if Redis is not available
     * @param {string} providerType - Provider type
     * @param {string} uuid - Provider UUID
     * @param {Object} updates - Fields to update
     * @private
     */
    async _persistProviderUpdate(providerType, uuid, updates) {
        const adapter = this._getStorageAdapter();
        if (adapter && adapter.getType() === 'redis') {
            try {
                await adapter.updateProvider(providerType, uuid, updates);
                this._log('debug', `Redis atomic update: ${providerType}:${uuid}`);
            } catch (error) {
                this._log('error', `Redis update failed, falling back to file: ${error.message}`);
                this._debouncedSave(providerType);
            }
        } else {
            // Fall back to debounced file save
            this._debouncedSave(providerType);
        }
    }

    /**
     * Increment usage count atomically in Redis
     * @param {string} providerType - Provider type
     * @param {string} uuid - Provider UUID
     * @returns {Promise<number>} New usage count
     * @private
     */
    async _incrementUsageAtomic(providerType, uuid) {
        const adapter = this._getStorageAdapter();
        if (adapter && adapter.getType() === 'redis') {
            try {
                const newCount = await adapter.incrementUsage(providerType, uuid);
                this._log('debug', `Redis atomic usage increment: ${providerType}:${uuid} -> ${newCount}`);
                return newCount;
            } catch (error) {
                this._log('error', `Redis usage increment failed: ${error.message}`);
            }
        }
        return 0;
    }

    /**
     * Increment error count atomically in Redis
     * @param {string} providerType - Provider type
     * @param {string} uuid - Provider UUID
     * @param {boolean} markUnhealthy - Whether to mark as unhealthy
     * @returns {Promise<number>} New error count
     * @private
     */
    async _incrementErrorAtomic(providerType, uuid, markUnhealthy = false) {
        const adapter = this._getStorageAdapter();
        if (adapter && adapter.getType() === 'redis') {
            try {
                const newCount = await adapter.incrementError(providerType, uuid, markUnhealthy);
                this._log('debug', `Redis atomic error increment: ${providerType}:${uuid} -> ${newCount}`);
                return newCount;
            } catch (error) {
                this._log('error', `Redis error increment failed: ${error.message}`);
            }
        }
        return 0;
    }

    /**
     * Update health status atomically in Redis
     * @param {string} providerType - Provider type
     * @param {string} uuid - Provider UUID
     * @param {boolean} isHealthy - Health status
     * @private
     */
    async _updateHealthStatusAtomic(providerType, uuid, isHealthy) {
        const adapter = this._getStorageAdapter();
        if (adapter && adapter.getType() === 'redis') {
            try {
                await adapter.updateHealthStatus(providerType, uuid, isHealthy);
                this._log('debug', `Redis atomic health update: ${providerType}:${uuid} -> ${isHealthy}`);
            } catch (error) {
                this._log('error', `Redis health update failed: ${error.message}`);
            }
        }
    }
    
    /**
     * 批量保存所有待保存的 providerType（优化为单次文件写入）
     * When using Redis, syncs the entire pool to Redis storage.
     * @private
     */
    async _flushPendingSaves() {
        const typesToSave = Array.from(this.pendingSaves);
        if (typesToSave.length === 0) return;

        // 清空待保存列表，避免重复处理
        this.pendingSaves.clear();
        this.saveTimer = null;

        // Check if we should use Redis storage
        const adapter = this._getStorageAdapter();
        if (adapter && adapter.getType() === 'redis') {
            try {
                // Sync all pending types to Redis
                for (const providerType of typesToSave) {
                    if (this.providerStatus[providerType]) {
                        const providers = this.providerStatus[providerType].map(p => {
                            const config = { ...p.config };
                            // Convert Date objects to ISOString
                            if (config.lastUsed instanceof Date) {
                                config.lastUsed = config.lastUsed.toISOString();
                            }
                            if (config.lastErrorTime instanceof Date) {
                                config.lastErrorTime = config.lastErrorTime.toISOString();
                            }
                            if (config.lastHealthCheckTime instanceof Date) {
                                config.lastHealthCheckTime = config.lastHealthCheckTime.toISOString();
                            }
                            return config;
                        });
                        await adapter.setProviderPool(providerType, providers);
                    }
                }
                this._log('debug', `Redis pool sync completed for: ${typesToSave.join(', ')}`);
                return;
            } catch (error) {
                this._log('error', `Redis pool sync failed, falling back to file: ${error.message}`);
                // Fall through to file-based save
            }
        }

        // 使用异步队列，避免阻塞API请求
        return new Promise((resolve) => {
            this._writeQueue.push({ typesToSave, resolve });
            this._processWriteQueue();
        });
    }
    
    /**
     * 处理写入队列，确保串行执行
     * @private
     */
    async _processWriteQueue() {
        if (this._isProcessingQueue || this._writeQueue.length === 0) {
            return;
        }
        
        this._isProcessingQueue = true;
        
        // 合并所有待写入的类型
        const allTypesToSave = new Set();
        const resolvers = [];
        
        while (this._writeQueue.length > 0) {
            const { typesToSave, resolve } = this._writeQueue.shift();
            typesToSave.forEach(type => allTypesToSave.add(type));
            resolvers.push(resolve);
        }
        
        try {
            await this._performFileWrite(Array.from(allTypesToSave));
            resolvers.forEach(resolve => resolve());
        } catch (error) {
            this._log('error', `Failed to write provider_pools.json: ${error.message}`);
            resolvers.forEach(resolve => resolve());
        } finally {
            this._isProcessingQueue = false;
            // 如果队列中还有新的写入请求，继续处理
            if (this._writeQueue.length > 0) {
                setImmediate(() => this._processWriteQueue());
            }
        }
    }
    
    /**
     * 执行实际的文件写入操作
     * @private
     */
    async _performFileWrite(typesToSave) {
        const filePath = this.globalConfig.PROVIDER_POOLS_FILE_PATH || 'configs/provider_pools.json';
        let currentPools = {};
        
        // 一次性读取文件
        try {
            const fileContent = await fs.promises.readFile(filePath, 'utf8');
            currentPools = JSON.parse(fileContent);
        } catch (readError) {
            if (readError.code === 'ENOENT') {
                this._log('info', 'configs/provider_pools.json does not exist, creating new file.');
            } else {
                this._log('error', `Failed to read provider_pools.json: ${readError.message}`);
                return; // 读取失败时直接返回，不进行写入
            }
        }

        // 更新所有待保存的 providerType
        for (const providerType of typesToSave) {
            if (this.providerStatus[providerType]) {
                currentPools[providerType] = this.providerStatus[providerType].map(p => {
                    // Convert Date objects to ISOString if they exist
                    const config = { ...p.config };
                    if (config.lastUsed instanceof Date) {
                        config.lastUsed = config.lastUsed.toISOString();
                    }
                    if (config.lastErrorTime instanceof Date) {
                        config.lastErrorTime = config.lastErrorTime.toISOString();
                    }
                    if (config.lastHealthCheckTime instanceof Date) {
                        config.lastHealthCheckTime = config.lastHealthCheckTime.toISOString();
                    }
                    return config;
                });
            } else {
                this._log('warn', `Attempted to save unknown providerType: ${providerType}`);
            }
        }
        
        // 原子写入：先写入临时文件，再重命名
        const tempFilePath = filePath + '.tmp';
        await fs.promises.writeFile(tempFilePath, JSON.stringify(currentPools, null, 2), 'utf8');
        await fs.promises.rename(tempFilePath, filePath);
        this._log('info', `configs/provider_pools.json updated successfully for types: ${typesToSave.join(', ')}`);
    }

}
