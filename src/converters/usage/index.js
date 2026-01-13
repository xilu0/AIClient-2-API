// 基类和工厂
export { UsageNormalizer } from './UsageNormalizer.js';
export { UsageNormalizerFactory } from './UsageNormalizerFactory.js';

// 标准化器策略
export { GeminiUsageNormalizer } from './strategies/GeminiUsageNormalizer.js';
export { ClaudeUsageNormalizer } from './strategies/ClaudeUsageNormalizer.js';
export { OpenAIUsageNormalizer } from './strategies/OpenAIUsageNormalizer.js';

// Token 分配策略
export { TokenDistributionStrategy } from './token-distribution/TokenDistributionStrategy.js';
export { RatioTokenDistribution } from './token-distribution/RatioTokenDistribution.js';
export { PassthroughTokenDistribution } from './token-distribution/PassthroughTokenDistribution.js';

// 便捷函数
import { UsageNormalizerFactory } from './UsageNormalizerFactory.js';

/**
 * 标准化 Gemini usageMetadata 数据
 * @param {Object} usage - 原始 usageMetadata
 * @returns {Object} 标准化后的 usageMetadata
 */
export function normalizeGeminiUsage(usage) {
    return UsageNormalizerFactory.getNormalizer('gemini').normalize(usage);
}

/**
 * 标准化 Claude usage 数据
 * @param {Object} usage - 原始 usage
 * @param {Object} [options] - 配置选项
 * @returns {Object} 标准化后的 usage
 */
export function normalizeClaudeUsage(usage, options = {}) {
    return UsageNormalizerFactory.getNormalizer('claude', options).normalize(usage, options);
}

/**
 * 标准化 OpenAI usage 数据
 * @param {Object} usage
 * @returns {Object} 标准化后的 usage
 */
export function normalizeOpenAIUsage(usage) {
    return UsageNormalizerFactory.getNormalizer('openai').normalize(usage);
}

/**
 * 计算 Kiro 风格的 token 分配
 * 默认比例 1:2:25，可通过 configureKiroTokenDistribution 修改
 * @param {number} inputTokens - 原始 input token 数量
 * @returns {{input_tokens: number, cache_creation_input_tokens: number, cache_read_input_tokens: number}}
 */
export function calculateKiroTokenDistribution(inputTokens) {
    return UsageNormalizerFactory.getTokenDistribution('kiro').distribute(inputTokens);
}

/**
 * 配置 Kiro token 分配比例
 * @param {Object} ratios - 分配比例
 * @param {number} ratios.input - input_tokens 比例
 * @param {number} ratios.cacheCreation - cache_creation_input_tokens 比例
 * @param {number} ratios.cacheRead - cache_read_input_tokens 比例
 * @param {number} [threshold] - 低于此阈值不进行分配（默认 100）
 * @example
 * // 设置为 1:3:20 比例
 * configureKiroTokenDistribution({ input: 1, cacheCreation: 3, cacheRead: 20 });
 * // 同时设置阈值
 * configureKiroTokenDistribution({ input: 1, cacheCreation: 2, cacheRead: 25 }, 50);
 */
export function configureKiroTokenDistribution(ratios, threshold) {
    UsageNormalizerFactory.configureKiroRatios(ratios, threshold);
}

/**
 * 获取当前 Kiro token 分配配置
 * @returns {{ratios: {input: number, cacheCreation: number, cacheRead: number}, threshold: number}}
 */
export function getKiroTokenDistributionConfig() {
    return UsageNormalizerFactory.getKiroConfig();
}

/**
 * 重置 Kiro token 分配配置为默认值（1:2:25，阈值 100）
 */
export function resetKiroTokenDistributionConfig() {
    UsageNormalizerFactory.resetKiroConfig();
}
