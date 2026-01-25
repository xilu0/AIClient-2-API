/**
 * Write Queue
 * Queues write operations during Redis unavailability for later replay.
 * @module write-queue
 */

/**
 * @typedef {Object} QueuedOperation
 * @property {Function} operation - Async function to execute
 * @property {number} timestamp - When the operation was queued
 * @property {string} [description] - Optional description for logging
 * @property {number} [retryCount] - Number of retry attempts
 */

/**
 * Manages a queue of pending write operations.
 * Used to buffer writes when Redis is temporarily unavailable.
 */
class WriteQueue {
    /**
     * @param {Object} options - Queue options
     * @param {number} [options.maxSize=1000] - Maximum queue size
     * @param {number} [options.maxRetries=3] - Maximum retry attempts per operation
     * @param {number} [options.retryDelay=1000] - Delay between retries in ms
     */
    constructor(options = {}) {
        /** @type {QueuedOperation[]} */
        this.queue = [];

        /** @type {number} */
        this.maxSize = options.maxSize || 1000;

        /** @type {number} */
        this.maxRetries = options.maxRetries || 3;

        /** @type {number} */
        this.retryDelay = options.retryDelay || 1000;

        /** @type {boolean} */
        this.isReplaying = false;

        /** @type {number} */
        this.droppedCount = 0;

        /** @type {Function[]} */
        this.overflowListeners = [];
    }

    /**
     * Get the current queue size
     * @returns {number}
     */
    get size() {
        return this.queue.length;
    }

    /**
     * Check if the queue is empty
     * @returns {boolean}
     */
    get isEmpty() {
        return this.queue.length === 0;
    }

    /**
     * Check if the queue is full
     * @returns {boolean}
     */
    get isFull() {
        return this.queue.length >= this.maxSize;
    }

    /**
     * Add a listener for overflow events
     * @param {Function} listener - Callback when queue overflows
     */
    onOverflow(listener) {
        this.overflowListeners.push(listener);
    }

    /**
     * Push an operation onto the queue
     * @param {Function} operation - Async function to execute
     * @param {string} [description] - Optional description for logging
     * @returns {boolean} Whether the operation was queued successfully
     */
    push(operation, description = '') {
        if (typeof operation !== 'function') {
            console.error('[WriteQueue] Invalid operation: must be a function');
            return false;
        }

        if (this.queue.length >= this.maxSize) {
            // Drop oldest operation if queue is full
            const dropped = this.queue.shift();
            this.droppedCount++;
            console.warn(`[WriteQueue] Queue overflow, dropped oldest operation: ${dropped?.description || 'unknown'}`);

            // Notify listeners
            for (const listener of this.overflowListeners) {
                try {
                    listener(dropped);
                } catch (err) {
                    console.error('[WriteQueue] Overflow listener error:', err.message);
                }
            }
        }

        this.queue.push({
            operation,
            timestamp: Date.now(),
            description,
            retryCount: 0,
        });

        console.log(`[WriteQueue] Queued operation: ${description || 'unnamed'} (queue size: ${this.queue.length})`);
        return true;
    }

    /**
     * Replay all queued operations
     * @param {Object} redisClient - Redis client instance
     * @returns {Promise<{success: number, failed: number}>}
     */
    async replay(redisClient) {
        if (this.isReplaying) {
            console.log('[WriteQueue] Replay already in progress');
            return { success: 0, failed: 0 };
        }

        if (this.queue.length === 0) {
            console.log('[WriteQueue] No operations to replay');
            return { success: 0, failed: 0 };
        }

        this.isReplaying = true;
        console.log(`[WriteQueue] Starting replay of ${this.queue.length} queued operations...`);

        let success = 0;
        let failed = 0;
        const failedOperations = [];

        while (this.queue.length > 0) {
            const item = this.queue.shift();

            try {
                await item.operation(redisClient);
                success++;
                console.log(`[WriteQueue] Replayed: ${item.description || 'unnamed'} (queued ${Date.now() - item.timestamp}ms ago)`);
            } catch (error) {
                console.error(`[WriteQueue] Replay failed: ${item.description || 'unnamed'} - ${error.message}`);

                // Check if we should retry
                if (item.retryCount < this.maxRetries) {
                    item.retryCount++;
                    failedOperations.push(item);
                    console.log(`[WriteQueue] Will retry operation (attempt ${item.retryCount}/${this.maxRetries})`);
                } else {
                    failed++;
                    console.error(`[WriteQueue] Operation permanently failed after ${this.maxRetries} retries: ${item.description || 'unnamed'}`);
                }
            }
        }

        // Re-queue failed operations for later retry
        if (failedOperations.length > 0) {
            console.log(`[WriteQueue] Re-queuing ${failedOperations.length} failed operations for retry`);
            this.queue.push(...failedOperations);
        }

        this.isReplaying = false;
        console.log(`[WriteQueue] Replay complete: ${success} succeeded, ${failed} permanently failed, ${failedOperations.length} pending retry`);

        return { success, failed };
    }

    /**
     * Clear all queued operations
     * @returns {number} Number of operations cleared
     */
    clear() {
        const count = this.queue.length;
        this.queue = [];
        console.log(`[WriteQueue] Cleared ${count} queued operations`);
        return count;
    }

    /**
     * Get queue statistics
     * @returns {Object}
     */
    getStats() {
        return {
            size: this.queue.length,
            maxSize: this.maxSize,
            droppedCount: this.droppedCount,
            isReplaying: this.isReplaying,
            oldestOperationAge: this.queue.length > 0
                ? Date.now() - this.queue[0].timestamp
                : null,
            newestOperationAge: this.queue.length > 0
                ? Date.now() - this.queue[this.queue.length - 1].timestamp
                : null,
        };
    }

    /**
     * Get operations pending for a specific duration
     * @param {number} maxAge - Maximum age in ms
     * @returns {QueuedOperation[]}
     */
    getOldOperations(maxAge) {
        const cutoff = Date.now() - maxAge;
        return this.queue.filter(op => op.timestamp < cutoff);
    }
}

export { WriteQueue };
export default WriteQueue;
