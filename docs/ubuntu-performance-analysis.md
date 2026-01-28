# Ubuntu 性能分析工具指南

## 概述

当程序出现性能问题，但通过静态代码分析无法找到线索时，需要使用运行时性能分析工具来定位瓶颈。本文档介绍在 Ubuntu 24.04 中常用的性能分析方法和工具。

## 1. 基础监控工具

### 1.1 top/htop - 实时进程监控

```bash
# 安装 htop（更友好的界面）
sudo apt update
sudo apt install htop

# 运行 htop
htop

# 按键说明：
# F5: 树形视图
# F6: 排序选择
# F9: 杀死进程
# Space: 标记进程
```

**关键指标：**
- `CPU%`: CPU 使用率
- `MEM%`: 内存使用率
- `TIME+`: 累计 CPU 时间
- `S`: 进程状态（R=运行, S=睡眠, D=不可中断睡眠）

### 1.2 pidstat - 进程统计

```bash
# 安装 sysstat 工具包
sudo apt install sysstat

# 监控特定进程的 CPU 使用（每秒更新）
pidstat -p <PID> 1

# 监控内存使用
pidstat -r -p <PID> 1

# 监控 I/O 操作
pidstat -d -p <PID> 1

# 监控线程级别的 CPU 使用
pidstat -t -p <PID> 1
```

## 2. CPU 性能分析

### 2.1 perf - Linux 性能分析器

```bash
# 安装 perf
sudo apt install linux-tools-common linux-tools-generic
sudo apt install linux-tools-$(uname -r)

# 记录进程的 CPU 性能数据（运行 30 秒）
sudo perf record -p <PID> -g -- sleep 30

# 查看性能报告
sudo perf report

# 实时监控进程
sudo perf top -p <PID>

# 分析特定事件（如缓存未命中）
sudo perf stat -p <PID> -e cache-misses,cache-references sleep 10
```

**perf report 输出解读：**
```
Overhead  Command  Shared Object       Symbol
  45.23%  node     node                [.] v8::internal::Parser::ParseFunctionLiteral
  12.45%  node     libc-2.35.so        [.] __memcpy_avx_unaligned
   8.67%  node     node                [.] v8::internal::Scanner::Scan
```

### 2.2 flamegraph - 火焰图生成

```bash
# 克隆火焰图工具
git clone https://github.com/brendangregg/FlameGraph
cd FlameGraph

# 记录性能数据
sudo perf record -F 99 -p <PID> -g -- sleep 60

# 生成火焰图
sudo perf script | ./stackcollapse-perf.pl | ./flamegraph.pl > flamegraph.svg

# 在浏览器中打开 flamegraph.svg
```

**火焰图解读：**
- X 轴：样本占比（越宽表示该函数占用 CPU 时间越多）
- Y 轴：调用栈深度
- 颜色：随机分配，无特殊含义
- 可点击交互，放大查看细节

## 3. 内存性能分析

### 3.1 valgrind - 内存分析工具

```bash
# 安装 valgrind
sudo apt install valgrind

# 内存泄漏检测
valgrind --leak-check=full --show-leak-kinds=all ./your_program

# 缓存性能分析
valgrind --tool=cachegrind ./your_program
cg_annotate cachegrind.out.<pid>

# 堆分析
valgrind --tool=massif ./your_program
ms_print massif.out.<pid>
```

### 3.2 heaptrack - 堆内存分析

```bash
# 安装 heaptrack
sudo apt install heaptrack heaptrack-gui

# 记录堆分配
heaptrack ./your_program

# 分析结果（GUI）
heaptrack_gui heaptrack.your_program.<pid>.gz

# 命令行分析
heaptrack_print heaptrack.your_program.<pid>.gz
```

### 3.3 pmap - 进程内存映射

```bash
# 查看进程内存映射
pmap -x <PID>

# 详细内存使用
cat /proc/<PID>/smaps

# 内存统计摘要
cat /proc/<PID>/status | grep -E "Vm|Rss"
```

## 4. I/O 性能分析

### 4.1 iotop - I/O 监控

```bash
# 安装 iotop
sudo apt install iotop

# 监控 I/O 使用
sudo iotop -p <PID>

# 只显示有 I/O 活动的进程
sudo iotop -o
```

### 4.2 strace - 系统调用跟踪

```bash
# 跟踪进程的系统调用
sudo strace -p <PID>

# 统计系统调用
sudo strace -c -p <PID>

# 只跟踪文件操作
sudo strace -e trace=file -p <PID>

# 只跟踪网络操作
sudo strace -e trace=network -p <PID>

# 跟踪并保存到文件
sudo strace -o trace.log -p <PID>
```

**strace 输出示例：**
```
% time     seconds  usecs/call     calls    errors syscall
------ ----------- ----------- --------- --------- ----------------
 52.35    0.234567        1234       190           read
 31.24    0.140123         701       200           write
 10.45    0.046890         234       200           poll
  5.96    0.026745         133       201           futex
```

### 4.3 lsof - 打开文件列表

```bash
# 查看进程打开的文件
sudo lsof -p <PID>

# 查看进程的网络连接
sudo lsof -i -p <PID>

# 查看特定文件被哪些进程打开
sudo lsof /path/to/file
```

## 5. Node.js/TypeScript 特定工具

### 5.1 Node.js 内置 Profiler

```bash
# 启动时开启 CPU profiler
node --prof your_script.js

# 处理生成的日志
node --prof-process isolate-0x*.log > processed.txt

# 使用 Chrome DevTools
node --inspect your_script.js
# 然后在 Chrome 中打开 chrome://inspect
```

### 5.2 clinic.js - Node.js 性能诊断

```bash
# 安装 clinic
npm install -g clinic

# CPU 分析
clinic doctor -- node your_script.js

# 火焰图
clinic flame -- node your_script.js

# 气泡图（异步操作）
clinic bubbleprof -- node your_script.js

# 堆分析
clinic heapprofiler -- node your_script.js
```

### 5.3 0x - 火焰图生成器

```bash
# 安装 0x
npm install -g 0x

# 生成火焰图
0x your_script.js

# 指定采样频率
0x -P 'autocannon -c 100 -d 30 http://localhost:3000' your_script.js
```

## 6. 实战案例：定位 Node.js 应用瓶颈

### 场景：Node.js 应用 CPU 使用率高

```bash
# 步骤 1: 找到进程 PID
ps aux | grep node

# 步骤 2: 使用 htop 确认 CPU 使用率
htop -p <PID>

# 步骤 3: 使用 perf 记录性能数据
sudo perf record -F 99 -p <PID> -g -- sleep 30

# 步骤 4: 生成火焰图
sudo perf script | ~/FlameGraph/stackcollapse-perf.pl | ~/FlameGraph/flamegraph.pl > cpu-flamegraph.svg

# 步骤 5: 使用 Node.js 内置 profiler
# 重启应用并添加 --prof 参数
node --prof your_script.js

# 步骤 6: 分析 profiler 输出
node --prof-process isolate-*.log > profile.txt
less profile.txt
```

### 场景：内存泄漏

```bash
# 步骤 1: 监控内存增长
pidstat -r -p <PID> 5

# 步骤 2: 使用 heaptrack
heaptrack node your_script.js

# 步骤 3: 分析堆快照
heaptrack_print heaptrack.node.*.gz | head -n 100

# 步骤 4: 使用 Node.js heap snapshot
# 在代码中添加：
const v8 = require('v8');
const fs = require('fs');
const heapSnapshot = v8.writeHeapSnapshot();
console.log('Heap snapshot written to', heapSnapshot);

# 步骤 5: 在 Chrome DevTools 中分析快照
# Memory -> Load -> 选择 .heapsnapshot 文件
```

### 场景：I/O 瓶颈

```bash
# 步骤 1: 监控 I/O
sudo iotop -p <PID>

# 步骤 2: 跟踪系统调用
sudo strace -c -p <PID>

# 步骤 3: 查看打开的文件
sudo lsof -p <PID>

# 步骤 4: 分析文件操作
sudo strace -e trace=file -p <PID> -o file-trace.log

# 步骤 5: 统计文件操作频率
grep -E "open|read|write|close" file-trace.log | cut -d'(' -f1 | sort | uniq -c | sort -rn
```

## 7. 性能优化检查清单

### CPU 瓶颈

- [ ] 使用 `perf top` 确认热点函数
- [ ] 生成火焰图查看调用栈
- [ ] 检查是否有死循环或低效算法
- [ ] 查看是否有不必要的计算
- [ ] 考虑使用缓存减少重复计算

### 内存瓶颈

- [ ] 使用 `pmap` 查看内存分布
- [ ] 使用 `heaptrack` 检测内存泄漏
- [ ] 检查是否有大对象未释放
- [ ] 查看是否有循环引用
- [ ] 考虑使用对象池或流式处理

### I/O 瓶颈

- [ ] 使用 `iotop` 监控 I/O 使用
- [ ] 使用 `strace` 统计系统调用
- [ ] 检查是否有频繁的小文件读写
- [ ] 查看是否可以批量处理
- [ ] 考虑使用缓冲或异步 I/O

### 网络瓶颈

- [ ] 使用 `netstat` 查看连接状态
- [ ] 使用 `tcpdump` 抓包分析
- [ ] 检查是否有连接池配置不当
- [ ] 查看是否有超时设置过长
- [ ] 考虑使用 keep-alive 或连接复用

## 8. 常用命令速查

```bash
# 查找进程
ps aux | grep <process_name>
pgrep -f <process_name>

# 实时监控
htop -p <PID>
watch -n 1 'ps -p <PID> -o %cpu,%mem,cmd'

# CPU 分析
sudo perf top -p <PID>
sudo perf record -p <PID> -g -- sleep 30 && sudo perf report

# 内存分析
pmap -x <PID>
cat /proc/<PID>/status | grep -E "Vm|Rss"

# I/O 分析
sudo iotop -p <PID>
sudo strace -c -p <PID>

# 网络分析
sudo lsof -i -p <PID>
sudo netstat -anp | grep <PID>

# 线程分析
ps -T -p <PID>
top -H -p <PID>
```

## 9. 工具对比

| 工具      | 用途         | 优点         | 缺点         | 适用场景          |
|-----------|--------------|--------------|--------------|-------------------|
| perf      | CPU 分析     | 低开销，详细 | 需要 root    | 生产环境 CPU 分析 |
| valgrind  | 内存分析     | 精确检测     | 运行慢       | 开发环境内存调试  |
| strace    | 系统调用     | 实时跟踪     | 有性能影响   | I/O 问题诊断      |
| heaptrack | 堆分析       | 可视化好     | 需要重启     | 内存泄漏定位      |
| clinic.js | Node.js 诊断 | 专门优化     | 仅限 Node.js | Node.js 应用分析  |

## 10. 最佳实践

1. **先宏观后微观**：从 htop 开始，确认问题类型，再使用专门工具
2. **最小化影响**：在生产环境使用低开销工具（如 perf）
3. **保存数据**：记录性能数据以便后续分析和对比
4. **重复验证**：多次测试确保问题可重现
5. **对比基线**：与正常状态对比，找出差异
6. **逐步排查**：一次只改变一个变量
7. **文档记录**：记录分析过程和发现的问题

## 参考资源

- [Linux perf Wiki](https://perf.wiki.kernel.org/)
- [Brendan Gregg's Performance Tools](https://www.brendangregg.com/)
- [Node.js Profiling Guide](https://nodejs.org/en/docs/guides/simple-profiling/)
- [Linux Kernel Perf Security](https://www.kernel.org/doc/html/latest/admin-guide/perf-security.html)
