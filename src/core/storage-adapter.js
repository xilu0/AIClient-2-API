/**
 * Storage Adapter Interface
 * Defines the contract for configuration storage backends (Redis, File).
 * @module storage-adapter
 */

/**
 * @typedef {Object} Provider
 * @property {string} uuid - Unique identifier
 * @property {string} [customName] - Custom display name
 * @property {boolean} isHealthy - Health status
 * @property {boolean} isDisabled - Whether disabled
 * @property {number} usageCount - Total usage count
 * @property {number} errorCount - Total error count
 * @property {string} [lastUsed] - ISO timestamp of last use
 * @property {string} [lastErrorTime] - ISO timestamp of last error
 * @property {string} [lastHealthCheckTime] - ISO timestamp of last health check
 * @property {number} [refreshCount] - Token refresh count
 * @property {boolean} [needsRefresh] - Whether token needs refresh
 */

/**
 * @typedef {Object} TokenCredential
 * @property {string} [accessToken] - Access token
 * @property {string} [refreshToken] - Refresh token
 * @property {string} [expiresAt] - Expiry timestamp
 * @property {string} [access_token] - Gemini-style access token
 * @property {string} [refresh_token] - Gemini-style refresh token
 * @property {number} [expiry_date] - Gemini-style expiry (Unix ms)
 */

/**
 * @typedef {Object.<string, Provider[]>} ProviderPools
 */

/**
 * Abstract base class for storage adapters.
 * Implementations must provide all methods.
 */
class StorageAdapter {
    /**
     * Get the adapter type identifier
     * @returns {string} Adapter type ('redis' or 'file')
     */
    getType() {
        throw new Error('StorageAdapter.getType() must be implemented');
    }

    /**
     * Check if the storage backend is available
     * @returns {Promise<boolean>}
     */
    async isAvailable() {
        throw new Error('StorageAdapter.isAvailable() must be implemented');
    }

    // ==================== Configuration ====================

    /**
     * Get the main service configuration
     * @returns {Promise<Object>}
     */
    async getConfig() {
        throw new Error('StorageAdapter.getConfig() must be implemented');
    }

    /**
     * Set the main service configuration
     * @param {Object} config - Configuration object
     * @returns {Promise<void>}
     */
    async setConfig(config) {
        throw new Error('StorageAdapter.setConfig() must be implemented');
    }

    // ==================== Provider Pools ====================

    /**
     * Get all provider pools
     * @returns {Promise<ProviderPools>}
     */
    async getProviderPools() {
        throw new Error('StorageAdapter.getProviderPools() must be implemented');
    }

    /**
     * Get a specific provider pool by type
     * @param {string} providerType - Provider type identifier
     * @returns {Promise<Provider[]>}
     */
    async getProviderPool(providerType) {
        throw new Error('StorageAdapter.getProviderPool() must be implemented');
    }

    /**
     * Set a provider pool
     * @param {string} providerType - Provider type identifier
     * @param {Provider[]} providers - Array of providers
     * @returns {Promise<void>}
     */
    async setProviderPool(providerType, providers) {
        throw new Error('StorageAdapter.setProviderPool() must be implemented');
    }

    /**
     * Get a single provider
     * @param {string} providerType - Provider type identifier
     * @param {string} uuid - Provider UUID
     * @returns {Promise<Provider|null>}
     */
    async getProvider(providerType, uuid) {
        throw new Error('StorageAdapter.getProvider() must be implemented');
    }

    /**
     * Update a provider (partial update)
     * @param {string} providerType - Provider type identifier
     * @param {string} uuid - Provider UUID
     * @param {Partial<Provider>} updates - Fields to update
     * @returns {Promise<void>}
     */
    async updateProvider(providerType, uuid, updates) {
        throw new Error('StorageAdapter.updateProvider() must be implemented');
    }

    /**
     * Add a new provider
     * @param {string} providerType - Provider type identifier
     * @param {Provider} provider - Provider object
     * @returns {Promise<void>}
     */
    async addProvider(providerType, provider) {
        throw new Error('StorageAdapter.addProvider() must be implemented');
    }

    /**
     * Delete a provider
     * @param {string} providerType - Provider type identifier
     * @param {string} uuid - Provider UUID
     * @returns {Promise<void>}
     */
    async deleteProvider(providerType, uuid) {
        throw new Error('StorageAdapter.deleteProvider() must be implemented');
    }

    // ==================== Atomic Counter Operations ====================

    /**
     * Atomically increment usage counter
     * @param {string} providerType - Provider type identifier
     * @param {string} uuid - Provider UUID
     * @returns {Promise<number>} New usage count
     */
    async incrementUsage(providerType, uuid) {
        throw new Error('StorageAdapter.incrementUsage() must be implemented');
    }

    /**
     * Atomically increment error counter
     * @param {string} providerType - Provider type identifier
     * @param {string} uuid - Provider UUID
     * @param {boolean} [markUnhealthy=false] - Whether to also mark as unhealthy
     * @returns {Promise<number>} New error count
     */
    async incrementError(providerType, uuid, markUnhealthy = false) {
        throw new Error('StorageAdapter.incrementError() must be implemented');
    }

    /**
     * Update health status
     * @param {string} providerType - Provider type identifier
     * @param {string} uuid - Provider UUID
     * @param {boolean} isHealthy - New health status
     * @returns {Promise<void>}
     */
    async updateHealthStatus(providerType, uuid, isHealthy) {
        throw new Error('StorageAdapter.updateHealthStatus() must be implemented');
    }

    // ==================== Token Credentials ====================

    /**
     * Get token credentials for a provider
     * @param {string} providerType - Provider type identifier
     * @param {string} uuid - Provider UUID
     * @returns {Promise<TokenCredential|null>}
     */
    async getToken(providerType, uuid) {
        throw new Error('StorageAdapter.getToken() must be implemented');
    }

    /**
     * Set token credentials for a provider
     * @param {string} providerType - Provider type identifier
     * @param {string} uuid - Provider UUID
     * @param {TokenCredential} token - Token credentials
     * @param {number} [ttl] - Optional TTL in seconds
     * @returns {Promise<void>}
     */
    async setToken(providerType, uuid, token, ttl) {
        throw new Error('StorageAdapter.setToken() must be implemented');
    }

    // ==================== UI Password ====================

    /**
     * Get the UI password
     * @returns {Promise<string|null>}
     */
    async getPassword() {
        throw new Error('StorageAdapter.getPassword() must be implemented');
    }

    /**
     * Set the UI password
     * @param {string} password - New password
     * @returns {Promise<void>}
     */
    async setPassword(password) {
        throw new Error('StorageAdapter.setPassword() must be implemented');
    }

    // ==================== Session Tokens ====================

    /**
     * Get a session token
     * @param {string} tokenHash - Token hash identifier
     * @returns {Promise<Object|null>}
     */
    async getSession(tokenHash) {
        throw new Error('StorageAdapter.getSession() must be implemented');
    }

    /**
     * Set a session token with TTL
     * @param {string} tokenHash - Token hash identifier
     * @param {Object} session - Session data
     * @param {number} ttl - TTL in seconds
     * @returns {Promise<void>}
     */
    async setSession(tokenHash, session, ttl) {
        throw new Error('StorageAdapter.setSession() must be implemented');
    }

    /**
     * Delete a session token
     * @param {string} tokenHash - Token hash identifier
     * @returns {Promise<void>}
     */
    async deleteSession(tokenHash) {
        throw new Error('StorageAdapter.deleteSession() must be implemented');
    }

    // ==================== Metadata ====================

    /**
     * Get metadata
     * @returns {Promise<Object>}
     */
    async getMetadata() {
        throw new Error('StorageAdapter.getMetadata() must be implemented');
    }

    /**
     * Set metadata field
     * @param {string} field - Field name
     * @param {string} value - Field value
     * @returns {Promise<void>}
     */
    async setMetadataField(field, value) {
        throw new Error('StorageAdapter.setMetadataField() must be implemented');
    }

    // ==================== Bulk Operations ====================

    /**
     * Save all provider pools (for file adapter compatibility)
     * @param {ProviderPools} pools - All provider pools
     * @returns {Promise<void>}
     */
    async saveAllProviderPools(pools) {
        throw new Error('StorageAdapter.saveAllProviderPools() must be implemented');
    }

    /**
     * Close the storage connection
     * @returns {Promise<void>}
     */
    async close() {
        // Default implementation does nothing
    }
}

export { StorageAdapter };
export default StorageAdapter;
