# Spec 002: 修复 Redis 存储模式下的并发文件操作问题

## 概述

**问题**：在启用 Redis 存储后，`provider-api.js` 中的 UI API 仍然直接使用同步文件操作，导致高并发场景下存在数据竞态条件。

**目标**：让所有 UI API 操作完全委托给 storage adapter，利用 Redis 的原子性保证并发安全。

## 背景

从提交历史可以看到，团队已经尝试修复过这个问题：
- `211da14`: "resolve provider deletion resurrection issue"
- `2d44a54`: "ensure immediate file save"
- `fafaaa1`: "ensure file sync happens even when Redis operations fail"

但这些都是临时补丁，没有从架构层面解决问题。

## 核心改动

将 `provider-api.js` 中所有函数的条件判断：
```javascript
// 改前
if (!adapter || adapter.getType() !== 'redis') {
    // 直接文件操作
}

// 改后
if (!adapter) {
    // 仅在无 adapter 时降级
}
```

## 影响范围

- **修改文件**: 1 个（`src/ui-modules/provider-api.js`）
- **修改函数**: 11 个 UI API 处理函数（10个写操作 + 1个读操作）
- **修改行数**: 约 100-150 行

## 文档结构

- `plan.md` - 详细的实施计划，包含问题分析、解决方案、测试方案
- `implementation.md` - 实施记录，包含修改详情、统计数据、验证结果
- `final-fix.md` - handleGetProviderType 函数的最终修复（移除遗漏的文件降级）
- `redis-only-implementation.md` - Redis-Only 架构实施记录（移除 provider_pools.json）
- `troubleshooting.md` - 问题诊断和解决过程
- `README.md` - 本文件，概述说明

## 状态

- [x] 问题分析完成（3个探索 agent）
- [x] 解决方案设计完成
- [x] 代码实施（提交 928cf26）
- [x] 优化增强（提交 fce6b27 - 添加严格模式和降级警告）
- [x] 最终修复（修复 handleGetProviderType 函数的文件降级）
- [x] **Redis-Only 架构实施**（移除 provider_pools.json，强制 Redis 存储）
- [x] 测试验证（服务成功启动，API 正常工作）
- [x] 文档更新（完整的实施文档和故障排查文档）

## 重要突破性改变

### 2026-01-26: Redis-Only 架构
- **移除** `configs/provider_pools.json` 文件（备份为 `.removed`）
- **修改** `src/core/storage-factory.js` - 强制要求 Redis，移除文件降级
- **修改** `src/core/file-storage-adapter.js` - 处理 `poolsPath: null`，provider pools 方法返回空数据
- **修改** `configs/config.json` - 添加 Redis 配置，默认启用
- **结果**: 服务直接使用 Redis 启动，无需环境变量，provider pools 完全存储在 Redis

## 相关资源

- 分支: `fix/redis-storage-concurrent-file-io`
- 相关 Spec: `001-redis-config` (Redis 存储架构设计)
