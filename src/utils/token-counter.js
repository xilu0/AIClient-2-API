/**
 * Token Counter with LRU Cache
 *
 * Provides cached token counting to avoid repeated synchronous tokenizer calls
 * which can block the event loop under high concurrency.
 */

import { countTokens } from '@anthropic-ai/tokenizer';

// LRU Cache implementation
class LRUCache {
    constructor(maxSize = 1000) {
        this.maxSize = maxSize;
        this.cache = new Map();
    }

    get(key) {
        if (!this.cache.has(key)) {
            return undefined;
        }
        // Move to end (most recently used)
        const value = this.cache.get(key);
        this.cache.delete(key);
        this.cache.set(key, value);
        return value;
    }

    set(key, value) {
        // If key exists, delete it first to update position
        if (this.cache.has(key)) {
            this.cache.delete(key);
        }
        // Evict oldest if at capacity
        if (this.cache.size >= this.maxSize) {
            const oldestKey = this.cache.keys().next().value;
            this.cache.delete(oldestKey);
        }
        this.cache.set(key, value);
    }

    has(key) {
        return this.cache.has(key);
    }

    clear() {
        this.cache.clear();
    }

    get size() {
        return this.cache.size;
    }
}

/**
 * TokenCache - Caches token counts with hash-based keys for long strings
 */
export class TokenCache {
    constructor(options = {}) {
        this.maxSize = options.maxSize || 2000;
        this.maxKeyLength = options.maxKeyLength || 200;
        this.cache = new LRUCache(this.maxSize);
        this.hits = 0;
        this.misses = 0;
    }

    /**
     * Generate cache key - use string directly for short strings, hash for long ones
     */
    _getCacheKey(text) {
        if (text.length <= this.maxKeyLength) {
            return text;
        }
        // For long strings, use a combination of length and sampled characters
        // This provides good uniqueness without expensive hashing
        const len = text.length;
        const sample = text.slice(0, 50) + text.slice(-50) +
                       text.slice(Math.floor(len/4), Math.floor(len/4) + 25) +
                       text.slice(Math.floor(len/2), Math.floor(len/2) + 25);
        return `L${len}:${sample}`;
    }

    /**
     * Get cached token count or compute and cache it
     */
    count(text) {
        if (!text) return 0;

        const key = this._getCacheKey(text);
        const cached = this.cache.get(key);

        if (cached !== undefined) {
            this.hits++;
            return cached;
        }

        this.misses++;
        try {
            const count = countTokens(text);
            this.cache.set(key, count);
            return count;
        } catch (error) {
            // Fallback to estimation
            const estimate = Math.ceil(text.length / 4);
            this.cache.set(key, estimate);
            return estimate;
        }
    }

    /**
     * Get cache statistics
     */
    getStats() {
        return {
            size: this.cache.size,
            maxSize: this.maxSize,
            hits: this.hits,
            misses: this.misses,
            hitRate: this.hits + this.misses > 0
                ? (this.hits / (this.hits + this.misses) * 100).toFixed(2) + '%'
                : '0%'
        };
    }

    clear() {
        this.cache.clear();
        this.hits = 0;
        this.misses = 0;
    }
}

// Global shared cache instance
const globalCache = new TokenCache();

/**
 * Count tokens with caching - drop-in replacement for countTokens
 * @param {string} text - Text to count tokens for
 * @returns {number} Token count
 */
export function countTokensCached(text) {
    return globalCache.count(text);
}

/**
 * Count tokens for multiple texts efficiently
 * Collects all texts, deduplicates, counts once, returns results
 *
 * @param {string[]} texts - Array of texts to count
 * @returns {number[]} Array of token counts in same order
 */
export function countTokensBatch(texts) {
    if (!texts || !Array.isArray(texts)) return [];

    const results = new Array(texts.length);
    const uniqueTexts = new Map(); // text -> indices where it appears

    // Collect unique texts and track their positions
    for (let i = 0; i < texts.length; i++) {
        const text = texts[i];
        if (!text) {
            results[i] = 0;
            continue;
        }

        if (uniqueTexts.has(text)) {
            uniqueTexts.get(text).push(i);
        } else {
            uniqueTexts.set(text, [i]);
        }
    }

    // Count each unique text once and populate results
    for (const [text, indices] of uniqueTexts) {
        const count = globalCache.count(text);
        for (const idx of indices) {
            results[idx] = count;
        }
    }

    return results;
}

/**
 * Sum token counts for multiple texts efficiently
 * @param {string[]} texts - Array of texts
 * @returns {number} Total token count
 */
export function countTokensTotal(texts) {
    const counts = countTokensBatch(texts);
    return counts.reduce((sum, count) => sum + count, 0);
}

/**
 * Get global cache statistics
 */
export function getTokenCacheStats() {
    return globalCache.getStats();
}

/**
 * Clear the global cache
 */
export function clearTokenCache() {
    globalCache.clear();
}

export default {
    TokenCache,
    countTokensCached,
    countTokensBatch,
    countTokensTotal,
    getTokenCacheStats,
    clearTokenCache
};
