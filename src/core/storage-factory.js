/**
 * Storage Adapter Factory
 * Creates the appropriate storage adapter based on configuration.
 * @module storage-factory
 */

import { FileStorageAdapter } from './file-storage-adapter.js';
import redisClientManager from './redis-client.js';

/** @type {import('./storage-adapter.js').StorageAdapter|null} */
let storageAdapter = null;

/** @type {boolean} */
let initialized = false;

/**
 * Create and initialize the storage adapter based on configuration.
 * Uses Redis if enabled and connected, falls back to file storage.
 *
 * @param {Object} config - Configuration object
 * @param {Object} [config.redis] - Redis configuration
 * @param {boolean} [config.redis.enabled] - Whether Redis is enabled
 * @param {string} [config.redis.url] - Redis URL
 * @param {string} [config.configPath] - Path to config.json
 * @param {string} [config.poolsPath] - Path to provider_pools.json
 * @returns {Promise<import('./storage-adapter.js').StorageAdapter>}
 */
async function createStorageAdapter(config = {}) {
    if (initialized && storageAdapter) {
        return storageAdapter;
    }

    const redisConfig = config.redis || {};

    // Check if Redis is enabled via config or environment
    const redisEnabled = redisConfig.enabled || process.env.REDIS_ENABLED === 'true';

    if (redisEnabled) {
        console.log('[Storage] Redis storage enabled, attempting connection...');

        try {
            // Initialize Redis client
            const connected = await redisClientManager.initialize(redisConfig);

            if (connected) {
                console.log('[Storage] Redis connection established successfully');

                // Create file adapter for fallback operations
                const fileAdapter = new FileStorageAdapter({
                    configPath: config.configPath,
                    poolsPath: config.poolsPath,
                });

                // Dynamically import RedisConfigManager to avoid circular dependencies
                const { RedisConfigManager } = await import('./redis-config-manager.js');
                storageAdapter = new RedisConfigManager(redisClientManager, {
                    keyPrefix: redisConfig.keyPrefix || redisClientManager.getKeyPrefix(),
                    fileAdapter,
                });

                // Check if Redis has data (for migration hints)
                await storageAdapter.checkEmptyAndWarn();

                console.log('[Storage] Redis storage adapter initialized with file fallback');
                initialized = true;
                return storageAdapter;
            } else {
                console.warn('[Storage] Redis connection failed - Redis may be unavailable');
            }
        } catch (error) {
            console.error('[Storage] Failed to initialize Redis storage:', error.message);
            console.warn('[Storage] Service will continue with file-based storage');
        }

        console.log('[Storage] Falling back to file storage (Redis unavailable)...');
    } else {
        console.log('[Storage] Redis storage disabled, using file storage');
    }

    // Fall back to file storage
    storageAdapter = new FileStorageAdapter({
        configPath: config.configPath,
        poolsPath: config.poolsPath,
    });
    console.log('[Storage] File storage adapter initialized');
    initialized = true;
    return storageAdapter;
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
