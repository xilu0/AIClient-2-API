# Provider Pools 同步工具

## 概述

`sync:pools` 工具用于将 `provider_pools.json` 与 `configs/` 目录下实际存在的 token 文件保持同步。

当你手动删除或添加 token 文件后，使用此工具可以自动更新 `provider_pools.json`，确保配置文件与实际文件一致。

## 问题场景

**问题**：`provider_pools.json` 中有 62 个账户，但 `configs/kiro/` 目录下实际只有 4 个 token 文件。

**原因**：
- 手动删除了 token 文件
- 文件被移动或重命名
- 配置文件与实际文件不同步

**后果**：
- 服务启动时尝试加载不存在的文件
- 产生大量错误日志
- 影响系统性能

## 使用方法

### 1. 预览模式（推荐首先使用）

查看会有什么变化，不实际修改文件：

```bash
npm run sync:pools -- --dry-run
```

**输出示例**：
```
=== Provider Pools Sync Tool ===

Reading current provider_pools.json...
Current state:
  gemini-cli-oauth: 0 providers
  openai-qwen-oauth: 0 providers
  gemini-antigravity: 0 providers
  openai-iflow: 0 providers
  claude-kiro-oauth: 62 providers
  openai-codex-oauth: 0 providers

Scanning token files...
Found on disk:
  claude-kiro-oauth: 4 providers
  gemini-cli-oauth: 0 providers
  openai-qwen-oauth: 0 providers
  openai-iflow: 0 providers
  openai-codex-oauth: 0 providers
  gemini-antigravity: 0 providers

--- Changes ---
  gemini-cli-oauth: no change (0)
  openai-qwen-oauth: no change (0)
  gemini-antigravity: no change (0)
  openai-iflow: no change (0)
  claude-kiro-oauth: 62 → 4 (-58)
  openai-codex-oauth: no change (0)

[DRY RUN] Changes would be applied but no files will be modified.
```

### 2. 同步并创建备份（推荐）

执行同步并自动创建备份文件：

```bash
npm run sync:pools -- --backup
```

备份文件会保存为 `provider_pools.json.backup.{timestamp}`

### 3. 直接同步（不创建备份）

```bash
npm run sync:pools
```

### 4. 查看帮助

```bash
npm run sync:pools -- --help
```

## 扫描规则

工具会扫描以下目录和文件模式：

| Provider Type | 目录 | 文件模式 | 凭证路径字段 |
|--------------|------|---------|------------|
| `claude-kiro-oauth` | `configs/kiro/` | `*kiro*.json` | `KIRO_OAUTH_CREDS_FILE_PATH` |
| `gemini-cli-oauth` | `configs/gemini/` | `*.json` | `GEMINI_OAUTH_CREDS_FILE_PATH` |
| `openai-qwen-oauth` | `configs/qwen/` | `oauth_creds.json` | `QWEN_OAUTH_CREDS_FILE_PATH` |
| `openai-iflow` | `configs/iflow/` | `oauth_creds.json` | `IFLOW_OAUTH_CREDS_FILE_PATH` |
| `openai-codex-oauth` | `configs/codex/` | `codex-*.json` | `CODEX_OAUTH_CREDS_FILE_PATH` |
| `gemini-antigravity` | `configs/antigravity/` | `*.json` | `GEMINI_ANTIGRAVITY_CREDS_FILE_PATH` |

## 生成的 Provider 配置

每个扫描到的 token 文件会生成一个 provider 配置：

```json
{
  "KIRO_OAUTH_CREDS_FILE_PATH": "./configs/kiro/1769414953949_kiro-auth-token/1769414953949_kiro-auth-token.json",
  "uuid": "3e3073c2-5e30-4a19-8a01-774fc3e32f3e",
  "checkModelName": "claude-haiku-4-5",
  "checkHealth": false,
  "isHealthy": true,
  "isDisabled": false,
  "lastUsed": null,
  "usageCount": 0,
  "errorCount": 0,
  "lastErrorTime": null,
  "lastHealthCheckTime": null,
  "lastHealthCheckModel": null,
  "lastErrorMessage": null,
  "customName": null,
  "needsRefresh": false,
  "refreshCount": 0
}
```

## 使用流程示例

### 场景：清理不需要的账户

```bash
# 1. 手动删除不需要的 token 文件
rm -rf configs/kiro/old-token-*

# 2. 预览同步变化
npm run sync:pools -- --dry-run

# 3. 如果确认无误，执行同步并备份
npm run sync:pools -- --backup

# 4. 验证结果
cat configs/provider_pools.json | jq '.["claude-kiro-oauth"] | length'

# 5. 重启服务
npm start
```

### 场景：添加新账户

```bash
# 1. 添加新的 token 文件到相应目录
cp new-token.json configs/kiro/1234567890_kiro-auth-token/

# 2. 同步配置
npm run sync:pools -- --backup

# 3. 重启服务
npm start
```

## 与 Redis 迁移的结合使用

如果你使用 Redis 存储，同步后需要重新迁移：

```bash
# 1. 同步 provider_pools.json
npm run sync:pools -- --backup

# 2. 迁移到 Redis
npm run migrate:redis -- --force --verify

# 3. 启动服务（Redis 模式）
REDIS_ENABLED=true REDIS_URL=redis://localhost:6379 npm start
```

## 恢复备份

如果同步后发现问题，可以恢复备份：

```bash
# 列出所有备份
ls -lh configs/provider_pools.json.backup.*

# 恢复最新备份
cp configs/provider_pools.json.backup.1769429945419 configs/provider_pools.json
```

## 注意事项

1. **同步前备份**：
   - 建议始终使用 `--backup` 参数
   - 或手动备份：`cp configs/provider_pools.json configs/provider_pools.json.backup`

2. **验证 token 文件**：
   - 工具会尝试读取每个 token 文件验证其有效性
   - 无效的 JSON 文件会被跳过并显示警告

3. **UUID 生成**：
   - 每次同步会为所有 provider 生成新的 UUID
   - 如果需要保留原有 UUID，请先备份

4. **目录结构**：
   - Kiro token 文件通常在子目录中（如 `kiro/1769414953949_kiro-auth-token/xxx.json`）
   - 其他 provider 可能直接在目录下（如 `gemini/oauth_creds.json`）

5. **重启服务**：
   - 同步后必须重启服务才能生效
   - 如果使用 Redis，还需要重新迁移

## 故障排查

### 问题：同步后账户数量仍然不对

**原因**：可能有隐藏文件或子目录未被扫描

**解决**：
```bash
# 检查目录结构
find configs/kiro -type f -name "*.json"

# 查看扫描日志
npm run sync:pools -- --dry-run | grep "Found on disk"
```

### 问题：Token 文件存在但未被扫描到

**原因**：文件名不匹配扫描模式

**解决**：
- 检查文件名是否符合模式（如 Kiro 需要包含 "kiro"）
- 确认文件在正确的目录下
- 检查文件是否为有效的 JSON

### 问题：同步后服务无法启动

**原因**：token 文件路径不正确

**解决**：
```bash
# 恢复备份
cp configs/provider_pools.json.backup.* configs/provider_pools.json

# 检查 token 文件路径
cat configs/provider_pools.json | jq '.["claude-kiro-oauth"][0]'
```

## 相关命令

- `npm run migrate:redis` - 迁移配置到 Redis
- `npm run export:redis` - 从 Redis 导出配置
- `npm run init:redis` - 初始化 Redis 配置
- `npm start` - 启动服务

## 技术细节

**脚本位置**：`src/cli/sync-provider-pools.js`

**工作原理**：
1. 读取当前的 `provider_pools.json`
2. 扫描 `configs/` 目录下的所有 token 文件
3. 为每个 token 文件生成 provider 配置
4. 比较新旧配置并显示变化
5. 写入更新后的 `provider_pools.json`

**依赖**：
- `uuid` - 生成 UUID
- `fs/promises` - 异步文件操作
- Node.js ES modules

## 版本历史

- **v1.0.0** (2026-01-26): 初始版本
  - 支持 6 种 provider 类型
  - 自动扫描 token 文件
  - 支持预览和备份模式
