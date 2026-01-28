// Package unit contains unit tests for the Kiro server.
package unit

import (
	"testing"

	"github.com/anthropics/AIClient-2-API/internal/claude"
	"github.com/anthropics/AIClient-2-API/internal/kiro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertMessageStart(t *testing.T) {
	converter := claude.NewConverter("claude-sonnet-4")

	kiroChunk := &kiro.KiroChunk{
		Type: "message_start",
		Message: &kiro.KiroMessage{
			ID:   "msg_kiro_123",
			Type: "message",
			Role: "assistant",
			Usage: &kiro.KiroUsage{
				InputTokens: 100,
			},
		},
	}

	events, err := converter.Convert(kiroChunk)
	require.NoError(t, err)
	require.NotEmpty(t, events)

	event := events[0]
	assert.Equal(t, "message_start", event.Type)

	// Check it's the correct typed struct
	data, ok := event.Data.(claude.MessageStartEvent)
	require.True(t, ok, "expected MessageStartEvent, got %T", event.Data)

	// Should have a generated ID (not the Kiro ID)
	assert.Contains(t, data.Message.ID, "msg_")
	assert.Equal(t, "message", data.Message.Type)
	assert.Equal(t, "assistant", data.Message.Role)
	assert.Equal(t, "claude-sonnet-4", data.Message.Model)
}

func TestConvertContentBlockDelta(t *testing.T) {
	converter := claude.NewConverter("claude-sonnet-4")

	kiroChunk := &kiro.KiroChunk{
		Type:  "content_block_delta",
		Index: intPtr(0),
		Delta: &kiro.KiroDelta{
			Type: "text_delta",
			Text: "Hello, world!",
		},
	}

	events, err := converter.Convert(kiroChunk)
	require.NoError(t, err)
	require.NotEmpty(t, events)

	event := events[0]
	assert.Equal(t, "content_block_delta", event.Type)

	// Check it's the correct typed struct
	data, ok := event.Data.(claude.ContentBlockDeltaEvent)
	require.True(t, ok, "expected ContentBlockDeltaEvent, got %T", event.Data)

	assert.Equal(t, 0, data.Index)
	assert.Equal(t, "text_delta", data.Delta.Type)
	assert.Equal(t, "Hello, world!", data.Delta.Text)
}

func TestConvertThinkingDelta(t *testing.T) {
	converter := claude.NewConverter("claude-sonnet-4")

	kiroChunk := &kiro.KiroChunk{
		Type:  "content_block_delta",
		Index: intPtr(0),
		Delta: &kiro.KiroDelta{
			Type: "thinking_delta",
			Text: "Let me think about this...",
		},
	}

	events, err := converter.Convert(kiroChunk)
	require.NoError(t, err)
	require.NotEmpty(t, events)

	event := events[0]
	assert.Equal(t, "content_block_delta", event.Type)

	// Check it's the correct typed struct
	data, ok := event.Data.(claude.ContentBlockDeltaEvent)
	require.True(t, ok, "expected ContentBlockDeltaEvent, got %T", event.Data)

	assert.Equal(t, "thinking_delta", data.Delta.Type)
	assert.Equal(t, "Let me think about this...", data.Delta.Text)
}

func TestConvertMessageDelta(t *testing.T) {
	converter := claude.NewConverter("claude-sonnet-4")

	kiroChunk := &kiro.KiroChunk{
		Type:       "message_delta",
		StopReason: "end_turn",
		Usage:      []byte(`{"input_tokens": 100, "output_tokens": 50}`),
	}

	events, err := converter.Convert(kiroChunk)
	require.NoError(t, err)
	require.NotEmpty(t, events)

	event := events[0]
	assert.Equal(t, "message_delta", event.Type)

	// Check it's the correct typed struct (FullMessageDeltaEvent for converter)
	data, ok := event.Data.(claude.FullMessageDeltaEvent)
	require.True(t, ok, "expected FullMessageDeltaEvent, got %T", event.Data)

	assert.Equal(t, "end_turn", data.Delta.StopReason)
	assert.Equal(t, 50, data.Usage.OutputTokens)
}

func TestConvertContentBlockStart(t *testing.T) {
	converter := claude.NewConverter("claude-sonnet-4")

	kiroChunk := &kiro.KiroChunk{
		Type:  "content_block_start",
		Index: intPtr(0),
		ContentBlock: &kiro.KiroContentBlock{
			Type: "text",
			Text: "",
		},
	}

	events, err := converter.Convert(kiroChunk)
	require.NoError(t, err)
	require.NotEmpty(t, events)

	event := events[0]
	assert.Equal(t, "content_block_start", event.Type)

	// Check it's the correct typed struct
	data, ok := event.Data.(claude.ContentBlockStartEvent)
	require.True(t, ok, "expected ContentBlockStartEvent, got %T", event.Data)

	assert.Equal(t, 0, data.Index)
	assert.Equal(t, "text", data.ContentBlock.Type)
}

func TestConvertContentBlockStop(t *testing.T) {
	converter := claude.NewConverter("claude-sonnet-4")

	kiroChunk := &kiro.KiroChunk{
		Type:  "content_block_stop",
		Index: intPtr(0),
	}

	events, err := converter.Convert(kiroChunk)
	require.NoError(t, err)
	require.NotEmpty(t, events)

	event := events[0]
	assert.Equal(t, "content_block_stop", event.Type)

	// Check it's the correct typed struct
	data, ok := event.Data.(claude.ContentBlockStopEvent)
	require.True(t, ok, "expected ContentBlockStopEvent, got %T", event.Data)
	assert.Equal(t, 0, data.Index)
}

func TestConvertMessageStop(t *testing.T) {
	converter := claude.NewConverter("claude-sonnet-4")

	kiroChunk := &kiro.KiroChunk{
		Type: "message_stop",
	}

	events, err := converter.Convert(kiroChunk)
	require.NoError(t, err)
	require.NotEmpty(t, events)

	event := events[0]
	assert.Equal(t, "message_stop", event.Type)
}

func TestConvertUnknownType(t *testing.T) {
	converter := claude.NewConverter("claude-sonnet-4")

	kiroChunk := &kiro.KiroChunk{
		Type: "unknown_event_type",
	}

	// Unknown types should be passed through or ignored
	events, err := converter.Convert(kiroChunk)
	// Should not error, but may return nil or empty
	assert.NoError(t, err)
	// Implementation may choose to skip unknown types
	_ = events
}

func TestConvertToolUse(t *testing.T) {
	converter := claude.NewConverter("claude-sonnet-4")

	kiroChunk := &kiro.KiroChunk{
		Type:  "content_block_start",
		Index: intPtr(1),
		ContentBlock: &kiro.KiroContentBlock{
			Type:  "tool_use",
			ID:    "tool_123",
			Name:  "get_weather",
			Input: []byte(`{"location": "San Francisco"}`),
		},
	}

	events, err := converter.Convert(kiroChunk)
	require.NoError(t, err)
	require.NotEmpty(t, events)

	event := events[0]
	// Check it's the correct typed struct
	data, ok := event.Data.(claude.ContentBlockStartEvent)
	require.True(t, ok, "expected ContentBlockStartEvent, got %T", event.Data)

	assert.Equal(t, "tool_use", data.ContentBlock.Type)
	assert.Equal(t, "tool_123", data.ContentBlock.ID)
	assert.Equal(t, "get_weather", data.ContentBlock.Name)
}

func TestConverterMessageID(t *testing.T) {
	converter := claude.NewConverter("claude-sonnet-4")

	// Get the message ID
	id1 := converter.GetMessageID()

	// Create a new converter and verify different ID
	converter2 := claude.NewConverter("claude-sonnet-4")
	id2 := converter2.GetMessageID()

	assert.NotEqual(t, id1, id2)
	assert.Contains(t, id1, "msg_")
	assert.Contains(t, id2, "msg_")
}

func TestGetFinalUsage_WithEstimatedTokens(t *testing.T) {
	// Create converter with estimated input tokens
	converter := claude.NewConverterWithEstimate("claude-sonnet-4", 1000)

	// Simulate processing some content chunks
	chunk1 := &kiro.KiroChunk{Content: "Hello, "}
	chunk2 := &kiro.KiroChunk{Content: "world!"}

	_, _ = converter.Convert(chunk1)
	_, _ = converter.Convert(chunk2)

	// Get final usage
	usage := converter.GetFinalUsage()

	// Should have distributed input tokens (1000 tokens with 1:2:25 ratio)
	// input_tokens = 1000 * 1 / 28 = 35
	// cache_creation = 1000 * 2 / 28 = 71
	// cache_read = 1000 - 35 - 71 = 894
	assert.Equal(t, 35, usage.InputTokens)
	assert.Equal(t, 71, usage.CacheCreationInputTokens)
	assert.Equal(t, 894, usage.CacheReadInputTokens)

	// Output tokens should be calculated from accumulated text
	// "Hello, world!" = 13 chars / 4 = 3 tokens
	assert.Equal(t, 3, usage.OutputTokens)
}

func TestGetFinalUsage_WithContextUsagePercentage(t *testing.T) {
	// Create converter with estimated input tokens
	converter := claude.NewConverterWithEstimate("claude-sonnet-4", 1000)

	// Simulate processing content with contextUsagePercentage at the end
	chunk1 := &kiro.KiroChunk{Content: "Hello!"}
	percentage := 1.0 // 1% of 172500 = 1725 total tokens
	chunk2 := &kiro.KiroChunk{ContextUsagePercentage: &percentage}

	_, _ = converter.Convert(chunk1)
	_, _ = converter.Convert(chunk2)

	// Get final usage
	usage := converter.GetFinalUsage()

	// Output tokens from "Hello!" = 6 chars / 4 = 1 token
	assert.Equal(t, 1, usage.OutputTokens)

	// Input tokens should be calculated from percentage:
	// total = 172500 * 1.0 / 100 = 1725
	// input = 1725 - 1 = 1724
	// After distribution (1:2:25 ratio):
	// input_tokens = 1724 * 1 / 28 = 61
	// cache_creation = 1724 * 2 / 28 = 123
	// cache_read = 1724 - 61 - 123 = 1540
	assert.Equal(t, 61, usage.InputTokens)
	assert.Equal(t, 123, usage.CacheCreationInputTokens)
	assert.Equal(t, 1540, usage.CacheReadInputTokens)

	// Verify total equals calculated input
	total := usage.InputTokens + usage.CacheCreationInputTokens + usage.CacheReadInputTokens
	assert.Equal(t, 1724, total)
}

func TestGetFinalUsage_EmptyContent(t *testing.T) {
	// Create converter with small estimated input tokens (below threshold)
	converter := claude.NewConverterWithEstimate("claude-sonnet-4", 50)

	// No content processed, get final usage
	usage := converter.GetFinalUsage()

	// Below 100 threshold, no distribution applied
	assert.Equal(t, 50, usage.InputTokens)
	assert.Equal(t, 0, usage.CacheCreationInputTokens)
	assert.Equal(t, 0, usage.CacheReadInputTokens)
	assert.Equal(t, 0, usage.OutputTokens)
}

// TestConvertKiroToolUse tests Kiro's native tool use format
func TestConvertKiroToolUse(t *testing.T) {
	converter := claude.NewConverter("claude-sonnet-4")

	// First chunk: tool use start
	kiroChunk := &kiro.KiroChunk{
		Name:      "get_weather",
		ToolUseID: "tool_abc123",
		Input:     `{"location": "San Francisco"}`,
	}

	events, err := converter.Convert(kiroChunk)
	require.NoError(t, err)
	require.NotEmpty(t, events)

	// Should return message_start + content_block_start
	assert.GreaterOrEqual(t, len(events), 1)

	// Find content_block_start event
	var toolUseStart *claude.SSEEvent
	for _, e := range events {
		if e.Type == "content_block_start" {
			toolUseStart = e
			break
		}
	}
	require.NotNil(t, toolUseStart, "expected content_block_start event")

	data, ok := toolUseStart.Data.(claude.ContentBlockStartEvent)
	require.True(t, ok, "expected ContentBlockStartEvent, got %T", toolUseStart.Data)

	assert.Equal(t, "tool_use", data.ContentBlock.Type)
	assert.Equal(t, "tool_abc123", data.ContentBlock.ID)
	assert.Equal(t, "get_weather", data.ContentBlock.Name)
}

// TestConvertToolUseWithInvalidJSON tests handling of invalid JSON input
func TestConvertToolUseWithInvalidJSON(t *testing.T) {
	// This tests the aggregator's JSON validation
	aggregator := claude.NewAggregator("claude-sonnet-4")

	// Add tool use with invalid JSON
	chunk := &kiro.KiroChunk{
		Name:      "test_tool",
		ToolUseID: "tool_123",
		Input:     `{invalid json`,
		Stop:      true,
	}

	err := aggregator.Add(chunk)
	require.NoError(t, err)

	// Build response
	resp := aggregator.Build()
	require.NotNil(t, resp)
	require.Len(t, resp.Content, 1)

	// Input should be wrapped in raw_arguments
	block := resp.Content[0]
	assert.Equal(t, "tool_use", block.Type)
	assert.Equal(t, "tool_123", block.ID)
	assert.Equal(t, "test_tool", block.Name)

	// Input should be valid JSON now (wrapped)
	assert.NotNil(t, block.Input)
	assert.Contains(t, string(block.Input), "raw_arguments")
}

func intPtr(i int) *int {
	return &i
}
