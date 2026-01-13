/**
 * Usage 数据标准化器基类（抽象类）
 * 负责确保 usage 数据结构完整，填充默认值
 */
export class UsageNormalizer {
    constructor(protocolName) {
        if (new.target === UsageNormalizer) {
            throw new Error('UsageNormalizer 是抽象类，不能直接实例化');
        }
        this.protocolName = protocolName;
    }

    /**
     * 标准化 usage 数据
     * @param {Object} usage - 原始 usage 数据
     * @param {Object} [options] - 可选配置
     * @returns {Object} 标准化后的 usage 数据
     */
    normalize(usage, options = {}) {
        throw new Error('normalize() 方法必须被实现');
    }

    /**
     * 获取默认 usage 结构
     * @returns {Object} 默认 usage 对象
     */
    getDefaultUsage() {
        throw new Error('getDefaultUsage() 方法必须被实现');
    }
}
