import * as fs from 'fs';
import { promises as pfs } from 'fs';
import { INPUT_SYSTEM_PROMPT_FILE, MODEL_PROVIDER } from '../utils/common.js';
import { getStorageAdapter, isStorageInitialized } from './storage-factory.js';

export let CONFIG = {}; // Make CONFIG exportable
export let PROMPT_LOG_FILENAME = ''; // Make PROMPT_LOG_FILENAME exportable

const ALL_MODEL_PROVIDERS = Object.values(MODEL_PROVIDER);

function normalizeConfiguredProviders(config) {
    const fallbackProvider = MODEL_PROVIDER.GEMINI_CLI;
    const dedupedProviders = [];

    const addProvider = (value) => {
        if (typeof value !== 'string') {
            return;
        }
        const trimmed = value.trim();
        if (!trimmed) {
            return;
        }
        const matched = ALL_MODEL_PROVIDERS.find((provider) => provider.toLowerCase() === trimmed.toLowerCase());
        if (!matched) {
            console.warn(`[Config Warning] Unknown model provider '${trimmed}'. This entry will be ignored.`);
            return;
        }
        if (!dedupedProviders.includes(matched)) {
            dedupedProviders.push(matched);
        }
    };

    const rawValue = config.MODEL_PROVIDER;
    if (Array.isArray(rawValue)) {
        rawValue.forEach((entry) => addProvider(typeof entry === 'string' ? entry : String(entry)));
    } else if (typeof rawValue === 'string') {
        rawValue.split(',').forEach(addProvider);
    } else if (rawValue != null) {
        addProvider(String(rawValue));
    }

    if (dedupedProviders.length === 0) {
        dedupedProviders.push(fallbackProvider);
    }

    config.DEFAULT_MODEL_PROVIDERS = dedupedProviders;
    config.MODEL_PROVIDER = dedupedProviders[0];
}

/**
 * Initializes the server configuration from config.json and command-line arguments.
 * @param {string[]} args - Command-line arguments.
 * @param {string} [configFilePath='configs/config.json'] - Path to the configuration file.
 * @returns {Object} The initialized configuration object.
 */
export async function initializeConfig(args = process.argv.slice(2), configFilePath = 'configs/config.json') {
    let currentConfig = {};

    try {
        const configData = fs.readFileSync(configFilePath, 'utf8');
        currentConfig = JSON.parse(configData);
        console.log('[Config] Loaded configuration from configs/config.json');
    } catch (error) {
        console.error('[Config Error] Failed to load configs/config.json:', error.message);
        // Fallback to default values if config.json is not found or invalid
        currentConfig = {
            REQUIRED_API_KEY: "123456",
            SERVER_PORT: 3000,
            HOST: '0.0.0.0',
            MODEL_PROVIDER: MODEL_PROVIDER.GEMINI_CLI,
            SYSTEM_PROMPT_FILE_PATH: INPUT_SYSTEM_PROMPT_FILE, // Default value
            SYSTEM_PROMPT_MODE: 'append',
            PROXY_URL: null, // HTTP/HTTPS/SOCKS5 代理地址，如 http://127.0.0.1:7890 或 socks5://127.0.0.1:1080
            PROXY_ENABLED_PROVIDERS: [], // 启用代理的提供商列表，如 ['gemini-cli-oauth', 'claude-kiro-oauth']
            PROMPT_LOG_BASE_NAME: "prompt_log",
            PROMPT_LOG_MODE: "none",
            REQUEST_MAX_RETRIES: 3,
            REQUEST_BASE_DELAY: 1000,
            CREDENTIAL_SWITCH_MAX_RETRIES: 5, // 坏凭证切换最大重试次数（用于认证错误后切换凭证）
            CRON_NEAR_MINUTES: 15,
            CRON_REFRESH_TOKEN: false,
            PROVIDER_POOLS_FILE_PATH: null, // 新增号池配置文件路径
            MAX_ERROR_COUNT: 10, // 提供商最大错误次数
            providerFallbackChain: {}, // 跨类型 Fallback 链配置
            // Redis configuration
            redis: {
                enabled: false,
                url: null,
                host: 'localhost',
                port: 6379,
                password: null,
                db: 0,
                keyPrefix: 'aiclient:',
                connectTimeout: 5000,
                commandTimeout: 1000
            }
        };
        console.log('[Config] Using default configuration.');
    }

    // Parse command-line arguments
    for (let i = 0; i < args.length; i++) {
        if (args[i] === '--api-key') {
            if (i + 1 < args.length) {
                currentConfig.REQUIRED_API_KEY = args[i + 1];
                i++;
            } else {
                console.warn(`[Config Warning] --api-key flag requires a value.`);
            }
        } else if (args[i] === '--log-prompts') {
            if (i + 1 < args.length) {
                const mode = args[i + 1];
                if (mode === 'console' || mode === 'file') {
                    currentConfig.PROMPT_LOG_MODE = mode;
                } else {
                    console.warn(`[Config Warning] Invalid mode for --log-prompts. Expected 'console' or 'file'. Prompt logging is disabled.`);
                }
                i++;
            } else {
                console.warn(`[Config Warning] --log-prompts flag requires a value.`);
            }
        } else if (args[i] === '--port') {
            if (i + 1 < args.length) {
                currentConfig.SERVER_PORT = parseInt(args[i + 1], 10);
                i++;
            } else {
                console.warn(`[Config Warning] --port flag requires a value.`);
            }
        } else if (args[i] === '--model-provider') {
            if (i + 1 < args.length) {
                currentConfig.MODEL_PROVIDER = args[i + 1];
                i++;
            } else {
                console.warn(`[Config Warning] --model-provider flag requires a value.`);
            }
        } else if (args[i] === '--system-prompt-file') {
            if (i + 1 < args.length) {
                currentConfig.SYSTEM_PROMPT_FILE_PATH = args[i + 1];
                i++;
            } else {
                console.warn(`[Config Warning] --system-prompt-file flag requires a value.`);
            }
        } else if (args[i] === '--system-prompt-mode') {
            if (i + 1 < args.length) {
                const mode = args[i + 1];
                if (mode === 'overwrite' || mode === 'append') {
                    currentConfig.SYSTEM_PROMPT_MODE = mode;
                } else {
                    console.warn(`[Config Warning] Invalid mode for --system-prompt-mode. Expected 'overwrite' or 'append'. Using default 'overwrite'.`);
                }
                i++;
            } else {
                console.warn(`[Config Warning] --system-prompt-mode flag requires a value.`);
            }
        } else if (args[i] === '--host') {
            if (i + 1 < args.length) {
                currentConfig.HOST = args[i + 1];
                i++;
            } else {
                console.warn(`[Config Warning] --host flag requires a value.`);
            }
        } else if (args[i] === '--prompt-log-base-name') {
            if (i + 1 < args.length) {
                currentConfig.PROMPT_LOG_BASE_NAME = args[i + 1];
                i++;
            } else {
                console.warn(`[Config Warning] --prompt-log-base-name flag requires a value.`);
            }
        } else if (args[i] === '--cron-near-minutes') {
            if (i + 1 < args.length) {
                currentConfig.CRON_NEAR_MINUTES = parseInt(args[i + 1], 10);
                i++;
            } else {
                console.warn(`[Config Warning] --cron-near-minutes flag requires a value.`);
            }
        } else if (args[i] === '--cron-refresh-token') {
            if (i + 1 < args.length) {
                currentConfig.CRON_REFRESH_TOKEN = args[i + 1].toLowerCase() === 'true';
                i++;
            } else {
                console.warn(`[Config Warning] --cron-refresh-token flag requires a value.`);
            }
        } else if (args[i] === '--provider-pools-file') {
            if (i + 1 < args.length) {
                currentConfig.PROVIDER_POOLS_FILE_PATH = args[i + 1];
                i++;
            } else {
                console.warn(`[Config Warning] --provider-pools-file flag requires a value.`);
            }
        } else if (args[i] === '--max-error-count') {
            if (i + 1 < args.length) {
                currentConfig.MAX_ERROR_COUNT = parseInt(args[i + 1], 10);
                i++;
            } else {
                console.warn(`[Config Warning] --max-error-count flag requires a value.`);
            }
        }
    }

    // Initialize Redis configuration from environment variables
    initializeRedisConfig(currentConfig);

    normalizeConfiguredProviders(currentConfig);

    if (!currentConfig.SYSTEM_PROMPT_FILE_PATH) {
        currentConfig.SYSTEM_PROMPT_FILE_PATH = INPUT_SYSTEM_PROMPT_FILE;
    }
    currentConfig.SYSTEM_PROMPT_CONTENT = await getSystemPromptFileContent(currentConfig.SYSTEM_PROMPT_FILE_PATH);

    // 设置号池配置文件路径（实际数据将从 storage adapter 加载）
    if (!currentConfig.PROVIDER_POOLS_FILE_PATH) {
        currentConfig.PROVIDER_POOLS_FILE_PATH = 'configs/provider_pools.json';
    }

    // 不再在配置加载时直接读取文件，而是在 storage adapter 初始化后再加载
    // 这样可以确保使用正确的数据源（Redis 或文件）
    currentConfig.providerPools = {};

    // Set PROMPT_LOG_FILENAME based on the determined config
    if (currentConfig.PROMPT_LOG_MODE === 'file') {
        const now = new Date();
        const pad = (num) => String(num).padStart(2, '0');
        const timestamp = `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}-${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`;
        PROMPT_LOG_FILENAME = `${currentConfig.PROMPT_LOG_BASE_NAME}-${timestamp}.log`;
    } else {
        PROMPT_LOG_FILENAME = ''; // Clear if not logging to file
    }

    // Assign to the exported CONFIG
    Object.assign(CONFIG, currentConfig);
    return CONFIG;
}

/**
 * Gets system prompt content from the specified file path.
 * @param {string} filePath - Path to the system prompt file.
 * @returns {Promise<string|null>} File content, or null if the file does not exist, is empty, or an error occurs.
 */
export async function getSystemPromptFileContent(filePath) {
    try {
        await pfs.access(filePath, pfs.constants.F_OK);
    } catch (error) {
        if (error.code === 'ENOENT') {
            console.warn(`[System Prompt] Specified system prompt file not found: ${filePath}`);
        } else {
            console.error(`[System Prompt] Error accessing system prompt file ${filePath}: ${error.message}`);
        }
        return null;
    }

    try {
        const content = await pfs.readFile(filePath, 'utf8');
        if (!content.trim()) {
            return null;
        }
        console.log(`[System Prompt] Loaded system prompt from ${filePath}`);
        return content;
    } catch (error) {
        console.error(`[System Prompt] Error reading system prompt file ${filePath}: ${error.message}`);
        return null;
    }
}

/**
 * Initialize Redis configuration from environment variables.
 * Environment variables take precedence over config file values.
 * @param {Object} config - Configuration object to update
 */
function initializeRedisConfig(config) {
    // Ensure redis config object exists
    if (!config.redis) {
        config.redis = {};
    }

    // REDIS_ENABLED - enable/disable Redis storage
    if (process.env.REDIS_ENABLED !== undefined) {
        config.redis.enabled = process.env.REDIS_ENABLED.toLowerCase() === 'true';
    }

    // REDIS_URL - full Redis URL (overrides host/port)
    if (process.env.REDIS_URL) {
        config.redis.url = process.env.REDIS_URL;
        config.redis.enabled = true; // Auto-enable if URL is provided
    }

    // REDIS_HOST - Redis server host
    if (process.env.REDIS_HOST) {
        config.redis.host = process.env.REDIS_HOST;
    }

    // REDIS_PORT - Redis server port
    if (process.env.REDIS_PORT) {
        config.redis.port = parseInt(process.env.REDIS_PORT, 10);
    }

    // REDIS_PASSWORD - Redis authentication password
    if (process.env.REDIS_PASSWORD) {
        config.redis.password = process.env.REDIS_PASSWORD;
    }

    // REDIS_DB - Redis database number (0-15)
    if (process.env.REDIS_DB) {
        config.redis.db = parseInt(process.env.REDIS_DB, 10);
    }

    // Set defaults for any missing values
    config.redis.host = config.redis.host || 'localhost';
    config.redis.port = config.redis.port || 6379;
    config.redis.db = config.redis.db || 0;
    config.redis.keyPrefix = config.redis.keyPrefix || 'aiclient:';
    config.redis.connectTimeout = config.redis.connectTimeout || 5000;
    config.redis.commandTimeout = config.redis.commandTimeout || 1000;

    if (config.redis.enabled) {
        console.log(`[Config] Redis storage enabled: ${config.redis.url || `${config.redis.host}:${config.redis.port}`}`);
    }
}

/**
 * Try to load configuration from Redis storage.
 * This should be called after Redis is initialized to sync config from Redis.
 * Falls back to current CONFIG if Redis is not available or has no config.
 * @returns {Promise<Object|null>} Config from Redis or null if not available
 */
export async function loadConfigFromRedis() {
    if (!isStorageInitialized()) {
        return null;
    }

    try {
        const adapter = getStorageAdapter();
        if (adapter.getType() !== 'redis') {
            return null;
        }

        const redisConfig = await adapter.getConfig();
        if (redisConfig && Object.keys(redisConfig).length > 0) {
            console.log('[Config] Loaded configuration from Redis');
            return redisConfig;
        }
    } catch (error) {
        console.warn('[Config] Failed to load config from Redis:', error.message);
    }

    return null;
}

/**
 * Save current configuration to Redis storage.
 * This should be called after config changes to sync to Redis.
 * @param {Object} [config] - Config to save, defaults to current CONFIG
 * @returns {Promise<boolean>} Whether save was successful
 */
export async function saveConfigToRedis(config = CONFIG) {
    if (!isStorageInitialized()) {
        return false;
    }

    try {
        const adapter = getStorageAdapter();
        if (adapter.getType() !== 'redis') {
            return false;
        }

        // Create a clean config object for saving (exclude runtime-only properties)
        const configToSave = {
            REQUIRED_API_KEY: config.REQUIRED_API_KEY,
            SERVER_PORT: config.SERVER_PORT,
            HOST: config.HOST,
            MODEL_PROVIDER: config.MODEL_PROVIDER,
            SYSTEM_PROMPT_FILE_PATH: config.SYSTEM_PROMPT_FILE_PATH,
            SYSTEM_PROMPT_MODE: config.SYSTEM_PROMPT_MODE,
            PROMPT_LOG_BASE_NAME: config.PROMPT_LOG_BASE_NAME,
            PROMPT_LOG_MODE: config.PROMPT_LOG_MODE,
            REQUEST_MAX_RETRIES: config.REQUEST_MAX_RETRIES,
            REQUEST_BASE_DELAY: config.REQUEST_BASE_DELAY,
            CREDENTIAL_SWITCH_MAX_RETRIES: config.CREDENTIAL_SWITCH_MAX_RETRIES,
            CRON_NEAR_MINUTES: config.CRON_NEAR_MINUTES,
            CRON_REFRESH_TOKEN: config.CRON_REFRESH_TOKEN,
            PROVIDER_POOLS_FILE_PATH: config.PROVIDER_POOLS_FILE_PATH,
            MAX_ERROR_COUNT: config.MAX_ERROR_COUNT,
            POOL_SIZE_LIMIT: config.POOL_SIZE_LIMIT,
            WARMUP_TARGET: config.WARMUP_TARGET,
            REFRESH_CONCURRENCY_PER_PROVIDER: config.REFRESH_CONCURRENCY_PER_PROVIDER,
            providerFallbackChain: config.providerFallbackChain,
            modelFallbackMapping: config.modelFallbackMapping,
            PROXY_URL: config.PROXY_URL,
            PROXY_ENABLED_PROVIDERS: config.PROXY_ENABLED_PROVIDERS,
            redis: config.redis
        };

        await adapter.setConfig(configToSave);
        console.log('[Config] Configuration saved to Redis');
        return true;
    } catch (error) {
        console.warn('[Config] Failed to save config to Redis:', error.message);
        return false;
    }
}

/**
 * Sync configuration between Redis and file storage.
 * Called after Redis connection is established.
 * @returns {Promise<void>}
 */
export async function syncConfigWithRedis() {
    if (!isStorageInitialized()) {
        return;
    }

    try {
        const adapter = getStorageAdapter();
        if (adapter.getType() !== 'redis') {
            return;
        }

        // Check if Redis has config
        const redisConfig = await adapter.getConfig();

        if (!redisConfig || Object.keys(redisConfig).length === 0) {
            // Redis is empty, push current config to Redis
            console.log('[Config] Redis config is empty, syncing from file config');
            await saveConfigToRedis(CONFIG);
        } else {
            // Redis has config, merge any missing fields from current CONFIG
            const merged = { ...CONFIG, ...redisConfig };
            Object.assign(CONFIG, merged);
            console.log('[Config] Synced configuration from Redis');
        }
    } catch (error) {
        console.warn('[Config] Failed to sync config with Redis:', error.message);
    }
}

export { ALL_MODEL_PROVIDERS, initializeRedisConfig };

