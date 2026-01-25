#!/usr/bin/env node
/**
 * CLI Migration Tool: File-based config to Redis
 *
 * Migrates configuration data from file-based storage to Redis:
 * - provider_pools.json -> Redis hash sets
 * - config.json -> Redis key
 * - pwd file -> Redis key
 * - Token files -> Redis keys with TTL
 *
 * Usage:
 *   node src/cli/migrate-to-redis.js [options]
 *
 * Options:
 *   --config-dir <path>   Config directory (default: ./configs)
 *   --redis-url <url>     Redis connection URL (default: redis://localhost:6379)
 *   --key-prefix <prefix> Redis key prefix (default: aiclient:)
 *   --dry-run             Show what would be migrated without making changes
 *   --force               Overwrite existing Redis data
 *   --verify              Verify migration after completion
 *   --help                Show help
 */

import Redis from 'ioredis';
import { promises as fs } from 'fs';
import path from 'path';
import os from 'os';

// ANSI color codes for output
const colors = {
    reset: '\x1b[0m',
    green: '\x1b[32m',
    yellow: '\x1b[33m',
    red: '\x1b[31m',
    cyan: '\x1b[36m',
    dim: '\x1b[2m'
};

/**
 * Parse command line arguments
 */
function parseArgs(args) {
    const options = {
        configDir: './configs',
        redisUrl: process.env.REDIS_URL || 'redis://localhost:6379',
        keyPrefix: 'aiclient:',
        dryRun: false,
        force: false,
        verify: false,
        help: false
    };

    for (let i = 0; i < args.length; i++) {
        switch (args[i]) {
            case '--config-dir':
                options.configDir = args[++i];
                break;
            case '--redis-url':
                options.redisUrl = args[++i];
                break;
            case '--key-prefix':
                options.keyPrefix = args[++i];
                break;
            case '--dry-run':
                options.dryRun = true;
                break;
            case '--force':
                options.force = true;
                break;
            case '--verify':
                options.verify = true;
                break;
            case '--help':
            case '-h':
                options.help = true;
                break;
        }
    }

    return options;
}

/**
 * Print help message
 */
function printHelp() {
    console.log(`
${colors.cyan}Redis Migration Tool${colors.reset}
Migrate file-based configuration to Redis storage.

${colors.yellow}Usage:${colors.reset}
  node src/cli/migrate-to-redis.js [options]

${colors.yellow}Options:${colors.reset}
  --config-dir <path>   Config directory (default: ./configs)
  --redis-url <url>     Redis connection URL (default: redis://localhost:6379)
  --key-prefix <prefix> Redis key prefix (default: aiclient:)
  --dry-run             Show what would be migrated without making changes
  --force               Overwrite existing Redis data
  --verify              Verify migration after completion
  --help, -h            Show this help message

${colors.yellow}Environment Variables:${colors.reset}
  REDIS_URL             Redis connection URL (alternative to --redis-url)

${colors.yellow}Examples:${colors.reset}
  # Dry run to see what would be migrated
  node src/cli/migrate-to-redis.js --dry-run

  # Migrate with verification
  node src/cli/migrate-to-redis.js --verify

  # Force overwrite existing data
  node src/cli/migrate-to-redis.js --force

  # Custom config directory and Redis URL
  node src/cli/migrate-to-redis.js --config-dir /path/to/configs --redis-url redis://redis-server:6379
`);
}

/**
 * Log with color
 */
function log(message, color = 'reset') {
    console.log(`${colors[color]}${message}${colors.reset}`);
}

/**
 * Check if file exists
 */
async function fileExists(filePath) {
    try {
        await fs.access(filePath);
        return true;
    } catch {
        return false;
    }
}

/**
 * Read JSON file
 */
async function readJsonFile(filePath) {
    const content = await fs.readFile(filePath, 'utf-8');
    return JSON.parse(content);
}

/**
 * Scan directory for token files
 */
async function scanTokenFiles(configDir) {
    const tokenFiles = [];

    // Known token file patterns and locations
    const tokenPatterns = [
        { dir: path.join(os.homedir(), '.gemini'), pattern: /oauth_creds\.json$/, provider: 'gemini-cli-oauth' },
        { dir: path.join(os.homedir(), '.qwen'), pattern: /oauth_creds\.json$/, provider: 'openai-qwen-oauth' },
        { dir: path.join(os.homedir(), '.iflow'), pattern: /oauth_creds\.json$/, provider: 'openai-iflow' },
        { dir: path.join(configDir, 'codex'), pattern: /codex-.*\.json$/, provider: 'openai-codex' },
        { dir: path.join(configDir, 'kiro'), pattern: /.*kiro.*\.json$/, provider: 'claude-kiro-oauth' },
        { dir: path.join(configDir, 'gemini'), pattern: /.*\.json$/, provider: 'gemini-cli-oauth' }
    ];

    for (const { dir, pattern, provider } of tokenPatterns) {
        try {
            if (await fileExists(dir)) {
                const files = await fs.readdir(dir);
                for (const file of files) {
                    const filePath = path.join(dir, file);
                    const stat = await fs.stat(filePath);

                    if (stat.isDirectory()) {
                        // Scan subdirectories for Kiro tokens (kiro/{uuid}_kiro-auth-token/{uuid}_kiro-auth-token.json)
                        const subFiles = await fs.readdir(filePath);
                        for (const subFile of subFiles) {
                            if (pattern.test(subFile)) {
                                const subFilePath = path.join(filePath, subFile);
                                // Extract UUID from directory name (e.g., 1769176045109_kiro-auth-token -> 1769176045109)
                                const uuid = file.split('_')[0] || subFile.replace(/\.json$/, '');
                                tokenFiles.push({ filePath: subFilePath, provider, uuid, fileName: subFile });
                            }
                        }
                    } else if (pattern.test(file)) {
                        // Direct file match
                        const uuid = file.replace(/\.json$/, '').replace(/^(kiro|codex|gemini)-/, '') || 'default';
                        tokenFiles.push({ filePath, provider, uuid, fileName: file });
                    }
                }
            }
        } catch (error) {
            // Directory doesn't exist or can't be read, skip
        }
    }

    return tokenFiles;
}

/**
 * Migrate provider pools to Redis
 */
async function migrateProviderPools(redis, configDir, keyPrefix, options) {
    const poolsFile = path.join(configDir, 'provider_pools.json');

    if (!await fileExists(poolsFile)) {
        log('  No provider_pools.json found, skipping', 'dim');
        return { migrated: 0, skipped: 0 };
    }

    const pools = await readJsonFile(poolsFile);
    let migrated = 0;
    let skipped = 0;

    for (const [providerType, providers] of Object.entries(pools)) {
        if (!Array.isArray(providers) || providers.length === 0) continue;

        const key = `${keyPrefix}pools:${providerType}`;

        // Check if key exists
        const exists = await redis.exists(key);
        if (exists && !options.force) {
            log(`  Skipping ${providerType} (already exists, use --force to overwrite)`, 'yellow');
            skipped += providers.length;
            continue;
        }

        if (options.dryRun) {
            log(`  [DRY RUN] Would migrate ${providers.length} providers for ${providerType}`, 'cyan');
            migrated += providers.length;
            continue;
        }

        // Delete existing and set new
        await redis.del(key);
        for (const provider of providers) {
            await redis.hset(key, provider.uuid, JSON.stringify(provider));
            migrated++;
        }
        log(`  Migrated ${providers.length} providers for ${providerType}`, 'green');
    }

    return { migrated, skipped };
}

/**
 * Migrate config.json to Redis
 */
async function migrateConfig(redis, configDir, keyPrefix, options) {
    const configFile = path.join(configDir, 'config.json');

    if (!await fileExists(configFile)) {
        log('  No config.json found, skipping', 'dim');
        return { migrated: false, skipped: false };
    }

    const key = `${keyPrefix}config`;
    const exists = await redis.exists(key);

    if (exists && !options.force) {
        log('  Skipping config.json (already exists, use --force to overwrite)', 'yellow');
        return { migrated: false, skipped: true };
    }

    const config = await readJsonFile(configFile);

    if (options.dryRun) {
        log('  [DRY RUN] Would migrate config.json', 'cyan');
        return { migrated: true, skipped: false };
    }

    await redis.set(key, JSON.stringify(config));
    log('  Migrated config.json', 'green');
    return { migrated: true, skipped: false };
}

/**
 * Migrate pwd file to Redis
 */
async function migratePassword(redis, configDir, keyPrefix, options) {
    const pwdFile = path.join(configDir, 'pwd');

    if (!await fileExists(pwdFile)) {
        log('  No pwd file found, skipping', 'dim');
        return { migrated: false, skipped: false };
    }

    const key = `${keyPrefix}pwd`;
    const exists = await redis.exists(key);

    if (exists && !options.force) {
        log('  Skipping pwd (already exists, use --force to overwrite)', 'yellow');
        return { migrated: false, skipped: true };
    }

    const password = (await fs.readFile(pwdFile, 'utf-8')).trim();

    if (options.dryRun) {
        log('  [DRY RUN] Would migrate pwd file', 'cyan');
        return { migrated: true, skipped: false };
    }

    await redis.set(key, password);
    log('  Migrated pwd file', 'green');
    return { migrated: true, skipped: false };
}

/**
 * Migrate token files to Redis (legacy method - scans directories)
 */
async function migrateTokens(redis, configDir, keyPrefix, options) {
    const tokenFiles = await scanTokenFiles(configDir);
    let migrated = 0;
    let skipped = 0;

    if (tokenFiles.length === 0) {
        log('  No token files found', 'dim');
        return { migrated: 0, skipped: 0 };
    }

    for (const { filePath, provider, uuid, fileName } of tokenFiles) {
        const key = `${keyPrefix}tokens:${provider}:${uuid}`;
        const exists = await redis.exists(key);

        if (exists && !options.force) {
            log(`  Skipping ${fileName} (already exists)`, 'yellow');
            skipped++;
            continue;
        }

        try {
            const tokenData = await readJsonFile(filePath);

            if (options.dryRun) {
                log(`  [DRY RUN] Would migrate ${fileName} -> ${provider}:${uuid}`, 'cyan');
                migrated++;
                continue;
            }

            // Calculate TTL based on expiry if available
            let ttl = null;
            const expiryField = tokenData.expiry_date || tokenData.expiryDate || tokenData.expired;
            if (expiryField) {
                const expiryMs = typeof expiryField === 'number'
                    ? expiryField
                    : new Date(expiryField).getTime();
                const remainingMs = expiryMs - Date.now();
                if (remainingMs > 0) {
                    ttl = Math.floor(remainingMs / 1000) + 3600; // Add 1 hour buffer
                }
            }

            if (ttl && ttl > 0) {
                await redis.setex(key, ttl, JSON.stringify(tokenData));
            } else {
                await redis.set(key, JSON.stringify(tokenData));
            }

            log(`  Migrated ${fileName} -> ${provider}:${uuid}`, 'green');
            migrated++;
        } catch (error) {
            log(`  Failed to migrate ${fileName}: ${error.message}`, 'red');
        }
    }

    return { migrated, skipped };
}

/**
 * Migrate provider tokens using UUID from provider pools
 * This links token files to their provider UUIDs correctly
 */
async function migrateProviderTokens(redis, configDir, keyPrefix, options) {
    const poolsFile = path.join(configDir, 'provider_pools.json');

    if (!await fileExists(poolsFile)) {
        log('  No provider_pools.json found, skipping', 'dim');
        return { migrated: 0, skipped: 0, errors: 0 };
    }

    const pools = await readJsonFile(poolsFile);
    let migrated = 0;
    let skipped = 0;
    let errors = 0;

    // Token credential path keys for different providers
    const credPathKeys = {
        'claude-kiro-oauth': 'KIRO_OAUTH_CREDS_FILE_PATH',
        'gemini-cli-oauth': 'GEMINI_OAUTH_CREDS_FILE_PATH',
        'openai-qwen-oauth': 'QWEN_OAUTH_CREDS_FILE_PATH',
        'openai-iflow': 'IFLOW_OAUTH_CREDS_FILE_PATH',
        'openai-codex': 'CODEX_OAUTH_CREDS_FILE_PATH',
        'gemini-antigravity': 'GEMINI_ANTIGRAVITY_CREDS_FILE_PATH'
    };

    for (const [providerType, providers] of Object.entries(pools)) {
        if (!Array.isArray(providers) || providers.length === 0) continue;

        const credPathKey = credPathKeys[providerType];
        if (!credPathKey) continue;

        for (const provider of providers) {
            const credPath = provider[credPathKey];
            const providerUuid = provider.uuid;

            if (!credPath || !providerUuid) continue;

            const key = `${keyPrefix}tokens:${providerType}:${providerUuid}`;
            const exists = await redis.exists(key);

            if (exists && !options.force) {
                log(`  Skipping ${providerType}:${providerUuid} (token already exists)`, 'yellow');
                skipped++;
                continue;
            }

            // Resolve the credential file path
            let resolvedPath = credPath;
            if (credPath.startsWith('./')) {
                resolvedPath = path.join(configDir, '..', credPath.substring(2));
            } else if (!path.isAbsolute(credPath)) {
                resolvedPath = path.join(configDir, credPath);
            }

            if (!await fileExists(resolvedPath)) {
                log(`  Token file not found: ${credPath} for ${providerUuid}`, 'red');
                errors++;
                continue;
            }

            try {
                const tokenData = await readJsonFile(resolvedPath);

                if (options.dryRun) {
                    log(`  [DRY RUN] Would migrate token for ${providerType}:${providerUuid}`, 'cyan');
                    migrated++;
                    continue;
                }

                // Calculate TTL based on expiry if available
                let ttl = null;
                const expiryField = tokenData.expiry_date || tokenData.expiryDate || tokenData.expired;
                if (expiryField) {
                    const expiryMs = typeof expiryField === 'number'
                        ? expiryField
                        : new Date(expiryField).getTime();
                    const remainingMs = expiryMs - Date.now();
                    if (remainingMs > 0) {
                        ttl = Math.floor(remainingMs / 1000) + 3600; // Add 1 hour buffer
                    }
                }

                if (ttl && ttl > 0) {
                    await redis.setex(key, ttl, JSON.stringify(tokenData));
                } else {
                    await redis.set(key, JSON.stringify(tokenData));
                }

                log(`  Migrated token for ${providerType}:${providerUuid}`, 'green');
                migrated++;
            } catch (error) {
                log(`  Failed to migrate token for ${providerUuid}: ${error.message}`, 'red');
                errors++;
            }
        }
    }

    return { migrated, skipped, errors };
}

/**
 * Record migration metadata
 */
async function recordMetadata(redis, keyPrefix, options, stats) {
    if (options.dryRun) {
        log('\n[DRY RUN] Would record migration metadata', 'cyan');
        return;
    }

    const metaKey = `${keyPrefix}meta`;
    await redis.hset(metaKey, {
        version: '1.0',
        migratedAt: new Date().toISOString(),
        migratedFrom: 'file',
        providerPoolsMigrated: stats.providerPools.migrated,
        tokensMigrated: stats.tokens.migrated,
        configMigrated: stats.config.migrated ? 'true' : 'false',
        passwordMigrated: stats.password.migrated ? 'true' : 'false'
    });

    log('\nRecorded migration metadata', 'green');
}

/**
 * Verify migration
 */
async function verifyMigration(redis, configDir, keyPrefix) {
    log('\n--- Verification ---', 'cyan');
    let errors = 0;

    // Verify provider pools
    const poolsFile = path.join(configDir, 'provider_pools.json');
    if (await fileExists(poolsFile)) {
        const filePools = await readJsonFile(poolsFile);
        for (const [providerType, providers] of Object.entries(filePools)) {
            if (!Array.isArray(providers)) continue;

            const key = `${keyPrefix}pools:${providerType}`;
            const redisProviders = await redis.hgetall(key);
            const redisCount = Object.keys(redisProviders).length;

            if (redisCount !== providers.length) {
                log(`  MISMATCH: ${providerType} - file: ${providers.length}, redis: ${redisCount}`, 'red');
                errors++;
            } else {
                log(`  OK: ${providerType} - ${providers.length} providers`, 'green');
            }
        }
    }

    // Verify config
    const configFile = path.join(configDir, 'config.json');
    if (await fileExists(configFile)) {
        const key = `${keyPrefix}config`;
        const exists = await redis.exists(key);
        if (exists) {
            log('  OK: config.json migrated', 'green');
        } else {
            log('  MISSING: config.json not in Redis', 'red');
            errors++;
        }
    }

    // Verify password
    const pwdFile = path.join(configDir, 'pwd');
    if (await fileExists(pwdFile)) {
        const key = `${keyPrefix}pwd`;
        const exists = await redis.exists(key);
        if (exists) {
            log('  OK: pwd file migrated', 'green');
        } else {
            log('  MISSING: pwd not in Redis', 'red');
            errors++;
        }
    }

    if (errors === 0) {
        log('\nVerification passed!', 'green');
    } else {
        log(`\nVerification failed with ${errors} error(s)`, 'red');
    }

    return errors === 0;
}

/**
 * Migrate session tokens (token-store.json) to Redis
 */
async function migrateSessionTokens(redis, configDir, keyPrefix, options) {
    const tokenStoreFile = path.join(configDir, 'token-store.json');

    if (!await fileExists(tokenStoreFile)) {
        log('  No token-store.json found, skipping', 'dim');
        return { migrated: 0, skipped: 0 };
    }

    try {
        const tokenStore = await readJsonFile(tokenStoreFile);
        const tokens = tokenStore.tokens || {};
        let migrated = 0;
        let skipped = 0;

        for (const [hash, data] of Object.entries(tokens)) {
            const key = `${keyPrefix}sessions:${hash}`;
            const exists = await redis.exists(key);

            if (exists && !options.force) {
                skipped++;
                continue;
            }

            if (options.dryRun) {
                migrated++;
                continue;
            }

            // Calculate TTL from expiryTime
            let ttl = 3600; // Default 1 hour
            if (data.expiryTime) {
                const remainingMs = data.expiryTime - Date.now();
                if (remainingMs > 0) {
                    ttl = Math.floor(remainingMs / 1000);
                } else {
                    // Skip expired tokens
                    continue;
                }
            }

            await redis.setex(key, ttl, JSON.stringify(data));
            migrated++;
        }

        if (options.dryRun) {
            log(`  [DRY RUN] Would migrate ${migrated} session tokens`, 'cyan');
        } else {
            log(`  Migrated ${migrated} session tokens`, 'green');
        }

        return { migrated, skipped };
    } catch (error) {
        log(`  Error migrating session tokens: ${error.message}`, 'red');
        return { migrated: 0, skipped: 0 };
    }
}

/**
 * Migrate usage cache (usage-cache.json) to Redis
 */
async function migrateUsageCache(redis, configDir, keyPrefix, options) {
    const usageCacheFile = path.join(configDir, 'usage-cache.json');

    if (!await fileExists(usageCacheFile)) {
        log('  No usage-cache.json found, skipping', 'dim');
        return { migrated: false, skipped: false };
    }

    const key = `${keyPrefix}usage:cache`;
    const exists = await redis.exists(key);

    if (exists && !options.force) {
        log('  Skipping usage-cache.json (already exists, use --force to overwrite)', 'yellow');
        return { migrated: false, skipped: true };
    }

    try {
        const usageCache = await readJsonFile(usageCacheFile);

        if (options.dryRun) {
            log('  [DRY RUN] Would migrate usage-cache.json', 'cyan');
            return { migrated: true, skipped: false };
        }

        await redis.set(key, JSON.stringify(usageCache));
        log('  Migrated usage-cache.json', 'green');
        return { migrated: true, skipped: false };
    } catch (error) {
        log(`  Error migrating usage cache: ${error.message}`, 'red');
        return { migrated: false, skipped: false };
    }
}

/**
 * Migrate plugins config (plugins.json) to Redis
 */
async function migratePlugins(redis, configDir, keyPrefix, options) {
    const pluginsFile = path.join(configDir, 'plugins.json');

    if (!await fileExists(pluginsFile)) {
        log('  No plugins.json found, skipping', 'dim');
        return { migrated: false, skipped: false };
    }

    const key = `${keyPrefix}plugins`;
    const exists = await redis.exists(key);

    if (exists && !options.force) {
        log('  Skipping plugins.json (already exists, use --force to overwrite)', 'yellow');
        return { migrated: false, skipped: true };
    }

    try {
        const pluginsConfig = await readJsonFile(pluginsFile);

        if (options.dryRun) {
            log('  [DRY RUN] Would migrate plugins.json', 'cyan');
            return { migrated: true, skipped: false };
        }

        await redis.set(key, JSON.stringify(pluginsConfig));
        log('  Migrated plugins.json', 'green');
        return { migrated: true, skipped: false };
    } catch (error) {
        log(`  Error migrating plugins: ${error.message}`, 'red');
        return { migrated: false, skipped: false };
    }
}

/**
 * Main migration function
 */
async function main() {
    const args = process.argv.slice(2);
    const options = parseArgs(args);

    if (options.help) {
        printHelp();
        process.exit(0);
    }

    log('\n=== Redis Migration Tool ===\n', 'cyan');
    log(`Config directory: ${options.configDir}`);
    log(`Redis URL: ${options.redisUrl}`);
    log(`Key prefix: ${options.keyPrefix}`);
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

    const stats = {
        providerPools: { migrated: 0, skipped: 0 },
        config: { migrated: false, skipped: false },
        password: { migrated: false, skipped: false },
        tokens: { migrated: 0, skipped: 0 },
        providerTokens: { migrated: 0, skipped: 0, errors: 0 },
        sessions: { migrated: 0, skipped: 0 },
        usageCache: { migrated: false, skipped: false },
        plugins: { migrated: false, skipped: false }
    };

    try {
        // Migrate provider pools
        log('\n--- Provider Pools ---', 'cyan');
        stats.providerPools = await migrateProviderPools(redis, options.configDir, options.keyPrefix, options);

        // Migrate config
        log('\n--- Configuration ---', 'cyan');
        stats.config = await migrateConfig(redis, options.configDir, options.keyPrefix, options);

        // Migrate password
        log('\n--- Password ---', 'cyan');
        stats.password = await migratePassword(redis, options.configDir, options.keyPrefix, options);

        // Migrate provider tokens (linked to provider UUIDs)
        log('\n--- Provider Tokens (UUID-linked) ---', 'cyan');
        stats.providerTokens = await migrateProviderTokens(redis, options.configDir, options.keyPrefix, options);

        // Migrate tokens (legacy scan method)
        log('\n--- Token Files (legacy scan) ---', 'cyan');
        stats.tokens = await migrateTokens(redis, options.configDir, options.keyPrefix, options);

        // Migrate session tokens
        log('\n--- Session Tokens ---', 'cyan');
        stats.sessions = await migrateSessionTokens(redis, options.configDir, options.keyPrefix, options);

        // Migrate usage cache
        log('\n--- Usage Cache ---', 'cyan');
        stats.usageCache = await migrateUsageCache(redis, options.configDir, options.keyPrefix, options);

        // Migrate plugins
        log('\n--- Plugins ---', 'cyan');
        stats.plugins = await migratePlugins(redis, options.configDir, options.keyPrefix, options);

        // Record metadata
        await recordMetadata(redis, options.keyPrefix, options, stats);

        // Verify if requested
        if (options.verify && !options.dryRun) {
            await verifyMigration(redis, options.configDir, options.keyPrefix);
        }

        // Summary
        log('\n=== Migration Summary ===', 'cyan');
        log(`Provider pools: ${stats.providerPools.migrated} migrated, ${stats.providerPools.skipped} skipped`);
        log(`Config: ${stats.config.migrated ? 'migrated' : (stats.config.skipped ? 'skipped' : 'not found')}`);
        log(`Password: ${stats.password.migrated ? 'migrated' : (stats.password.skipped ? 'skipped' : 'not found')}`);
        log(`Provider tokens: ${stats.providerTokens.migrated} migrated, ${stats.providerTokens.skipped} skipped, ${stats.providerTokens.errors || 0} errors`);
        log(`Tokens (legacy): ${stats.tokens.migrated} migrated, ${stats.tokens.skipped} skipped`);
        log(`Sessions: ${stats.sessions.migrated} migrated, ${stats.sessions.skipped} skipped`);
        log(`Usage cache: ${stats.usageCache.migrated ? 'migrated' : (stats.usageCache.skipped ? 'skipped' : 'not found')}`);
        log(`Plugins: ${stats.plugins.migrated ? 'migrated' : (stats.plugins.skipped ? 'skipped' : 'not found')}`);

        if (options.dryRun) {
            log('\nDry run complete. No changes were made.', 'yellow');
        } else {
            log('\nMigration complete!', 'green');
        }

    } catch (error) {
        log(`\nMigration failed: ${error.message}`, 'red');
        console.error(error);
        process.exit(1);
    } finally {
        await redis.quit();
    }
}

main().catch(console.error);
