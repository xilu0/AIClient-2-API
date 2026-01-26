#!/usr/bin/env node
/**
 * Redis Initialization CLI Tool
 *
 * Initializes Redis with default configuration for fresh deployments
 * without requiring existing config files.
 *
 * Usage:
 *   node src/cli/init-redis.js --redis-url redis://localhost:6379
 *   npm run init:redis -- --redis-url redis://localhost:6379
 *
 * Options:
 *   --redis-url <url>     Redis connection URL (default: redis://localhost:6379)
 *   --key-prefix <prefix> Redis key prefix (default: aiclient:)
 *   --api-key <key>       API key for authentication (default: random)
 *   --password <pwd>      Web UI password (default: admin123)
 *   --port <port>         Server port (default: 3000)
 *   --force               Overwrite existing data
 *   --dry-run             Show what would be created without making changes
 *   --help                Show help
 */

import Redis from 'ioredis';
import crypto from 'crypto';

// ANSI color codes
const colors = {
    reset: '\x1b[0m',
    red: '\x1b[31m',
    green: '\x1b[32m',
    yellow: '\x1b[33m',
    cyan: '\x1b[36m',
    dim: '\x1b[2m'
};

function log(message, color = 'reset') {
    console.log(`${colors[color]}${message}${colors.reset}`);
}

/**
 * Generate a random API key
 */
function generateApiKey() {
    return 'sk-' + crypto.randomBytes(24).toString('hex');
}

/**
 * Parse command line arguments
 */
function parseArgs(args) {
    const options = {
        redisUrl: 'redis://localhost:6379',
        keyPrefix: 'aiclient:',
        apiKey: null,
        password: 'admin123',
        port: 3000,
        force: false,
        dryRun: false,
        help: false
    };

    for (let i = 0; i < args.length; i++) {
        const arg = args[i];
        switch (arg) {
            case '--redis-url':
                options.redisUrl = args[++i];
                break;
            case '--key-prefix':
                options.keyPrefix = args[++i];
                break;
            case '--api-key':
                options.apiKey = args[++i];
                break;
            case '--password':
                options.password = args[++i];
                break;
            case '--port':
                options.port = parseInt(args[++i], 10);
                break;
            case '--force':
                options.force = true;
                break;
            case '--dry-run':
                options.dryRun = true;
                break;
            case '--help':
            case '-h':
                options.help = true;
                break;
        }
    }

    // Generate API key if not provided
    if (!options.apiKey) {
        options.apiKey = generateApiKey();
    }

    return options;
}

/**
 * Print help message
 */
function printHelp() {
    console.log(`
Redis Initialization CLI Tool

Initializes Redis with default configuration for fresh deployments.

Usage:
  node src/cli/init-redis.js [options]
  npm run init:redis -- [options]

Options:
  --redis-url <url>     Redis connection URL (default: redis://localhost:6379)
  --key-prefix <prefix> Redis key prefix (default: aiclient:)
  --api-key <key>       API key for authentication (default: randomly generated)
  --password <pwd>      Web UI password (default: admin123)
  --port <port>         Server port (default: 3000)
  --force               Overwrite existing data
  --dry-run             Show what would be created without making changes
  --help, -h            Show this help message

Examples:
  # Initialize with defaults
  npm run init:redis

  # Initialize with custom API key
  npm run init:redis -- --api-key my-secret-key

  # Dry run to see what would be created
  npm run init:redis -- --dry-run

  # Force overwrite existing data
  npm run init:redis -- --force
`);
}

/**
 * Get default configuration
 */
function getDefaultConfig(options) {
    return {
        REQUIRED_API_KEY: options.apiKey,
        SERVER_PORT: options.port,
        HOST: '0.0.0.0',
        MODEL_PROVIDER: 'gemini-cli-oauth',
        SYSTEM_PROMPT_FILE_PATH: 'configs/input_system_prompt.txt',
        SYSTEM_PROMPT_MODE: 'append',
        PROMPT_LOG_BASE_NAME: 'prompt_log',
        PROMPT_LOG_MODE: 'none',
        REQUEST_MAX_RETRIES: 3,
        REQUEST_BASE_DELAY: 1000,
        CREDENTIAL_SWITCH_MAX_RETRIES: 5,
        CRON_NEAR_MINUTES: 15,
        CRON_REFRESH_TOKEN: false,
        PROVIDER_POOLS_FILE_PATH: 'configs/provider_pools.json',
        MAX_ERROR_COUNT: 10,
        WARMUP_TARGET: 0,
        REFRESH_TOKENS_AT_STARTUP: true,
        providerFallbackChain: {},
        modelFallbackMapping: {}
    };
}

/**
 * Get default plugins configuration
 */
function getDefaultPlugins() {
    return {
        plugins: {
            'api-potluck': {
                enabled: true,
                description: 'API 大锅饭 - Key 管理和用量统计插件'
            },
            'default-auth': {
                enabled: true,
                description: '默认 API Key 认证插件'
            }
        }
    };
}

/**
 * Check if Redis has existing data
 */
async function checkExistingData(redis, keyPrefix) {
    const keys = await redis.keys(`${keyPrefix}*`);
    return keys.length > 0;
}

/**
 * Initialize Redis with default configuration
 */
async function initializeRedis(redis, options) {
    const keyPrefix = options.keyPrefix;
    const results = {
        config: false,
        plugins: false,
        password: false,
        meta: false
    };

    // Initialize config
    const configKey = `${keyPrefix}config`;
    const configExists = await redis.exists(configKey);

    if (configExists && !options.force) {
        log(`  Skipping config (already exists, use --force to overwrite)`, 'yellow');
    } else {
        const defaultConfig = getDefaultConfig(options);
        if (options.dryRun) {
            log(`  [DRY RUN] Would create default config`, 'cyan');
            log(`    API Key: ${options.apiKey}`, 'dim');
            log(`    Port: ${options.port}`, 'dim');
        } else {
            await redis.set(configKey, JSON.stringify(defaultConfig));
            log(`  Created default config`, 'green');
            log(`    API Key: ${options.apiKey}`, 'dim');
        }
        results.config = true;
    }

    // Initialize plugins
    const pluginsKey = `${keyPrefix}plugins`;
    const pluginsExists = await redis.exists(pluginsKey);

    if (pluginsExists && !options.force) {
        log(`  Skipping plugins (already exists, use --force to overwrite)`, 'yellow');
    } else {
        const defaultPlugins = getDefaultPlugins();
        if (options.dryRun) {
            log(`  [DRY RUN] Would create default plugins config`, 'cyan');
        } else {
            await redis.set(pluginsKey, JSON.stringify(defaultPlugins));
            log(`  Created default plugins config`, 'green');
        }
        results.plugins = true;
    }

    // Initialize password
    const pwdKey = `${keyPrefix}pwd`;
    const pwdExists = await redis.exists(pwdKey);

    if (pwdExists && !options.force) {
        log(`  Skipping password (already exists, use --force to overwrite)`, 'yellow');
    } else {
        if (options.dryRun) {
            log(`  [DRY RUN] Would create web UI password`, 'cyan');
            log(`    Password: ${options.password}`, 'dim');
        } else {
            await redis.set(pwdKey, options.password);
            log(`  Created web UI password`, 'green');
            log(`    Password: ${options.password}`, 'dim');
        }
        results.password = true;
    }

    // Initialize metadata
    const metaKey = `${keyPrefix}meta`;
    if (options.dryRun) {
        log(`  [DRY RUN] Would create initialization metadata`, 'cyan');
    } else {
        await redis.hset(metaKey, {
            version: '1.0',
            initializedAt: new Date().toISOString(),
            initializedFrom: 'cli-init'
        });
        log(`  Created initialization metadata`, 'green');
    }
    results.meta = true;

    return results;
}

/**
 * Main function
 */
async function main() {
    const args = process.argv.slice(2);
    const options = parseArgs(args);

    if (options.help) {
        printHelp();
        process.exit(0);
    }

    log('\n=== Redis Initialization Tool ===\n', 'cyan');
    log(`Redis URL: ${options.redisUrl}`);
    log(`Key prefix: ${options.keyPrefix}`);
    log(`Server port: ${options.port}`);
    if (options.dryRun) log('Mode: DRY RUN (no changes will be made)', 'yellow');
    if (options.force) log('Mode: FORCE (will overwrite existing data)', 'yellow');

    // Connect to Redis
    let redis;
    try {
        redis = new Redis(options.redisUrl, {
            lazyConnect: true,
            connectTimeout: 5000
        });
        await redis.connect();
        log('\nConnected to Redis', 'green');
    } catch (error) {
        log(`\nFailed to connect to Redis: ${error.message}`, 'red');
        process.exit(1);
    }

    try {
        // Check for existing data
        const hasExisting = await checkExistingData(redis, options.keyPrefix);
        if (hasExisting && !options.force) {
            log('\nRedis already contains data with this prefix.', 'yellow');
            log('Use --force to overwrite existing data, or choose a different --key-prefix.', 'yellow');

            // Still show what would be created
            log('\n--- Initialization Preview ---', 'cyan');
            await initializeRedis(redis, { ...options, dryRun: true });

            log('\nNo changes made. Use --force to proceed.', 'yellow');
            process.exit(0);
        }

        // Initialize Redis
        log('\n--- Initializing Redis ---', 'cyan');
        const results = await initializeRedis(redis, options);

        // Summary
        log('\n=== Initialization Summary ===', 'cyan');
        log(`Config: ${results.config ? 'created' : 'skipped'}`);
        log(`Plugins: ${results.plugins ? 'created' : 'skipped'}`);
        log(`Password: ${results.password ? 'created' : 'skipped'}`);
        log(`Metadata: ${results.meta ? 'created' : 'skipped'}`);

        if (options.dryRun) {
            log('\nDry run complete. No changes were made.', 'yellow');
        } else {
            log('\nInitialization complete!', 'green');
            log('\nYou can now start the service with:', 'cyan');
            log(`  REDIS_ENABLED=true REDIS_URL=${options.redisUrl} npm start`);
            log('\nOr set these in your docker-compose.yml environment.', 'dim');
        }

    } catch (error) {
        log(`\nInitialization failed: ${error.message}`, 'red');
        console.error(error);
        process.exit(1);
    } finally {
        await redis.quit();
    }
}

main().catch(console.error);
