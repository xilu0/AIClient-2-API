/**
 * Redis Performance Benchmark - T064
 * Verifies <10ms Redis operations.
 *
 * Usage: node tests/redis-benchmark.js [--redis-url redis://localhost:6379]
 *
 * This test benchmarks:
 * 1. GET operations (config read)
 * 2. SET operations (config write)
 * 3. HGET/HSET operations (provider pool access)
 * 4. INCR operations (atomic counter)
 * 5. Lua script execution (compound atomic operation)
 */

import Redis from 'ioredis';

const ITERATIONS = 1000;
const TARGET_LATENCY_MS = 10;
const PREFIX = 'aiclient:benchmark:';

async function benchmark(name, iterations, operation) {
    const times = [];

    for (let i = 0; i < iterations; i++) {
        const start = process.hrtime.bigint();
        await operation(i);
        const end = process.hrtime.bigint();
        times.push(Number(end - start) / 1e6); // Convert to ms
    }

    times.sort((a, b) => a - b);

    const avg = times.reduce((a, b) => a + b, 0) / times.length;
    const min = times[0];
    const max = times[times.length - 1];
    const p50 = times[Math.floor(times.length * 0.5)];
    const p95 = times[Math.floor(times.length * 0.95)];
    const p99 = times[Math.floor(times.length * 0.99)];

    return {
        name,
        iterations,
        avg: avg.toFixed(3),
        min: min.toFixed(3),
        max: max.toFixed(3),
        p50: p50.toFixed(3),
        p95: p95.toFixed(3),
        p99: p99.toFixed(3),
        passed: p95 < TARGET_LATENCY_MS
    };
}

async function runBenchmarks(redisUrl) {
    const redis = new Redis(redisUrl);

    console.log(`[Benchmark] Connecting to ${redisUrl}...`);

    try {
        await redis.ping();
        console.log('[Benchmark] Connected to Redis');
        console.log(`[Benchmark] Running ${ITERATIONS} iterations per operation`);
        console.log(`[Benchmark] Target: p95 < ${TARGET_LATENCY_MS}ms\n`);

        const results = [];

        // Setup test data
        await redis.set(`${PREFIX}config`, JSON.stringify({ test: 'data', nested: { value: 123 } }));
        await redis.hset(`${PREFIX}pool`, 'provider1', JSON.stringify({ uuid: 'test', enabled: true }));

        // Define Lua script for compound operation
        const luaScript = `
            local key = KEYS[1]
            local count = redis.call('INCR', key)
            redis.call('SET', key .. ':timestamp', ARGV[1])
            return count
        `;

        // 1. GET operation (config read)
        results.push(await benchmark('GET (config read)', ITERATIONS, async () => {
            await redis.get(`${PREFIX}config`);
        }));

        // 2. SET operation (config write)
        results.push(await benchmark('SET (config write)', ITERATIONS, async (i) => {
            await redis.set(`${PREFIX}config`, JSON.stringify({ iteration: i }));
        }));

        // 3. HGET operation (provider read)
        results.push(await benchmark('HGET (provider read)', ITERATIONS, async () => {
            await redis.hget(`${PREFIX}pool`, 'provider1');
        }));

        // 4. HSET operation (provider write)
        results.push(await benchmark('HSET (provider write)', ITERATIONS, async (i) => {
            await redis.hset(`${PREFIX}pool`, 'provider1', JSON.stringify({ uuid: 'test', count: i }));
        }));

        // 5. INCR operation (atomic counter)
        results.push(await benchmark('INCR (atomic counter)', ITERATIONS, async () => {
            await redis.incr(`${PREFIX}counter`);
        }));

        // 6. Lua script (compound operation)
        results.push(await benchmark('EVAL (Lua script)', ITERATIONS, async () => {
            await redis.eval(luaScript, 1, `${PREFIX}lua-counter`, Date.now().toString());
        }));

        // Cleanup
        const keys = await redis.keys(`${PREFIX}*`);
        if (keys.length > 0) {
            await redis.del(...keys);
        }

        // Print results table
        console.log('Results:');
        console.log('─'.repeat(100));
        console.log(
            'Operation'.padEnd(25) +
            'Avg(ms)'.padStart(10) +
            'Min(ms)'.padStart(10) +
            'Max(ms)'.padStart(10) +
            'P50(ms)'.padStart(10) +
            'P95(ms)'.padStart(10) +
            'P99(ms)'.padStart(10) +
            'Status'.padStart(10)
        );
        console.log('─'.repeat(100));

        let allPassed = true;
        for (const r of results) {
            const status = r.passed ? '✅ PASS' : '❌ FAIL';
            if (!r.passed) allPassed = false;
            console.log(
                r.name.padEnd(25) +
                r.avg.padStart(10) +
                r.min.padStart(10) +
                r.max.padStart(10) +
                r.p50.padStart(10) +
                r.p95.padStart(10) +
                r.p99.padStart(10) +
                status.padStart(10)
            );
        }
        console.log('─'.repeat(100));

        if (allPassed) {
            console.log('\n✅ BENCHMARK PASSED: All operations under target latency');
        } else {
            console.log('\n❌ BENCHMARK FAILED: Some operations exceeded target latency');
        }

        await redis.quit();
        return allPassed;
    } catch (error) {
        console.error('[Benchmark] Error:', error.message);
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

// Run benchmarks
runBenchmarks(redisUrl).then(success => {
    process.exit(success ? 0 : 1);
}).catch(error => {
    console.error('Benchmark failed:', error);
    process.exit(1);
});
