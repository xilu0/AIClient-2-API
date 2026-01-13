import { TokenDistributionStrategy } from './TokenDistributionStrategy.js';

/**
 * 直通策略 - 不做任何分配
 * 用于不需要模拟 cache token 的场景
 */
export class PassthroughTokenDistribution extends TokenDistributionStrategy {
    /**
     * 直接返回原始 token 数量，不进行分配
     * @param {number} inputTokens - 原始 input token 数量
     * @returns {{input_tokens: number, cache_creation_input_tokens: number, cache_read_input_tokens: number}}
     */
    distribute(inputTokens) {
        return {
            input_tokens: inputTokens,
            cache_creation_input_tokens: 0,
            cache_read_input_tokens: 0
        };
    }
}
