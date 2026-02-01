# Kiro Model Names Integration Test

测试 Kiro API 接受哪些模型名称格式。

## 运行方式

### 测试所有模型名称

```bash
INTEGRATION_TEST=true REDIS_URL=redis://localhost:6379 go test ./tests/integration/... -v -run TestKiroModelNames -timeout 120s
```

### 测试单个模型

```bash
INTEGRATION_TEST=true REDIS_URL=redis://localhost:6379 TEST_MODEL="claude-haiku-4.5" go test ./tests/integration/... -v -run TestKiroSingleModel -timeout 60s
```

### 使用已有的 kiro_request.json 直接发送

```bash
INTEGRATION_TEST=true REDIS_URL=redis://localhost:6379 KIRO_REQUEST_FILE=/path/to/kiro_request.json go test ./tests/integration/... -v -run TestKiroRawRequest
```

## 环境变量

| 变量 | 必需 | 说明 |
|------|------|------|
| `INTEGRATION_TEST` | 是 | 设为 `true` 启用集成测试 |
| `REDIS_URL` | 是 | Redis 连接地址 |
| `TEST_MODEL` | 否 | 单个模型测试时指定模型名称 |
| `KIRO_REQUEST_FILE` | 否 | 直接发送已有的 kiro_request.json 文件 |

## 测试的模型名称

### Sonnet
- `CLAUDE_SONNET_4_5_20250929_V1_0`
- `claude-sonnet-4.5`
- `claude-sonnet-4-5`
- `claude-sonnet-4-5-20250929`

### Haiku
- `CLAUDE_HAIKU_4_5_20251001_V1_0`
- `claude-haiku-4.5`
- `claude-haiku-4-5`
- `claude-haiku-4-5-20251001`

### Opus
- `CLAUDE_OPUS_4_5_20251101_V1_0`
- `claude-opus-4.5`
- `claude-opus-4-5`
- `claude-opus-4-5-20251101`

### Other
- `CLAUDE_3_7_SONNET_20250219_V1_0`
- `claude-3-7-sonnet-20250219`

## 对比 Node.js 和 Go 的 kiro_request.json

已生成的对比文件在 `tests/testdata/compare/` 目录：
- `nodejs_kiro_request.json` - Node.js 生成的
- `go_kiro_request.json` - Go 生成的

```bash
# 排序后对比（忽略键顺序和动态字段）
jq -S 'del(.conversationState.conversationId)' tests/testdata/compare/go_kiro_request.json > /tmp/go_sorted.json
jq -S 'del(.conversationState.conversationId)' tests/testdata/compare/nodejs_kiro_request.json > /tmp/nodejs_sorted.json
diff /tmp/go_sorted.json /tmp/nodejs_sorted.json
```
