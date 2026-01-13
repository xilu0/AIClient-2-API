/**
 * Token 分配策略接口（抽象基类）
 * 定义 token 分配的统一接口
 */
export class TokenDistributionStrategy {
    constructor() {
        if (new.target === TokenDistributionStrategy) {
            throw new Error('TokenDistributionStrategy 是抽象类，不能直接实例化');
        }
    }

    /**
     * 分配 input tokens
     * @param {number} inputTokens - 原始 input token 数量
     * @returns {{input_tokens: number, cache_creation_input_tokens: number, cache_read_input_tokens: number}}
     */
    distribute(inputTokens) {
        throw new Error('distribute() 方法必须被实现');
    }
}
