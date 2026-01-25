/**
 * Redis Stress Test - T063
 * Verifies 100% counter accuracy under concurrent load.
 *
 * Usage: node tests/redis-stress-test.js [--redis-url redis://localhost:6379]
 *
 * This test:
 * 1. Connects to Redis
 * 2. Sends 100 concurrent increment operations
 * 3. Verifies final counter equals 100
 */

import Redis from 'ioredis';

const CONCURRENT_REQUESTS = 100;
const TEST_KEY = 'aiclient:test:stress-counter';

async function runStressTest(redisUrl) {
    const redis = new Redis(redisUrl);

    console.log(`[Stress Test] Connecting to ${redisUrl}...`);

    try {
        await redis.ping();
        console.log('[Stress Test] Connected to Redis');

        // Reset test counter
        await redis.del(TEST_KEY);
        console.log(`[Stress Test] Reset counter: ${TEST_KEY}`);

        // Create 100 concurrent increment operations
        console.log(`[Stress Test] Sending ${CONCURRENT_REQUESTS} concurrent INCR operations...`);

        const startTime = Date.now();
        const promises = [];

        for (let i = 0; i < CONCURRENT_REQUESTS; i++) {
            promises.push(redis.incr(TEST_KEY));
        }

        // Wait for all operations to complete
        const results = await Promise.all(promises);
        const duration = Date.now() - startTime;

        // Verify final counter value
        const finalValue = await redis.get(TEST_KEY);
        const finalCount = parseInt(finalValue, 10);

        console.log(`[Stress Test] Completed in ${duration}ms`);
        console.log(`[Stress Test] Final counter value: ${finalCount}`);
        console.log(`[Stress Test] Expected value: ${CONCURRENT_REQUESTS}`);

        // Check intermediate results (each INCR returns the new value)
        const uniqueResults = new Set(results);
        console.log(`[Stress Test] Unique intermediate values: ${uniqueResults.size}`);

        // Cleanup
        await redis.del(TEST_KEY);

        // Report results
        if (finalCount === CONCURRENT_REQUESTS) {
            console.log('\n✅ STRESS TEST PASSED: 100% counter accuracy achieved');
            console.log(`   - ${CONCURRENT_REQUESTS} concurrent operations`);
            console.log(`   - ${duration}ms total duration`);
            console.log(`   - ${(CONCURRENT_REQUESTS / duration * 1000).toFixed(0)} ops/sec`);
            await redis.quit();
            return true;
        } else {
            console.log('\n❌ STRESS TEST FAILED: Counter mismatch');
            console.log(`   - Expected: ${CONCURRENT_REQUESTS}`);
            console.log(`   - Got: ${finalCount}`);
            console.log(`   - Lost: ${CONCURRENT_REQUESTS - finalCount} operations`);
            await redis.quit();
            return false;
        }
    } catch (error) {
        console.error('[Stress Test] Error:', error.message);
        await redis.quit();
        return false;
    }
}

// Parse arguments
const args = process.argv.slice(2);
let redisUrl = 'redis://localhost:6379';

for (let i = 0; i < args.length; i++) {
    if (args[i] === '--redis-url' && args[i + 1]) {
        redisUrl = args[i + 1];
        i++;
    }
}

// Run test
runStressTest(redisUrl).then(success => {
    process.exit(success ? 0 : 1);
}).catch(error => {
    console.error('Test failed:', error);
    process.exit(1);
});
