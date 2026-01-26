/**
 * Storage Adapter Factory
 * Creates Redis storage adapter only. Provider pools must be stored in Redis.
 * @module storage-factory
 */

import { FileStorageAdapter } from './file-storage-adapter.js';
import redisClientManager from './redis-client.js';

/** @type {import('./storage-adapter.js').StorageAdapter|null} */
let storageAdapter = null;

/** @type {boolean} */
let initialized = false;

/**
 * Create and initialize Redis storage adapter.
 * Redis is required for provider pools storage.
 *
 * @param {Object} config - Configuration object
 * @param {Object} [config.redis] - Redis configuration
 * @param {boolean} [config.redis.enabled] - Whether Redis is enabled
 * @param {string} [config.redis.url] - Redis URL
 * @param {string} [config.configPath] - Path to config.json (for file backup only)
 * @returns {Promise<import('./storage-adapter.js').StorageAdapter>}
 */
async function createStorageAdapter(config = {}) {
    if (initialized && storageAdapter) {
        return storageAdapter;
    }

    const redisConfig = config.redis || {};

    // Redis is always enabled now (provider_pools.json removed)
    // Check config first, then environment variable
    const redisEnabled = redisConfig.enabled !== false &&
                        (redisConfig.enabled === true || process.env.REDIS_ENABLED === 'true');

    if (!redisEnabled) {
        console.error('[Storage] ❌ Redis storage is disabled but required!');
        console.error('[Storage] Provider pools require Redis storage (provider_pools.json removed)');
        console.error('[Storage] Please enable Redis in config.json or set REDIS_ENABLED=true');
        throw new Error('Redis storage is required but disabled');
    }

    console.log('[Storage] Redis storage enabled, attempting connection...');

    try {
        // Initialize Redis client
        const connected = await redisClientManager.initialize(redisConfig);

        if (!connected) {
            console.error('[Storage] ❌ Redis connection failed!');
            console.error('[Storage] Provider pools require Redis storage');
            console.error('[Storage] Please ensure Redis is running at:', redisConfig.url || `${redisConfig.host || 'localhost'}:${redisConfig.port || 6379}`);
            throw new Error('Redis connection failed');
        }

        console.log('[Storage] Redis connection established successfully');

        // Create file adapter ONLY for config.json backup (NOT for provider pools)
        const fileAdapter = new FileStorageAdapter({
            configPath: config.configPath,
            poolsPath: null, // No longer use provider_pools.json
        });

        // Dynamically import RedisConfigManager to avoid circular dependencies
        const { RedisConfigManager } = await import('./redis-config-manager.js');
        storageAdapter = new RedisConfigManager(redisClientManager, {
            keyPrefix: redisConfig.keyPrefix || redisClientManager.getKeyPrefix(),
            fileAdapter,
        });

        // Check if Redis has data (for migration hints)
        await storageAdapter.checkEmptyAndWarn();

        console.log('[Storage] Redis storage adapter initialized (config file backup enabled)');
        initialized = true;
        return storageAdapter;
    } catch (error) {
        console.error('[Storage] Failed to initialize Redis storage:', error.message);
        throw error;
    }
}

/**
 * Get the current storage adapter.
 * Throws if not initialized.
 *
 * @returns {import('./storage-adapter.js').StorageAdapter}
 */
function getStorageAdapter() {
    if (!storageAdapter) {
        throw new Error('Storage adapter not initialized. Call createStorageAdapter() first.');
    }
    return storageAdapter;
}

/**
 * Check if storage adapter is initialized.
 * @returns {boolean}
 */
function isStorageInitialized() {
    return initialized && storageAdapter !== null;
}

/**
 * Get the storage adapter type.
 * @returns {string|null} 'redis', 'file', or null if not initialized
 */
function getStorageType() {
    if (!storageAdapter) return null;
    return storageAdapter.getType();
}

/**
 * Reset the storage adapter (for testing).
 * @returns {Promise<void>}
 */
async function resetStorageAdapter() {
    if (storageAdapter) {
        await storageAdapter.close();
    }
    storageAdapter = null;
    initialized = false;
}

export {
    createStorageAdapter,
    getStorageAdapter,
    isStorageInitialized,
    getStorageType,
    resetStorageAdapter,
};
