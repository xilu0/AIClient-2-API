#!/usr/bin/env node
/**
 * CLI Export Tool: Redis to File-based config
 *
 * Exports configuration data from Redis back to file-based storage:
 * - Redis hash sets -> provider_pools.json
 * - Redis key -> config.json
 * - Redis key -> pwd file
 * - Redis keys -> Token files
 *
 * Usage:
 *   node src/cli/export-from-redis.js [options]
 *
 * Options:
 *   --output-dir <path>  Output directory (default: ./configs)
 *   --redis-url <url>    Redis connection URL (default: redis://localhost:6379)
 *   --key-prefix <prefix> Redis key prefix (default: aiclient:)
 *   --dry-run            Show what would be exported without making changes
 *   --force              Overwrite existing files
 *   --help               Show help
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
        outputDir: './configs',
        redisUrl: process.env.REDIS_URL || 'redis://localhost:6379',
        keyPrefix: 'aiclient:',
        dryRun: false,
        force: false,
        help: false
    };

    for (let i = 0; i < args.length; i++) {
        switch (args[i]) {
            case '--output-dir':
                options.outputDir = args[++i];
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
${colors.cyan}Redis Export Tool${colors.reset}
Export Redis configuration back to file-based storage.

${colors.yellow}Usage:${colors.reset}
  node src/cli/export-from-redis.js [options]

${colors.yellow}Options:${colors.reset}
  --output-dir <path>  Output directory (default: ./configs)
  --redis-url <url>    Redis connection URL (default: redis://localhost:6379)
  --key-prefix <prefix> Redis key prefix (default: aiclient:)
  --dry-run            Show what would be exported without making changes
  --force              Overwrite existing files
  --help, -h           Show this help message

${colors.yellow}Environment Variables:${colors.reset}
  REDIS_URL            Redis connection URL (alternative to --redis-url)

${colors.yellow}Examples:${colors.reset}
  # Dry run to see what would be exported
  node src/cli/export-from-redis.js --dry-run

  # Export to custom directory
  node src/cli/export-from-redis.js --output-dir /path/to/backup

  # Force overwrite existing files
  node src/cli/export-from-redis.js --force
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
 * Ensure directory exists
 */
async function ensureDir(dirPath) {
    try {
        await fs.mkdir(dirPath, { recursive: true });
    } catch (error) {
        if (error.code !== 'EEXIST') throw error;
    }
}

/**
 * Write JSON file with formatting
 */
async function writeJsonFile(filePath, data, options) {
    if (options.dryRun) {
        log(`  [DRY RUN] Would write ${filePath}`, 'cyan');
        return true;
    }

    await ensureDir(path.dirname(filePath));
    await fs.writeFile(filePath, JSON.stringify(data, null, 2), 'utf-8');
    return true;
}

/**
 * Export provider pools from Redis
 */
async function exportProviderPools(redis, outputDir, keyPrefix, options) {
    const pattern = `${keyPrefix}pools:*`;
    const keys = await redis.keys(pattern);

    if (keys.length === 0) {
        log('  No provider pools found in Redis', 'dim');
        return { exported: false, providers: 0 };
    }

    const outputFile = path.join(outputDir, 'provider_pools.json');

    if (await fileExists(outputFile) && !options.force) {
        log(`  Skipping provider_pools.json (already exists, use --force to overwrite)`, 'yellow');
        return { exported: false, skipped: true, providers: 0 };
    }

    const pools = {};
    let totalProviders = 0;

    for (const key of keys) {
        const providerType = key.replace(`${keyPrefix}pools:`, '');
        const providers = await redis.hgetall(key);

        pools[providerType] = Object.values(providers).map(p => JSON.parse(p));
        totalProviders += pools[providerType].length;
        log(`  Found ${pools[providerType].length} providers for ${providerType}`, 'dim');
    }

    if (options.dryRun) {
        log(`  [DRY RUN] Would export ${totalProviders} providers to provider_pools.json`, 'cyan');
        return { exported: true, providers: totalProviders };
    }

    await writeJsonFile(outputFile, pools, options);
    log(`  Exported ${totalProviders} providers to provider_pools.json`, 'green');
    return { exported: true, providers: totalProviders };
}

/**
 * Export config from Redis
 */
async function exportConfig(redis, outputDir, keyPrefix, options) {
    const key = `${keyPrefix}config`;
    const configData = await redis.get(key);

    if (!configData) {
        log('  No config found in Redis', 'dim');
        return { exported: false };
    }

    const outputFile = path.join(outputDir, 'config.json');

    if (await fileExists(outputFile) && !options.force) {
        log(`  Skipping config.json (already exists, use --force to overwrite)`, 'yellow');
        return { exported: false, skipped: true };
    }

    const config = JSON.parse(configData);

    if (options.dryRun) {
        log(`  [DRY RUN] Would export config.json`, 'cyan');
        return { exported: true };
    }

    await writeJsonFile(outputFile, config, options);
    log(`  Exported config.json`, 'green');
    return { exported: true };
}

/**
 * Export password from Redis
 */
async function exportPassword(redis, outputDir, keyPrefix, options) {
    const key = `${keyPrefix}pwd`;
    const password = await redis.get(key);

    if (!password) {
        log('  No password found in Redis', 'dim');
        return { exported: false };
    }

    const outputFile = path.join(outputDir, 'pwd');

    if (await fileExists(outputFile) && !options.force) {
        log(`  Skipping pwd (already exists, use --force to overwrite)`, 'yellow');
        return { exported: false, skipped: true };
    }

    if (options.dryRun) {
        log(`  [DRY RUN] Would export pwd file`, 'cyan');
        return { exported: true };
    }

    await ensureDir(outputDir);
    await fs.writeFile(outputFile, password, 'utf-8');
    log(`  Exported pwd file`, 'green');
    return { exported: true };
}

/**
 * Export token files from Redis
 */
async function exportTokens(redis, outputDir, keyPrefix, options) {
    const pattern = `${keyPrefix}tokens:*`;
    const keys = await redis.keys(pattern);

    if (keys.length === 0) {
        log('  No tokens found in Redis', 'dim');
        return { exported: 0, skipped: 0 };
    }

    let exported = 0;
    let skipped = 0;

    // Provider type to directory mapping
    const providerDirs = {
        'gemini-cli-oauth': path.join(outputDir, 'gemini'),
        'openai-qwen-oauth': path.join(outputDir, 'qwen'),
        'openai-iflow': path.join(outputDir, 'iflow'),
        'openai-codex': path.join(outputDir, 'codex'),
        'claude-kiro-oauth': path.join(outputDir, 'kiro')
    };

    for (const key of keys) {
        // Parse key: aiclient:tokens:provider-type:uuid
        const keyParts = key.replace(`${keyPrefix}tokens:`, '').split(':');
        const providerType = keyParts[0];
        const uuid = keyParts.slice(1).join(':') || 'default';

        const tokenDir = providerDirs[providerType] || path.join(outputDir, providerType);
        const fileName = `${providerType.replace('openai-', '').replace('claude-', '').replace('-oauth', '')}-${uuid}.json`;
        const outputFile = path.join(tokenDir, fileName);

        if (await fileExists(outputFile) && !options.force) {
            log(`  Skipping ${fileName} (already exists)`, 'yellow');
            skipped++;
            continue;
        }

        try {
            const tokenData = await redis.get(key);
            if (!tokenData) continue;

            const token = JSON.parse(tokenData);

            if (options.dryRun) {
                log(`  [DRY RUN] Would export ${providerType}:${uuid} -> ${fileName}`, 'cyan');
                exported++;
                continue;
            }

            await ensureDir(tokenDir);
            await fs.writeFile(outputFile, JSON.stringify(token, null, 2), 'utf-8');
            log(`  Exported ${providerType}:${uuid} -> ${fileName}`, 'green');
            exported++;
        } catch (error) {
            log(`  Failed to export ${key}: ${error.message}`, 'red');
        }
    }

    return { exported, skipped };
}

/**
 * Show Redis metadata
 */
async function showMetadata(redis, keyPrefix) {
    const metaKey = `${keyPrefix}meta`;
    const meta = await redis.hgetall(metaKey);

    if (Object.keys(meta).length > 0) {
        log('\n--- Redis Metadata ---', 'cyan');
        for (const [key, value] of Object.entries(meta)) {
            log(`  ${key}: ${value}`, 'dim');
        }
    }
}

/**
 * Main export function
 */
async function main() {
    const args = process.argv.slice(2);
    const options = parseArgs(args);

    if (options.help) {
        printHelp();
        process.exit(0);
    }

    log('\n=== Redis Export Tool ===\n', 'cyan');
    log(`Output directory: ${options.outputDir}`);
    log(`Redis URL: ${options.redisUrl}`);
    log(`Key prefix: ${options.keyPrefix}`);
    if (options.dryRun) log('Mode: DRY RUN (no changes will be made)', 'yellow');
    if (options.force) log('Mode: FORCE (will overwrite existing files)', 'yellow');

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
        providerPools: { exported: false, providers: 0 },
        config: { exported: false },
        password: { exported: false },
        tokens: { exported: 0, skipped: 0 }
    };

    try {
        // Show metadata
        await showMetadata(redis, options.keyPrefix);

        // Export provider pools
        log('\n--- Provider Pools ---', 'cyan');
        stats.providerPools = await exportProviderPools(redis, options.outputDir, options.keyPrefix, options);

        // Export config
        log('\n--- Configuration ---', 'cyan');
        stats.config = await exportConfig(redis, options.outputDir, options.keyPrefix, options);

        // Export password
        log('\n--- Password ---', 'cyan');
        stats.password = await exportPassword(redis, options.outputDir, options.keyPrefix, options);

        // Export tokens
        log('\n--- Token Files ---', 'cyan');
        stats.tokens = await exportTokens(redis, options.outputDir, options.keyPrefix, options);

        // Summary
        log('\n=== Export Summary ===', 'cyan');
        log(`Provider pools: ${stats.providerPools.exported ? `${stats.providerPools.providers} providers exported` : (stats.providerPools.skipped ? 'skipped' : 'not found')}`);
        log(`Config: ${stats.config.exported ? 'exported' : (stats.config.skipped ? 'skipped' : 'not found')}`);
        log(`Password: ${stats.password.exported ? 'exported' : (stats.password.skipped ? 'skipped' : 'not found')}`);
        log(`Tokens: ${stats.tokens.exported} exported, ${stats.tokens.skipped} skipped`);

        if (options.dryRun) {
            log('\nDry run complete. No changes were made.', 'yellow');
        } else {
            log('\nExport complete!', 'green');
        }

    } catch (error) {
        log(`\nExport failed: ${error.message}`, 'red');
        console.error(error);
        process.exit(1);
    } finally {
        await redis.quit();
    }
}

main().catch(console.error);
