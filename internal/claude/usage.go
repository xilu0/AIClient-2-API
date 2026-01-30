// Package claude provides token distribution for Claude API compatibility.
package claude

import (
	"encoding/json"
)

// Constants for token calculation
const (
	// TotalContextTokens is the total context window size for Kiro (173k tokens)
	TotalContextTokens = 172500

	// CharsPerToken is the average number of characters per token
	// Used for simple estimation when tokenizer is not available
	// Using 3 instead of 4 for conservative estimation (overestimate tokens)
	CharsPerToken = 3

	// ImageTokensEstimate is the conservative token estimate for images
	// Claude typically uses 1000-5000 tokens per image depending on size
	// Using 2500 as a conservative middle estimate
	ImageTokensEstimate = 2500

	// DocumentTokensMultiplier is the multiplier for document base64 data
	// base64 to bytes ratio is ~0.75, then divide by chars per token
	// Using conservative estimate: 0.75 / 3 ≈ 0.25
	DocumentTokensMultiplier = 0.25

	// ToolOverheadTokens is the overhead tokens per tool definition
	// Accounts for JSON structure, formatting, etc.
	ToolOverheadTokens = 20

	// MessageOverheadTokens is the overhead tokens per message
	// Accounts for role markers, formatting, etc.
	MessageOverheadTokens = 4
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
// Uses conservative character-based estimation to avoid exceeding model limits.
// This implementation intentionally overestimates to prevent context overflow.
func EstimateInputTokens(req *MessageRequest) int {
	var totalTokens int

	// Count system prompt
	systemStr := req.GetSystemString()
	if systemStr != "" {
		totalTokens += countTextTokens(systemStr)
	}

	// Count thinking prefix if enabled
	if req.Thinking != nil && req.Thinking.Type == "enabled" {
		// Add thinking prefix tokens (conservative estimate)
		totalTokens += 50
	}

	// Count all messages
	for _, msg := range req.Messages {
		totalTokens += MessageOverheadTokens // Role marker overhead
		totalTokens += countMessageTokens(msg.Content)
	}

	// Count tool definitions
	for _, tool := range req.Tools {
		totalTokens += ToolOverheadTokens
		totalTokens += countTextTokens(tool.Name)
		totalTokens += countTextTokens(tool.Description)
		if len(tool.InputSchema) > 0 {
			totalTokens += len(tool.InputSchema) / CharsPerToken
		}
	}

	if totalTokens < 1 {
		totalTokens = 1
	}

	return totalTokens
}

// countTextTokens counts tokens for plain text using conservative estimation.
func countTextTokens(text string) int {
	if text == "" {
		return 0
	}
	tokens := len(text) / CharsPerToken
	if tokens < 1 {
		tokens = 1
	}
	return tokens
}

// countMessageTokens counts tokens for message content.
// Handles string content, content blocks (text, image, document, tool_use, tool_result).
func countMessageTokens(content json.RawMessage) int {
	if len(content) == 0 {
		return 0
	}

	// Try to parse as string first
	var str string
	if err := json.Unmarshal(content, &str); err == nil {
		return countTextTokens(str)
	}

	// Try to parse as array of content blocks
	var blocks []ContentBlock
	if err := json.Unmarshal(content, &blocks); err == nil {
		var total int
		for _, block := range blocks {
			total += countContentBlockTokens(&block)
		}
		return total
	}

	// Fallback: conservative estimate from raw JSON length
	return len(content) / CharsPerToken
}

// countContentBlockTokens counts tokens for a single content block.
func countContentBlockTokens(block *ContentBlock) int {
	switch block.Type {
	case "text":
		return countTextTokens(block.Text)

	case "thinking":
		return countTextTokens(block.Thinking)

	case "image":
		// Conservative fixed estimate for images
		return ImageTokensEstimate

	case "document":
		// Estimate from base64 data if available
		if block.Source != nil && block.Source.Data != "" {
			tokens := int(float64(len(block.Source.Data)) * DocumentTokensMultiplier)
			if tokens < 100 {
				tokens = 100 // Minimum for any document
			}
			return tokens
		}
		return 500 // Default estimate for documents without data

	case "tool_use":
		tokens := countTextTokens(block.Name)
		if len(block.Input) > 0 {
			tokens += len(block.Input) / CharsPerToken
		}
		return tokens

	case "tool_result":
		// Tool results can have nested content
		if len(block.Content) > 0 {
			return countMessageTokens(block.Content)
		}
		return 0

	default:
		return 0
	}
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
// Uses character count divided by average chars per token (conservative).
func CountTextTokens(text string) int {
	return countTextTokens(text)
}

// TokenEstimateDetails contains detailed breakdown of token estimation.
type TokenEstimateDetails struct {
	SystemTokens     int `json:"system_tokens"`
	MessagesTokens   int `json:"messages_tokens"`
	ToolsTokens      int `json:"tools_tokens"`
	ThinkingOverhead int `json:"thinking_overhead"`
}

// EstimateInputTokensWithDetails estimates input tokens and returns detailed breakdown.
// This is useful for debugging and comparing with actual token counts.
func EstimateInputTokensWithDetails(req *MessageRequest) (int, TokenEstimateDetails) {
	var details TokenEstimateDetails
	var totalTokens int

	// Count system prompt
	systemStr := req.GetSystemString()
	if systemStr != "" {
		details.SystemTokens = countTextTokens(systemStr)
		totalTokens += details.SystemTokens
	}

	// Count thinking prefix if enabled
	if req.Thinking != nil && req.Thinking.Type == "enabled" {
		details.ThinkingOverhead = 50
		totalTokens += details.ThinkingOverhead
	}

	// Count all messages
	for _, msg := range req.Messages {
		details.MessagesTokens += MessageOverheadTokens // Role marker overhead
		details.MessagesTokens += countMessageTokens(msg.Content)
	}
	totalTokens += details.MessagesTokens

	// Count tool definitions
	for _, tool := range req.Tools {
		details.ToolsTokens += ToolOverheadTokens
		details.ToolsTokens += countTextTokens(tool.Name)
		details.ToolsTokens += countTextTokens(tool.Description)
		if len(tool.InputSchema) > 0 {
			details.ToolsTokens += len(tool.InputSchema) / CharsPerToken
		}
	}
	totalTokens += details.ToolsTokens

	if totalTokens < 1 {
		totalTokens = 1
	}

	return totalTokens, details
}
