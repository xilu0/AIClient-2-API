import { GeminiUsageNormalizer } from './strategies/GeminiUsageNormalizer.js';
import { ClaudeUsageNormalizer } from './strategies/ClaudeUsageNormalizer.js';
import { OpenAIUsageNormalizer } from './strategies/OpenAIUsageNormalizer.js';
import { RatioTokenDistribution } from './token-distribution/RatioTokenDistribution.js';
import { PassthroughTokenDistribution } from './token-distribution/PassthroughTokenDistribution.js';

/**
 * 默认 Kiro token 分配比例配置
 */
const DEFAULT_KIRO_RATIOS = {
    input: 1,
    cacheCreation: 2,
    cacheRead: 25
};

/**
 * 默认 token 分配阈值（低于此值不进行分配）
 */
const DEFAULT_THRESHOLD = 100;

/**
 * 生成稳定的缓存键
 * 对 options 对象的键进行排序，确保相同内容生成相同的键
 * @param {string} protocol - 协议名称
 * @param {Object} options - 配置选项
 * @returns {string} 稳定的缓存键
 */
function generateStableCacheKey(protocol, options) {
    const sortedOptions = Object.keys(options).sort().reduce((obj, key) => {
        obj[key] = options[key];
        return obj;
    }, {});
    return `${protocol}_${JSON.stringify(sortedOptions)}`;
}

/**
 * Usage 标准化器工厂
 * 管理标准化器实例的创建和缓存
 */
export class UsageNormalizerFactory {
    static #normalizers = new Map();
    static #kiroRatios = { ...DEFAULT_KIRO_RATIOS };
    static #kiroThreshold = DEFAULT_THRESHOLD;

    /**
     * 配置 Kiro token 分配比例
     * @param {Object} ratios - 分配比例
     * @param {number} ratios.input - input_tokens 比例
     * @param {number} ratios.cacheCreation - cache_creation_input_tokens 比例
     * @param {number} ratios.cacheRead - cache_read_input_tokens 比例
     * @param {number} [threshold] - 低于此阈值不进行分配
     */
    static configureKiroRatios(ratios, threshold) {
        if (ratios) {
            this.#kiroRatios = {
                input: ratios.input ?? DEFAULT_KIRO_RATIOS.input,
                cacheCreation: ratios.cacheCreation ?? DEFAULT_KIRO_RATIOS.cacheCreation,
                cacheRead: ratios.cacheRead ?? DEFAULT_KIRO_RATIOS.cacheRead
            };
        }
        if (threshold !== undefined) {
            this.#kiroThreshold = threshold;
        }
        // 清除缓存以使新配置生效
        this.clearCache();
    }

    /**
     * 获取当前 Kiro 配置
     * @returns {{ratios: Object, threshold: number}}
     */
    static getKiroConfig() {
        return {
            ratios: { ...this.#kiroRatios },
            threshold: this.#kiroThreshold
        };
    }

    /**
     * 重置 Kiro 配置为默认值
     */
    static resetKiroConfig() {
        this.#kiroRatios = { ...DEFAULT_KIRO_RATIOS };
        this.#kiroThreshold = DEFAULT_THRESHOLD;
        this.clearCache();
    }

    /**
     * 获取 Usage 标准化器（带缓存）
     * @param {string} protocol - 协议名称 ('gemini', 'claude', 'openai')
     * @param {Object} [options] - 配置选项
     * @returns {UsageNormalizer}
     */
    static getNormalizer(protocol, options = {}) {
        const cacheKey = generateStableCacheKey(protocol, options);

        if (!this.#normalizers.has(cacheKey)) {
            const normalizer = this.createNormalizer(protocol, options);
            this.#normalizers.set(cacheKey, normalizer);
        }

        return this.#normalizers.get(cacheKey);
    }

    /**
     * 创建 Usage 标准化器（不缓存）
     * @param {string} protocol - 协议名称
     * @param {Object} [options] - 配置选项
     * @returns {UsageNormalizer}
     */
    static createNormalizer(protocol, options = {}) {
        switch (protocol) {
            case 'gemini':
                return new GeminiUsageNormalizer();
            case 'claude': {
                const claudeNormalizer = new ClaudeUsageNormalizer();
                if (options.tokenDistribution) {
                    claudeNormalizer.setTokenDistributionStrategy(
                        this.getTokenDistribution(options.tokenDistribution)
                    );
                }
                return claudeNormalizer;
            }
            case 'openai':
                return new OpenAIUsageNormalizer();
            default:
                throw new Error(`未知的协议: ${protocol}`);
        }
    }

    /**
     * 获取 Token 分配策略
     * @param {string|Object} config - 策略配置
     *   - 'kiro': 使用当前配置的 Kiro 比例（可通过 configureKiroRatios 修改）
     *   - 'passthrough': 不进行分配
     *   - { type: 'ratio', ratios: {...}, threshold: number }: 自定义比例
     * @returns {TokenDistributionStrategy}
     */
    static getTokenDistribution(config) {
        if (typeof config === 'string') {
            switch (config) {
                case 'kiro':
                    return new RatioTokenDistribution(this.#kiroRatios, this.#kiroThreshold);
                case 'passthrough':
                default:
                    return new PassthroughTokenDistribution();
            }
        }

        if (typeof config === 'object' && config.type === 'ratio') {
            return new RatioTokenDistribution(config.ratios, config.threshold);
        }

        return new PassthroughTokenDistribution();
    }

    /**
     * 清除缓存
     */
    static clearCache() {
        this.#normalizers.clear();
    }
}
