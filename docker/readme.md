curl http://172.26.2.112:3000/claude-kiro-oauth/v1/messages \
  -H "Content-Type: application/json" \
  -H "X-API-Key: AI_club2026" \
  -d '{
    "model": "claude-sonnet-4-5-20250929",
    "max_tokens": 1000,
    "messages": [{"role": "user", "content": "Hello! who are you?"}]
  }' | jq .


curl http://172.26.2.112:3000/claude-kiro-oauth/v1/messages \
    -H "Content-Type: application/json" \
    -H "X-API-Key: AI_club2026" \
    -d '{
      "model": "claude-sonnet-4-5-20250929",
      "max_tokens": 1000,
      "system": [{"type": "text", "text": "You are a helpful assistant.", "cache_control": {"type": "ephemeral"}}],
      "messages": [{"role": "user", "content": "Hello! who are you?"}]
    }' | jq .

export ANTHROPIC_BASE_URL="http://172.26.2.112:8080"
export ANTHROPIC_AUTH_TOKEN="sk_cccccccccccccccc"


curl http://172.26.2.112:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "X-API-Key: sk_cccccccccccccccc" \
  -d '{
    "model": "claude-sonnet-4-5-20250929",
    "max_tokens": 1000,
    "messages": [{"role": "user", "content": "Hello! who are you?"}]
  }' | jq .


'claude-kiro-oauth': [
        'claude-opus-4-5',
        'claude-opus-4-5-20251101',
        'claude-haiku-4-5',
        'claude-sonnet-4-5',
        'claude-sonnet-4-5-20250929',
        'claude-sonnet-4-20250514',
        'claude-3-7-sonnet-20250219'
    ],


curl http://127.0.0.1:8081/v1/messages \
  -H "Content-Type: application/json" \
  -H "X-API-Key: AI_club2026" \
  -d '{
    "model": "claude-haiku-4-5",
    "max_tokens": 1000,
    "messages": [{"role": "user", "content": "Hello! who are you?"}]
  }'

export ANTHROPIC_BASE_URL="http://127.0.0.1:8081"
export ANTHROPIC_AUTH_TOKEN="AI_club2026"

curl http://172.26.2.112:3000/claude-kiro-oauth/v1/messages \
  -H "Content-Type: application/json" \
  -H "X-API-Key: AI_club2026" \
  -d '{
    "model": "claude-sonnet-4-5-20250929",
    "max_tokens": 1000,
    "messages": [{"role": "user", "content": "Hello! who are you?"}]
  }' | jq .

curl http://172.26.2.112:3000/claude-kiro-oauth/v1/messages \
  -H "Content-Type: application/json" \
  -H "X-API-Key: AI_club2026" \
  -d '{
    "model": "claude-opus-4-5-20251101",
    "max_tokens": 1000,
    "messages": [{"role": "user", "content": "Hello! who are you?"}]
  }' | jq .


curl http://127.0.0.1:8081/v1/messages \
  -H "Content-Type: application/json" \
  -H "X-API-Key: AI_club2026" \
  -d '{
    "model": "claude-sonnet-4-5-20250929",
    "max_tokens": 1000,
    "messages": [{"role": "user", "content": "Hello! who are you?"}]
  }'

curl http://172.26.2.112:8081/v1/messages \
  -H "Content-Type: application/json" \
  -H "X-API-Key: AI_club2026" \
  -d '{
    "model": "claude-haiku-4-5-20251001",
    "max_tokens": 1000,
    "messages": [{"role": "user", "content": "Hello! who are you?"}]
  }'

curl http://127.0.0.1:8081/v1/messages \
  -H "Content-Type: application/json" \
  -H "X-API-Key: AI_club2026" \
  -d '{
    "model": "claude-opus-4-5-20251101",
    "max_tokens": 1000,
    "messages": [{"role": "user", "content": "Hello! who are you?"}]
  }'

claude-haiku-4-5-20251001 claude-sonnet-4-5-20250929 claude-opus-4-5-20251101

功能测试已经完成，请审查代码实现，确保没有性能问题

export ANTHROPIC_BASE_URL="http://127.0.0.1:8081"
export ANTHROPIC_AUTH_TOKEN="AI_club2026"