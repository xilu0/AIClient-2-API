// Package claude provides Claude API constants for context limits.
package claude

const (
	// ContextWindowTokens is the Claude API context window size (200K tokens).
	// Input tokens + max_tokens must not exceed this.
	ContextWindowTokens = 200000

	// MaxOutputTokens is the maximum output tokens for Claude 4.5 (includes thinking).
	MaxOutputTokens = 64000

	// MaxKiroRequestBodyDefault is the default max Kiro request body size (32 MB).
	MaxKiroRequestBodyDefault = 32 << 20
)
