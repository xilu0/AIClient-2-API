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
	// "Hello, world!" = 13 chars / 3 = 4 tokens (conservative estimation)
	assert.Equal(t, 4, usage.OutputTokens)
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

	// Output tokens from "Hello!" = 6 chars / 3 = 2 tokens (conservative estimation)
	assert.Equal(t, 2, usage.OutputTokens)

	// Input tokens should be calculated from percentage:
	// total = 172500 * 1.0 / 100 = 1725
	// input = 1725 - 2 = 1723
	// After distribution (1:2:25 ratio):
	// input_tokens = 1723 * 1 / 28 = 61
	// cache_creation = 1723 * 2 / 28 = 123
	// cache_read = 1723 - 61 - 123 = 1539
	assert.Equal(t, 61, usage.InputTokens)
	assert.Equal(t, 123, usage.CacheCreationInputTokens)
	assert.Equal(t, 1539, usage.CacheReadInputTokens)

	// Verify total equals calculated input
	total := usage.InputTokens + usage.CacheCreationInputTokens + usage.CacheReadInputTokens
	assert.Equal(t, 1723, total)
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

// TestConverterGetStopReason tests that stop_reason is correctly determined
func TestConverterGetStopReason(t *testing.T) {
	t.Run("text_only_returns_end_turn", func(t *testing.T) {
		converter := claude.NewConverter("claude-sonnet-4")

		// Process only text content
		chunk := &kiro.KiroChunk{Content: "Hello world"}
		_, err := converter.Convert(chunk)
		require.NoError(t, err)

		assert.Equal(t, "end_turn", converter.GetStopReason())
	})

	t.Run("tool_use_returns_tool_use", func(t *testing.T) {
		converter := claude.NewConverter("claude-sonnet-4")

		// Process tool use
		chunk := &kiro.KiroChunk{
			Name:      "get_weather",
			ToolUseID: "tool_abc123",
			Input:     `{"location": "NYC"}`,
		}
		_, err := converter.Convert(chunk)
		require.NoError(t, err)

		assert.Equal(t, "tool_use", converter.GetStopReason())
	})
}

// TestAggregatorStopReasonFallback tests that aggregator sets correct stop_reason
func TestAggregatorStopReasonFallback(t *testing.T) {
	t.Run("text_only_returns_end_turn", func(t *testing.T) {
		aggregator := claude.NewAggregator("claude-sonnet-4")

		// Add only text content
		chunk := &kiro.KiroChunk{Content: "Hello world"}
		err := aggregator.Add(chunk)
		require.NoError(t, err)

		resp := aggregator.Build()
		assert.Equal(t, "end_turn", resp.StopReason)
	})

	t.Run("tool_use_returns_tool_use", func(t *testing.T) {
		aggregator := claude.NewAggregator("claude-sonnet-4")

		// Add tool use
		chunk := &kiro.KiroChunk{
			Name:      "test_tool",
			ToolUseID: "tool_123",
			Input:     `{"param": "value"}`,
			Stop:      true,
		}
		err := aggregator.Add(chunk)
		require.NoError(t, err)

		resp := aggregator.Build()
		assert.Equal(t, "tool_use", resp.StopReason)
	})

	t.Run("upstream_stop_reason_preserved", func(t *testing.T) {
		aggregator := claude.NewAggregator("claude-sonnet-4")

		// Add message_delta with stop_reason
		chunk := &kiro.KiroChunk{
			Type:       "message_delta",
			StopReason: "max_tokens",
		}
		err := aggregator.Add(chunk)
		require.NoError(t, err)

		resp := aggregator.Build()
		assert.Equal(t, "max_tokens", resp.StopReason)
	})
}

// TestConverterTextThenToolUse verifies that text block is properly closed before tool_use starts.
// This is critical for Claude API compliance - each content block must receive a content_block_stop.
func TestConverterTextThenToolUse(t *testing.T) {
	converter := claude.NewConverter("claude-sonnet-4")

	// First, send some text content
	textChunk := &kiro.KiroChunk{Content: "Let me check the weather for you."}
	textEvents, err := converter.Convert(textChunk)
	require.NoError(t, err)
	require.NotEmpty(t, textEvents)

	// Verify text events: message_start, content_block_start, content_block_delta
	assert.Equal(t, 3, len(textEvents))
	assert.Equal(t, "message_start", textEvents[0].Type)
	assert.Equal(t, "content_block_start", textEvents[1].Type)
	assert.Equal(t, "content_block_delta", textEvents[2].Type)

	// Check text content_block_start is at index 0
	textBlockStart := textEvents[1].Data.(claude.ContentBlockStartEvent)
	assert.Equal(t, 0, textBlockStart.Index)
	assert.Equal(t, "text", textBlockStart.ContentBlock.Type)

	// Now send tool use - this should first close the text block
	toolChunk := &kiro.KiroChunk{
		Name:      "get_weather",
		ToolUseID: "tool_abc123",
		Input:     `{"location": "San Francisco"}`,
	}
	toolEvents, err := converter.Convert(toolChunk)
	require.NoError(t, err)
	require.NotEmpty(t, toolEvents)

	// CRITICAL: The first event should be content_block_stop for the text block (index 0)
	require.GreaterOrEqual(t, len(toolEvents), 2, "expected at least content_block_stop + content_block_start")

	// First event: close the text block
	assert.Equal(t, "content_block_stop", toolEvents[0].Type)
	textBlockStop := toolEvents[0].Data.(claude.ContentBlockStopEvent)
	assert.Equal(t, 0, textBlockStop.Index, "text block at index 0 should be closed")

	// Second event: start the tool_use block at index 1
	assert.Equal(t, "content_block_start", toolEvents[1].Type)
	toolBlockStart := toolEvents[1].Data.(claude.ContentBlockStartEvent)
	assert.Equal(t, 1, toolBlockStart.Index, "tool_use should be at index 1")
	assert.Equal(t, "tool_use", toolBlockStart.ContentBlock.Type)
	assert.Equal(t, "get_weather", toolBlockStart.ContentBlock.Name)
	assert.Equal(t, "tool_abc123", toolBlockStart.ContentBlock.ID)

	// If there's a third event, it should be the input delta
	if len(toolEvents) >= 3 {
		assert.Equal(t, "content_block_delta", toolEvents[2].Type)
		inputDelta := toolEvents[2].Data.(claude.ContentBlockDeltaEvent)
		assert.Equal(t, 1, inputDelta.Index)
		assert.Equal(t, "input_json_delta", inputDelta.Delta.Type)
	}
}

// TestConverterNoDoubleDelta verifies that message_delta is not emitted twice.
func TestConverterNoDoubleDelta(t *testing.T) {
	converter := claude.NewConverter("claude-sonnet-4")

	// Process some content
	textChunk := &kiro.KiroChunk{Content: "Hello"}
	_, err := converter.Convert(textChunk)
	require.NoError(t, err)

	// Initially, message_delta should not be emitted
	assert.False(t, converter.WasMessageDeltaEmitted())

	// Simulate receiving message_delta from Kiro
	deltaChunk := &kiro.KiroChunk{
		Type:       "message_delta",
		StopReason: "end_turn",
		Usage:      []byte(`{"input_tokens": 100, "output_tokens": 10}`),
	}
	deltaEvents, err := converter.Convert(deltaChunk)
	require.NoError(t, err)
	require.Len(t, deltaEvents, 1)
	assert.Equal(t, "message_delta", deltaEvents[0].Type)

	// Now message_delta should be marked as emitted
	assert.True(t, converter.WasMessageDeltaEmitted())
}

// TestConverterNoDoubleStop verifies that content_block_stop is not emitted twice.
func TestConverterNoDoubleStop(t *testing.T) {
	converter := claude.NewConverter("claude-sonnet-4")

	// Process text content - this opens a text block
	textChunk := &kiro.KiroChunk{Content: "Hello world"}
	_, err := converter.Convert(textChunk)
	require.NoError(t, err)

	// Verify text block is open
	assert.True(t, converter.HasOpenContentBlock())
	assert.Equal(t, 0, converter.GetCurrentContentIndex())

	// Mark the content block as closed (simulates handler sending content_block_stop)
	converter.MarkContentBlockClosed()

	// Now it should report no open content block
	assert.False(t, converter.HasOpenContentBlock())
}

// TestConverterToolUseOnly verifies correct behavior when only tool_use is present (no preceding text).
func TestConverterToolUseOnly(t *testing.T) {
	converter := claude.NewConverter("claude-sonnet-4")

	// Send tool use directly without any text
	toolChunk := &kiro.KiroChunk{
		Name:      "search_files",
		ToolUseID: "tool_xyz789",
		Input:     `{"query": "*.go"}`,
	}
	events, err := converter.Convert(toolChunk)
	require.NoError(t, err)
	require.NotEmpty(t, events)

	// Should have: message_start, content_block_start, possibly content_block_delta
	// But NO content_block_stop for a non-existent text block
	hasTextBlockStop := false
	for _, e := range events {
		if e.Type == "content_block_stop" {
			stopEvent := e.Data.(claude.ContentBlockStopEvent)
			if stopEvent.Index == 0 {
				hasTextBlockStop = true
			}
		}
	}
	assert.False(t, hasTextBlockStop, "should not emit content_block_stop for non-existent text block")

	// Find the content_block_start for tool_use
	var toolStart *claude.ContentBlockStartEvent
	for _, e := range events {
		if e.Type == "content_block_start" {
			data := e.Data.(claude.ContentBlockStartEvent)
			if data.ContentBlock.Type == "tool_use" {
				toolStart = &data
				break
			}
		}
	}
	require.NotNil(t, toolStart, "expected tool_use content_block_start")

	// Tool use should be at index 1 (index 0 would be text if there was any,
	// but the converter increments before creating tool_use block)
	assert.Equal(t, 1, toolStart.Index)
	assert.Equal(t, "search_files", toolStart.ContentBlock.Name)
}

// TestConverterStateTrackingMethods tests the new state tracking methods.
func TestConverterStateTrackingMethods(t *testing.T) {
	t.Run("initial_state", func(t *testing.T) {
		converter := claude.NewConverter("claude-sonnet-4")

		// Initially no open content block
		assert.False(t, converter.HasOpenContentBlock())
		assert.Equal(t, 0, converter.GetCurrentContentIndex())
		assert.False(t, converter.WasMessageDeltaEmitted())
	})

	t.Run("after_text_content", func(t *testing.T) {
		converter := claude.NewConverter("claude-sonnet-4")

		// Process text content
		_, _ = converter.Convert(&kiro.KiroChunk{Content: "Hello"})

		// Text block should be open
		assert.True(t, converter.HasOpenContentBlock())
		assert.Equal(t, 0, converter.GetCurrentContentIndex())
	})

	t.Run("after_tool_use_start", func(t *testing.T) {
		converter := claude.NewConverter("claude-sonnet-4")

		// Start tool use
		_, _ = converter.Convert(&kiro.KiroChunk{
			Name:      "test_tool",
			ToolUseID: "tool_123",
		})

		// Tool use block should be open (inToolUse = true)
		assert.True(t, converter.HasOpenContentBlock())
		assert.Equal(t, 1, converter.GetCurrentContentIndex()) // incremented for tool_use
	})

	t.Run("after_tool_use_stop", func(t *testing.T) {
		converter := claude.NewConverter("claude-sonnet-4")

		// Start and complete tool use
		_, _ = converter.Convert(&kiro.KiroChunk{
			Name:      "test_tool",
			ToolUseID: "tool_123",
		})
		_, _ = converter.Convert(&kiro.KiroChunk{Stop: true})

		// Tool use block should be closed
		assert.False(t, converter.HasOpenContentBlock())
	})
}

func intPtr(i int) *int {
	return &i
}
