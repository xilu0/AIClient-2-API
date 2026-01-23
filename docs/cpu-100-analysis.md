# CPU 100% 问题分析报告

## 问题描述
在UI界面导入AWS账号文件时，系统出现CPU占用100%的性能问题。

## 根本原因

### 问题代码位置
`src/auth/oauth-handlers.js:1597-1647` - `checkKiroCredentialsDuplicate` 函数

### 问题分析

#### 1. **同步递归扫描目录**
每次导入AWS账号时，系统会执行以下操作：
```javascript
// 递归扫描整个 configs/kiro 目录
const scanDirectory = async (dirPath) => {
    const entries = await fs.promises.readdir(dirPath, { withFileTypes: true });

    for (const entry of entries) {
        if (entry.isDirectory()) {
            await scanDirectory(fullPath);  // 递归扫描子目录
        } else if (entry.isFile() && entry.name.endsWith('.json')) {
            // 读取并解析每个JSON文件
            const content = await fs.promises.readFile(fullPath, 'utf8');
            const credentials = JSON.parse(content);
            // 比对 refreshToken
        }
    }
};
```

#### 2. **性能瓶颈**
- **大量文件I/O**：如果有100个账号，就要读取100个文件
- **串行处理**：使用 `for...of` 循环，每个文件依次处理
- **CPU密集型**：每个文件都要进行JSON解析和字符串比对
- **无缓存机制**：每次导入都重新扫描所有文件
- **阻塞主线程**：所有操作在主线程执行，导致UI卡死

#### 3. **触发场景**
```javascript
// src/auth/oauth-handlers.js:1902
// 默认情况下会进行重复检查
if (!skipDuplicateCheck) {
    const duplicateCheck = await checkKiroCredentialsDuplicate(credentials.refreshToken);
    // ...
}
```

## 性能影响测算

假设有 N 个已存在的账号文件：
- **文件读取时间**：N × 5ms = 500ms (N=100)
- **JSON解析时间**：N × 2ms = 200ms (N=100)
- **字符串比对时间**：N × 0.5ms = 50ms (N=100)
- **总耗时**：约 750ms (N=100)

当账号数量达到500个时，单次导入耗时可能超过3秒，导致CPU持续高占用。

## 解决方案

### 方案1：添加内存缓存（推荐）⭐
**优点**：
- 实现简单，改动最小
- 性能提升显著（首次扫描后，后续检查仅需O(1)时间）
- 不改变现有逻辑

**实现**：
```javascript
// 在文件顶部添加缓存
const credentialCache = new Map(); // refreshToken -> path
let cacheLastUpdated = 0;
const CACHE_TTL = 5 * 60 * 1000; // 5分钟缓存

export async function checkKiroCredentialsDuplicate(refreshToken, provider = 'claude-kiro-oauth') {
    const now = Date.now();

    // 检查缓存
    if (now - cacheLastUpdated < CACHE_TTL) {
        if (credentialCache.has(refreshToken)) {
            return {
                isDuplicate: true,
                existingPath: credentialCache.get(refreshToken)
            };
        }
        // 缓存中没有，说明不重复
        return { isDuplicate: false };
    }

    // 缓存过期，重新扫描
    credentialCache.clear();
    const kiroDir = path.join(process.cwd(), 'configs', 'kiro');

    if (!fs.existsSync(kiroDir)) {
        cacheLastUpdated = now;
        return { isDuplicate: false };
    }

    // 扫描并构建缓存
    await buildCache(kiroDir);
    cacheLastUpdated = now;

    // 再次检查
    if (credentialCache.has(refreshToken)) {
        return {
            isDuplicate: true,
            existingPath: credentialCache.get(refreshToken)
        };
    }

    return { isDuplicate: false };
}

async function buildCache(dirPath) {
    const entries = await fs.promises.readdir(dirPath, { withFileTypes: true });

    for (const entry of entries) {
        const fullPath = path.join(dirPath, entry.name);

        if (entry.isDirectory()) {
            await buildCache(fullPath);
        } else if (entry.isFile() && entry.name.endsWith('.json')) {
            try {
                const content = await fs.promises.readFile(fullPath, 'utf8');
                const credentials = JSON.parse(content);

                if (credentials.refreshToken) {
                    const relativePath = path.relative(process.cwd(), fullPath);
                    credentialCache.set(credentials.refreshToken, relativePath);
                }
            } catch (parseError) {
                // 忽略解析错误的文件
            }
        }
    }
}
```

### 方案2：并发优化
**优点**：
- 充分利用多核CPU
- 减少总耗时

**实现**：
```javascript
// 使用 Promise.all 并发读取文件
const filePromises = jsonFiles.map(async (filePath) => {
    try {
        const content = await fs.promises.readFile(filePath, 'utf8');
        const credentials = JSON.parse(content);
        return { filePath, refreshToken: credentials.refreshToken };
    } catch (error) {
        return null;
    }
});

const results = await Promise.all(filePromises);
```

### 方案3：索引文件
**优点**：
- 最快的查询速度
- 持久化存储

**缺点**：
- 需要维护索引文件
- 可能出现索引不一致

**实现**：
```javascript
// configs/kiro/.index.json
{
  "version": 1,
  "lastUpdated": "2026-01-23T10:00:00Z",
  "tokens": {
    "refreshToken1": "configs/kiro/xxx/xxx.json",
    "refreshToken2": "configs/kiro/yyy/yyy.json"
  }
}
```

### 方案4：跳过重复检查（临时方案）
**优点**：
- 立即生效，无需修改代码

**缺点**：
- 可能导入重复账号

**实现**：
```javascript
// 在 src/ui-modules/oauth-api.js:282 修改
const result = await importAwsCredentials(credentials, true); // 跳过重复检查
```

## 推荐实施方案

### 短期方案（立即实施）
1. **添加内存缓存**（方案1）
2. **并发优化**（方案2）

### 长期方案（后续优化）
1. 实现索引文件机制
2. 添加后台任务队列
3. 使用 Worker Threads 处理文件扫描

## 预期效果

实施方案1+方案2后：
- **首次导入**：耗时减少50%（并发优化）
- **后续导入**：耗时减少99%（缓存命中）
- **CPU占用**：从100%降至10-20%
- **用户体验**：UI不再卡顿

## 其他优化建议

1. **添加进度提示**：在UI显示"正在检查重复..."
2. **异步处理**：将重复检查放到后台队列
3. **限流机制**：限制同时导入的数量
4. **文件监听**：使用 fs.watch 监听文件变化，自动更新缓存

## 总结

导致CPU 100%的根本原因是**每次导入都要递归扫描并读取所有已存在的账号文件**。通过添加内存缓存和并发优化，可以将性能提升100倍以上。
