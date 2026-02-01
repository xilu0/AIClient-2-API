# Node.js 性能分析指南

本文档提供 AIClient-2-API 项目的性能瓶颈定位方案和工具使用指南。

## 目录

- [推荐工具](#推荐工具)
- [分析流程](#分析流程)
- [快速开始](#快速开始)
- [项目特定分析点](#项目特定分析点)
- [常见问题排查](#常见问题排查)

---

## 推荐工具

### 1. Clinic.js（综合诊断套件）⭐

**最推荐使用** - 提供三种互补的分析工具，生成可视化 HTML 报告。

#### 安装

```bash
npm install -g clinic
```

#### 使用方法

**CPU 火焰图分析**（定位计算密集函数）
```bash
clinic flame -- node src/services/api-server.js
NODE_OPTIONS='--inspect' \
	REDIS_ENABLED=true \
	REDIS_URL=redis://127.0.0.1:6379 \
	REDIS_KEY_PREFIX=aiclient: \
	clinic flame -- node src/core/master.js --api-key AI_club2026
```

**事件循环分析**（定位异步阻塞）
```bash
clinic bubbleprof -- node src/services/api-server.js
```

**堆内存分析**（定位内存泄漏）
```bash
clinic heapprofiler -- node src/services/api-server.js
```

**输出**：运行压测后停止服务，自动生成 `.clinic-*/*.html` 报告并在浏览器打开。

---

### 2. 0x（火焰图生成器）

专注于生成交互式火焰图，适合快速定位 CPU 热点。

#### 安装

```bash
npm install -g 0x
```

#### 使用方法

**单进程分析**
```bash
0x --output-dir ./profiles src/services/api-server.js
```

**多进程分析**（分析 master.js）
```bash
0x src/core/master.js
```

**输出**：按 Ctrl+C 停止后生成 `./profiles/*.html` 火焰图。

**火焰图阅读提示**：
- X 轴宽度 = CPU 时间占比
- Y 轴高度 = 调用栈深度
- 顶部宽条 = 热点函数
- 点击放大查看调用细节

---

### 3. Node.js 内置 CPU Profiler

无需安装，适合生产环境快速采样。

#### 使用方法

```bash
# 启动时开启 CPU profiler（每 500μs 采样一次）
node --cpu-prof --cpu-prof-interval=500 src/services/api-server.js

# 运行压测...

# 停止后生成 CPU.*.cpuprofile 文件
```

#### 分析 .cpuprofile 文件

**使用 Chrome DevTools**：
1. 打开 `chrome://inspect`
2. 点击 "Open dedicated DevTools for Node"
3. 切换到 **Profiler** 标签
4. 点击 **Load** 加载 `.cpuprofile` 文件
5. 查看火焰图和函数调用树

---

### 4. autocannon（HTTP 压测工具）

高性能 HTTP 基准测试工具，用于生成负载。

#### 安装

```bash
npm install -g autocannon
```

#### 使用方法

**基础压测**
```bash
autocannon -c 100 -d 60 http://localhost:3000/v1/chat/completions \
  -H "Authorization: Bearer AI_club2026" \
  -H "Content-Type: application/json" \
  -m POST \
  -b '{"model":"gpt-4","messages":[{"role":"user","content":"test"}]}'
```

**参数说明**：
- `-c 100`：100 个并发连接
- `-d 60`：持续 60 秒
- `-m POST`：HTTP 方法
- `-b`：请求体

**输出指标**：
- Latency（延迟）：p50/p90/p99/p99.9
- Throughput（吞吐量）：req/sec
- Errors（错误率）

**测试流式接口**
```bash
autocannon -c 50 -d 30 http://localhost:3000/v1/chat/completions \
  -H "Authorization: Bearer AI_club2026" \
  -H "Content-Type: application/json" \
  -m POST \
  -b '{"model":"gpt-4","messages":[{"role":"user","content":"hello"}],"stream":true}'
```

---

### 5. perf（Linux 系统级分析）

适合深入分析原生模块或 V8 引擎层面的问题。

#### 前置条件

```bash
# Ubuntu/Debian
sudo apt-get install linux-tools-common linux-tools-generic

# 安装火焰图生成脚本
git clone https://github.com/brendangregg/FlameGraph.git ~/FlameGraph
```

#### 使用方法

```bash
# 1. 启动服务并获取进程 ID
node src/services/api-server.js &
PID=$(pgrep -f api-server)

# 2. 记录 60 秒 CPU 事件（99Hz 采样）
sudo perf record -F 99 -p $PID -g -- sleep 60

# 3. 生成火焰图
sudo perf script | ~/FlameGraph/stackcollapse-perf.pl | ~/FlameGraph/flamegraph.pl > flame.svg

# 4. 浏览器打开 flame.svg
```

---

## 分析流程

### 完整诊断流程图

```
┌─────────────────────────┐
│ 1. 启动 Clinic flame    │
│    clinic flame --      │
│    npm run start:...    │
└───────────┬─────────────┘
            │
            ▼
┌─────────────────────────┐
│ 2. 运行压测 autocannon  │
│    (30-60 秒)           │
└───────────┬─────────────┘
            │
            ▼
┌─────────────────────────┐
│ 3. 停止服务查看报告     │
│    (自动打开 HTML)      │
└───────────┬─────────────┘
            │
            ▼
     ┌──────┴───────┐
     │ 发现问题类型  │
     └──┬───┬───┬───┘
        │   │   │
   ┌────┘   │   └────┐
   │        │        │
   ▼        ▼        ▼
CPU 密集  I/O 阻塞  事件循环延迟
   │        │        │
   ▼        ▼        ▼
优化计算  改异步调用  bubbleprof 分析
```

### 推荐顺序

1. **初步诊断**：Clinic flame（火焰图）
   - 发现 CPU 占用最高的函数
   - 识别同步计算热点

2. **深入分析**：根据初步结果选择
   - **CPU 密集型**：使用 0x 生成更详细的火焰图
   - **异步阻塞**：使用 Clinic bubbleprof
   - **内存问题**：使用 Clinic heapprofiler

3. **系统级验证**：perf（可选）
   - 分析原生模块性能
   - 验证 V8 优化效果

---

## 快速开始

### 一键分析脚本

```bash
#!/bin/bash
# 保存为 scripts/profile.sh

echo "🔥 启动性能分析..."

# 1. 启动 clinic flame
clinic flame -- npm run start:standalone &
CLINIC_PID=$!

# 等待服务启动
sleep 5

# 2. 运行压测
echo "📊 运行压测 30 秒..."
autocannon -c 50 -d 30 http://localhost:3000/v1/chat/completions \
  -H "Authorization: Bearer AI_club2026" \
  -H "Content-Type: application/json" \
  -m POST \
  -b '{"model":"gpt-4","messages":[{"role":"user","content":"hello"}],"stream":false}'

# 3. 停止服务
echo "🛑 停止服务并生成报告..."
kill -SIGINT $CLINIC_PID
wait $CLINIC_PID

echo "✅ 分析完成，报告已在浏览器打开"
```

### 使用方法

```bash
chmod +x scripts/profile.sh
./scripts/profile.sh
```

---

## 项目特定分析点

基于 AIClient-2-API 架构，以下是常见的性能瓶颈来源：

### 🔍 高优先级检查点

#### 1. 协议转换层（`src/converters/`）

**问题**：频繁的 JSON 序列化/反序列化

**检查**：
- `ConverterFactory.getConverter()` 调用频率
- `OpenAIConverter.convertRequest/Response()` CPU 占比
- `GeminiConverter` 的 token 计算逻辑

**优化方向**：
- 缓存 converter 实例
- 使用 JSON.parse/stringify 的原生优化
- 减少不必要的深拷贝

#### 2. Provider Pool 管理（`src/providers/provider-pool-manager.js`）

**问题**：定时健康检查、account 轮询

**检查**：
- `healthCheckInterval` 定时器频率
- `selectAccount()` 选择算法复杂度
- Redis 读写频率（如果启用）

**优化方向**：
- 增加健康检查间隔
- 实现懒加载健康检查（仅在失败时触发）
- 使用 Redis pipeline 批量操作

#### 3. 流式响应处理

**问题**：SSE 格式化、token 实时计算

**检查**：
- `generateContentStream()` 中的事件循环阻塞
- `calculateKiroTokenDistribution()` 调用频率
- 字符串拼接（`data: ${JSON.stringify(...)}\n\n`）

**优化方向**：
- 使用 Buffer 池减少内存分配
- 批量累积 token 计数（而非逐个字符计算）
- 预分配 SSE 格式模板

#### 4. 路由匹配（`src/handlers/request-handler.js`）

**问题**：model name prefix 正则匹配

**检查**：
- 正则表达式编译次数（是否缓存？）
- `getServiceAdapter()` 查找逻辑

**优化方向**：
- 使用 Map 替代正则匹配
- 预编译正则表达式

#### 5. Redis 序列化（如果启用）

**问题**：provider pools 频繁序列化

**检查**：
- `redis-config-manager.js` 的 `get/set` 调用频率
- JSON.stringify 大对象的 CPU 开销

**优化方向**：
- 使用 MessagePack 替代 JSON
- 实现写延迟合并（debounce）
- 分片存储大型 pool 对象

---

## 常见问题排查

### Q1: CPU 100% 但火焰图显示大部分时间在 V8 内部？

**可能原因**：
- 大量小对象分配导致 GC 压力
- 正则表达式回溯（ReDoS）

**分析方法**：
```bash
# 启用 GC 跟踪
node --trace-gc src/services/api-server.js

# 或使用 heapprofiler
clinic heapprofiler -- node src/services/api-server.js
```

### Q2: p99 延迟高但 CPU 不高？

**可能原因**：
- 事件循环阻塞（同步 I/O）
- 定时器/Promise 调度延迟

**分析方法**：
```bash
clinic bubbleprof -- node src/services/api-server.js
```

查看红色气泡（阻塞操作）。

### Q3: 多进程模式（master.js）如何分析？

**方法 1**：分析单个 worker
```bash
# 以 standalone 模式运行
clinic flame -- npm run start:standalone
```

**方法 2**：分析 master 进程
```bash
0x src/core/master.js
```

### Q4: Docker 环境如何分析？

**方法 1**：容器内安装工具
```dockerfile
# Dockerfile 添加
RUN npm install -g clinic 0x autocannon
```

**方法 2**：使用 --cpu-prof（无需额外工具）
```bash
docker exec -it <container> node --cpu-prof --cpu-prof-interval=500 src/services/api-server.js

# 拷贝 .cpuprofile 文件到宿主机
docker cp <container>:/app/CPU.*.cpuprofile ./
```

---

## 性能优化检查清单

- [ ] 运行 `clinic flame` 确认 CPU 热点函数
- [ ] 使用 `autocannon` 测试 p99 延迟 < 100ms
- [ ] 检查协议转换层是否有不必要的深拷贝
- [ ] 验证 Redis 操作是否使用 pipeline
- [ ] 确认正则表达式已预编译且缓存
- [ ] 检查流式响应是否批量处理（而非逐字符）
- [ ] 运行 `clinic bubbleprof` 排除事件循环阻塞
- [ ] 使用 `--trace-gc` 检查 GC 频率
- [ ] 对比优化前后的火焰图差异
- [ ] 在生产环境使用 `--cpu-prof` 验证优化效果

---

## 参考资料

- [Clinic.js 官方文档](https://clinicjs.org/)
- [0x 使用指南](https://github.com/davidmarkclements/0x)
- [Node.js 性能优化最佳实践](https://nodejs.org/en/docs/guides/simple-profiling/)
- [火焰图解读](https://www.brendangregg.com/flamegraphs.html)
- [autocannon GitHub](https://github.com/mcollina/autocannon)

---

## 更新日志

- **2026-01-29**：初始版本，包含 Clinic.js、0x、perf、autocannon 使用指南
