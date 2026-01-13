import { TokenDistributionStrategy } from './TokenDistributionStrategy.js';

/**
 * 按比例分配 Token 策略
 * 用于 Kiro 等需要模拟 cache token 的场景
 */
export class RatioTokenDistribution extends TokenDistributionStrategy {
    /**
     * @param {Object} ratios - 分配比例配置
     * @param {number} ratios.input - input_tokens 比例
     * @param {number} ratios.cacheCreation - cache_creation_input_tokens 比例
     * @param {number} ratios.cacheRead - cache_read_input_tokens 比例
     * @param {number} [threshold=100] - 低于此阈值不进行分配
     */
    constructor(ratios = { input: 1, cacheCreation: 2, cacheRead: 25 }, threshold = 100) {
        super();
        this.ratios = ratios;
        this.threshold = threshold;
        this.totalParts = ratios.input + ratios.cacheCreation + ratios.cacheRead;
    }

    /**
     * 按比例分配 input tokens
     * @param {number} inputTokens - 原始 input token 数量
     * @returns {{input_tokens: number, cache_creation_input_tokens: number, cache_read_input_tokens: number}}
     */
    distribute(inputTokens) {
        if (inputTokens < this.threshold) {
            return {
                input_tokens: inputTokens,
                cache_creation_input_tokens: 0,
                cache_read_input_tokens: 0
            };
        }

        const inputPart = Math.floor(inputTokens * this.ratios.input / this.totalParts);
        const creationPart = Math.floor(inputTokens * this.ratios.cacheCreation / this.totalParts);
        const readPart = inputTokens - inputPart - creationPart;

        return {
            input_tokens: inputPart,
            cache_creation_input_tokens: creationPart,
            cache_read_input_tokens: readPart
        };
    }
}
