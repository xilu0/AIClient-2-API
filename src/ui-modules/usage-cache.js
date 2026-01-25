import { existsSync } from 'fs';
import { promises as fs } from 'fs';
import path from 'path';
import { getStorageAdapter, isStorageInitialized } from '../core/storage-factory.js';

// 用量缓存文件路径 (fallback)
const USAGE_CACHE_FILE = path.join(process.cwd(), 'configs', 'usage-cache.json');

/**
 * Check if Redis storage is available
 */
function isRedisAvailable() {
    if (!isStorageInitialized()) return false;
    try {
        const adapter = getStorageAdapter();
        return adapter.getType() === 'redis';
    } catch {
        return false;
    }
}

/**
 * 读取用量缓存文件 (fallback)
 * @returns {Promise<Object|null>} 缓存的用量数据，如果不存在或读取失败则返回 null
 */
async function readUsageCacheFromFile() {
    try {
        if (existsSync(USAGE_CACHE_FILE)) {
            const content = await fs.readFile(USAGE_CACHE_FILE, 'utf8');
            return JSON.parse(content);
        }
        return null;
    } catch (error) {
        console.warn('[Usage Cache] Failed to read usage cache file:', error.message);
        return null;
    }
}

/**
 * 写入用量缓存文件 (fallback)
 * @param {Object} usageData - 用量数据
 */
async function writeUsageCacheToFile(usageData) {
    try {
        await fs.writeFile(USAGE_CACHE_FILE, JSON.stringify(usageData, null, 2), 'utf8');
        console.log('[Usage Cache] Usage data cached to', USAGE_CACHE_FILE);
    } catch (error) {
        console.error('[Usage Cache] Failed to write usage cache file:', error.message);
    }
}

/**
 * 读取用量缓存
 * @returns {Promise<Object|null>} 缓存的用量数据，如果不存在或读取失败则返回 null
 */
export async function readUsageCache() {
    // Try Redis first
    if (isRedisAvailable()) {
        try {
            const adapter = getStorageAdapter();
            const cache = await adapter.getUsageCache();
            if (cache) {
                return cache;
            }
        } catch (error) {
            console.warn('[Usage Cache] Redis error, falling back to file:', error.message);
        }
    }

    // Fallback to file storage
    return readUsageCacheFromFile();
}

/**
 * 写入用量缓存
 * @param {Object} usageData - 用量数据
 */
export async function writeUsageCache(usageData) {
    // Try Redis first
    if (isRedisAvailable()) {
        try {
            const adapter = getStorageAdapter();
            await adapter.setUsageCache(usageData);
            console.log('[Usage Cache] Usage data cached to Redis');
            return;
        } catch (error) {
            console.warn('[Usage Cache] Redis error, falling back to file:', error.message);
        }
    }

    // Fallback to file storage
    await writeUsageCacheToFile(usageData);
}

/**
 * 读取特定提供商类型的用量缓存
 * @param {string} providerType - 提供商类型
 * @returns {Promise<Object|null>} 缓存的用量数据
 */
export async function readProviderUsageCache(providerType) {
    const cache = await readUsageCache();
    if (cache && cache.providers && cache.providers[providerType]) {
        return {
            ...cache.providers[providerType],
            cachedAt: cache.timestamp,
            fromCache: true
        };
    }
    return null;
}

/**
 * 更新特定提供商类型的用量缓存
 * @param {string} providerType - 提供商类型
 * @param {Object} usageData - 用量数据
 */
export async function updateProviderUsageCache(providerType, usageData) {
    let cache = await readUsageCache();
    if (!cache) {
        cache = {
            timestamp: new Date().toISOString(),
            providers: {}
        };
    }
    cache.providers[providerType] = usageData;
    cache.timestamp = new Date().toISOString();
    await writeUsageCache(cache);
}
