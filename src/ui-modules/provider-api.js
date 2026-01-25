import { existsSync, readFileSync, writeFileSync } from 'fs';
import { getRequestBody } from '../utils/common.js';
import { getAllProviderModels, getProviderModels } from '../providers/provider-models.js';
import { generateUUID, createProviderConfig, formatSystemPath, detectProviderFromPath, addToUsedPaths, isPathUsed, pathsEqual } from '../utils/provider-utils.js';
import { broadcastEvent } from './event-broadcast.js';
import { getStorageAdapter, isStorageInitialized } from '../core/storage-factory.js';

/**
 * Get the storage adapter if available
 * @returns {import('../core/storage-adapter.js').StorageAdapter|null}
 */
function getAdapter() {
    if (isStorageInitialized()) {
        return getStorageAdapter();
    }
    return null;
}

/**
 * Check if Redis storage is being used
 * @returns {boolean}
 */
function isUsingRedis() {
    const adapter = getAdapter();
    return adapter && adapter.getType() === 'redis';
}

/**
 * 获取提供商池摘要
 */
export async function handleGetProviders(req, res, currentConfig, providerPoolManager) {
    let providerPools = {};
    const filePath = currentConfig.PROVIDER_POOLS_FILE_PATH || 'configs/provider_pools.json';
    try {
        // Try storage adapter first
        const adapter = getAdapter();
        if (adapter) {
            providerPools = await adapter.getProviderPools();
        } else if (providerPoolManager && providerPoolManager.providerPools) {
            providerPools = providerPoolManager.providerPools;
        } else if (filePath && existsSync(filePath)) {
            const poolsData = JSON.parse(readFileSync(filePath, 'utf-8'));
            providerPools = poolsData;
        }
    } catch (error) {
        console.warn('[UI API] Failed to load provider pools:', error.message);
    }

    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(providerPools));
    return true;
}

/**
 * 获取特定提供商类型的详细信息
 */
export async function handleGetProviderType(req, res, currentConfig, providerPoolManager, providerType) {
    let providerPools = {};
    const filePath = currentConfig.PROVIDER_POOLS_FILE_PATH || 'configs/provider_pools.json';
    try {
        if (providerPoolManager && providerPoolManager.providerPools) {
            providerPools = providerPoolManager.providerPools;
        } else if (filePath && existsSync(filePath)) {
            const poolsData = JSON.parse(readFileSync(filePath, 'utf-8'));
            providerPools = poolsData;
        }
    } catch (error) {
        console.warn('[UI API] Failed to load provider pools:', error.message);
    }

    const providers = providerPools[providerType] || [];
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({
        providerType,
        providers,
        totalCount: providers.length,
        healthyCount: providers.filter(p => p.isHealthy).length
    }));
    return true;
}

/**
 * 获取所有提供商的可用模型
 */
export async function handleGetProviderModels(req, res) {
    const allModels = getAllProviderModels();
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(allModels));
    return true;
}

/**
 * 获取特定提供商类型的可用模型
 */
export async function handleGetProviderTypeModels(req, res, providerType) {
    const models = getProviderModels(providerType);
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({
        providerType,
        models
    }));
    return true;
}

/**
 * 添加新的提供商配置
 */
export async function handleAddProvider(req, res, currentConfig, providerPoolManager) {
    try {
        const body = await getRequestBody(req);
        const { providerType, providerConfig } = body;

        if (!providerType || !providerConfig) {
            res.writeHead(400, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ error: { message: 'providerType and providerConfig are required' } }));
            return true;
        }

        // Generate UUID if not provided
        if (!providerConfig.uuid) {
            providerConfig.uuid = generateUUID();
        }

        // Set default values
        providerConfig.isHealthy = providerConfig.isHealthy !== undefined ? providerConfig.isHealthy : true;
        providerConfig.lastUsed = providerConfig.lastUsed || null;
        providerConfig.usageCount = providerConfig.usageCount || 0;
        providerConfig.errorCount = providerConfig.errorCount || 0;
        providerConfig.lastErrorTime = providerConfig.lastErrorTime || null;

        const filePath = currentConfig.PROVIDER_POOLS_FILE_PATH || 'provider_pools.json';
        const adapter = getAdapter();

        // Use storage adapter if available
        if (adapter) {
            try {
                await adapter.addProvider(providerType, providerConfig);
                console.log(`[UI API] Added new provider to ${providerType} via ${adapter.getType()}: ${providerConfig.uuid}`);
            } catch (adapterError) {
                console.error('[UI API] Storage adapter add failed:', adapterError.message);
                // Fall through to file-based storage
            }
        }

        // Also update file storage for backward compatibility (if not using Redis-only mode)
        if (!adapter || adapter.getType() !== 'redis') {
            let providerPools = {};
            // Load existing pools
            if (existsSync(filePath)) {
                try {
                    const fileContent = readFileSync(filePath, 'utf-8');
                    providerPools = JSON.parse(fileContent);
                } catch (readError) {
                    console.warn('[UI API] Failed to read existing provider pools:', readError.message);
                }
            }

            // Add new provider to the appropriate type
            if (!providerPools[providerType]) {
                providerPools[providerType] = [];
            }
            providerPools[providerType].push(providerConfig);

            // Save to file
            writeFileSync(filePath, JSON.stringify(providerPools, null, 2), 'utf-8');
            console.log(`[UI API] Added new provider to ${providerType}: ${providerConfig.uuid}`);
        }

        // Update provider pool manager if available
        if (providerPoolManager) {
            // Reload from storage or update in-memory
            if (adapter) {
                const pools = await adapter.getProviderPools();
                providerPoolManager.providerPools = pools;
            } else {
                if (!providerPoolManager.providerPools[providerType]) {
                    providerPoolManager.providerPools[providerType] = [];
                }
                providerPoolManager.providerPools[providerType].push(providerConfig);
            }
            providerPoolManager.initializeProviderStatus();
        }

        // 广播更新事件
        broadcastEvent('config_update', {
            action: 'add',
            filePath: filePath,
            providerType,
            providerConfig,
            timestamp: new Date().toISOString()
        });

        // 广播提供商更新事件
        broadcastEvent('provider_update', {
            action: 'add',
            providerType,
            providerConfig,
            timestamp: new Date().toISOString()
        });

        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
            success: true,
            message: 'Provider added successfully',
            provider: providerConfig,
            providerType
        }));
        return true;
    } catch (error) {
        res.writeHead(500, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ error: { message: error.message } }));
        return true;
    }
}

/**
 * 更新特定提供商配置
 */
export async function handleUpdateProvider(req, res, currentConfig, providerPoolManager, providerType, providerUuid) {
    try {
        const body = await getRequestBody(req);
        const { providerConfig } = body;

        if (!providerConfig) {
            res.writeHead(400, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ error: { message: 'providerConfig is required' } }));
            return true;
        }

        const filePath = currentConfig.PROVIDER_POOLS_FILE_PATH || 'configs/provider_pools.json';
        const adapter = getAdapter();
        let existingProvider = null;
        let providerPools = {};

        // Get existing provider - try storage adapter first
        if (adapter) {
            existingProvider = await adapter.getProvider(providerType, providerUuid);
        }

        // Fall back to file if needed
        if (!existingProvider) {
            if (existsSync(filePath)) {
                try {
                    const fileContent = readFileSync(filePath, 'utf-8');
                    providerPools = JSON.parse(fileContent);
                    const providers = providerPools[providerType] || [];
                    existingProvider = providers.find(p => p.uuid === providerUuid);
                } catch (readError) {
                    res.writeHead(404, { 'Content-Type': 'application/json' });
                    res.end(JSON.stringify({ error: { message: 'Provider pools file not found' } }));
                    return true;
                }
            }
        }

        if (!existingProvider) {
            res.writeHead(404, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ error: { message: 'Provider not found' } }));
            return true;
        }

        // Update provider while preserving certain fields
        const updatedProvider = {
            ...existingProvider,
            ...providerConfig,
            uuid: providerUuid, // Ensure UUID doesn't change
            lastUsed: existingProvider.lastUsed, // Preserve usage stats
            usageCount: existingProvider.usageCount,
            errorCount: existingProvider.errorCount,
            lastErrorTime: existingProvider.lastErrorTime
        };

        // Use storage adapter if available
        if (adapter) {
            try {
                // For updates, we need to compute the diff
                const updates = {};
                for (const key of Object.keys(providerConfig)) {
                    if (key !== 'uuid' && key !== 'lastUsed' && key !== 'usageCount' &&
                        key !== 'errorCount' && key !== 'lastErrorTime') {
                        updates[key] = providerConfig[key];
                    }
                }
                await adapter.updateProvider(providerType, providerUuid, updates);
                console.log(`[UI API] Updated provider ${providerUuid} via ${adapter.getType()}`);
            } catch (adapterError) {
                console.error('[UI API] Storage adapter update failed:', adapterError.message);
            }
        }

        // Also update file storage for backward compatibility
        if (!adapter || adapter.getType() !== 'redis') {
            if (Object.keys(providerPools).length === 0 && existsSync(filePath)) {
                providerPools = JSON.parse(readFileSync(filePath, 'utf-8'));
            }
            const providers = providerPools[providerType] || [];
            const providerIndex = providers.findIndex(p => p.uuid === providerUuid);
            if (providerIndex !== -1) {
                providerPools[providerType][providerIndex] = updatedProvider;
                writeFileSync(filePath, JSON.stringify(providerPools, null, 2), 'utf-8');
                console.log(`[UI API] Updated provider ${providerUuid} in ${providerType}`);
            }
        }

        // Update provider pool manager if available
        if (providerPoolManager) {
            if (adapter) {
                const pools = await adapter.getProviderPools();
                providerPoolManager.providerPools = pools;
            } else {
                providerPoolManager.providerPools = providerPools;
            }
            providerPoolManager.initializeProviderStatus();
        }

        // 广播更新事件
        broadcastEvent('config_update', {
            action: 'update',
            filePath: filePath,
            providerType,
            providerConfig: updatedProvider,
            timestamp: new Date().toISOString()
        });

        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
            success: true,
            message: 'Provider updated successfully',
            provider: updatedProvider
        }));
        return true;
    } catch (error) {
        res.writeHead(500, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ error: { message: error.message } }));
        return true;
    }
}

/**
 * 删除特定提供商配置
 */
export async function handleDeleteProvider(req, res, currentConfig, providerPoolManager, providerType, providerUuid) {
    try {
        const filePath = currentConfig.PROVIDER_POOLS_FILE_PATH || 'configs/provider_pools.json';
        const adapter = getAdapter();
        let providerPools = {};
        let deletedProvider = null;

        // Get provider to delete - try storage adapter first
        if (adapter) {
            deletedProvider = await adapter.getProvider(providerType, providerUuid);
        }

        // Fall back to file if needed
        if (!deletedProvider && existsSync(filePath)) {
            try {
                const fileContent = readFileSync(filePath, 'utf-8');
                providerPools = JSON.parse(fileContent);
                const providers = providerPools[providerType] || [];
                deletedProvider = providers.find(p => p.uuid === providerUuid);
            } catch (readError) {
                res.writeHead(404, { 'Content-Type': 'application/json' });
                res.end(JSON.stringify({ error: { message: 'Provider pools file not found' } }));
                return true;
            }
        }

        if (!deletedProvider) {
            res.writeHead(404, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ error: { message: 'Provider not found' } }));
            return true;
        }

        // Use storage adapter if available
        let deleteResult = null;
        if (adapter) {
            try {
                deleteResult = await adapter.deleteProvider(providerType, providerUuid);
                console.log(`[UI API] Deleted provider ${providerUuid} via ${adapter.getType()}${deleteResult?.queued ? ' (queued)' : ''}`);
            } catch (adapterError) {
                console.error('[UI API] Storage adapter delete failed:', adapterError.message);
            }
        }

        // Always update file storage for consistency (regardless of adapter type)
        // This ensures data persistence even if Redis operations are queued or fail
        if (!adapter || adapter.getType() !== 'redis') {
            // For non-Redis adapters, update file storage directly
            if (Object.keys(providerPools).length === 0 && existsSync(filePath)) {
                providerPools = JSON.parse(readFileSync(filePath, 'utf-8'));
            }
            const providers = providerPools[providerType] || [];
            const providerIndex = providers.findIndex(p => p.uuid === providerUuid);
            if (providerIndex !== -1) {
                providers.splice(providerIndex, 1);
                if (providers.length === 0) {
                    delete providerPools[providerType];
                }
                writeFileSync(filePath, JSON.stringify(providerPools, null, 2), 'utf-8');
                console.log(`[UI API] Deleted provider ${providerUuid} from file storage`);
            }
        }
        // Note: For Redis adapter, deleteProvider already syncs to file backup internally

        // Update provider pool manager directly from memory to avoid race conditions
        // Do NOT reload from adapter as it may return stale data if Redis is unavailable
        if (providerPoolManager) {
            const providers = providerPoolManager.providerPools[providerType] || [];
            providerPoolManager.providerPools[providerType] = providers.filter(p => p.uuid !== providerUuid);
            // Clean up empty provider type
            if (providerPoolManager.providerPools[providerType]?.length === 0) {
                delete providerPoolManager.providerPools[providerType];
            }
            providerPoolManager.initializeProviderStatus();
            console.log(`[UI API] Updated in-memory provider pool manager`);
        }

        // 广播更新事件
        broadcastEvent('config_update', {
            action: 'delete',
            filePath: filePath,
            providerType,
            providerConfig: deletedProvider,
            timestamp: new Date().toISOString()
        });

        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
            success: true,
            message: 'Provider deleted successfully',
            deletedProvider
        }));
        return true;
    } catch (error) {
        res.writeHead(500, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ error: { message: error.message } }));
        return true;
    }
}

/**
 * 禁用/启用特定提供商配置
 */
export async function handleDisableEnableProvider(req, res, currentConfig, providerPoolManager, providerType, providerUuid, action) {
    try {
        const filePath = currentConfig.PROVIDER_POOLS_FILE_PATH || 'configs/provider_pools.json';
        let providerPools = {};
        
        // Load existing pools
        if (existsSync(filePath)) {
            try {
                const fileContent = readFileSync(filePath, 'utf-8');
                providerPools = JSON.parse(fileContent);
            } catch (readError) {
                res.writeHead(404, { 'Content-Type': 'application/json' });
                res.end(JSON.stringify({ error: { message: 'Provider pools file not found' } }));
                return true;
            }
        }

        // Find and update the provider
        const providers = providerPools[providerType] || [];
        const providerIndex = providers.findIndex(p => p.uuid === providerUuid);
        
        if (providerIndex === -1) {
            res.writeHead(404, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ error: { message: 'Provider not found' } }));
            return true;
        }

        // Update isDisabled field
        const provider = providers[providerIndex];
        provider.isDisabled = action === 'disable';
        
        // Save to file
        writeFileSync(filePath, JSON.stringify(providerPools, null, 2), 'utf-8');
        console.log(`[UI API] ${action === 'disable' ? 'Disabled' : 'Enabled'} provider ${providerUuid} in ${providerType}`);

        // Update provider pool manager if available
        if (providerPoolManager) {
            providerPoolManager.providerPools = providerPools;
            
            // Call the appropriate method
            if (action === 'disable') {
                providerPoolManager.disableProvider(providerType, provider);
            } else {
                providerPoolManager.enableProvider(providerType, provider);
            }
        }

        // 广播更新事件
        broadcastEvent('config_update', {
            action: action,
            filePath: filePath,
            providerType,
            providerConfig: provider,
            timestamp: new Date().toISOString()
        });

        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
            success: true,
            message: `Provider ${action}d successfully`,
            provider: provider
        }));
        return true;
    } catch (error) {
        res.writeHead(500, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ error: { message: error.message } }));
        return true;
    }
}

/**
 * 重置特定提供商类型的所有提供商健康状态
 */
export async function handleResetProviderHealth(req, res, currentConfig, providerPoolManager, providerType) {
    try {
        const filePath = currentConfig.PROVIDER_POOLS_FILE_PATH || 'configs/provider_pools.json';
        let providerPools = {};
        
        // Load existing pools
        if (existsSync(filePath)) {
            try {
                const fileContent = readFileSync(filePath, 'utf-8');
                providerPools = JSON.parse(fileContent);
            } catch (readError) {
                res.writeHead(404, { 'Content-Type': 'application/json' });
                res.end(JSON.stringify({ error: { message: 'Provider pools file not found' } }));
                return true;
            }
        }

        // Reset health status for all providers of this type
        const providers = providerPools[providerType] || [];
        
        if (providers.length === 0) {
            res.writeHead(404, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ error: { message: 'No providers found for this type' } }));
            return true;
        }

        let resetCount = 0;
        providers.forEach(provider => {
            // 统计 isHealthy 从 false 变为 true 的节点数量
            if (!provider.isHealthy) {
                resetCount++;
            }
            // 重置所有节点的状态
            provider.isHealthy = true;
            provider.errorCount = 0;
            provider.refreshCount = 0;
            provider.needsRefresh = false;
            provider.lastErrorTime = null;
        });

        // Save to file
        writeFileSync(filePath, JSON.stringify(providerPools, null, 2), 'utf-8');
        console.log(`[UI API] Reset health status for ${resetCount} providers in ${providerType}`);

        // Update provider pool manager if available
        if (providerPoolManager) {
            providerPoolManager.providerPools = providerPools;
            providerPoolManager.initializeProviderStatus();
        }

        // 广播更新事件
        broadcastEvent('config_update', {
            action: 'reset_health',
            filePath: filePath,
            providerType,
            resetCount,
            timestamp: new Date().toISOString()
        });

        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
            success: true,
            message: `Successfully reset health status for ${resetCount} providers`,
            resetCount,
            totalCount: providers.length
        }));
        return true;
    } catch (error) {
        res.writeHead(500, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ error: { message: error.message } }));
        return true;
    }
}

/**
 * 删除特定提供商类型的所有不健康节点
 */
export async function handleDeleteUnhealthyProviders(req, res, currentConfig, providerPoolManager, providerType) {
    try {
        const filePath = currentConfig.PROVIDER_POOLS_FILE_PATH || 'configs/provider_pools.json';
        const adapter = getAdapter();
        let providers = [];

        // Try to get providers from storage adapter first (Redis), then fall back to file
        if (adapter) {
            try {
                const pools = await adapter.getProviderPools();
                providers = pools[providerType] || [];
            } catch (adapterError) {
                console.error('[UI API] Storage adapter read failed:', adapterError.message);
            }
        }

        // Fall back to file if adapter didn't return data
        if (providers.length === 0 && existsSync(filePath)) {
            try {
                const fileContent = readFileSync(filePath, 'utf-8');
                const filePools = JSON.parse(fileContent);
                providers = filePools[providerType] || [];
            } catch (readError) {
                // File read failed, continue with empty providers
            }
        }

        // Also check in-memory providerPoolManager as last resort
        if (providers.length === 0 && providerPoolManager) {
            providers = providerPoolManager.providerPools[providerType] || [];
        }

        if (providers.length === 0) {
            res.writeHead(404, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ error: { message: 'No providers found for this type' } }));
            return true;
        }

        // Filter out unhealthy providers (keep only healthy ones)
        const unhealthyProviders = providers.filter(p => !p.isHealthy);
        const healthyProviders = providers.filter(p => p.isHealthy);

        if (unhealthyProviders.length === 0) {
            res.writeHead(200, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({
                success: true,
                message: 'No unhealthy providers to delete',
                deletedCount: 0,
                remainingCount: providers.length
            }));
            return true;
        }

        // Delete unhealthy providers from storage adapter
        if (adapter) {
            for (const provider of unhealthyProviders) {
                try {
                    await adapter.deleteProvider(providerType, provider.uuid);
                    console.log(`[UI API] Deleted unhealthy provider ${provider.uuid} via ${adapter.getType()}`);
                } catch (adapterError) {
                    console.error(`[UI API] Storage adapter delete failed for ${provider.uuid}:`, adapterError.message);
                }
            }
        }

        // Always update file storage for consistency (for non-Redis or as backup)
        if (!adapter || adapter.getType() !== 'redis') {
            // For non-Redis adapters, update file storage directly
            if (existsSync(filePath)) {
                try {
                    const fileContent = readFileSync(filePath, 'utf-8');
                    const filePools = JSON.parse(fileContent);
                    if (healthyProviders.length === 0) {
                        delete filePools[providerType];
                    } else {
                        filePools[providerType] = healthyProviders;
                    }
                    writeFileSync(filePath, JSON.stringify(filePools, null, 2), 'utf-8');
                    console.log(`[UI API] Updated file storage after deleting ${unhealthyProviders.length} unhealthy providers`);
                } catch (fileError) {
                    console.error('[UI API] File storage update failed:', fileError.message);
                }
            }
        }
        // Note: For Redis adapter, deleteProvider already syncs to file backup internally

        // Update provider pool manager directly from memory to avoid race conditions
        if (providerPoolManager) {
            providerPoolManager.providerPools[providerType] = healthyProviders;
            if (healthyProviders.length === 0) {
                delete providerPoolManager.providerPools[providerType];
            }
            providerPoolManager.initializeProviderStatus();
            console.log(`[UI API] Updated in-memory provider pool manager`);
        }

        // 广播更新事件
        broadcastEvent('config_update', {
            action: 'delete_unhealthy',
            filePath: filePath,
            providerType,
            deletedCount: unhealthyProviders.length,
            deletedProviders: unhealthyProviders.map(p => ({ uuid: p.uuid, customName: p.customName })),
            timestamp: new Date().toISOString()
        });

        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
            success: true,
            message: `Successfully deleted ${unhealthyProviders.length} unhealthy providers`,
            deletedCount: unhealthyProviders.length,
            remainingCount: healthyProviders.length,
            deletedProviders: unhealthyProviders.map(p => ({ uuid: p.uuid, customName: p.customName }))
        }));
        return true;
    } catch (error) {
        res.writeHead(500, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ error: { message: error.message } }));
        return true;
    }
}

/**
 * 批量刷新特定提供商类型的所有不健康节点的 UUID
 */
export async function handleRefreshUnhealthyUuids(req, res, currentConfig, providerPoolManager, providerType) {
    try {
        const filePath = currentConfig.PROVIDER_POOLS_FILE_PATH || 'configs/provider_pools.json';
        const adapter = getAdapter();
        let providers = [];

        // Try to get providers from storage adapter first (Redis), then fall back to file
        if (adapter) {
            try {
                const pools = await adapter.getProviderPools();
                providers = pools[providerType] || [];
            } catch (adapterError) {
                console.error('[UI API] Storage adapter read failed:', adapterError.message);
            }
        }

        // Fall back to file if adapter didn't return data
        if (providers.length === 0 && existsSync(filePath)) {
            try {
                const fileContent = readFileSync(filePath, 'utf-8');
                const filePools = JSON.parse(fileContent);
                providers = filePools[providerType] || [];
            } catch (readError) {
                // File read failed, continue with empty providers
            }
        }

        // Also check in-memory providerPoolManager as last resort
        if (providers.length === 0 && providerPoolManager) {
            providers = providerPoolManager.providerPools[providerType] || [];
        }

        if (providers.length === 0) {
            res.writeHead(404, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ error: { message: 'No providers found for this type' } }));
            return true;
        }

        // Filter unhealthy providers and refresh their UUIDs
        const refreshedProviders = [];
        for (const provider of providers) {
            if (!provider.isHealthy) {
                const oldUuid = provider.uuid;
                const newUuid = generateUUID();

                // Delete old entry and add new one in storage adapter
                if (adapter) {
                    try {
                        await adapter.deleteProvider(providerType, oldUuid);
                        provider.uuid = newUuid;
                        await adapter.addProvider(providerType, provider);
                        console.log(`[UI API] Refreshed UUID ${oldUuid} -> ${newUuid} via ${adapter.getType()}`);
                    } catch (adapterError) {
                        console.error(`[UI API] Storage adapter UUID refresh failed for ${oldUuid}:`, adapterError.message);
                        provider.uuid = newUuid; // Still update in memory
                    }
                } else {
                    provider.uuid = newUuid;
                }

                refreshedProviders.push({
                    oldUuid,
                    newUuid,
                    customName: provider.customName
                });
            }
        }

        if (refreshedProviders.length === 0) {
            res.writeHead(200, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({
                success: true,
                message: 'No unhealthy providers to refresh',
                refreshedCount: 0,
                totalCount: providers.length
            }));
            return true;
        }

        // Always update file storage for consistency (for non-Redis or as backup)
        if (!adapter || adapter.getType() !== 'redis') {
            // For non-Redis adapters, update file storage directly
            if (existsSync(filePath)) {
                try {
                    const fileContent = readFileSync(filePath, 'utf-8');
                    const filePools = JSON.parse(fileContent);
                    filePools[providerType] = providers;
                    writeFileSync(filePath, JSON.stringify(filePools, null, 2), 'utf-8');
                    console.log(`[UI API] Updated file storage after refreshing ${refreshedProviders.length} UUIDs`);
                } catch (fileError) {
                    console.error('[UI API] File storage update failed:', fileError.message);
                }
            }
        }
        // Note: For Redis adapter, deleteProvider/addProvider already sync to file backup internally

        // Update provider pool manager directly from memory
        if (providerPoolManager) {
            providerPoolManager.providerPools[providerType] = providers;
            providerPoolManager.initializeProviderStatus();
            console.log(`[UI API] Updated in-memory provider pool manager`);
        }

        // 广播更新事件
        broadcastEvent('config_update', {
            action: 'refresh_unhealthy_uuids',
            filePath: filePath,
            providerType,
            refreshedCount: refreshedProviders.length,
            refreshedProviders,
            timestamp: new Date().toISOString()
        });

        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
            success: true,
            message: `Successfully refreshed UUIDs for ${refreshedProviders.length} unhealthy providers`,
            refreshedCount: refreshedProviders.length,
            totalCount: providers.length,
            refreshedProviders
        }));
        return true;
    } catch (error) {
        res.writeHead(500, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ error: { message: error.message } }));
        return true;
    }
}

/**
 * 对特定提供商类型的所有提供商执行健康检查
 */
export async function handleHealthCheck(req, res, currentConfig, providerPoolManager, providerType) {
    try {
        if (!providerPoolManager) {
            res.writeHead(400, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ error: { message: 'Provider pool manager not initialized' } }));
            return true;
        }

        const providers = providerPoolManager.providerStatus[providerType] || [];
        
        if (providers.length === 0) {
            res.writeHead(404, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ error: { message: 'No providers found for this type' } }));
            return true;
        }

        // 只检测不健康的节点
        const unhealthyProviders = providers.filter(ps => !ps.config.isHealthy);
        
        if (unhealthyProviders.length === 0) {
            res.writeHead(200, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({
                success: true,
                message: 'No unhealthy providers to check',
                successCount: 0,
                failCount: 0,
                totalCount: providers.length,
                results: []
            }));
            return true;
        }

        console.log(`[UI API] Starting health check for ${unhealthyProviders.length} unhealthy providers in ${providerType} (total: ${providers.length})`);

        // 执行健康检测（强制检查，忽略 checkHealth 配置）
        const results = [];
        for (const providerStatus of unhealthyProviders) {
            const providerConfig = providerStatus.config;
            
            // 跳过已禁用的节点
            if (providerConfig.isDisabled) {
                console.log(`[UI API] Skipping health check for disabled provider: ${providerConfig.uuid}`);
                continue;
            }

            try {
                // 传递 forceCheck = true 强制执行健康检查，忽略 checkHealth 配置
                const healthResult = await providerPoolManager._checkProviderHealth(providerType, providerConfig, true);
                
                if (healthResult === null) {
                    results.push({
                        uuid: providerConfig.uuid,
                        success: null,
                        message: 'Health check not supported for this provider type'
                    });
                    continue;
                }
                
                if (healthResult.success) {
                    providerPoolManager.markProviderHealthy(providerType, providerConfig, false, healthResult.modelName);
                    results.push({
                        uuid: providerConfig.uuid,
                        success: true,
                        modelName: healthResult.modelName,
                        message: 'Healthy'
                    });
                } else {
                    // 检查是否为认证错误（401/403），如果是则立即标记为不健康
                    const errorMessage = healthResult.errorMessage || 'Check failed';
                    const isAuthError = /\b(401|403)\b/.test(errorMessage) ||
                                       /\b(Unauthorized|Forbidden|AccessDenied|InvalidToken|ExpiredToken)\b/i.test(errorMessage);
                    
                    if (isAuthError) {
                        providerPoolManager.markProviderUnhealthyImmediately(providerType, providerConfig, errorMessage);
                        console.log(`[UI API] Auth error detected for ${providerConfig.uuid}, immediately marked as unhealthy`);
                    } else {
                        providerPoolManager.markProviderUnhealthy(providerType, providerConfig, errorMessage);
                    }
                    
                    providerStatus.config.lastHealthCheckTime = new Date().toISOString();
                    if (healthResult.modelName) {
                        providerStatus.config.lastHealthCheckModel = healthResult.modelName;
                    }
                    results.push({
                        uuid: providerConfig.uuid,
                        success: false,
                        modelName: healthResult.modelName,
                        message: errorMessage,
                        isAuthError: isAuthError
                    });
                }
            } catch (error) {
                const errorMessage = error.message || 'Unknown error';
                // 检查是否为认证错误（401/403），如果是则立即标记为不健康
                const isAuthError = /\b(401|403)\b/.test(errorMessage) ||
                                   /\b(Unauthorized|Forbidden|AccessDenied|InvalidToken|ExpiredToken)\b/i.test(errorMessage);
                
                if (isAuthError) {
                    providerPoolManager.markProviderUnhealthyImmediately(providerType, providerConfig, errorMessage);
                    console.log(`[UI API] Auth error detected for ${providerConfig.uuid}, immediately marked as unhealthy`);
                } else {
                    providerPoolManager.markProviderUnhealthy(providerType, providerConfig, errorMessage);
                }
                
                results.push({
                    uuid: providerConfig.uuid,
                    success: false,
                    message: errorMessage,
                    isAuthError: isAuthError
                });
            }
        }

        // 保存更新后的状态到文件
        const filePath = currentConfig.PROVIDER_POOLS_FILE_PATH || 'configs/provider_pools.json';
        
        // 从 providerStatus 构建 providerPools 对象并保存
        const providerPools = {};
        for (const pType in providerPoolManager.providerStatus) {
            providerPools[pType] = providerPoolManager.providerStatus[pType].map(ps => ps.config);
        }
        writeFileSync(filePath, JSON.stringify(providerPools, null, 2), 'utf-8');

        const successCount = results.filter(r => r.success === true).length;
        const failCount = results.filter(r => r.success === false).length;

        console.log(`[UI API] Health check completed for ${providerType}: ${successCount} recovered, ${failCount} still unhealthy (checked ${unhealthyProviders.length} unhealthy nodes)`);

        // 广播更新事件
        broadcastEvent('config_update', {
            action: 'health_check',
            filePath: filePath,
            providerType,
            results,
            timestamp: new Date().toISOString()
        });

        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
            success: true,
            message: `Health check completed: ${successCount} healthy, ${failCount} unhealthy`,
            successCount,
            failCount,
            totalCount: providers.length,
            results
        }));
        return true;
    } catch (error) {
        console.error('[UI API] Health check error:', error);
        res.writeHead(500, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ error: { message: error.message } }));
        return true;
    }
}

/**
 * 快速链接配置文件到对应的提供商
 */
export async function handleQuickLinkProvider(req, res, currentConfig, providerPoolManager) {
    try {
        const body = await getRequestBody(req);
        const { filePath } = body;

        if (!filePath) {
            res.writeHead(400, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ error: { message: 'filePath is required' } }));
            return true;
        }

        const normalizedPath = filePath.replace(/\\/g, '/').toLowerCase();
        
        // 根据文件路径自动识别提供商类型
        const providerMapping = detectProviderFromPath(normalizedPath);
        
        if (!providerMapping) {
            res.writeHead(400, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({
                error: {
                    message: 'Unable to identify provider type for config file, please ensure file is in configs/kiro/, configs/gemini/, configs/qwen/ or configs/antigravity/ directory'
                }
            }));
            return true;
        }

        const { providerType, credPathKey, defaultCheckModel, displayName } = providerMapping;
        const poolsFilePath = currentConfig.PROVIDER_POOLS_FILE_PATH || 'configs/provider_pools.json';
        
        // Load existing pools
        let providerPools = {};
        if (existsSync(poolsFilePath)) {
            try {
                const fileContent = readFileSync(poolsFilePath, 'utf-8');
                providerPools = JSON.parse(fileContent);
            } catch (readError) {
                console.warn('[UI API] Failed to read existing provider pools:', readError.message);
            }
        }

        // Ensure provider type array exists
        if (!providerPools[providerType]) {
            providerPools[providerType] = [];
        }

        // Check if already linked - 使用标准化路径进行比较
        const normalizedForComparison = filePath.replace(/\\/g, '/');
        const isAlreadyLinked = providerPools[providerType].some(p => {
            const existingPath = p[credPathKey];
            if (!existingPath) return false;
            const normalizedExistingPath = existingPath.replace(/\\/g, '/');
            return normalizedExistingPath === normalizedForComparison ||
                   normalizedExistingPath === './' + normalizedForComparison ||
                   './' + normalizedExistingPath === normalizedForComparison;
        });

        if (isAlreadyLinked) {
            res.writeHead(400, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ error: { message: 'This config file is already linked' } }));
            return true;
        }

        // Create new provider config based on provider type
        const newProvider = createProviderConfig({
            credPathKey,
            credPath: formatSystemPath(filePath),
            defaultCheckModel,
            needsProjectId: providerMapping.needsProjectId
        });

        providerPools[providerType].push(newProvider);

        // Save to file
        writeFileSync(poolsFilePath, JSON.stringify(providerPools, null, 2), 'utf-8');
        console.log(`[UI API] Quick linked config: ${filePath} -> ${providerType}`);

        // Update provider pool manager if available
        if (providerPoolManager) {
            providerPoolManager.providerPools = providerPools;
            providerPoolManager.initializeProviderStatus();
        }

        // Broadcast update event
        broadcastEvent('config_update', {
            action: 'quick_link',
            filePath: poolsFilePath,
            providerType,
            newProvider,
            timestamp: new Date().toISOString()
        });

        broadcastEvent('provider_update', {
            action: 'add',
            providerType,
            providerConfig: newProvider,
            timestamp: new Date().toISOString()
        });

        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
            success: true,
            message: `Config successfully linked to ${displayName}`,
            provider: newProvider,
            providerType: providerType
        }));
        return true;
    } catch (error) {
        console.error('[UI API] Quick link failed:', error);
        res.writeHead(500, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
            error: {
                message: 'Link failed: ' + error.message
            }
        }));
        return true;
    }
}

/**
 * 刷新特定提供商的UUID
 */
export async function handleRefreshProviderUuid(req, res, currentConfig, providerPoolManager, providerType, providerUuid) {
    try {
        const filePath = currentConfig.PROVIDER_POOLS_FILE_PATH || 'configs/provider_pools.json';
        let providerPools = {};
        
        // Load existing pools
        if (existsSync(filePath)) {
            try {
                const fileContent = readFileSync(filePath, 'utf-8');
                providerPools = JSON.parse(fileContent);
            } catch (readError) {
                res.writeHead(404, { 'Content-Type': 'application/json' });
                res.end(JSON.stringify({ error: { message: 'Provider pools file not found' } }));
                return true;
            }
        }

        // Find the provider
        const providers = providerPools[providerType] || [];
        const providerIndex = providers.findIndex(p => p.uuid === providerUuid);
        
        if (providerIndex === -1) {
            res.writeHead(404, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ error: { message: 'Provider not found' } }));
            return true;
        }

        // Generate new UUID
        const oldUuid = providerUuid;
        const newUuid = generateUUID();
        
        // Update provider UUID
        providerPools[providerType][providerIndex].uuid = newUuid;

        // Save to file
        writeFileSync(filePath, JSON.stringify(providerPools, null, 2), 'utf-8');
        console.log(`[UI API] Refreshed UUID for provider in ${providerType}: ${oldUuid} -> ${newUuid}`);

        // Update provider pool manager if available
        if (providerPoolManager) {
            providerPoolManager.providerPools = providerPools;
            providerPoolManager.initializeProviderStatus();
        }

        // 广播更新事件
        broadcastEvent('config_update', {
            action: 'refresh_uuid',
            filePath: filePath,
            providerType,
            oldUuid,
            newUuid,
            timestamp: new Date().toISOString()
        });

        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
            success: true,
            message: 'UUID refreshed successfully',
            oldUuid,
            newUuid,
            provider: providerPools[providerType][providerIndex]
        }));
        return true;
    } catch (error) {
        res.writeHead(500, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ error: { message: error.message } }));
        return true;
    }
}