import { UsageNormalizer } from '../UsageNormalizer.js';

/**
 * Claude 格式 Usage 标准化器
 * 支持可选的 token 分配策略
 */
export class ClaudeUsageNormalizer extends UsageNormalizer {
    /**
     * @param {TokenDistributionStrategy} [tokenDistributionStrategy] - 可选的 token 分配策略
     */
    constructor(tokenDistributionStrategy = null) {
        super('claude');
        this.tokenDistribution = tokenDistributionStrategy;
    }

    /**
     * 设置 token 分配策略
     * @param {TokenDistributionStrategy} strategy
     */
    setTokenDistributionStrategy(strategy) {
        this.tokenDistribution = strategy;
    }

    /**
     * 标准化 Claude usage 数据
     * @param {Object} usage - 原始 usage 数据
     * @param {Object} [options] - 可选配置
     * @param {boolean} [options.applyDistribution] - 是否应用 token 分配策略
     * @param {number} [options.originalInputTokens] - 原始 input token 数量（用于分配）
     * @returns {Object} 标准化后的 usage
     */
    normalize(usage, options = {}) {
        if (!usage) {
            return this.getDefaultUsage();
        }

        let result = {
            input_tokens: usage.input_tokens ?? 0,
            output_tokens: usage.output_tokens ?? 0,
            cache_creation_input_tokens: usage.cache_creation_input_tokens ?? 0,
            cache_read_input_tokens: usage.cache_read_input_tokens ?? 0
        };

        // P2-18: 如果配置了 token 分配策略且需要应用（直接修改对象避免展开操作）
        if (this.tokenDistribution && options.applyDistribution) {
            const originalInputTokens = options.originalInputTokens ?? result.input_tokens;
            const distributed = this.tokenDistribution.distribute(originalInputTokens);
            // P2-18: 直接赋值字段，避免对象展开创建新对象
            result.input_tokens = distributed.input_tokens;
            result.cache_creation_input_tokens = distributed.cache_creation_input_tokens;
            result.cache_read_input_tokens = distributed.cache_read_input_tokens;
        }

        return result;
    }

    /**
     * 获取默认 Claude usage 结构
     * @returns {Object}
     */
    getDefaultUsage() {
        return {
            input_tokens: 0,
            output_tokens: 0,
            cache_creation_input_tokens: 0,
            cache_read_input_tokens: 0
        };
    }
}
