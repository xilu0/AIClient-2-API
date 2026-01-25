/**
 * Redis Client Manager
 * Handles Redis connection with auto-reconnect, connection events, and health checks.
 * @module redis-client
 */

import Redis from 'ioredis';

/**
 * @typedef {Object} RedisClientOptions
 * @property {boolean} [enabled=false] - Whether Redis is enabled
 * @property {string} [url] - Full Redis URL (overrides host/port)
 * @property {string} [host='localhost'] - Redis host
 * @property {number} [port=6379] - Redis port
 * @property {string} [password] - Redis password
 * @property {number} [db=0] - Redis database number
 * @property {string} [keyPrefix='aiclient:'] - Key prefix for all Redis operations
 * @property {number} [connectTimeout=5000] - Connection timeout in ms
 * @property {number} [commandTimeout=1000] - Command timeout in ms
 */

/**
 * @typedef {Object} ConnectionStatus
 * @property {boolean} connected - Whether currently connected
 * @property {string|null} lastConnectedAt - ISO timestamp of last connection
 * @property {string|null} lastErrorAt - ISO timestamp of last error
 * @property {string|null} lastError - Last error message
 * @property {number} queuedWrites - Number of pending writes in queue
 */

class RedisClientManager {
    constructor() {
        /** @type {Redis|null} */
        this.client = null;
        /** @type {boolean} */
        this.connected = false;
        /** @type {string|null} */
        this.lastConnectedAt = null;
        /** @type {string|null} */
        this.lastErrorAt = null;
        /** @type {string|null} */
        this.lastError = null;
        /** @type {RedisClientOptions} */
        this.options = {};
        /** @type {number} */
        this.queuedWrites = 0;
        /** @type {Function[]} */
        this.connectionListeners = [];
        /** @type {Function[]} */
        this.disconnectionListeners = [];

        // Operational monitoring stats
        /** @type {number} */
        this.reconnectCount = 0;
        /** @type {string|null} */
        this.initialConnectionAt = null;
        /** @type {number} */
        this.totalCommands = 0;
        /** @type {number} */
        this.failedCommands = 0;
    }

    /**
     * Initialize Redis client with configuration
     * @param {RedisClientOptions} options - Redis configuration options
     * @returns {Promise<boolean>} Whether connection was successful
     */
    async initialize(options = {}) {
        this.options = {
            enabled: options.enabled ?? false,
            url: options.url || process.env.REDIS_URL,
            host: options.host || process.env.REDIS_HOST || 'localhost',
            port: parseInt(options.port || process.env.REDIS_PORT || '6379', 10),
            password: options.password || process.env.REDIS_PASSWORD || undefined,
            db: parseInt(options.db || process.env.REDIS_DB || '0', 10),
            keyPrefix: options.keyPrefix || 'aiclient:',
            connectTimeout: options.connectTimeout || 5000,
            commandTimeout: options.commandTimeout || 1000,
        };

        // Check if Redis is enabled via environment variable
        if (process.env.REDIS_ENABLED !== undefined) {
            this.options.enabled = process.env.REDIS_ENABLED.toLowerCase() === 'true';
        }

        if (!this.options.enabled) {
            console.log('[Redis] Redis storage is disabled');
            return false;
        }

        try {
            await this._connect();
            return true;
        } catch (error) {
            console.error('[Redis] Failed to initialize Redis client:', error.message);
            this.lastError = error.message;
            this.lastErrorAt = new Date().toISOString();
            return false;
        }
    }

    /**
     * Internal connection logic
     * @private
     */
    async _connect() {
        const redisOptions = {
            retryDelayOnFailover: 100,
            retryDelayOnClusterDown: 100,
            maxRetriesPerRequest: 3,
            connectTimeout: this.options.connectTimeout,
            commandTimeout: this.options.commandTimeout,
            lazyConnect: true,
            enableReadyCheck: true,
            showFriendlyErrorStack: true,
        };

        // Add password if provided
        if (this.options.password) {
            redisOptions.password = this.options.password;
        }

        // Add db if not 0
        if (this.options.db !== 0) {
            redisOptions.db = this.options.db;
        }

        // Use URL if provided, otherwise use host/port
        if (this.options.url) {
            this.client = new Redis(this.options.url, redisOptions);
        } else {
            redisOptions.host = this.options.host;
            redisOptions.port = this.options.port;
            this.client = new Redis(redisOptions);
        }

        // Set up event handlers
        this._setupEventHandlers();

        // Attempt to connect
        await this.client.connect();

        // Verify connection with PING
        await this.client.ping();

        this.connected = true;
        this.lastConnectedAt = new Date().toISOString();
        console.log(`[Redis] Connected to Redis at ${this.options.url || `${this.options.host}:${this.options.port}`}`);
    }

    /**
     * Set up Redis event handlers
     * @private
     */
    _setupEventHandlers() {
        this.client.on('connect', () => {
            console.log('[Redis] Connection established');
        });

        this.client.on('ready', () => {
            const wasConnected = this.connected;
            this.connected = true;
            this.lastConnectedAt = new Date().toISOString();
            if (!this.initialConnectionAt) {
                this.initialConnectionAt = this.lastConnectedAt;
                console.log('[Redis] ✓ Initial connection ready');
            } else if (!wasConnected) {
                this.reconnectCount++;
                console.log(`[Redis] ✓ Reconnected successfully (reconnect #${this.reconnectCount})`);
                console.log('[Redis] Exiting graceful degradation mode - resuming normal operations');
            } else {
                console.log('[Redis] Client is ready');
            }
            this._notifyConnectionListeners();
        });

        this.client.on('error', (err) => {
            this.lastError = err.message;
            this.lastErrorAt = new Date().toISOString();
            this.failedCommands++;
            console.error(`[Redis] Client error: ${err.message}`);
        });

        this.client.on('close', () => {
            const wasConnected = this.connected;
            this.connected = false;
            if (wasConnected) {
                console.warn('[Redis] ⚠️  Connection lost - entering graceful degradation mode');
                console.warn('[Redis] Service will continue operating from cache');
                console.warn('[Redis] Write operations will be queued for replay on reconnection');
            } else {
                console.log('[Redis] Connection closed');
            }
            this._notifyDisconnectionListeners();
        });

        this.client.on('reconnecting', (delay) => {
            console.log(`[Redis] Attempting to reconnect in ${delay}ms... (attempt ${this.reconnectCount + 1})`);
        });

        this.client.on('end', () => {
            console.log('[Redis] Connection ended');
            this.connected = false;
        });
    }

    /**
     * Add a listener for connection events
     * @param {Function} listener - Callback when connected
     */
    onConnect(listener) {
        this.connectionListeners.push(listener);
    }

    /**
     * Add a listener for disconnection events
     * @param {Function} listener - Callback when disconnected
     */
    onDisconnect(listener) {
        this.disconnectionListeners.push(listener);
    }

    /**
     * Notify connection listeners
     * @private
     */
    _notifyConnectionListeners() {
        for (const listener of this.connectionListeners) {
            try {
                listener();
            } catch (err) {
                console.error('[Redis] Connection listener error:', err.message);
            }
        }
    }

    /**
     * Notify disconnection listeners
     * @private
     */
    _notifyDisconnectionListeners() {
        for (const listener of this.disconnectionListeners) {
            try {
                listener();
            } catch (err) {
                console.error('[Redis] Disconnection listener error:', err.message);
            }
        }
    }

    /**
     * Get the Redis client instance
     * @returns {Redis|null}
     */
    getClient() {
        return this.client;
    }

    /**
     * Check if Redis is connected
     * @returns {boolean}
     */
    isConnected() {
        return this.connected && this.client !== null;
    }

    /**
     * Check if Redis is enabled
     * @returns {boolean}
     */
    isEnabled() {
        return this.options.enabled === true;
    }

    /**
     * Get the key prefix
     * @returns {string}
     */
    getKeyPrefix() {
        return this.options.keyPrefix || 'aiclient:';
    }

    /**
     * Get connection status
     * @returns {ConnectionStatus}
     */
    getStatus() {
        return {
            connected: this.connected,
            enabled: this.options.enabled || false,
            lastConnectedAt: this.lastConnectedAt,
            lastErrorAt: this.lastErrorAt,
            lastError: this.lastError,
            queuedWrites: this.queuedWrites,
            reconnectCount: this.reconnectCount,
            initialConnectionAt: this.initialConnectionAt,
            totalCommands: this.totalCommands,
            failedCommands: this.failedCommands,
            url: this.options.url || `${this.options.host}:${this.options.port}`,
            keyPrefix: this.options.keyPrefix,
        };
    }

    /**
     * Increment command counter (for monitoring)
     */
    incrementCommandCount() {
        this.totalCommands++;
    }

    /**
     * Increment failed command counter (for monitoring)
     */
    incrementFailedCommandCount() {
        this.failedCommands++;
    }

    /**
     * Update queued writes count
     * @param {number} count - New count
     */
    setQueuedWrites(count) {
        this.queuedWrites = count;
    }

    /**
     * Perform a health check
     * @returns {Promise<boolean>} Whether Redis is healthy
     */
    async healthCheck() {
        if (!this.client || !this.connected) {
            return false;
        }

        try {
            const result = await this.client.ping();
            return result === 'PONG';
        } catch (error) {
            console.error('[Redis] Health check failed:', error.message);
            this.lastError = error.message;
            this.lastErrorAt = new Date().toISOString();
            return false;
        }
    }

    /**
     * Close the Redis connection
     * @returns {Promise<void>}
     */
    async close() {
        if (this.client) {
            try {
                await this.client.quit();
                console.log('[Redis] Connection closed gracefully');
            } catch (error) {
                console.error('[Redis] Error closing connection:', error.message);
                // Force disconnect if quit fails
                this.client.disconnect();
            }
            this.client = null;
            this.connected = false;
        }
    }
}

// Singleton instance
const redisClientManager = new RedisClientManager();

export { RedisClientManager, redisClientManager };
export default redisClientManager;
