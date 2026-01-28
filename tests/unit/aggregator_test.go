// Package unit contains unit tests for the Kiro server.
package unit

import (
	"testing"

	"github.com/anthropics/AIClient-2-API/internal/claude"
	"github.com/anthropics/AIClient-2-API/internal/kiro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAggregator_SingleTextBlock(t *testing.T) {
	agg := claude.NewAggregator("claude-sonnet-4")

	// Simulate a complete response sequence
	chunks := []*kiro.KiroChunk{
		{
			Type: "message_start",
			Message: &kiro.KiroMessage{
				ID:   "msg_123",
				Role: "assistant",
				Usage: &kiro.KiroUsage{
					InputTokens: 100,
				},
			},
		},
		{
			Type:  "content_block_start",
			Index: intPtr(0),
			ContentBlock: &kiro.KiroContentBlock{
				Type: "text",
				Text: "",
			},
		},
		{
			Type:  "content_block_delta",
			Index: intPtr(0),
			Delta: &kiro.KiroDelta{
				Type: "text_delta",
				Text: "Hello, ",
			},
		},
		{
			Type:  "content_block_delta",
			Index: intPtr(0),
			Delta: &kiro.KiroDelta{
				Type: "text_delta",
				Text: "world!",
			},
		},
		{
			Type:  "content_block_stop",
			Index: intPtr(0),
		},
		{
			Type:       "message_delta",
			StopReason: "end_turn",
			Usage:      []byte(`{"output_tokens": 10}`),
		},
		{
			Type: "message_stop",
		},
	}

	for _, chunk := range chunks {
		err := agg.Add(chunk)
		require.NoError(t, err)
	}

	response := agg.Build()
	require.NotNil(t, response)

	assert.Equal(t, "message", response.Type)
	assert.Equal(t, "assistant", response.Role)
	assert.Equal(t, "claude-sonnet-4", response.Model)
	assert.Equal(t, "end_turn", response.StopReason)

	// Check content
	require.Len(t, response.Content, 1)
	assert.Equal(t, "text", response.Content[0].Type)
	assert.Equal(t, "Hello, world!", response.Content[0].Text)

	// Check usage (with 1:2:25 distribution)
	assert.Equal(t, 10, response.Usage.OutputTokens)
	// Input tokens should be distributed
	total := response.Usage.InputTokens + response.Usage.CacheCreationInputTokens + response.Usage.CacheReadInputTokens
	assert.Equal(t, 100, total)
}

func TestAggregator_MultipleContentBlocks(t *testing.T) {
	agg := claude.NewAggregator("claude-sonnet-4")

	chunks := []*kiro.KiroChunk{
		{Type: "message_start", Message: &kiro.KiroMessage{Role: "assistant"}},
		{Type: "content_block_start", Index: intPtr(0), ContentBlock: &kiro.KiroContentBlock{Type: "text"}},
		{Type: "content_block_delta", Index: intPtr(0), Delta: &kiro.KiroDelta{Text: "First block"}},
		{Type: "content_block_stop", Index: intPtr(0)},
		{Type: "content_block_start", Index: intPtr(1), ContentBlock: &kiro.KiroContentBlock{Type: "text"}},
		{Type: "content_block_delta", Index: intPtr(1), Delta: &kiro.KiroDelta{Text: "Second block"}},
		{Type: "content_block_stop", Index: intPtr(1)},
		{Type: "message_delta", StopReason: "end_turn"},
		{Type: "message_stop"},
	}

	for _, chunk := range chunks {
		_ = agg.Add(chunk)
	}

	response := agg.Build()
	require.Len(t, response.Content, 2)
	assert.Equal(t, "First block", response.Content[0].Text)
	assert.Equal(t, "Second block", response.Content[1].Text)
}

func TestAggregator_ToolUse(t *testing.T) {
	agg := claude.NewAggregator("claude-sonnet-4")

	chunks := []*kiro.KiroChunk{
		{Type: "message_start", Message: &kiro.KiroMessage{Role: "assistant"}},
		{
			Type:  "content_block_start",
			Index: intPtr(0),
			ContentBlock: &kiro.KiroContentBlock{
				Type:  "tool_use",
				ID:    "tool_123",
				Name:  "get_weather",
				Input: []byte(`{"location":"San Francisco"}`),
			},
		},
		{Type: "content_block_stop", Index: intPtr(0)},
		{Type: "message_delta", StopReason: "tool_use"},
		{Type: "message_stop"},
	}

	for _, chunk := range chunks {
		_ = agg.Add(chunk)
	}

	response := agg.Build()
	require.Len(t, response.Content, 1)
	assert.Equal(t, "tool_use", response.Content[0].Type)
	assert.Equal(t, "tool_123", response.Content[0].ID)
	assert.Equal(t, "get_weather", response.Content[0].Name)
	assert.Equal(t, "tool_use", response.StopReason)
}

func TestAggregator_StopSequence(t *testing.T) {
	agg := claude.NewAggregator("claude-sonnet-4")

	stopSeq := "END"
	chunks := []*kiro.KiroChunk{
		{Type: "message_start", Message: &kiro.KiroMessage{Role: "assistant"}},
		{Type: "content_block_start", Index: intPtr(0), ContentBlock: &kiro.KiroContentBlock{Type: "text"}},
		{Type: "content_block_delta", Index: intPtr(0), Delta: &kiro.KiroDelta{Text: "Some text"}},
		{Type: "content_block_stop", Index: intPtr(0)},
		{Type: "message_delta", StopReason: "stop_sequence", StopSequence: &stopSeq},
		{Type: "message_stop"},
	}

	for _, chunk := range chunks {
		_ = agg.Add(chunk)
	}

	response := agg.Build()
	assert.Equal(t, "stop_sequence", response.StopReason)
	require.NotNil(t, response.StopSequence)
	assert.Equal(t, "END", *response.StopSequence)
}

func TestAggregator_EmptyResponse(t *testing.T) {
	agg := claude.NewAggregator("claude-sonnet-4")

	// No chunks added
	response := agg.Build()
	require.NotNil(t, response)
	assert.Equal(t, "message", response.Type)
	assert.Equal(t, "assistant", response.Role)
	assert.Empty(t, response.Content)
}

func TestAggregator_MessageID(t *testing.T) {
	agg := claude.NewAggregator("claude-sonnet-4")

	chunks := []*kiro.KiroChunk{
		{Type: "message_start", Message: &kiro.KiroMessage{Role: "assistant"}},
		{Type: "message_stop"},
	}

	for _, chunk := range chunks {
		_ = agg.Add(chunk)
	}

	response := agg.Build()
	// Should have a generated message ID
	assert.Contains(t, response.ID, "msg_")
}
