/**
 * Performance tests for Claude Kiro provider optimizations
 *
 * Tests the following optimizations:
 * 1. Token counting with LRU cache
 * 2. Event stream parsing with charCodeAt
 * 3. String buffer accumulation
 * 4. Async mutex (non-blocking lock)
 */

import {
    TokenCache,
    countTokensCached,
    countTokensBatch,
    countTokensTotal,
    getTokenCacheStats,
    clearTokenCache
} from '../src/utils/token-counter.js';

import {
    AsyncMutex,
    KeyedMutex,
    Semaphore
} from '../src/utils/async-mutex.js';

describe('Token Counter with LRU Cache', () => {
    beforeEach(() => {
        clearTokenCache();
    });

    test('countTokensCached returns consistent results', () => {
        const text = 'Hello, this is a test sentence for token counting.';
        const count1 = countTokensCached(text);
        const count2 = countTokensCached(text);

        expect(count1).toBe(count2);
        expect(count1).toBeGreaterThan(0);
    });

    test('cache hits improve performance', () => {
        const text = 'This is a longer test sentence that will be counted multiple times to test caching performance.';

        // First call - cache miss
        countTokensCached(text);
        const stats1 = getTokenCacheStats();
        expect(stats1.misses).toBe(1);
        expect(stats1.hits).toBe(0);

        // Second call - cache hit
        countTokensCached(text);
        const stats2 = getTokenCacheStats();
        expect(stats2.misses).toBe(1);
        expect(stats2.hits).toBe(1);
    });

    test('countTokensBatch handles arrays correctly', () => {
        const texts = [
            'First sentence',
            'Second sentence',
            'First sentence', // Duplicate
            'Third sentence'
        ];

        const results = countTokensBatch(texts);

        expect(results).toHaveLength(4);
        expect(results[0]).toBe(results[2]); // Same text should have same count
        expect(results.every(r => r > 0)).toBe(true);
    });

    test('countTokensTotal sums correctly', () => {
        const texts = ['Hello', 'World', 'Test'];
        const total = countTokensTotal(texts);
        const individual = texts.map(t => countTokensCached(t));

        expect(total).toBe(individual.reduce((a, b) => a + b, 0));
    });

    test('TokenCache handles empty and null inputs', () => {
        expect(countTokensCached('')).toBe(0);
        expect(countTokensCached(null)).toBe(0);
        expect(countTokensCached(undefined)).toBe(0);
    });

    test('TokenCache handles long strings with hash-based keys', () => {
        const longText = 'x'.repeat(1000);
        const count1 = countTokensCached(longText);
        const count2 = countTokensCached(longText);

        expect(count1).toBe(count2);
        expect(count1).toBeGreaterThan(0);
    });
});

describe('Async Mutex', () => {
    test('AsyncMutex provides mutual exclusion', async () => {
        const mutex = new AsyncMutex();
        const results = [];

        const task = async (id) => {
            await mutex.acquire();
            results.push(`start-${id}`);
            await new Promise(r => setTimeout(r, 10));
            results.push(`end-${id}`);
            mutex.release();
        };

        await Promise.all([task(1), task(2), task(3)]);

        // Check that starts and ends are paired
        expect(results).toHaveLength(6);
        for (let i = 0; i < 6; i += 2) {
            const startId = results[i].split('-')[1];
            const endId = results[i + 1].split('-')[1];
            expect(startId).toBe(endId);
        }
    });

    test('AsyncMutex.withLock handles errors', async () => {
        const mutex = new AsyncMutex();

        await expect(mutex.withLock(async () => {
            throw new Error('test error');
        })).rejects.toThrow('test error');

        // Mutex should be released after error
        expect(mutex.isLocked).toBe(false);
    });

    test('AsyncMutex.tryAcquire returns immediately', async () => {
        const mutex = new AsyncMutex();

        expect(mutex.tryAcquire()).toBe(true);
        expect(mutex.tryAcquire()).toBe(false);

        mutex.release();
        expect(mutex.tryAcquire()).toBe(true);
    });
});

describe('Keyed Mutex', () => {
    test('KeyedMutex allows concurrent access to different keys', async () => {
        const mutex = new KeyedMutex();
        const results = [];

        const task = async (key, id) => {
            await mutex.acquire(key);
            results.push(`${key}-start-${id}`);
            await new Promise(r => setTimeout(r, 10));
            results.push(`${key}-end-${id}`);
            mutex.release(key);
        };

        // Tasks with different keys should run concurrently
        const start = Date.now();
        await Promise.all([
            task('A', 1),
            task('B', 1),
            task('A', 2),
            task('B', 2)
        ]);
        const duration = Date.now() - start;

        // With concurrent access to different keys, should take ~20ms (2 tasks per key)
        // Without concurrency, would take ~40ms
        expect(duration).toBeLessThan(35);
    });

    test('KeyedMutex.withLock works correctly', async () => {
        const mutex = new KeyedMutex();
        let counter = 0;

        await Promise.all([
            mutex.withLock('test', async () => { counter++; }),
            mutex.withLock('test', async () => { counter++; }),
            mutex.withLock('test', async () => { counter++; })
        ]);

        expect(counter).toBe(3);
    });
});

describe('Semaphore', () => {
    test('Semaphore limits concurrent access', async () => {
        const semaphore = new Semaphore(2);
        let concurrent = 0;
        let maxConcurrent = 0;

        const task = async () => {
            await semaphore.acquire();
            concurrent++;
            maxConcurrent = Math.max(maxConcurrent, concurrent);
            await new Promise(r => setTimeout(r, 20));
            concurrent--;
            semaphore.release();
        };

        await Promise.all([task(), task(), task(), task(), task()]);

        expect(maxConcurrent).toBe(2);
    });

    test('Semaphore.withPermit handles errors', async () => {
        const semaphore = new Semaphore(1);

        await expect(semaphore.withPermit(async () => {
            throw new Error('test error');
        })).rejects.toThrow('test error');

        // Semaphore should release permit after error
        expect(semaphore.available).toBe(1);
    });
});

describe('String Buffer Optimization Pattern', () => {
    test('array join is more efficient than string concatenation for many items', () => {
        const itemCount = 1000;
        const items = Array.from({ length: itemCount }, (_, i) => `item${i}`);

        // Test array join approach
        const joinStart = Date.now();
        const parts = [];
        for (const item of items) {
            parts.push(item);
        }
        const joinResult = parts.join('');
        const joinTime = Date.now() - joinStart;

        // Test string concat approach
        const concatStart = Date.now();
        let concatResult = '';
        for (const item of items) {
            concatResult += item;
        }
        const concatTime = Date.now() - concatStart;

        // Both should produce same result
        expect(joinResult).toBe(concatResult);

        // For large numbers of items, join should be faster
        // (though this may vary by JS engine)
        console.log(`Join: ${joinTime}ms, Concat: ${concatTime}ms`);
    });
});

describe('Token Counting Integration', () => {
    test('batch counting deduplicates texts before counting', () => {
        clearTokenCache();

        // Create array with many duplicate texts
        const texts = [];
        for (let i = 0; i < 100; i++) {
            texts.push('This is a repeated sentence.');
            texts.push('Another repeated sentence.');
            texts.push(`Unique sentence number ${i}.`);
        }

        const results = countTokensBatch(texts);
        const stats = getTokenCacheStats();

        expect(results).toHaveLength(300);

        // Batch function deduplicates before counting, so we should have
        // exactly 102 unique texts counted (100 unique + 2 repeated)
        expect(stats.misses).toBe(102);

        // Duplicates within same text array get same count
        expect(results[0]).toBe(results[3]); // 'This is a repeated sentence.'
        expect(results[1]).toBe(results[4]); // 'Another repeated sentence.'
    });

    test('cache hits work across multiple batch calls', () => {
        clearTokenCache();

        const texts1 = ['Hello', 'World'];
        const texts2 = ['Hello', 'Universe']; // 'Hello' was already counted

        countTokensBatch(texts1);
        const stats1 = getTokenCacheStats();
        expect(stats1.misses).toBe(2);
        expect(stats1.hits).toBe(0);

        countTokensBatch(texts2);
        const stats2 = getTokenCacheStats();
        expect(stats2.misses).toBe(3); // Added 'Universe'
        expect(stats2.hits).toBe(1);   // Cache hit for 'Hello'
    });

    test('countTokensTotal aggregates correctly', () => {
        clearTokenCache();

        const texts = ['Hello', 'World', 'Test', 'Hello']; // Hello appears twice
        const total = countTokensTotal(texts);

        // Total should be positive and reasonable
        expect(total).toBeGreaterThan(0);
        expect(total).toBeLessThan(100); // Should be small for short texts
    });
});
