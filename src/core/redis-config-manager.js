/**
 * Redis Configuration Manager
 * Implements StorageAdapter interface for Redis-based configuration storage.
 * @module redis-config-manager
 */

import { StorageAdapter } from './storage-adapter.js';
import { WriteQueue } from './write-queue.js';

/**
 * Redis-based storage adapter implementation.
 * Provides atomic operations for provider pools, tokens, and configuration.
 */
class RedisConfigManager extends StorageAdapter {
    /**
     * @param {import('./redis-client.js').RedisClientManager} redisClientManager - Redis client manager
     * @param {Object} options - Configuration options
     * @param {string} [options.keyPrefix='aiclient:'] - Redis key prefix
     * @param {import('./file-storage-adapter.js').FileStorageAdapter} [options.fileAdapter] - Fallback file adapter
     */
    constructor(redisClientManager, options = {}) {
        super();
        this.redisManager = redisClientManager;
        this.keyPrefix = options.keyPrefix || 'aiclient:';
        this.fileAdapter = options.fileAdapter;
        this.writeQueue = new WriteQueue({ maxSize: 1000 });

        // In-memory cache for read performance
        this._poolsCache = null;
        this._poolsCacheTime = 0;
        this._configCache = null;
        this._configCacheTime = 0;
        this._usageCacheData = null;
        this._usageCacheTime = 0;
        this._pluginsCache = null;
        this._pluginsCacheTime = 0;
        this._cacheMaxAge = 5000; // 5 seconds cache TTL

        // Degraded mode flag - when true, rely on cache exclusively
        this._degradedMode = false;
        this._degradedModeStartTime = null;

        // Define Lua scripts for atomic operations
        this._defineLuaScripts();

        // Set up connection event handlers
        this._setupConnectionHandlers();
    }

    /**
     * Define reusable Lua scripts for atomic operations
     * @private
     */
    _defineLuaScripts() {
        const client = this.redisManager.getClient();
        if (!client) return;

        // Atomic usage update: increment counter and update timestamp
        client.defineCommand('atomicUsageUpdate', {
            numberOfKeys: 1,
            lua: `
                local poolKey = KEYS[1]
                local uuid = ARGV[1]
                local timestamp = ARGV[2]
                local provider = redis.call('HGET', poolKey, uuid)
                if not provider then
                    return nil
                end
                local data = cjson.decode(provider)
                data.usageCount = (data.usageCount or 0) + 1
                data.lastUsed = timestamp
                local updated = cjson.encode(data)
                redis.call('HSET', poolKey, uuid, updated)
                return data.usageCount
            `
        });

        // Atomic error update: increment counter, update timestamp, optionally mark unhealthy
        client.defineCommand('atomicErrorUpdate', {
            numberOfKeys: 1,
            lua: `
                local poolKey = KEYS[1]
                local uuid = ARGV[1]
                local timestamp = ARGV[2]
                local markUnhealthy = ARGV[3] == 'true'
                local provider = redis.call('HGET', poolKey, uuid)
                if not provider then
                    return nil
                end
                local data = cjson.decode(provider)
                data.errorCount = (data.errorCount or 0) + 1
                data.lastErrorTime = timestamp
                if markUnhealthy then
                    data.isHealthy = false
                end
                local updated = cjson.encode(data)
                redis.call('HSET', poolKey, uuid, updated)
                return data.errorCount
            `
        });

        // Atomic health status update
        client.defineCommand('atomicHealthUpdate', {
            numberOfKeys: 1,
            lua: `
                local poolKey = KEYS[1]
                local uuid = ARGV[1]
                local isHealthy = ARGV[2] == 'true'
                local timestamp = ARGV[3]
                local provider = redis.call('HGET', poolKey, uuid)
                if not provider then
                    return nil
                end
                local data = cjson.decode(provider)
                data.isHealthy = isHealthy
                data.lastHealthCheckTime = timestamp
                local updated = cjson.encode(data)
                redis.call('HSET', poolKey, uuid, updated)
                return 1
            `
        });

        // Atomic provider update: merge updates into existing provider
        client.defineCommand('atomicProviderUpdate', {
            numberOfKeys: 1,
            lua: `
                local poolKey = KEYS[1]
                local uuid = ARGV[1]
                local updates = cjson.decode(ARGV[2])
                local provider = redis.call('HGET', poolKey, uuid)
                if not provider then
                    return nil
                end
                local data = cjson.decode(provider)
                for k, v in pairs(updates) do
                    data[k] = v
                end
                local updated = cjson.encode(data)
                redis.call('HSET', poolKey, uuid, updated)
                return 1
            `
        });

        // Atomic token update: compare-and-swap pattern to prevent concurrent refresh conflicts
        // Returns: 1 = updated, 0 = conflict (token was already refreshed), -1 = not found
        client.defineCommand('atomicTokenUpdate', {
            numberOfKeys: 1,
            lua: `
                local tokenKey = KEYS[1]
                local expectedRefreshToken = ARGV[1]
                local newTokenData = ARGV[2]
                local existingToken = redis.call('GET', tokenKey)
                if not existingToken then
                    -- Token doesn't exist, just set it
                    redis.call('SET', tokenKey, newTokenData)
                    return 1
                end
                local existing = cjson.decode(existingToken)
                -- Compare refresh tokens to detect concurrent refresh
                if expectedRefreshToken ~= '' and existing.refreshToken and existing.refreshToken ~= expectedRefreshToken then
                    -- Another process already refreshed the token
                    return 0
                end
                -- Update the token
                redis.call('SET', tokenKey, newTokenData)
                return 1
            `
        });

        // Atomic token update with TTL
        client.defineCommand('atomicTokenUpdateWithTTL', {
            numberOfKeys: 1,
            lua: `
                local tokenKey = KEYS[1]
                local expectedRefreshToken = ARGV[1]
                local newTokenData = ARGV[2]
                local ttl = tonumber(ARGV[3])
                local existingToken = redis.call('GET', tokenKey)
                if not existingToken then
                    -- Token doesn't exist, just set it
                    if ttl and ttl > 0 then
                        redis.call('SETEX', tokenKey, ttl, newTokenData)
                    else
                        redis.call('SET', tokenKey, newTokenData)
                    end
                    return 1
                end
                local existing = cjson.decode(existingToken)
                -- Compare refresh tokens to detect concurrent refresh
                if expectedRefreshToken ~= '' and existing.refreshToken and existing.refreshToken ~= expectedRefreshToken then
                    -- Another process already refreshed the token
                    return 0
                end
                -- Update the token
                if ttl and ttl > 0 then
                    redis.call('SETEX', tokenKey, ttl, newTokenData)
                else
                    redis.call('SET', tokenKey, newTokenData)
                end
                return 1
            `
        });
    }

    /**
     * Set up Redis connection event handlers
     * @private
     */
    _setupConnectionHandlers() {
        this.redisManager.onConnect(() => {
            if (this._degradedMode) {
                const degradedDuration = Date.now() - this._degradedModeStartTime;
                console.log(`[RedisConfig] Redis reconnected after ${Math.round(degradedDuration / 1000)}s in degraded mode`);
                this._degradedMode = false;
                this._degradedModeStartTime = null;
            }

            console.log('[RedisConfig] Replaying queued writes...');
            this._replayWrites();
            console.log('[RedisConfig] Resuming normal Redis operations');
        });

        this.redisManager.onDisconnect(() => {
            this._degradedMode = true;
            this._degradedModeStartTime = Date.now();

            console.warn('[RedisConfig] ⚠️  Redis connection lost - entering degraded mode');
            console.log('[RedisConfig] Read operations will use in-memory cache (no expiry)');
            console.log('[RedisConfig] Write operations will be queued for replay on reconnection');
            console.log(`[RedisConfig] ${this.writeQueue.size} writes currently queued`);
        });
    }

    /**
     * Check if operating in degraded mode (Redis disconnected)
     * @returns {boolean}
     */
    isDegradedMode() {
        return this._degradedMode;
    }

    /**
     * Get degraded mode status info
     * @returns {Object}
     */
    getDegradedModeInfo() {
        if (!this._degradedMode) {
            return { degraded: false };
        }

        return {
            degraded: true,
            since: this._degradedModeStartTime,
            duration: Date.now() - this._degradedModeStartTime,
            queuedWrites: this.writeQueue.size
        };
    }

    /**
     * Check if Redis is empty and warn about migration
     * Called during initialization to help users migrate from file storage
     */
    async checkEmptyAndWarn() {
        const client = this.redisManager.getClient();
        if (!client || !this.redisManager.isConnected()) {
            return;
        }

        try {
            // Check for any keys with our prefix
            const keys = await client.keys(`${this.keyPrefix}*`);

            if (keys.length === 0) {
                console.warn('[RedisConfig] ⚠️  Redis is connected but empty - no configuration data found');
                console.warn('[RedisConfig] If migrating from file storage, run: npm run migrate:redis');
                console.warn('[RedisConfig] Until migration, file-based configuration will be used as fallback');
            } else {
                console.log(`[RedisConfig] Found ${keys.length} configuration keys in Redis`);

                // Check for metadata to see migration info
                const meta = await client.hgetall(`${this.keyPrefix}meta`);
                if (meta && meta.migratedAt) {
                    console.log(`[RedisConfig] Data migrated at: ${meta.migratedAt}`);
                }
            }
        } catch (error) {
            console.warn('[RedisConfig] Failed to check Redis state:', error.message);
        }
    }

    /**
     * Replay queued writes when Redis reconnects
     * @private
     */
    async _replayWrites() {
        const client = this.redisManager.getClient();
        if (!client || !this.redisManager.isConnected()) {
            return;
        }

        const result = await this.writeQueue.replay(client);
        this.redisManager.setQueuedWrites(this.writeQueue.size);
        console.log(`[RedisConfig] Write replay complete: ${result.success} succeeded, ${result.failed} failed`);
    }

    /**
     * Get Redis key with prefix
     * @private
     */
    _key(suffix) {
        return `${this.keyPrefix}${suffix}`;
    }

    /**
     * Execute a Redis command with fallback to queue on failure
     * @private
     * @param {Function} command - The command to execute
     * @param {string} description - Description of the command for logging
     * @returns {Promise<{queued: boolean, result?: any}>} Object indicating if command was queued and optional result
     */
    async _execute(command, description = '') {
        const client = this.redisManager.getClient();

        if (!client || !this.redisManager.isConnected()) {
            // Queue the write operation for later
            this.writeQueue.push(async (redisClient) => {
                await command(redisClient);
            }, description);
            this.redisManager.setQueuedWrites(this.writeQueue.size);
            console.log(`[RedisConfig] Queued write: ${description}`);
            return { queued: true, result: null };
        }

        try {
            const result = await command(client);
            return { queued: false, result };
        } catch (error) {
            console.error(`[RedisConfig] Command failed: ${description} - ${error.message}`);
            // Queue for retry if it's a connection error
            if (error.message.includes('ECONNREFUSED') || error.message.includes('ENOTCONN')) {
                this.writeQueue.push(async (redisClient) => {
                    await command(redisClient);
                }, description);
                this.redisManager.setQueuedWrites(this.writeQueue.size);
            }
            throw error;
        }
    }

    getType() {
        return 'redis';
    }

    async isAvailable() {
        return this.redisManager.isConnected();
    }

    // ==================== Configuration ====================

    async getConfig() {
        // In degraded mode, always use cache without expiry
        if (this._degradedMode && this._configCache) {
            return this._configCache;
        }

        // Check cache first (with TTL in normal mode)
        if (this._configCache && Date.now() - this._configCacheTime < this._cacheMaxAge) {
            return this._configCache;
        }

        const client = this.redisManager.getClient();
        if (!client || !this.redisManager.isConnected()) {
            // Fallback to file adapter
            if (this.fileAdapter) {
                console.debug('[RedisConfig] Using file fallback for getConfig');
                return this.fileAdapter.getConfig();
            }
            return {};
        }

        try {
            const data = await client.get(this._key('config'));
            if (data) {
                this._configCache = JSON.parse(data);
                this._configCacheTime = Date.now();
                return this._configCache;
            }
            return {};
        } catch (error) {
            console.error('[RedisConfig] Failed to get config:', error.message);
            if (this.fileAdapter) {
                return this.fileAdapter.getConfig();
            }
            return {};
        }
    }

    async setConfig(config) {
        this._configCache = config;
        this._configCacheTime = Date.now();

        await this._execute(async (client) => {
            await client.set(this._key('config'), JSON.stringify(config));
        }, 'setConfig');
    }

    // ==================== Provider Pools ====================

    async getProviderPools() {
        // In degraded mode, always use cache without expiry
        if (this._degradedMode && this._poolsCache) {
            return this._poolsCache;
        }

        // Check cache first (with TTL in normal mode)
        if (this._poolsCache && Date.now() - this._poolsCacheTime < this._cacheMaxAge) {
            return this._poolsCache;
        }

        // If there are queued writes, prefer file as source of truth
        // because Redis data may be stale (writes haven't been applied yet)
        if (this.writeQueue.size > 0 && this.fileAdapter) {
            console.debug(`[RedisConfig] ${this.writeQueue.size} queued writes, using file as source of truth`);
            const pools = await this.fileAdapter.getProviderPools();
            this._poolsCache = pools;
            this._poolsCacheTime = Date.now();
            return pools;
        }

        const client = this.redisManager.getClient();
        if (!client || !this.redisManager.isConnected()) {
            if (this.fileAdapter) {
                console.debug('[RedisConfig] Using file fallback for getProviderPools');
                return this.fileAdapter.getProviderPools();
            }
            return {};
        }

        try {
            // Get all pool keys
            const keys = await client.keys(this._key('pools:*'));
            const pools = {};

            for (const key of keys) {
                const providerType = key.replace(this._key('pools:'), '');
                const providers = await client.hgetall(key);

                pools[providerType] = Object.values(providers).map(p => JSON.parse(p));
            }

            this._poolsCache = pools;
            this._poolsCacheTime = Date.now();
            return pools;
        } catch (error) {
            console.error('[RedisConfig] Failed to get provider pools:', error.message);
            if (this.fileAdapter) {
                return this.fileAdapter.getProviderPools();
            }
            return {};
        }
    }

    async getProviderPool(providerType) {
        const client = this.redisManager.getClient();
        if (!client || !this.redisManager.isConnected()) {
            if (this.fileAdapter) {
                return this.fileAdapter.getProviderPool(providerType);
            }
            return [];
        }

        try {
            const providers = await client.hgetall(this._key(`pools:${providerType}`));
            return Object.values(providers).map(p => JSON.parse(p));
        } catch (error) {
            console.error(`[RedisConfig] Failed to get provider pool ${providerType}:`, error.message);
            if (this.fileAdapter) {
                return this.fileAdapter.getProviderPool(providerType);
            }
            return [];
        }
    }

    async setProviderPool(providerType, providers) {
        // Update cache
        if (this._poolsCache) {
            this._poolsCache[providerType] = providers;
            this._poolsCacheTime = Date.now();
        }

        await this._execute(async (client) => {
            const key = this._key(`pools:${providerType}`);
            // Clear existing pool
            await client.del(key);
            // Add all providers
            if (providers.length > 0) {
                const multi = client.multi();
                for (const provider of providers) {
                    multi.hset(key, provider.uuid, JSON.stringify(provider));
                }
                await multi.exec();
            }
        }, `setProviderPool:${providerType}`);
    }

    async getProvider(providerType, uuid) {
        const client = this.redisManager.getClient();
        if (!client || !this.redisManager.isConnected()) {
            if (this.fileAdapter) {
                return this.fileAdapter.getProvider(providerType, uuid);
            }
            return null;
        }

        try {
            const data = await client.hget(this._key(`pools:${providerType}`), uuid);
            return data ? JSON.parse(data) : null;
        } catch (error) {
            console.error(`[RedisConfig] Failed to get provider ${uuid}:`, error.message);
            if (this.fileAdapter) {
                return this.fileAdapter.getProvider(providerType, uuid);
            }
            return null;
        }
    }

    /**
     * Update a provider in Redis and sync to file backup
     * @param {string} providerType - The provider type
     * @param {string} uuid - The provider UUID
     * @param {Object} updates - The fields to update
     * @returns {Promise<{queued: boolean}>} Status indicating if operation was queued
     */
    async updateProvider(providerType, uuid, updates) {
        // Invalidate cache
        this._poolsCache = null;

        let executeResult = { queued: false };
        try {
            executeResult = await this._execute(async (client) => {
                await client.atomicProviderUpdate(
                    this._key(`pools:${providerType}`),
                    uuid,
                    JSON.stringify(updates)
                );
            }, `updateProvider:${providerType}:${uuid}`);
        } catch (redisError) {
            console.error(`[RedisConfig] Redis update failed for ${uuid}: ${redisError.message}`);
            executeResult = { queued: true };
        }

        // ALWAYS sync to file backup regardless of Redis result
        if (this.fileAdapter) {
            try {
                await this.fileAdapter.updateProvider(providerType, uuid, updates);
                console.log(`[RedisConfig] Synced provider ${uuid} update to file backup`);
            } catch (err) {
                console.warn(`[RedisConfig] File backup update failed: ${err.message}`);
            }
        }

        return { queued: executeResult.queued };
    }

    /**
     * Add a provider to Redis and sync to file backup
     * @param {string} providerType - The provider type
     * @param {Object} provider - The provider configuration
     * @returns {Promise<{queued: boolean}>} Status indicating if operation was queued
     */
    async addProvider(providerType, provider) {
        // Invalidate cache
        this._poolsCache = null;

        let executeResult = { queued: false };
        try {
            executeResult = await this._execute(async (client) => {
                await client.hset(
                    this._key(`pools:${providerType}`),
                    provider.uuid,
                    JSON.stringify(provider)
                );
            }, `addProvider:${providerType}:${provider.uuid}`);
        } catch (redisError) {
            console.error(`[RedisConfig] Redis add failed for ${provider.uuid}: ${redisError.message}`);
            executeResult = { queued: true };
        }

        // ALWAYS sync to file backup regardless of Redis result
        if (this.fileAdapter) {
            try {
                await this.fileAdapter.addProvider(providerType, provider);
                console.log(`[RedisConfig] Synced provider ${provider.uuid} addition to file backup`);
            } catch (err) {
                console.warn(`[RedisConfig] File backup add failed: ${err.message}`);
            }
        }

        return { queued: executeResult.queued };
    }

    /**
     * Delete a provider from Redis and sync to file backup
     * @param {string} providerType - The provider type
     * @param {string} uuid - The provider UUID
     * @returns {Promise<{queued: boolean}>} Status indicating if deletion was queued
     */
    async deleteProvider(providerType, uuid) {
        // Invalidate cache
        this._poolsCache = null;

        let executeResult = { queued: false };
        try {
            executeResult = await this._execute(async (client) => {
                await client.hdel(this._key(`pools:${providerType}`), uuid);
            }, `deleteProvider:${providerType}:${uuid}`);
        } catch (redisError) {
            console.error(`[RedisConfig] Redis delete failed for ${uuid}: ${redisError.message}`);
            executeResult = { queued: true }; // Treat as queued since Redis failed
        }

        // ALWAYS sync to file backup regardless of Redis result
        // This ensures data consistency even if Redis fails or has queued writes
        if (this.fileAdapter) {
            try {
                await this.fileAdapter.deleteProvider(providerType, uuid);
                console.log(`[RedisConfig] Synced provider ${uuid} deletion to file backup`);
            } catch (err) {
                console.warn(`[RedisConfig] File backup delete failed: ${err.message}`);
            }
        }

        return { queued: executeResult.queued };
    }

    // ==================== Atomic Counter Operations ====================

    async incrementUsage(providerType, uuid) {
        // Invalidate cache
        this._poolsCache = null;

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

    async incrementError(providerType, uuid, markUnhealthy = false) {
        // Invalidate cache
        this._poolsCache = null;

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

    async updateHealthStatus(providerType, uuid, isHealthy) {
        // Invalidate cache
        this._poolsCache = null;

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

    // ==================== Token Credentials ====================

    async getToken(providerType, uuid) {
        const client = this.redisManager.getClient();
        if (!client || !this.redisManager.isConnected()) {
            if (this.fileAdapter) {
                return this.fileAdapter.getToken(providerType, uuid);
            }
            return null;
        }

        try {
            const data = await client.get(this._key(`tokens:${providerType}:${uuid}`));
            return data ? JSON.parse(data) : null;
        } catch (error) {
            console.error(`[RedisConfig] Failed to get token ${providerType}:${uuid}:`, error.message);
            if (this.fileAdapter) {
                return this.fileAdapter.getToken(providerType, uuid);
            }
            return null;
        }
    }

    async setToken(providerType, uuid, token, ttl) {
        await this._execute(async (client) => {
            const key = this._key(`tokens:${providerType}:${uuid}`);
            if (ttl) {
                await client.setex(key, ttl, JSON.stringify(token));
            } else {
                await client.set(key, JSON.stringify(token));
            }
        }, `setToken:${providerType}:${uuid}`);
    }

    /**
     * Atomically update a token with compare-and-swap pattern.
     * Prevents concurrent refresh conflicts by checking the refresh token.
     * @param {string} providerType - Provider type
     * @param {string} uuid - Provider UUID
     * @param {Object} token - New token data
     * @param {string} [expectedRefreshToken] - Expected current refresh token for CAS check
     * @param {number} [ttl] - Optional TTL in seconds
     * @returns {Promise<{success: boolean, conflict: boolean}>}
     */
    async atomicTokenUpdate(providerType, uuid, token, expectedRefreshToken = '', ttl = 0) {
        const client = this.redisManager.getClient();
        if (!client || !this.redisManager.isConnected()) {
            // Can't do atomic update without Redis, fall back to simple set
            await this.setToken(providerType, uuid, token, ttl);
            return { success: true, conflict: false };
        }

        try {
            const key = this._key(`tokens:${providerType}:${uuid}`);
            const tokenData = JSON.stringify(token);
            let result;

            if (ttl && ttl > 0) {
                result = await client.atomicTokenUpdateWithTTL(
                    key,
                    expectedRefreshToken || '',
                    tokenData,
                    ttl.toString()
                );
            } else {
                result = await client.atomicTokenUpdate(
                    key,
                    expectedRefreshToken || '',
                    tokenData
                );
            }

            if (result === 0) {
                console.log(`[RedisConfig] Token update conflict detected for ${providerType}:${uuid} - another process already refreshed`);
                return { success: false, conflict: true };
            }

            console.log(`[RedisConfig] Token updated atomically for ${providerType}:${uuid}`);
            return { success: true, conflict: false };
        } catch (error) {
            console.error(`[RedisConfig] Atomic token update failed for ${providerType}:${uuid}:`, error.message);
            // Fall back to simple set on error
            await this.setToken(providerType, uuid, token, ttl);
            return { success: true, conflict: false };
        }
    }

    /**
     * Get token with lock acquisition for refresh operations.
     * Returns the current token and a lock ID that should be passed to releaseTokenLock.
     * @param {string} providerType - Provider type
     * @param {string} uuid - Provider UUID
     * @param {number} [lockTimeout=30] - Lock timeout in seconds
     * @returns {Promise<{token: Object|null, lockId: string|null, alreadyLocked: boolean}>}
     */
    async getTokenWithLock(providerType, uuid, lockTimeout = 30) {
        const client = this.redisManager.getClient();
        if (!client || !this.redisManager.isConnected()) {
            // Can't lock without Redis
            const token = await this.getToken(providerType, uuid);
            return { token, lockId: null, alreadyLocked: false };
        }

        const lockKey = this._key(`token-lock:${providerType}:${uuid}`);
        const lockId = `${Date.now()}-${Math.random().toString(36).substr(2, 9)}`;

        try {
            // Try to acquire lock with NX (only set if not exists) and EX (expiry)
            const acquired = await client.set(lockKey, lockId, 'NX', 'EX', lockTimeout);

            if (!acquired) {
                // Lock already held by another process
                const token = await this.getToken(providerType, uuid);
                return { token, lockId: null, alreadyLocked: true };
            }

            // Lock acquired, get the token
            const token = await this.getToken(providerType, uuid);
            return { token, lockId, alreadyLocked: false };
        } catch (error) {
            console.error(`[RedisConfig] Failed to acquire token lock for ${providerType}:${uuid}:`, error.message);
            const token = await this.getToken(providerType, uuid);
            return { token, lockId: null, alreadyLocked: false };
        }
    }

    /**
     * Release a token lock acquired by getTokenWithLock.
     * @param {string} providerType - Provider type
     * @param {string} uuid - Provider UUID
     * @param {string} lockId - Lock ID returned by getTokenWithLock
     * @returns {Promise<boolean>} Whether the lock was released
     */
    async releaseTokenLock(providerType, uuid, lockId) {
        if (!lockId) return true;

        const client = this.redisManager.getClient();
        if (!client || !this.redisManager.isConnected()) {
            return true;
        }

        const lockKey = this._key(`token-lock:${providerType}:${uuid}`);

        try {
            // Only release if we own the lock (compare lockId)
            const currentLockId = await client.get(lockKey);
            if (currentLockId === lockId) {
                await client.del(lockKey);
                return true;
            }
            return false;
        } catch (error) {
            console.error(`[RedisConfig] Failed to release token lock for ${providerType}:${uuid}:`, error.message);
            return false;
        }
    }

    // ==================== UI Password ====================

    async getPassword() {
        const client = this.redisManager.getClient();
        if (!client || !this.redisManager.isConnected()) {
            if (this.fileAdapter) {
                return this.fileAdapter.getPassword();
            }
            return null;
        }

        try {
            return await client.get(this._key('pwd'));
        } catch (error) {
            console.error('[RedisConfig] Failed to get password:', error.message);
            if (this.fileAdapter) {
                return this.fileAdapter.getPassword();
            }
            return null;
        }
    }

    async setPassword(password) {
        await this._execute(async (client) => {
            await client.set(this._key('pwd'), password);
        }, 'setPassword');
    }

    // ==================== Session Tokens ====================

    async getSession(tokenHash) {
        const client = this.redisManager.getClient();
        if (!client || !this.redisManager.isConnected()) {
            if (this.fileAdapter) {
                return this.fileAdapter.getSession(tokenHash);
            }
            return null;
        }

        try {
            const data = await client.get(this._key(`sessions:${tokenHash}`));
            return data ? JSON.parse(data) : null;
        } catch (error) {
            console.error(`[RedisConfig] Failed to get session ${tokenHash}:`, error.message);
            if (this.fileAdapter) {
                return this.fileAdapter.getSession(tokenHash);
            }
            return null;
        }
    }

    async setSession(tokenHash, session, ttl) {
        await this._execute(async (client) => {
            await client.setex(
                this._key(`sessions:${tokenHash}`),
                ttl,
                JSON.stringify(session)
            );
        }, `setSession:${tokenHash}`);
    }

    async deleteSession(tokenHash) {
        await this._execute(async (client) => {
            await client.del(this._key(`sessions:${tokenHash}`));
        }, `deleteSession:${tokenHash}`);
    }

    // ==================== Metadata ====================

    async getMetadata() {
        const client = this.redisManager.getClient();
        if (!client || !this.redisManager.isConnected()) {
            return { version: '1.0', storageType: 'redis' };
        }

        try {
            const data = await client.hgetall(this._key('meta'));
            return {
                version: data.version || '1.0',
                migratedAt: data.migratedAt,
                migratedFrom: data.migratedFrom,
                storageType: 'redis'
            };
        } catch (error) {
            console.error('[RedisConfig] Failed to get metadata:', error.message);
            return { version: '1.0', storageType: 'redis' };
        }
    }

    async setMetadataField(field, value) {
        await this._execute(async (client) => {
            await client.hset(this._key('meta'), field, value);
        }, `setMetadataField:${field}`);
    }

    // ==================== Bulk Operations ====================

    async saveAllProviderPools(pools) {
        // Update cache
        this._poolsCache = pools;
        this._poolsCacheTime = Date.now();

        await this._execute(async (client) => {
            const multi = client.multi();

            for (const [providerType, providers] of Object.entries(pools)) {
                const key = this._key(`pools:${providerType}`);
                multi.del(key);
                if (providers.length > 0) {
                    for (const provider of providers) {
                        multi.hset(key, provider.uuid, JSON.stringify(provider));
                    }
                }
            }

            await multi.exec();
        }, 'saveAllProviderPools');
    }

    /**
     * Get the write queue stats
     * @returns {Object}
     */
    getQueueStats() {
        return this.writeQueue.getStats();
    }

    /**
     * Invalidate all caches
     */
    invalidateCache() {
        this._poolsCache = null;
        this._poolsCacheTime = 0;
        this._configCache = null;
        this._configCacheTime = 0;
        this._usageCacheData = null;
        this._usageCacheTime = 0;
        this._pluginsCache = null;
        this._pluginsCacheTime = 0;
    }

    // ==================== Session Token Methods (US6) ====================

    /**
     * Get a session token by hash
     * @param {string} hash - The token hash
     * @returns {Promise<Object|null>}
     */
    async getSessionToken(hash) {
        return this._execute(async (client) => {
            const key = this._key(`sessions:${hash}`);
            const data = await client.get(key);
            return data ? JSON.parse(data) : null;
        }, 'getSessionToken');
    }

    /**
     * Set a session token with TTL
     * @param {string} hash - The token hash
     * @param {Object} data - Session data (username, loginTime, expiryTime)
     * @param {number} [ttlSeconds] - TTL in seconds (default: 1 hour)
     * @returns {Promise<void>}
     */
    async setSessionToken(hash, data, ttlSeconds = 3600) {
        return this._execute(async (client) => {
            const key = this._key(`sessions:${hash}`);
            await client.setex(key, ttlSeconds, JSON.stringify(data));
        }, 'setSessionToken');
    }

    /**
     * Delete a session token
     * @param {string} hash - The token hash
     * @returns {Promise<void>}
     */
    async deleteSessionToken(hash) {
        return this._execute(async (client) => {
            const key = this._key(`sessions:${hash}`);
            await client.del(key);
        }, 'deleteSessionToken');
    }

    /**
     * Get all session tokens (for migration/export)
     * @returns {Promise<Object>} - { tokens: { hash: data, ... } }
     */
    async getAllSessionTokens() {
        return this._execute(async (client) => {
            const pattern = this._key('sessions:*');
            const keys = await client.keys(pattern);
            const tokens = {};

            for (const key of keys) {
                const hash = key.replace(this._key('sessions:'), '');
                const data = await client.get(key);
                if (data) {
                    tokens[hash] = JSON.parse(data);
                }
            }

            return { tokens };
        }, 'getAllSessionTokens');
    }

    /**
     * Clean expired sessions (Redis handles this via TTL, but useful for manual cleanup)
     * @returns {Promise<number>} - Number of expired sessions removed
     */
    async cleanExpiredSessions() {
        return this._execute(async (client) => {
            const pattern = this._key('sessions:*');
            const keys = await client.keys(pattern);
            const now = Date.now();
            let removed = 0;

            for (const key of keys) {
                const data = await client.get(key);
                if (data) {
                    const session = JSON.parse(data);
                    if (session.expiryTime && session.expiryTime < now) {
                        await client.del(key);
                        removed++;
                    }
                }
            }

            return removed;
        }, 'cleanExpiredSessions');
    }

    // ==================== Usage Cache Methods (US7) ====================

    /**
     * Get the full usage cache
     * @returns {Promise<Object|null>}
     */
    async getUsageCache() {
        // Check in-memory cache first
        if (this._usageCacheData && (Date.now() - this._usageCacheTime) < 30000) {
            return this._usageCacheData;
        }

        return this._execute(async (client) => {
            const key = this._key('usage:cache');
            const data = await client.get(key);
            if (data) {
                this._usageCacheData = JSON.parse(data);
                this._usageCacheTime = Date.now();
                return this._usageCacheData;
            }
            return null;
        }, 'getUsageCache');
    }

    /**
     * Set the full usage cache
     * @param {Object} data - Usage cache data
     * @returns {Promise<void>}
     */
    async setUsageCache(data) {
        this._usageCacheData = data;
        this._usageCacheTime = Date.now();

        return this._execute(async (client) => {
            const key = this._key('usage:cache');
            await client.set(key, JSON.stringify(data));
        }, 'setUsageCache');
    }

    /**
     * Get usage cache for a specific provider type
     * @param {string} providerType
     * @returns {Promise<Object|null>}
     */
    async getProviderUsageCache(providerType) {
        const cache = await this.getUsageCache();
        if (cache && cache.providers && cache.providers[providerType]) {
            return cache.providers[providerType];
        }
        return null;
    }

    /**
     * Update usage cache for a specific provider type
     * @param {string} providerType
     * @param {Object} data - Provider usage data
     * @returns {Promise<void>}
     */
    async updateProviderUsageCache(providerType, data) {
        let cache = await this.getUsageCache() || { timestamp: new Date().toISOString(), providers: {} };
        cache.providers[providerType] = data;
        cache.timestamp = new Date().toISOString();
        await this.setUsageCache(cache);
    }

    // ==================== Plugin Configuration Methods (US8) ====================

    /**
     * Get plugin configuration
     * @returns {Promise<Object>}
     */
    async getPlugins() {
        // Check in-memory cache first
        if (this._pluginsCache && (Date.now() - this._pluginsCacheTime) < 60000) {
            return this._pluginsCache;
        }

        return this._execute(async (client) => {
            const key = this._key('plugins');
            const data = await client.get(key);
            if (data) {
                this._pluginsCache = JSON.parse(data);
                this._pluginsCacheTime = Date.now();
                return this._pluginsCache;
            }
            // Return default structure if not found
            return { plugins: {} };
        }, 'getPlugins');
    }

    /**
     * Set plugin configuration
     * @param {Object} config - Plugin configuration
     * @returns {Promise<void>}
     */
    async setPlugins(config) {
        this._pluginsCache = config;
        this._pluginsCacheTime = Date.now();

        return this._execute(async (client) => {
            const key = this._key('plugins');
            await client.set(key, JSON.stringify(config));
        }, 'setPlugins');
    }

    /**
     * Get a specific plugin configuration
     * @param {string} name - Plugin name
     * @returns {Promise<Object|null>}
     */
    async getPlugin(name) {
        const config = await this.getPlugins();
        return config.plugins?.[name] || null;
    }

    /**
     * Update a specific plugin
     * @param {string} name - Plugin name
     * @param {Object} pluginConfig - Plugin configuration (enabled, description, etc.)
     * @returns {Promise<void>}
     */
    async updatePlugin(name, pluginConfig) {
        const config = await this.getPlugins();
        if (!config.plugins) {
            config.plugins = {};
        }
        config.plugins[name] = { ...config.plugins[name], ...pluginConfig };
        await this.setPlugins(config);
    }

    async close() {
        // Flush any pending writes
        if (this.writeQueue.size > 0) {
            console.log(`[RedisConfig] ${this.writeQueue.size} writes still queued at shutdown`);
        }
    }
}

export { RedisConfigManager };
export default RedisConfigManager;
