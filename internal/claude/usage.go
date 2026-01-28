// Package claude provides token distribution for Claude API compatibility.
package claude

import (
	"encoding/json"
	"strings"
)

// Constants for token calculation
const (
	// TotalContextTokens is the total context window size for Kiro (173k tokens)
	TotalContextTokens = 172500

	// CharsPerToken is the average number of characters per token
	// Used for simple estimation when tokenizer is not available
	CharsPerToken = 4
)

// TokenUsage represents the distributed token usage with cache tokens.
type TokenUsage struct {
	InputTokens              int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
}

// DistributeTokens applies the 1:2:25 token distribution ratio.
// This matches the Node.js implementation in RatioTokenDistribution.js.
//
// Algorithm:
//   - Total parts = 1 + 2 + 25 = 28
//   - input_tokens = floor(tokens * 1 / 28)
//   - cache_creation_input_tokens = floor(tokens * 2 / 28)
//   - cache_read_input_tokens = tokens - input - creation (gets remainder)
//
// Threshold: 100 tokens (below this, no distribution applied)
//
// Example (from CLAUDE.md):
//
//	DistributeTokens(1000) = { input_tokens: 35, cache_creation_input_tokens: 71, cache_read_input_tokens: 894 }
func DistributeTokens(inputTokens int) TokenUsage {
	// Threshold check - below 100 tokens, no distribution
	if inputTokens < 100 {
		return TokenUsage{InputTokens: inputTokens}
	}

	const totalParts = 28 // 1 + 2 + 25

	// Calculate each component
	input := inputTokens * 1 / totalParts
	creation := inputTokens * 2 / totalParts
	read := inputTokens - input - creation // Remainder goes to cache_read

	return TokenUsage{
		InputTokens:              input,
		CacheCreationInputTokens: creation,
		CacheReadInputTokens:     read,
	}
}

// ToUsage converts TokenUsage to a Usage struct for API responses.
func (t TokenUsage) ToUsage(outputTokens int) Usage {
	return Usage{
		InputTokens:              t.InputTokens,
		OutputTokens:             outputTokens,
		CacheCreationInputTokens: t.CacheCreationInputTokens,
		CacheReadInputTokens:     t.CacheReadInputTokens,
	}
}

// TotalInputTokens returns the sum of all input-related tokens.
func (t TokenUsage) TotalInputTokens() int {
	return t.InputTokens + t.CacheCreationInputTokens + t.CacheReadInputTokens
}

// EstimateInputTokens estimates the input token count from a request.
// Uses simple character-based estimation (chars / 4).
func EstimateInputTokens(req *MessageRequest) int {
	var totalChars int

	// Count system prompt
	systemStr := req.GetSystemString()
	if systemStr != "" {
		totalChars += len(systemStr)
	}

	// Count thinking prefix if enabled
	if req.Thinking != nil && req.Thinking.Type == "enabled" {
		// Add thinking prefix tokens
		totalChars += 100 // Approximate overhead for thinking mode
	}

	// Count all messages
	for _, msg := range req.Messages {
		totalChars += countContentChars(msg.Content)
	}

	// Convert chars to tokens (approximate: 4 chars per token)
	tokens := totalChars / CharsPerToken
	if tokens < 1 && totalChars > 0 {
		tokens = 1
	}

	return tokens
}

// countContentChars counts characters in message content.
// Content can be a string or array of content blocks.
func countContentChars(content json.RawMessage) int {
	if len(content) == 0 {
		return 0
	}

	// Try to parse as string first
	var str string
	if err := json.Unmarshal(content, &str); err == nil {
		return len(str)
	}

	// Try to parse as array of content blocks
	var blocks []ContentBlock
	if err := json.Unmarshal(content, &blocks); err == nil {
		var total int
		for _, block := range blocks {
			switch block.Type {
			case "text":
				total += len(block.Text)
			case "thinking":
				total += len(block.Thinking)
			case "tool_use":
				if block.Input != nil {
					total += len(block.Input)
				}
			case "tool_result":
				// Tool results can have nested content
				if len(block.Content) > 0 {
					total += countContentChars(block.Content)
				}
			}
		}
		return total
	}

	// Fallback: count raw JSON length
	return len(content)
}

// CalculateInputTokensFromPercentage calculates input tokens from context usage percentage.
// Formula: inputTokens = (TotalContextTokens * percentage / 100) - outputTokens
func CalculateInputTokensFromPercentage(percentage float64, outputTokens int) int {
	if percentage <= 0 {
		return 0
	}
	totalTokens := int(float64(TotalContextTokens) * percentage / 100)
	inputTokens := totalTokens - outputTokens
	if inputTokens < 0 {
		inputTokens = 0
	}
	return inputTokens
}

// CountTextTokens provides a simple token count estimation for text.
// Uses character count divided by average chars per token.
func CountTextTokens(text string) int {
	if text == "" {
		return 0
	}
	// Simple estimation: ~4 characters per token on average
	tokens := len(strings.TrimSpace(text)) / CharsPerToken
	if tokens < 1 && len(text) > 0 {
		tokens = 1
	}
	return tokens
}
