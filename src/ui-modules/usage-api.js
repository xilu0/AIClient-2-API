import { CONFIG } from '../core/config-manager.js';
import { serviceInstances, getServiceAdapter } from '../providers/adapter.js';
import { formatKiroUsage, formatGeminiUsage, formatAntigravityUsage } from '../services/usage-service.js';
import { readUsageCache, writeUsageCache, readProviderUsageCache, updateProviderUsageCache } from './usage-cache.js';
import path from 'path';

/**
 * 获取所有支持用量查询的提供商的用量信息
 * @param {Object} currentConfig - 当前配置
 * @param {Object} providerPoolManager - 提供商池管理器
 * @returns {Promise<Object>} 所有提供商的用量信息
 */
async function getAllProvidersUsage(currentConfig, providerPoolManager) {
    const results = {
        timestamp: new Date().toISOString(),
        providers: {}
    };

    // 支持用量查询的提供商列表
    const supportedProviders = ['claude-kiro-oauth', 'gemini-cli-oauth', 'gemini-antigravity'];

    // 并发获取所有提供商的用量数据
    const usagePromises = supportedProviders.map(async (providerType) => {
        try {
            const providerUsage = await getProviderTypeUsage(providerType, currentConfig, providerPoolManager);
            return { providerType, data: providerUsage, success: true };
        } catch (error) {
            return {
                providerType,
                data: {
                    error: error.message,
                    instances: []
                },
                success: false
            };
        }
    });

    // 等待所有并发请求完成
    const usageResults = await Promise.all(usagePromises);

    // 将结果整合到 results.providers 中
    for (const result of usageResults) {
        results.providers[result.providerType] = result.data;
    }

    return results;
}

/**
 * 获取单个提供商实例的用量信息
 * @param {Object} provider - 提供商配置
 * @param {string} providerType - 提供商类型
 * @returns {Promise<Object>} 实例用量结果
 */
async function getSingleProviderUsage(provider, providerType) {
    const providerKey = providerType + (provider.uuid || '');
    let adapter = serviceInstances[providerKey];

    const instanceResult = {
        uuid: provider.uuid || 'unknown',
        name: getProviderDisplayName(provider, providerType),
        isHealthy: provider.isHealthy !== false,
        isDisabled: provider.isDisabled === true,
        success: false,
        usage: null,
        error: null
    };

    // First check if disabled, skip initialization for disabled providers
    if (provider.isDisabled) {
        instanceResult.error = 'Provider is disabled';
        return instanceResult;
    }

    if (!adapter) {
        // Service instance not initialized, try auto-initialization
        try {
            console.log(`[Usage API] Auto-initializing service adapter for ${providerType}: ${provider.uuid}`);
            // Build configuration object
            const serviceConfig = {
                ...CONFIG,
                ...provider,
                MODEL_PROVIDER: providerType
            };
            adapter = getServiceAdapter(serviceConfig);
        } catch (initError) {
            console.error(`[Usage API] Failed to initialize adapter for ${providerType}: ${provider.uuid}:`, initError.message);
            instanceResult.error = `Service instance initialization failed: ${initError.message}`;
            return instanceResult;
        }
    }

    // If adapter exists (including just initialized), try to get usage
    if (adapter) {
        try {
            // 添加超时控制，避免长时间等待
            const usagePromise = getAdapterUsage(adapter, providerType);
            const timeoutPromise = new Promise((_, reject) =>
                setTimeout(() => reject(new Error('Usage query timeout')), 10000)
            );

            const usage = await Promise.race([usagePromise, timeoutPromise]);
            instanceResult.success = true;
            instanceResult.usage = usage;
        } catch (error) {
            instanceResult.error = error.message;
        }
    }

    return instanceResult;
}

/**
 * 获取指定提供商类型的用量信息
 * @param {string} providerType - 提供商类型
 * @param {Object} currentConfig - 当前配置
 * @param {Object} providerPoolManager - 提供商池管理器
 * @returns {Promise<Object>} 提供商用量信息
 */
async function getProviderTypeUsage(providerType, currentConfig, providerPoolManager) {
    const result = {
        providerType,
        instances: [],
        totalCount: 0,
        successCount: 0,
        errorCount: 0
    };

    // 获取提供商池中的所有实例
    let providers = [];
    if (providerPoolManager && providerPoolManager.providerPools && providerPoolManager.providerPools[providerType]) {
        providers = providerPoolManager.providerPools[providerType];
    } else if (currentConfig.providerPools && currentConfig.providerPools[providerType]) {
        providers = currentConfig.providerPools[providerType];
    }

    result.totalCount = providers.length;

    // 并发获取所有提供商的用量，使用 Promise.all 提高效率
    // 每批次最多处理 5 个，避免同时发起太多请求
    const BATCH_SIZE = 5;
    const batches = [];
    for (let i = 0; i < providers.length; i += BATCH_SIZE) {
        batches.push(providers.slice(i, i + BATCH_SIZE));
    }

    for (const batch of batches) {
        const batchPromises = batch.map(provider => getSingleProviderUsage(provider, providerType));
        const batchResults = await Promise.all(batchPromises);

        for (const instanceResult of batchResults) {
            if (instanceResult.success) {
                result.successCount++;
            } else if (instanceResult.error) {
                result.errorCount++;
            }
            result.instances.push(instanceResult);
        }
    }

    return result;
}

/**
 * 从适配器获取用量信息
 * @param {Object} adapter - 服务适配器
 * @param {string} providerType - 提供商类型
 * @returns {Promise<Object>} 用量信息
 */
async function getAdapterUsage(adapter, providerType) {
    if (providerType === 'claude-kiro-oauth') {
        if (typeof adapter.getUsageLimits === 'function') {
            const rawUsage = await adapter.getUsageLimits();
            return formatKiroUsage(rawUsage);
        } else if (adapter.kiroApiService && typeof adapter.kiroApiService.getUsageLimits === 'function') {
            const rawUsage = await adapter.kiroApiService.getUsageLimits();
            return formatKiroUsage(rawUsage);
        }
        throw new Error('This adapter does not support usage query');
    }
    
    if (providerType === 'gemini-cli-oauth') {
        if (typeof adapter.getUsageLimits === 'function') {
            const rawUsage = await adapter.getUsageLimits();
            return formatGeminiUsage(rawUsage);
        } else if (adapter.geminiApiService && typeof adapter.geminiApiService.getUsageLimits === 'function') {
            const rawUsage = await adapter.geminiApiService.getUsageLimits();
            return formatGeminiUsage(rawUsage);
        }
        throw new Error('This adapter does not support usage query');
    }
    
    if (providerType === 'gemini-antigravity') {
        if (typeof adapter.getUsageLimits === 'function') {
            const rawUsage = await adapter.getUsageLimits();
            return formatAntigravityUsage(rawUsage);
        } else if (adapter.antigravityApiService && typeof adapter.antigravityApiService.getUsageLimits === 'function') {
            const rawUsage = await adapter.antigravityApiService.getUsageLimits();
            return formatAntigravityUsage(rawUsage);
        }
        throw new Error('This adapter does not support usage query');
    }
    
    throw new Error(`Unsupported provider type: ${providerType}`);
}

/**
 * 获取提供商显示名称
 * @param {Object} provider - 提供商配置
 * @param {string} providerType - 提供商类型
 * @returns {string} 显示名称
 */
function getProviderDisplayName(provider, providerType) {
    // 优先使用自定义名称
    if (provider.customName) {
        return provider.customName;
    }

    if (provider.uuid) {
        return provider.uuid;
    }

    // 尝试从凭据文件路径提取名称
    const credPathKey = {
        'claude-kiro-oauth': 'KIRO_OAUTH_CREDS_FILE_PATH',
        'gemini-cli-oauth': 'GEMINI_OAUTH_CREDS_FILE_PATH',
        'gemini-antigravity': 'ANTIGRAVITY_OAUTH_CREDS_FILE_PATH',
        'openai-qwen-oauth': 'QWEN_OAUTH_CREDS_FILE_PATH',
        'openai-iflow': 'IFLOW_TOKEN_FILE_PATH'
    }[providerType];

    if (credPathKey && provider[credPathKey]) {
        const filePath = provider[credPathKey];
        const fileName = path.basename(filePath);
        const dirName = path.basename(path.dirname(filePath));
        return `${dirName}/${fileName}`;
    }

    return 'Unnamed';
}

/**
 * 获取所有提供商的用量限制
 */
export async function handleGetUsage(req, res, currentConfig, providerPoolManager) {
    try {
        // 解析查询参数，检查是否需要强制刷新
        const url = new URL(req.url, `http://${req.headers.host}`);
        const refresh = url.searchParams.get('refresh') === 'true';
        
        let usageResults;
        
        if (!refresh) {
            // 优先读取缓存
            const cachedData = await readUsageCache();
            if (cachedData) {
                console.log('[Usage API] Returning cached usage data');
                usageResults = { ...cachedData, fromCache: true };
            }
        }
        
        if (!usageResults) {
            // 缓存不存在或需要刷新，重新查询
            console.log('[Usage API] Fetching fresh usage data');
            usageResults = await getAllProvidersUsage(currentConfig, providerPoolManager);
            // 写入缓存
            await writeUsageCache(usageResults);
        }
        
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(usageResults));
        return true;
    } catch (error) {
        console.error('[UI API] Failed to get usage:', error);
        res.writeHead(500, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
            error: {
                message: 'Failed to get usage info: ' + error.message
            }
        }));
        return true;
    }
}

/**
 * 获取特定提供商类型的用量限制
 */
export async function handleGetProviderUsage(req, res, currentConfig, providerPoolManager, providerType) {
    try {
        // 解析查询参数，检查是否需要强制刷新
        const url = new URL(req.url, `http://${req.headers.host}`);
        const refresh = url.searchParams.get('refresh') === 'true';
        
        let usageResults;
        
        if (!refresh) {
            // Prefer reading from cache
            const cachedData = await readProviderUsageCache(providerType);
            if (cachedData) {
                console.log(`[Usage API] Returning cached usage data for ${providerType}`);
                usageResults = cachedData;
            }
        }
        
        if (!usageResults) {
            // Cache does not exist or refresh required, re-query
            console.log(`[Usage API] Fetching fresh usage data for ${providerType}`);
            usageResults = await getProviderTypeUsage(providerType, currentConfig, providerPoolManager);
            // 更新缓存
            await updateProviderUsageCache(providerType, usageResults);
        }
        
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(usageResults));
        return true;
    } catch (error) {
        console.error(`[UI API] Failed to get usage for ${providerType}:`, error);
        res.writeHead(500, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
            error: {
                message: `Failed to get usage info for ${providerType}: ` + error.message
            }
        }));
        return true;
    }
}