// Package unit contains unit tests for the Kiro server.
package unit

import (
	"testing"

	"github.com/anthropics/AIClient-2-API/internal/claude"
	"github.com/stretchr/testify/assert"
)

func TestDistributeTokens_BelowThreshold(t *testing.T) {
	// Below 100 tokens, no distribution applied
	tests := []struct {
		input    int
		expected claude.TokenUsage
	}{
		{0, claude.TokenUsage{InputTokens: 0}},
		{1, claude.TokenUsage{InputTokens: 1}},
		{50, claude.TokenUsage{InputTokens: 50}},
		{99, claude.TokenUsage{InputTokens: 99}},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := claude.DistributeTokens(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDistributeTokens_ExactRatio(t *testing.T) {
	// Test with 1000 tokens (documented in CLAUDE.md)
	// Expected: { input_tokens: 35, cache_creation_input_tokens: 71, cache_read_input_tokens: 894 }
	result := claude.DistributeTokens(1000)

	assert.Equal(t, 35, result.InputTokens)
	assert.Equal(t, 71, result.CacheCreationInputTokens)
	assert.Equal(t, 894, result.CacheReadInputTokens)

	// Verify total equals input
	total := result.InputTokens + result.CacheCreationInputTokens + result.CacheReadInputTokens
	assert.Equal(t, 1000, total)
}

func TestDistributeTokens_Ratios(t *testing.T) {
	// Test 1:2:25 ratio with various inputs
	// For larger token counts, ratios should be more accurate
	tests := []struct {
		name         string
		input        int
		checkRatios  bool  // Only check ratios for larger values
	}{
		{"100 tokens", 100, false},  // Too small for ratio check
		{"280 tokens (exact multiple)", 280, true},
		{"500 tokens", 500, true},
		{"1000 tokens", 1000, true},
		{"2800 tokens (100x ratio)", 2800, true},
		{"10000 tokens", 10000, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := claude.DistributeTokens(tt.input)

			// Verify total equals input
			total := result.InputTokens + result.CacheCreationInputTokens + result.CacheReadInputTokens
			assert.Equal(t, tt.input, total, "total should equal input")

			if tt.checkRatios && result.InputTokens > 0 {
				// Verify ratios are approximately 1:2:25
				// input:creation should be ~1:2
				ratio := float64(result.CacheCreationInputTokens) / float64(result.InputTokens)
				assert.InDelta(t, 2.0, ratio, 0.5, "creation:input ratio should be ~2")

				// input:read should be ~1:25
				ratio = float64(result.CacheReadInputTokens) / float64(result.InputTokens)
				assert.InDelta(t, 25.0, ratio, 3.0, "read:input ratio should be ~25")
			}
		})
	}
}

func TestDistributeTokens_ThresholdBoundary(t *testing.T) {
	// At exactly 100, distribution should apply
	result := claude.DistributeTokens(100)

	// Total parts = 28, input = 100
	// input_tokens = 100 * 1 / 28 = 3
	// cache_creation = 100 * 2 / 28 = 7
	// cache_read = 100 - 3 - 7 = 90
	assert.Equal(t, 3, result.InputTokens)
	assert.Equal(t, 7, result.CacheCreationInputTokens)
	assert.Equal(t, 90, result.CacheReadInputTokens)

	// Verify total
	total := result.InputTokens + result.CacheCreationInputTokens + result.CacheReadInputTokens
	assert.Equal(t, 100, total)
}

func TestDistributeTokens_LargeNumbers(t *testing.T) {
	// Test with large token counts
	result := claude.DistributeTokens(1000000)

	// Verify total equals input
	total := result.InputTokens + result.CacheCreationInputTokens + result.CacheReadInputTokens
	assert.Equal(t, 1000000, total)

	// Verify all values are positive
	assert.Greater(t, result.InputTokens, 0)
	assert.Greater(t, result.CacheCreationInputTokens, 0)
	assert.Greater(t, result.CacheReadInputTokens, 0)
}

func TestCalculateInputTokensFromPercentage(t *testing.T) {
	tests := []struct {
		name         string
		percentage   float64
		outputTokens int
		expected     int
	}{
		{"zero percentage", 0, 100, 0},
		{"1% with 100 output", 1.0, 100, 1625},     // 172500 * 0.01 - 100 = 1625
		{"5% with 500 output", 5.0, 500, 8125},     // 172500 * 0.05 - 500 = 8125
		{"output exceeds total", 1.0, 5000, 0},     // 172500 * 0.01 - 5000 = -3275 -> 0
		{"10% with 0 output", 10.0, 0, 17250},      // 172500 * 0.10 = 17250
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := claude.CalculateInputTokensFromPercentage(tt.percentage, tt.outputTokens)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEstimateInputTokens(t *testing.T) {
	tests := []struct {
		name     string
		req      *claude.MessageRequest
		minTokens int
		maxTokens int
	}{
		{
			name: "simple request",
			req: &claude.MessageRequest{
				Messages: []claude.Message{
					{Role: "user", Content: []byte(`"Hello, world!"`)},
				},
			},
			minTokens: 1,
			maxTokens: 10,
		},
		{
			name: "request with system prompt",
			req: &claude.MessageRequest{
				System: []byte(`"You are a helpful assistant."`),
				Messages: []claude.Message{
					{Role: "user", Content: []byte(`"Hello, world!"`)},
				},
			},
			minTokens: 5,
			maxTokens: 20,
		},
		{
			name: "request with thinking enabled",
			req: &claude.MessageRequest{
				Thinking: &claude.ThinkingConfig{Type: "enabled"},
				Messages: []claude.Message{
					{Role: "user", Content: []byte(`"Hello"`)},
				},
			},
			minTokens: 20,
			maxTokens: 50,
		},
		{
			name: "empty request",
			req: &claude.MessageRequest{
				Messages: []claude.Message{},
			},
			minTokens: 0,
			maxTokens: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := claude.EstimateInputTokens(tt.req)
			assert.GreaterOrEqual(t, result, tt.minTokens, "should be at least minTokens")
			assert.LessOrEqual(t, result, tt.maxTokens, "should be at most maxTokens")
		})
	}
}

func TestCountTextTokens(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{"empty string", "", 0},
		{"short text", "Hi", 1},
		{"medium text", "Hello, world!", 3},
		{"longer text", "This is a longer text that should have more tokens", 12},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := claude.CountTextTokens(tt.text)
			assert.Equal(t, tt.expected, result)
		})
	}
}
