/**
 * Async Mutex Implementation
 *
 * Provides Promise-based mutual exclusion to replace busy-waiting patterns.
 * This avoids blocking the event loop with setImmediate polling.
 */

/**
 * AsyncMutex - A simple promise-based mutex
 *
 * Instead of busy-waiting with while loops and setImmediate,
 * waiters are queued and resolved in order when the lock is released.
 */
export class AsyncMutex {
    constructor() {
        this._locked = false;
        this._queue = [];
    }

    /**
     * Check if the mutex is currently locked
     */
    get isLocked() {
        return this._locked;
    }

    /**
     * Acquire the mutex. Returns a promise that resolves when the lock is acquired.
     * @returns {Promise<void>}
     */
    async acquire() {
        if (!this._locked) {
            this._locked = true;
            return;
        }

        // Wait in queue
        return new Promise(resolve => {
            this._queue.push(resolve);
        });
    }

    /**
     * Release the mutex. The next waiter in the queue will be granted the lock.
     */
    release() {
        if (this._queue.length > 0) {
            // Pass lock to next waiter
            const next = this._queue.shift();
            // Use setImmediate to prevent stack overflow in tight loops
            setImmediate(next);
        } else {
            this._locked = false;
        }
    }

    /**
     * Execute a function while holding the lock
     * @param {Function} fn - Async or sync function to execute
     * @returns {Promise<*>} Result of the function
     */
    async withLock(fn) {
        await this.acquire();
        try {
            return await fn();
        } finally {
            this.release();
        }
    }

    /**
     * Try to acquire the lock without waiting
     * @returns {boolean} True if lock was acquired, false if already locked
     */
    tryAcquire() {
        if (!this._locked) {
            this._locked = true;
            return true;
        }
        return false;
    }
}

/**
 * KeyedMutex - Manages separate mutexes for different keys
 *
 * Useful when you need to lock by provider type or other identifier.
 * Each key gets its own mutex, so different keys can operate concurrently.
 */
export class KeyedMutex {
    constructor() {
        this._mutexes = new Map();
    }

    /**
     * Get or create a mutex for the given key
     * @param {string} key
     * @returns {AsyncMutex}
     */
    _getMutex(key) {
        if (!this._mutexes.has(key)) {
            this._mutexes.set(key, new AsyncMutex());
        }
        return this._mutexes.get(key);
    }

    /**
     * Check if a specific key's mutex is locked
     * @param {string} key
     * @returns {boolean}
     */
    isLocked(key) {
        const mutex = this._mutexes.get(key);
        return mutex ? mutex.isLocked : false;
    }

    /**
     * Acquire the mutex for a specific key
     * @param {string} key
     * @returns {Promise<void>}
     */
    async acquire(key) {
        return this._getMutex(key).acquire();
    }

    /**
     * Release the mutex for a specific key
     * @param {string} key
     */
    release(key) {
        const mutex = this._mutexes.get(key);
        if (mutex) {
            mutex.release();
        }
    }

    /**
     * Execute a function while holding the lock for a specific key
     * @param {string} key
     * @param {Function} fn - Async or sync function to execute
     * @returns {Promise<*>} Result of the function
     */
    async withLock(key, fn) {
        return this._getMutex(key).withLock(fn);
    }

    /**
     * Try to acquire lock for a key without waiting
     * @param {string} key
     * @returns {boolean}
     */
    tryAcquire(key) {
        return this._getMutex(key).tryAcquire();
    }

    /**
     * Get number of managed mutexes
     */
    get size() {
        return this._mutexes.size;
    }

    /**
     * Clean up mutexes that are no longer in use (not locked and no waiters)
     * Call periodically if you have many transient keys
     */
    cleanup() {
        for (const [key, mutex] of this._mutexes) {
            if (!mutex.isLocked && mutex._queue.length === 0) {
                this._mutexes.delete(key);
            }
        }
    }
}

/**
 * Semaphore - Allows up to N concurrent holders
 *
 * Useful for limiting concurrent operations (e.g., max 5 concurrent API calls)
 */
export class Semaphore {
    constructor(maxConcurrent = 1) {
        this._max = maxConcurrent;
        this._current = 0;
        this._queue = [];
    }

    get available() {
        return this._max - this._current;
    }

    get waiting() {
        return this._queue.length;
    }

    async acquire() {
        if (this._current < this._max) {
            this._current++;
            return;
        }

        return new Promise(resolve => {
            this._queue.push(resolve);
        });
    }

    release() {
        if (this._queue.length > 0) {
            const next = this._queue.shift();
            setImmediate(next);
        } else {
            this._current = Math.max(0, this._current - 1);
        }
    }

    async withPermit(fn) {
        await this.acquire();
        try {
            return await fn();
        } finally {
            this.release();
        }
    }
}

export default {
    AsyncMutex,
    KeyedMutex,
    Semaphore
};
