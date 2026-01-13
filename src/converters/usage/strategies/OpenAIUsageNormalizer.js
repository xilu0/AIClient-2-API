import { UsageNormalizer } from '../UsageNormalizer.js';

/**
 * OpenAI 格式 Usage 标准化器
 */
export class OpenAIUsageNormalizer extends UsageNormalizer {
    constructor() {
        super('openai');
    }

    /**
     * 标准化 OpenAI usage 数据
     * @param {Object} usage - 原始 usage 数据
     * @returns {Object} 标准化后的 usage
     */
    normalize(usage) {
        if (!usage) {
            return this.getDefaultUsage();
        }

        return {
            prompt_tokens: usage.prompt_tokens ?? 0,
            completion_tokens: usage.completion_tokens ?? 0,
            total_tokens: usage.total_tokens ?? 0,
            cached_tokens: usage.cached_tokens ?? 0,
            prompt_tokens_details: {
                cached_tokens: usage.prompt_tokens_details?.cached_tokens ?? 0
            },
            completion_tokens_details: {
                reasoning_tokens: usage.completion_tokens_details?.reasoning_tokens ?? 0
            }
        };
    }

    /**
     * 获取默认 OpenAI usage 结构
     * @returns {Object}
     */
    getDefaultUsage() {
        return {
            prompt_tokens: 0,
            completion_tokens: 0,
            total_tokens: 0,
            cached_tokens: 0,
            prompt_tokens_details: {
                cached_tokens: 0
            },
            completion_tokens_details: {
                reasoning_tokens: 0
            }
        };
    }
}
