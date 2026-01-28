maxIterations 原理解析

### 什么是 maxIterations？

maxIterations 是一个**防护机制**，用于防止解析循环陷入无限循环。在 parseAwsEventStreamBuffer 函数中，它限制了解析单个数据块时的最大循环次数。

### 工作原理

javascript
parseAwsEventStreamBuffer(buffer) {
    let maxIterations = 50; // 原来的值
    let iterations = 0;
    
    while (iterations < maxIterations) {
        // 1. 查找JSON事件的起始位置
        const contentStart = remaining.indexOf('{"content":');
        const nameStart = remaining.indexOf('{"name":');
        // ... 其他事件类型
        
        // 2. 解析完整的JSON对象
        // 3. 提取事件数据
        // 4. 移动到下一个位置
        
        iterations++; // 每次循环计数+1
    }
}


### 为什么需要增大？

#### 1. 流式数据特性
Kiro API 返回的是**流式数据**，单个数据块可能包含大量JSON事件：
{"content":"Hello"}{"content":" world"}{"content":"!"}{"name":"tool1"}...


#### 2. 原来的限制过小
- **50次迭代** = 最多处理50个JSON事件
- 当单个buffer包含超过50个事件时，剩余数据被丢弃
- 导致数据丢失和重复处理

#### 3. 实际场景需求
- **长对话**：可能产生数百个内容片段
- **工具调用**：每个工具调用包含多个事件（开始、输入、结束）
- **大文本生成**：AI生成长文本时会分成很多小块

### 增大到500的好处

#### 1. 处理能力提升
50次  → 最多50个事件
500次 → 最多500个事件 (10倍提升)


#### 2. 减少数据丢失
- 原来：超过50个事件的部分被截断
- 现在：可以处理更完整的数据流

#### 3. 降低CPU占用
- **原来**：频繁触发警告，数据累积，重复处理
- **现在**：一次性处理更多数据，减少重复工作

### 为什么不设置更大？

#### 1. 内存保护
过大的值可能导致：
- 内存占用过高
- 处理时间过长

#### 2. 异常检测
仍需要检测真正的无限循环情况

#### 3. 平衡点
500是一个合理的平衡：
- 足够处理正常的大数据流
- 仍能检测异常情况

### 类比理解

想象你在**拆快递包裹**：
- **maxIterations = 50**：最多拆50个小包装，剩下的扔掉
- **maxIterations = 500**：最多拆500个小包装，基本能处理完整个包裹
