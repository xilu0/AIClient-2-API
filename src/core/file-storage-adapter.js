/**
 * File Storage Adapter
 * Wraps existing file I/O logic for configuration storage.
 * Provides backward compatibility when Redis is not available.
 * @module file-storage-adapter
 */

import * as fs from 'fs';
import { promises as pfs } from 'fs';
import * as path from 'path';
import { StorageAdapter } from './storage-adapter.js';

/**
 * File-based storage adapter implementation.
 * Uses the existing file paths and formats from the project.
 */
class FileStorageAdapter extends StorageAdapter {
    /**
     * @param {Object} options - Configuration options
     * @param {string} [options.configPath='configs/config.json'] - Path to config.json
     * @param {string|null} [options.poolsPath=null] - Path to provider_pools.json (null for Redis-only mode)
     * @param {string} [options.pwdPath='configs/pwd'] - Path to password file
     * @param {string} [options.tokenStorePath='configs/token-store.json'] - Path to session token store
     */
    constructor(options = {}) {
        super();
        this.configPath = options.configPath || 'configs/config.json';
        // Default to null for poolsPath - Redis-only mode by default
        this.poolsPath = options.poolsPath !== undefined ? options.poolsPath : null;
        this.pwdPath = options.pwdPath || 'configs/pwd';
        this.tokenStorePath = options.tokenStorePath || 'configs/token-store.json';

        // In-memory cache for provider pools
        this._poolsCache = null;
        this._configCache = null;

        // Debounce timer for saving pools
        this._saveTimer = null;
        this._saveDebounceTime = options.saveDebounceTime || 1000;
        this._pendingSave = false;

        // File write lock
        this._writeLock = false;
        this._writeQueue = [];
    }

    getType() {
        return 'file';
    }

    async isAvailable() {
        // File storage is always available
        return true;
    }

    // ==================== Configuration ====================

    async getConfig() {
        if (this._configCache) {
            return this._configCache;
        }

        try {
            const data = await pfs.readFile(this.configPath, 'utf8');
            this._configCache = JSON.parse(data);
            return this._configCache;
        } catch (error) {
            if (error.code === 'ENOENT') {
                return {};
            }
            throw error;
        }
    }

    async setConfig(config) {
        this._configCache = config;
        await this._atomicWrite(this.configPath, JSON.stringify(config, null, 2));
    }

    // ==================== Provider Pools ====================

    async getProviderPools() {
        // Provider pools no longer stored in files when poolsPath is null
        // (Redis-only mode with file backup disabled for pools)
        if (!this.poolsPath) {
            return {};
        }

        if (this._poolsCache) {
            return this._poolsCache;
        }

        try {
            const data = await pfs.readFile(this.poolsPath, 'utf8');
            this._poolsCache = JSON.parse(data);
            return this._poolsCache;
        } catch (error) {
            if (error.code === 'ENOENT') {
                this._poolsCache = {};
                return {};
            }
            throw error;
        }
    }

    async getProviderPool(providerType) {
        const pools = await this.getProviderPools();
        return pools[providerType] || [];
    }

    async setProviderPool(providerType, providers) {
        // Do nothing when poolsPath is null (Redis-only mode)
        if (!this.poolsPath) {
            return;
        }

        const pools = await this.getProviderPools();
        pools[providerType] = providers;
        this._poolsCache = pools;
        await this._debouncedSavePools();
    }

    async getProvider(providerType, uuid) {
        const pool = await this.getProviderPool(providerType);
        return pool.find(p => p.uuid === uuid) || null;
    }

    async updateProvider(providerType, uuid, updates) {
        // Do nothing when poolsPath is null (Redis-only mode)
        if (!this.poolsPath) {
            return;
        }

        const pools = await this.getProviderPools();
        const pool = pools[providerType] || [];
        const index = pool.findIndex(p => p.uuid === uuid);

        if (index === -1) {
            throw new Error(`Provider ${uuid} not found in ${providerType}`);
        }

        pool[index] = { ...pool[index], ...updates };
        pools[providerType] = pool;
        this._poolsCache = pools;
        await this._debouncedSavePools();
    }

    async addProvider(providerType, provider) {
        // Do nothing when poolsPath is null (Redis-only mode)
        if (!this.poolsPath) {
            return;
        }

        const pools = await this.getProviderPools();
        if (!pools[providerType]) {
            pools[providerType] = [];
        }
        pools[providerType].push(provider);
        this._poolsCache = pools;
        // Use immediate save for add operations to ensure data consistency
        await this._forceSavePools();
    }

    async deleteProvider(providerType, uuid) {
        // Do nothing when poolsPath is null (Redis-only mode)
        if (!this.poolsPath) {
            return;
        }

        const pools = await this.getProviderPools();
        const pool = pools[providerType] || [];
        pools[providerType] = pool.filter(p => p.uuid !== uuid);
        // Clean up empty provider type
        if (pools[providerType].length === 0) {
            delete pools[providerType];
        }
        this._poolsCache = pools;
        // Use immediate save for delete operations to ensure data consistency
        await this._forceSavePools();
    }

    // ==================== Atomic Counter Operations ====================

    async incrementUsage(providerType, uuid) {
        // Do nothing when poolsPath is null (Redis-only mode)
        if (!this.poolsPath) {
            return 0;
        }

        const pools = await this.getProviderPools();
        const pool = pools[providerType] || [];
        const provider = pool.find(p => p.uuid === uuid);

        if (!provider) {
            throw new Error(`Provider ${uuid} not found in ${providerType}`);
        }

        provider.usageCount = (provider.usageCount || 0) + 1;
        provider.lastUsed = new Date().toISOString();
        this._poolsCache = pools;
        await this._debouncedSavePools();
        return provider.usageCount;
    }

    async incrementError(providerType, uuid, markUnhealthy = false) {
        // Do nothing when poolsPath is null (Redis-only mode)
        if (!this.poolsPath) {
            return 0;
        }

        const pools = await this.getProviderPools();
        const pool = pools[providerType] || [];
        const provider = pool.find(p => p.uuid === uuid);

        if (!provider) {
            throw new Error(`Provider ${uuid} not found in ${providerType}`);
        }

        provider.errorCount = (provider.errorCount || 0) + 1;
        provider.lastErrorTime = new Date().toISOString();
        if (markUnhealthy) {
            provider.isHealthy = false;
        }
        this._poolsCache = pools;
        await this._debouncedSavePools();
        return provider.errorCount;
    }

    async updateHealthStatus(providerType, uuid, isHealthy) {
        await this.updateProvider(providerType, uuid, {
            isHealthy,
            lastHealthCheckTime: new Date().toISOString()
        });
    }

    // ==================== Token Credentials ====================

    async getToken(providerType, uuid) {
        // Token path is stored in the provider config
        const provider = await this.getProvider(providerType, uuid);
        if (!provider) return null;

        // Determine token file path based on provider type
        const tokenPath = this._getTokenPath(providerType, provider);
        if (!tokenPath) return null;

        try {
            const data = await pfs.readFile(tokenPath, 'utf8');
            return JSON.parse(data);
        } catch (error) {
            if (error.code === 'ENOENT') {
                return null;
            }
            throw error;
        }
    }

    async setToken(providerType, uuid, token, ttl) {
        const provider = await this.getProvider(providerType, uuid);
        if (!provider) {
            throw new Error(`Provider ${uuid} not found in ${providerType}`);
        }

        const tokenPath = this._getTokenPath(providerType, provider);
        if (!tokenPath) {
            throw new Error(`No token path configured for ${providerType}/${uuid}`);
        }

        // Ensure directory exists
        const dir = path.dirname(tokenPath);
        await pfs.mkdir(dir, { recursive: true });

        await this._atomicWrite(tokenPath, JSON.stringify(token, null, 2));
    }

    /**
     * Get token file path for a provider
     * @private
     */
    _getTokenPath(providerType, provider) {
        if (providerType.startsWith('claude-kiro')) {
            return provider.KIRO_OAUTH_CREDS_FILE_PATH;
        } else if (providerType.startsWith('gemini-cli')) {
            return provider.GEMINI_OAUTH_CREDS_FILE_PATH;
        } else if (providerType.startsWith('gemini-antigravity')) {
            return provider.ANTIGRAVITY_OAUTH_CREDS_FILE_PATH;
        } else if (providerType.startsWith('openai-qwen')) {
            return provider.QWEN_OAUTH_CREDS_FILE_PATH;
        } else if (providerType.startsWith('openai-iflow')) {
            return provider.IFLOW_OAUTH_CREDS_FILE_PATH;
        } else if (providerType.startsWith('openai-codex')) {
            return provider.CODEX_OAUTH_CREDS_FILE_PATH;
        }
        return null;
    }

    // ==================== UI Password ====================

    async getPassword() {
        try {
            const data = await pfs.readFile(this.pwdPath, 'utf8');
            return data.trim();
        } catch (error) {
            if (error.code === 'ENOENT') {
                return null;
            }
            throw error;
        }
    }

    async setPassword(password) {
        await this._atomicWrite(this.pwdPath, password);
    }

    // ==================== Session Tokens ====================

    async getSession(tokenHash) {
        try {
            const data = await pfs.readFile(this.tokenStorePath, 'utf8');
            const store = JSON.parse(data);
            const session = store[tokenHash];
            if (!session) return null;

            // Check expiry
            if (session.expiresAt && new Date(session.expiresAt) < new Date()) {
                await this.deleteSession(tokenHash);
                return null;
            }
            return session;
        } catch (error) {
            if (error.code === 'ENOENT') {
                return null;
            }
            throw error;
        }
    }

    async setSession(tokenHash, session, ttl) {
        let store = {};
        try {
            const data = await pfs.readFile(this.tokenStorePath, 'utf8');
            store = JSON.parse(data);
        } catch (error) {
            if (error.code !== 'ENOENT') {
                throw error;
            }
        }

        // Add expiry time
        session.expiresAt = new Date(Date.now() + ttl * 1000).toISOString();
        store[tokenHash] = session;

        await this._atomicWrite(this.tokenStorePath, JSON.stringify(store, null, 2));
    }

    async deleteSession(tokenHash) {
        try {
            const data = await pfs.readFile(this.tokenStorePath, 'utf8');
            const store = JSON.parse(data);
            delete store[tokenHash];
            await this._atomicWrite(this.tokenStorePath, JSON.stringify(store, null, 2));
        } catch (error) {
            if (error.code !== 'ENOENT') {
                throw error;
            }
        }
    }

    // ==================== Metadata ====================

    async getMetadata() {
        // File adapter doesn't have separate metadata
        return {
            version: '1.0',
            storageType: 'file'
        };
    }

    async setMetadataField(field, value) {
        // No-op for file adapter
    }

    // ==================== Bulk Operations ====================

    async saveAllProviderPools(pools) {
        // Do nothing when poolsPath is null (Redis-only mode)
        if (!this.poolsPath) {
            return;
        }

        this._poolsCache = pools;
        await this._forceSavePools();
    }

    // ==================== Internal Helpers ====================

    /**
     * Debounced save for provider pools
     * @private
     */
    async _debouncedSavePools() {
        if (this._saveTimer) {
            clearTimeout(this._saveTimer);
        }

        this._pendingSave = true;

        return new Promise((resolve) => {
            this._saveTimer = setTimeout(async () => {
                await this._forceSavePools();
                resolve();
            }, this._saveDebounceTime);
        });
    }

    /**
     * Force immediate save of provider pools
     * @private
     */
    async _forceSavePools() {
        // Do nothing when poolsPath is null (Redis-only mode)
        if (!this.poolsPath || !this._poolsCache) return;

        if (this._saveTimer) {
            clearTimeout(this._saveTimer);
            this._saveTimer = null;
        }

        this._pendingSave = false;
        await this._atomicWrite(this.poolsPath, JSON.stringify(this._poolsCache, null, 2));
    }

    /**
     * Atomic file write using temp file + rename
     * @private
     */
    async _atomicWrite(filePath, content) {
        // Queue the write if lock is held
        if (this._writeLock) {
            return new Promise((resolve, reject) => {
                this._writeQueue.push({ filePath, content, resolve, reject });
            });
        }

        this._writeLock = true;

        try {
            const tempPath = `${filePath}.tmp.${Date.now()}`;
            await pfs.writeFile(tempPath, content, 'utf8');
            await pfs.rename(tempPath, filePath);
        } finally {
            this._writeLock = false;

            // Process queued writes
            if (this._writeQueue.length > 0) {
                const next = this._writeQueue.shift();
                this._atomicWrite(next.filePath, next.content)
                    .then(next.resolve)
                    .catch(next.reject);
            }
        }
    }

    /**
     * Invalidate caches (for testing or forced reload)
     */
    invalidateCache() {
        this._poolsCache = null;
        this._configCache = null;
    }

    async close() {
        // Flush any pending writes
        if (this._pendingSave) {
            await this._forceSavePools();
        }
    }
}

export { FileStorageAdapter };
export default FileStorageAdapter;
