# Node.js 集成测试：直接发送 kiro_request.json

## 用途

将 debug dump 中的 `kiro_request.json` 直接发送到 Kiro API，绕过 Go/Node.js 服务层。
用于确认问题在请求体本身还是在服务层的请求构建逻辑。

## 前置条件

- Redis 运行中（存有 Kiro 账号和 token）
- `ioredis` 和 `axios` 已安装（项目依赖中已包含）

## 基本用法

```bash
# 发送 Node.js dump
node tests/integration/send-kiro-request.mjs kiro-debug-31/nodejs/d3ef718c-.../kiro_request.json

# 发送 Go dump（对比用）
node tests/integration/send-kiro-request.mjs kiro-debug/errors/2b83ad92-.../kiro_request.json
```

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `REDIS_URL` | `redis://localhost:6379` | Redis 连接地址 |
| `KIRO_ACCOUNT_UUID` | 自动选择健康账号 | 指定使用的账号 UUID |
| `KIRO_REGION` | 从 token/账号读取 | 覆盖 region |
| `VERBOSE` | `false` | 显示所有 SSE 事件 |

## 典型使用场景

### 场景 1：验证 dump 文件是否可直接发送成功

```bash
node tests/integration/send-kiro-request.mjs /path/to/kiro_request.json
```

如果成功，说明请求体没问题，问题在 Go 服务的请求构建。
如果失败，说明请求体本身有问题（如包含 Kiro API 不支持的工具 schema）。

### 场景 2：对比 Go dump vs Node.js dump

```bash
# 分别发送，对比结果
node tests/integration/send-kiro-request.mjs kiro-debug/errors/<go-session>/kiro_request.json
node tests/integration/send-kiro-request.mjs kiro-debug-31/nodejs/<node-session>/kiro_request.json
```

### 场景 3：指定账号

```bash
KIRO_ACCOUNT_UUID=d6c76e76-654e-49b7-9f47-1234513bf310 \
  node tests/integration/send-kiro-request.mjs /path/to/kiro_request.json
```

### 场景 4：配合 Go 诊断测试

```bash
# 先用 Go 诊断测试定位问题工具
KIRO_REQUEST_FILE=/path/to/kiro_request.json \
  go test ./tests/integration/... -v -run TestKiroRequestDiagnose -timeout 300s

# 再用 Node.js 测试验证修复后的请求
node tests/integration/send-kiro-request.mjs /path/to/kiro_request.json
```

## 输出示例

### 成功

```
[10:55:38.771] Reading request file: /path/to/kiro_request.json
[10:55:38.823] JSON compacted: 196095 → 108739 bytes (45% reduction)
[10:55:38.825] Request summary: model=CLAUDE_SONNET_4_5_20250929_V1_0 messages=2 tools=87 size=108739 bytes
[10:55:38.880] Target URL: https://q.us-east-1.amazonaws.com/generateAssistantResponse
[10:55:38.883] Sending request (session=7e63b2cc-...)...
[10:55:40.100] Response status: 200
[10:55:40.200] [message_start] model=claude-sonnet-4.5 id=msg_xxx
[10:55:41.500] [message_stop]
[10:55:41.500] SUCCESS: Received 42 events
```

### 失败

```
[10:55:40.294] ERROR: HTTP 400 from Kiro API
[10:55:40.295] ERROR: Response body: {"message":"Improperly formed request.","reason":null}
```

## 与 Go 集成测试的对比

| 维度 | Node.js 测试 | Go TestKiroRequestFromFile |
|------|-------------|---------------------------|
| 命令 | `node tests/integration/send-kiro-request.mjs <file>` | `KIRO_REQUEST_FILE=<file> go test ./tests/integration/... -v -run TestKiroRequestFromFile` |
| HTTP 客户端 | axios（与生产一致） | Go net/http |
| Headers | 与 claude-kiro.js 生产完全一致 | 与 Go kiro.Client 一致 |
| 请求体 | 原样发送（JSON compact） | 原样发送（JSON compact） |
| 用途 | 排除 HTTP 层差异 | 排除 HTTP 层差异 |

两个测试发送相同的请求体，如果结果不同则说明 HTTP 层（headers/client）有差异。
如果结果相同则说明问题在请求体内容。
