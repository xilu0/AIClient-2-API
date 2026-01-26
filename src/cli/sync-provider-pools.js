#!/usr/bin/env node
/**
 * Sync provider_pools.json with actual token files
 *
 * This script scans the configs directory for actual token files
 * and updates provider_pools.json to match the files on disk.
 *
 * Usage:
 *   node src/cli/sync-provider-pools.js [--dry-run] [--backup]
 */

import { promises as fs } from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';
import { v4 as uuidv4 } from 'uuid';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// ANSI colors
const colors = {
    reset: '\x1b[0m',
    green: '\x1b[32m',
    yellow: '\x1b[33m',
    red: '\x1b[31m',
    cyan: '\x1b[36m',
    dim: '\x1b[2m'
};

function log(message, color = 'reset') {
    console.log(`${colors[color]}${message}${colors.reset}`);
}

// Parse command line arguments
function parseArgs() {
    const args = process.argv.slice(2);
    return {
        dryRun: args.includes('--dry-run'),
        backup: args.includes('--backup'),
        help: args.includes('--help') || args.includes('-h')
    };
}

function printHelp() {
    console.log(`
${colors.cyan}Provider Pools Sync Tool${colors.reset}
Synchronizes provider_pools.json with actual token files in configs directory.

${colors.yellow}Usage:${colors.reset}
  node src/cli/sync-provider-pools.js [options]

${colors.yellow}Options:${colors.reset}
  --dry-run    Show what would be changed without modifying files
  --backup     Create a backup of provider_pools.json before modifying
  --help, -h   Show this help message

${colors.yellow}Examples:${colors.reset}
  # Preview changes
  node src/cli/sync-provider-pools.js --dry-run

  # Sync with backup
  node src/cli/sync-provider-pools.js --backup

  # Just sync
  node src/cli/sync-provider-pools.js
`);
}

/**
 * Scan directory for token files
 */
async function scanTokenFiles(configsDir) {
    const tokenConfigs = {
        'claude-kiro-oauth': {
            dir: path.join(configsDir, 'kiro'),
            pattern: /.*kiro.*\.json$/,
            credPathKey: 'KIRO_OAUTH_CREDS_FILE_PATH',
            checkModelName: 'claude-haiku-4-5'
        },
        'gemini-cli-oauth': {
            dir: path.join(configsDir, 'gemini'),
            pattern: /.*\.json$/,
            credPathKey: 'GEMINI_OAUTH_CREDS_FILE_PATH',
            checkModelName: 'gemini-2.5-flash'
        },
        'openai-qwen-oauth': {
            dir: path.join(configsDir, 'qwen'),
            pattern: /oauth_creds\.json$/,
            credPathKey: 'QWEN_OAUTH_CREDS_FILE_PATH',
            checkModelName: 'qwen3-coder-flash'
        },
        'openai-iflow': {
            dir: path.join(configsDir, 'iflow'),
            pattern: /oauth_creds\.json$/,
            credPathKey: 'IFLOW_OAUTH_CREDS_FILE_PATH',
            checkModelName: 'qwen3-coder-plus'
        },
        'openai-codex-oauth': {
            dir: path.join(configsDir, 'codex'),
            pattern: /codex-.*\.json$/,
            credPathKey: 'CODEX_OAUTH_CREDS_FILE_PATH',
            checkModelName: 'gpt-5-codex-mini'
        },
        'gemini-antigravity': {
            dir: path.join(configsDir, 'antigravity'),
            pattern: /.*\.json$/,
            credPathKey: 'GEMINI_ANTIGRAVITY_CREDS_FILE_PATH',
            checkModelName: 'gemini-2.5-flash'
        }
    };

    const result = {};

    for (const [providerType, config] of Object.entries(tokenConfigs)) {
        result[providerType] = [];

        try {
            // Check if directory exists
            const dirExists = await fs.access(config.dir).then(() => true).catch(() => false);
            if (!dirExists) {
                continue;
            }

            const entries = await fs.readdir(config.dir, { withFileTypes: true });

            for (const entry of entries) {
                if (entry.name === '.DS_Store') continue;

                const fullPath = path.join(config.dir, entry.name);

                if (entry.isDirectory()) {
                    // Scan subdirectory (e.g., kiro/1769414953949_kiro-auth-token/)
                    const subEntries = await fs.readdir(fullPath);
                    for (const subFile of subEntries) {
                        if (config.pattern.test(subFile)) {
                            const tokenFilePath = path.join(fullPath, subFile);
                            const relPath = path.relative(path.join(configsDir, '..'), tokenFilePath);

                            // Try to read token file to validate it
                            try {
                                await fs.access(tokenFilePath);
                                const tokenData = JSON.parse(await fs.readFile(tokenFilePath, 'utf-8'));

                                // Generate UUID from directory name if possible
                                const uuid = uuidv4();

                                const provider = {
                                    [config.credPathKey]: `./${relPath}`,
                                    uuid: uuid,
                                    checkModelName: config.checkModelName,
                                    checkHealth: false,
                                    isHealthy: true,
                                    isDisabled: false,
                                    lastUsed: null,
                                    usageCount: 0,
                                    errorCount: 0,
                                    lastErrorTime: null,
                                    lastHealthCheckTime: null,
                                    lastHealthCheckModel: null,
                                    lastErrorMessage: null,
                                    customName: null,
                                    needsRefresh: false,
                                    refreshCount: 0
                                };

                                result[providerType].push(provider);
                            } catch (error) {
                                log(`  Warning: Failed to read ${tokenFilePath}: ${error.message}`, 'yellow');
                            }
                        }
                    }
                } else if (config.pattern.test(entry.name)) {
                    // Direct file match
                    const tokenFilePath = fullPath;
                    const relPath = path.relative(path.join(configsDir, '..'), tokenFilePath);

                    try {
                        await fs.access(tokenFilePath);
                        const tokenData = JSON.parse(await fs.readFile(tokenFilePath, 'utf-8'));

                        const uuid = uuidv4();

                        const provider = {
                            [config.credPathKey]: `./${relPath}`,
                            uuid: uuid,
                            checkModelName: config.checkModelName,
                            checkHealth: false,
                            isHealthy: true,
                            isDisabled: false,
                            lastUsed: null,
                            usageCount: 0,
                            errorCount: 0,
                            lastErrorTime: null,
                            lastHealthCheckTime: null,
                            lastHealthCheckModel: null,
                            lastErrorMessage: null,
                            customName: null,
                            needsRefresh: false,
                            refreshCount: 0
                        };

                        result[providerType].push(provider);
                    } catch (error) {
                        log(`  Warning: Failed to read ${tokenFilePath}: ${error.message}`, 'yellow');
                    }
                }
            }
        } catch (error) {
            log(`  Warning: Failed to scan ${config.dir}: ${error.message}`, 'yellow');
        }
    }

    return result;
}

/**
 * Main function
 */
async function main() {
    const options = parseArgs();

    if (options.help) {
        printHelp();
        process.exit(0);
    }

    log('\n=== Provider Pools Sync Tool ===\n', 'cyan');

    const configsDir = path.join(process.cwd(), 'configs');
    const poolsFile = path.join(configsDir, 'provider_pools.json');

    // Check if provider_pools.json exists
    const poolsExists = await fs.access(poolsFile).then(() => true).catch(() => false);
    if (!poolsExists) {
        log('Error: provider_pools.json not found', 'red');
        process.exit(1);
    }

    // Read current provider_pools.json
    log('Reading current provider_pools.json...', 'cyan');
    const currentPools = JSON.parse(await fs.readFile(poolsFile, 'utf-8'));

    const currentStats = {};
    for (const [type, providers] of Object.entries(currentPools)) {
        currentStats[type] = providers.length;
    }

    log('Current state:');
    for (const [type, count] of Object.entries(currentStats)) {
        log(`  ${type}: ${count} providers`, 'dim');
    }

    // Scan actual token files
    log('\nScanning token files...', 'cyan');
    const scannedPools = await scanTokenFiles(configsDir);

    const scannedStats = {};
    for (const [type, providers] of Object.entries(scannedPools)) {
        scannedStats[type] = providers.length;
    }

    log('Found on disk:');
    for (const [type, count] of Object.entries(scannedStats)) {
        log(`  ${type}: ${count} providers`, 'dim');
    }

    // Calculate changes
    log('\n--- Changes ---', 'cyan');
    let hasChanges = false;
    for (const type of new Set([...Object.keys(currentStats), ...Object.keys(scannedStats)])) {
        const current = currentStats[type] || 0;
        const scanned = scannedStats[type] || 0;
        const diff = scanned - current;

        if (diff !== 0) {
            hasChanges = true;
            const color = diff > 0 ? 'green' : 'red';
            const sign = diff > 0 ? '+' : '';
            log(`  ${type}: ${current} → ${scanned} (${sign}${diff})`, color);
        } else {
            log(`  ${type}: no change (${current})`, 'dim');
        }
    }

    if (!hasChanges) {
        log('\nNo changes needed. provider_pools.json is already in sync.', 'green');
        process.exit(0);
    }

    if (options.dryRun) {
        log('\n[DRY RUN] Changes would be applied but no files will be modified.', 'yellow');
        process.exit(0);
    }

    // Create backup if requested
    if (options.backup) {
        const backupFile = `${poolsFile}.backup.${Date.now()}`;
        await fs.copyFile(poolsFile, backupFile);
        log(`\nBackup created: ${path.basename(backupFile)}`, 'green');
    }

    // Write updated provider_pools.json
    log('\nWriting updated provider_pools.json...', 'cyan');
    await fs.writeFile(poolsFile, JSON.stringify(scannedPools, null, 2), 'utf-8');

    log('\n=== Sync Complete ===', 'green');
    log('provider_pools.json has been updated to match token files on disk.\n');
}

main().catch((error) => {
    log(`\nError: ${error.message}`, 'red');
    console.error(error);
    process.exit(1);
});
