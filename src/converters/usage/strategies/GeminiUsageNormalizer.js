import { UsageNormalizer } from '../UsageNormalizer.js';

/**
 * Gemini 格式 Usage 标准化器
 * 确保 usageMetadata 结构完整，特别是 cachedContentTokenCount 字段
 */
export class GeminiUsageNormalizer extends UsageNormalizer {
    constructor() {
        super('gemini');
    }

    /**
     * 标准化 Gemini usage 数据
     * @param {Object} usage - 原始 usageMetadata 数据
     * @returns {Object} 标准化后的 usageMetadata
     */
    normalize(usage) {
        if (!usage) {
            return this.getDefaultUsage();
        }

        return {
            promptTokenCount: usage.promptTokenCount ?? 0,
            candidatesTokenCount: usage.candidatesTokenCount ?? 0,
            totalTokenCount: usage.totalTokenCount ?? 0,
            cachedContentTokenCount: usage.cachedContentTokenCount ?? 0,
            ...(usage.thoughtsTokenCount !== undefined && { thoughtsTokenCount: usage.thoughtsTokenCount }),
            ...(usage.promptTokensDetails && { promptTokensDetails: usage.promptTokensDetails }),
            ...(usage.candidatesTokensDetails && { candidatesTokensDetails: usage.candidatesTokensDetails })
        };
    }

    /**
     * 获取默认 Gemini usage 结构
     * @returns {Object}
     */
    getDefaultUsage() {
        return {
            promptTokenCount: 0,
            candidatesTokenCount: 0,
            totalTokenCount: 0,
            cachedContentTokenCount: 0
        };
    }
}
