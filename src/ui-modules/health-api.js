/**
 * Health API Module
 * Provides health check and status endpoints for system components.
 * @module health-api
 */

import redisClientManager from '../core/redis-client.js';
import { getStorageAdapter, isStorageInitialized, getStorageType } from '../core/storage-factory.js';

/**
 * Get Redis status and health information
 * GET /api/redis/status
 */
export async function handleGetRedisStatus(req, res) {
    try {
        const status = redisClientManager.getStatus();
        const storageType = getStorageType();

        // Build response object
        const response = {
            enabled: status.enabled,
            connected: status.connected,
            storageType: storageType,
            url: status.enabled ? status.url : null,
            keyPrefix: status.keyPrefix,
            stats: {
                reconnectCount: status.reconnectCount,
                totalCommands: status.totalCommands,
                failedCommands: status.failedCommands,
                queuedWrites: status.queuedWrites,
            },
            timestamps: {
                initialConnectionAt: status.initialConnectionAt,
                lastConnectedAt: status.lastConnectedAt,
                lastErrorAt: status.lastErrorAt,
            },
            lastError: status.lastError,
        };

        // Add degraded mode info if using Redis adapter
        if (isStorageInitialized() && storageType === 'redis') {
            try {
                const adapter = getStorageAdapter();
                if (typeof adapter.isDegradedMode === 'function') {
                    response.degradedMode = adapter.isDegradedMode();
                    if (response.degradedMode && typeof adapter.getDegradedModeInfo === 'function') {
                        response.degradedModeInfo = adapter.getDegradedModeInfo();
                    }
                }
            } catch (error) {
                // Ignore errors getting degraded mode info
            }
        }

        // Perform health check if connected
        if (status.connected) {
            const startTime = Date.now();
            const healthy = await redisClientManager.healthCheck();
            const latency = Date.now() - startTime;

            response.health = {
                status: healthy ? 'healthy' : 'unhealthy',
                latencyMs: latency,
                checkedAt: new Date().toISOString(),
            };
        } else {
            response.health = {
                status: status.enabled ? 'disconnected' : 'disabled',
                latencyMs: null,
                checkedAt: new Date().toISOString(),
            };
        }

        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(response));
        return true;
    } catch (error) {
        console.error('[Health API] Error getting Redis status:', error.message);
        res.writeHead(500, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
            error: {
                message: 'Failed to get Redis status: ' + error.message,
                code: 'REDIS_STATUS_ERROR'
            }
        }));
        return true;
    }
}

/**
 * Get storage adapter status
 * GET /api/storage/status
 */
export async function handleGetStorageStatus(req, res) {
    try {
        const storageType = getStorageType();
        const redisStatus = redisClientManager.getStatus();

        const response = {
            initialized: isStorageInitialized(),
            type: storageType,
            redis: {
                enabled: redisStatus.enabled,
                connected: redisStatus.connected,
            },
        };

        // Add adapter-specific info
        if (isStorageInitialized()) {
            try {
                const adapter = getStorageAdapter();
                if (typeof adapter.isDegradedMode === 'function') {
                    response.degradedMode = adapter.isDegradedMode();
                }
            } catch (error) {
                // Ignore
            }
        }

        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(response));
        return true;
    } catch (error) {
        console.error('[Health API] Error getting storage status:', error.message);
        res.writeHead(500, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
            error: {
                message: 'Failed to get storage status: ' + error.message,
                code: 'STORAGE_STATUS_ERROR'
            }
        }));
        return true;
    }
}
