
import { RedisConfigManager } from '../src/core/redis-config-manager.js';
import { RedisClientManager } from '../src/core/redis-client.js';

// Mock Redis client to simulate behavior without a real Redis server if needed,
// but better to use real one if available. The environment has a redis server?
// The user context implies a working environment.

async function testAddProviderFix() {
    console.log('Starting RedisConfigManager AddProvider Test...');

    // 1. Initialize Redis Client
    const redisManager = new RedisClientManager({
        // Default to localhost:6379 or use env vars
        host: process.env.REDIS_HOST || 'localhost',
        port: process.env.REDIS_PORT || 6379,
    });
    
    await redisManager.connect();
    
    // 2. Initialize Config Manager
    const configManager = new RedisConfigManager(redisManager, {
        keyPrefix: 'test:aiclient:'
    });

    try {
        const providerType = 'claude-kiro-oauth-test';
        const uuid = 'test-uuid-123';
        const providerData = {
            uuid: uuid,
            name: 'Test Provider',
            isHealthy: true
        };

        // Clean up previous test data
        console.log('Cleaning up old test data...');
        const client = redisManager.getClient();
        await client.del(`test:aiclient:pools:${providerType}`);
        await client.srem('test:aiclient:pool-types', providerType);

        // Verify clean state
        let pools = await configManager.getProviderPools();
        if (pools[providerType]) {
            throw new Error('Pre-test cleanup failed: provider type still exists');
        }

        // 3. Add Provider
        console.log('Adding provider...');
        await configManager.addProvider(providerType, providerData);

        // 4. Verify pool-types set contains the new type
        const isMember = await client.sismember('test:aiclient:pool-types', providerType);
        console.log(`Is '${providerType}' in pool-types? ${isMember === 1 ? 'YES' : 'NO'}`);
        
        if (isMember !== 1) {
            console.error('FAIL: addProvider did not add providerType to pool-types set!');
        } else {
             console.log('PASS: addProvider correctly updated pool-types set.');
        }

        // 5. Verify getProviderPools returns it
        // Force clear internal cache to ensure we read from Redis
        configManager.invalidateCache();
        
        pools = await configManager.getProviderPools();
        if (pools[providerType] && pools[providerType].length > 0) {
            console.log('PASS: getProviderPools returned the new provider.');
        } else {
             console.error('FAIL: getProviderPools did not return the new provider!');
        }

    } catch (err) {
        console.error('Test Error:', err);
    } finally {
        await redisManager.disconnect();
    }
}

testAddProviderFix();
