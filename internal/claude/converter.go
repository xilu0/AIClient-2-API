// Package claude provides Kiro to Claude format conversion.
package claude

import (
	"encoding/json"
	"strings"

	"github.com/anthropics/AIClient-2-API/internal/kiro"
)

// Converter converts Kiro API responses to Claude API format.
type Converter struct {
	model            string
	messageID        string
	messageStartSent bool // Track if message_start has been sent
	contentStarted   bool
	contentIndex     int

	// Tool use tracking
	inToolUse        bool
	toolUseStartSent bool
	hadToolUse       bool // Track if any tool use blocks were processed (for stop_reason)
	inputDeltaSent   bool // Track if input_json_delta has been sent for current tool_use

	// Content block state tracking (for proper SSE event ordering)
	textBlockStarted    bool // Track if a text content block is open and needs closing
	messageDeltaEmitted bool // Track if message_delta has been sent by converter

	// Token tracking
	estimatedInputTokens   int
	contextUsagePercentage float64
	outputTokens           int
	outputBuilder          strings.Builder // Accumulated output for token counting (efficient O(n) concatenation)
}

// NewConverter creates a new converter for the given model.
func NewConverter(model string) *Converter {
	return &Converter{
		model:     model,
		messageID: GenerateMessageID(),
	}
}

// NewConverterWithEstimate creates a converter with pre-estimated input tokens.
func NewConverterWithEstimate(model string, estimatedInputTokens int) *Converter {
	return &Converter{
		model:                model,
		messageID:            GenerateMessageID(),
		estimatedInputTokens: estimatedInputTokens,
	}
}

// GetMessageID returns the generated message ID.
func (c *Converter) GetMessageID() string {
	return c.messageID
}

// GetStopReason returns the appropriate stop_reason based on what was processed.
// Returns "tool_use" if any tool use blocks were emitted, otherwise "end_turn".
func (c *Converter) GetStopReason() string {
	if c.hadToolUse {
		return "tool_use"
	}
	return "end_turn"
}

// Convert converts a Kiro chunk to Claude SSE events.
// Kiro returns simple {content: "..."} chunks, which we convert to Claude format.
// Returns a slice of events since some conversions require multiple events (e.g., message_start + content_block_start).
func (c *Converter) Convert(chunk *kiro.KiroChunk) ([]*SSEEvent, error) {
	if chunk == nil {
		return nil, nil
	}

	// Track context usage percentage if present (sent at end of stream)
	if chunk.ContextUsagePercentage != nil {
		c.contextUsagePercentage = *chunk.ContextUsagePercentage
	}

	// Handle Kiro's simple content format
	if chunk.Content != "" && chunk.FollowupPrompt == nil {
		// Track output text for token counting (O(n) with strings.Builder)
		c.outputBuilder.WriteString(chunk.Content)
		return c.convertKiroContent(chunk)
	}

	// Handle tool use events (initial event with name/toolUseId OR continuation with input/stop)
	if chunk.Name != "" && chunk.ToolUseID != "" {
		// First event with name and ID
		return c.convertToolUse(chunk)
	}
	if c.inToolUse && (chunk.Input != "" || chunk.Stop) {
		// Continuation or stop event for ongoing tool use
		return c.convertToolUse(chunk)
	}

	// Handle legacy Claude-style events (for compatibility)
	switch chunk.Type {
	case "message_start", kiro.EventTypeMessageStart:
		return c.convertMessageStart(chunk)

	case "content_block_start", kiro.EventTypeContentBlockStart:
		return c.convertContentBlockStart(chunk)

	case "content_block_delta", kiro.EventTypeContentBlockDelta:
		// Track output text from deltas (O(n) with strings.Builder)
		if chunk.Delta != nil && chunk.Delta.Text != "" {
			c.outputBuilder.WriteString(chunk.Delta.Text)
		}
		return c.convertContentBlockDelta(chunk)

	case "content_block_stop", kiro.EventTypeContentBlockStop:
		return c.convertContentBlockStop(chunk)

	case "message_delta", kiro.EventTypeMessageDelta:
		return c.convertMessageDelta(chunk)

	case "message_stop", "message_complete", kiro.EventTypeMessageStop, kiro.EventTypeMessageComplete:
		return c.convertMessageStop(chunk)

	default:
		// Unknown event type - skip
		return nil, nil
	}
}

// convertKiroContent converts Kiro's simple content format to Claude SSE events.
func (c *Converter) convertKiroContent(chunk *kiro.KiroChunk) ([]*SSEEvent, error) {
	var events []*SSEEvent

	// If this is the first content, we need to send message_start and content_block_start first
	if !c.contentStarted {
		c.contentStarted = true
		c.messageStartSent = true
		c.textBlockStarted = true // Mark that we have an open text block

		// Send message_start
		events = append(events, c.createMessageStart())

		// Send content_block_start for text
		contentBlockStart := ContentBlockStartEvent{
			Type:  "content_block_start",
			Index: c.contentIndex,
			ContentBlock: ContentStart{
				Type: "text",
				Text: "",
			},
		}
		events = append(events, &SSEEvent{Type: "content_block_start", Data: contentBlockStart})
	}

	// Send content_block_delta with the text (using typed struct for efficiency)
	event := ContentBlockDeltaEvent{
		Type:  "content_block_delta",
		Index: c.contentIndex,
		Delta: DeltaBlock{
			Type: "text_delta",
			Text: chunk.Content,
		},
	}
	events = append(events, &SSEEvent{Type: "content_block_delta", Data: event})

	return events, nil
}

// createMessageStart creates a message_start event.
func (c *Converter) createMessageStart() *SSEEvent {
	// Apply token distribution to estimated input tokens
	distributed := DistributeTokens(c.estimatedInputTokens)

	event := MessageStartEvent{
		Type: "message_start",
		Message: MessageStartMessage{
			ID:      c.messageID,
			Type:    "message",
			Role:    "assistant",
			Model:   c.model,
			Content: []interface{}{},
			Usage: SSEUsage{
				InputTokens:              distributed.InputTokens,
				OutputTokens:             0,
				CacheCreationInputTokens: distributed.CacheCreationInputTokens,
				CacheReadInputTokens:     distributed.CacheReadInputTokens,
			},
		},
	}
	return &SSEEvent{Type: "message_start", Data: event}
}

// convertToolUse converts Kiro tool use events to Claude format.
// Kiro sends tool use as chunks: {name, toolUseId, input, stop}
// We convert to: content_block_start + content_block_delta (input_json_delta) + content_block_stop
func (c *Converter) convertToolUse(chunk *kiro.KiroChunk) ([]*SSEEvent, error) {
	var events []*SSEEvent

	// First event with name + toolUseId starts a new tool use block
	if chunk.Name != "" && chunk.ToolUseID != "" && !c.toolUseStartSent {
		// If message_start hasn't been sent yet, send it first
		if !c.messageStartSent {
			c.messageStartSent = true
			events = append(events, c.createMessageStart())
		}

		// CRITICAL: Close any open text block before starting tool_use
		// This ensures proper SSE event ordering: text content_block_stop must come before tool_use content_block_start
		if c.textBlockStarted {
			stopEvent := ContentBlockStopEvent{
				Type:  "content_block_stop",
				Index: c.contentIndex,
			}
			events = append(events, &SSEEvent{Type: "content_block_stop", Data: stopEvent})
			c.textBlockStarted = false
			// Increment content index ONLY after closing a preceding text block
			// This ensures tool_use-only responses start at index 0
			c.contentIndex++
		}

		c.inToolUse = true
		c.toolUseStartSent = true
		c.hadToolUse = true // Mark that we've processed a tool use block

		// Send content_block_start
		contentBlockStart := ContentBlockStartEvent{
			Type:  "content_block_start",
			Index: c.contentIndex,
			ContentBlock: ContentStart{
				Type:  "tool_use",
				ID:    chunk.ToolUseID,
				Name:  chunk.Name,
				Input: json.RawMessage("{}"), // Start with empty object
			},
		}
		events = append(events, &SSEEvent{Type: "content_block_start", Data: contentBlockStart})

		// Reset inputDeltaSent for new tool_use block
		c.inputDeltaSent = false

		// If the start chunk also contains input, send it immediately as a delta
		if chunk.Input != "" {
			c.outputBuilder.WriteString(chunk.Input)
			inputDelta := ContentBlockDeltaEvent{
				Type:  "content_block_delta",
				Index: c.contentIndex,
				Delta: DeltaBlock{
					Type:        "input_json_delta",
					PartialJSON: strPtr(chunk.Input),
				},
			}
			events = append(events, &SSEEvent{Type: "content_block_delta", Data: inputDelta})
			c.inputDeltaSent = true
		}

		return events, nil
	}

	// Send input as delta if present
	if c.inToolUse && chunk.Input != "" {
		// Track input for token counting
		c.outputBuilder.WriteString(chunk.Input)

		event := ContentBlockDeltaEvent{
			Type:  "content_block_delta",
			Index: c.contentIndex,
			Delta: DeltaBlock{
				Type:        "input_json_delta",
				PartialJSON: strPtr(chunk.Input),
			},
		}
		c.inputDeltaSent = true

		return []*SSEEvent{{Type: "content_block_delta", Data: event}}, nil
	}

	// Send stop event when tool use is complete
	if c.inToolUse && chunk.Stop {
		var events []*SSEEvent

		// CRITICAL: Always send at least one input_json_delta before content_block_stop
		// Claude API clients expect this event, even if input is empty
		// Node.js implementation sends JSON.stringify({}) = "{}", so we must match that behavior
		if !c.inputDeltaSent {
			emptyJSON := "{}"
			inputDelta := ContentBlockDeltaEvent{
				Type:  "content_block_delta",
				Index: c.contentIndex,
				Delta: DeltaBlock{
					Type:        "input_json_delta",
					PartialJSON: &emptyJSON, // Empty JSON object to match Node.js behavior
				},
			}
			events = append(events, &SSEEvent{Type: "content_block_delta", Data: inputDelta})
		}

		c.inToolUse = false
		c.toolUseStartSent = false

		stopEvent := ContentBlockStopEvent{
			Type:  "content_block_stop",
			Index: c.contentIndex,
		}
		events = append(events, &SSEEvent{Type: "content_block_stop", Data: stopEvent})

		return events, nil
	}

	return nil, nil
}

func (c *Converter) convertMessageStart(chunk *kiro.KiroChunk) ([]*SSEEvent, error) {
	c.messageStartSent = true

	var usage SSEUsage
	if chunk.Message != nil && chunk.Message.Usage != nil {
		// Apply 1:2:25 token distribution
		distributed := DistributeTokens(chunk.Message.Usage.InputTokens)
		usage = SSEUsage{
			InputTokens:              distributed.InputTokens,
			CacheCreationInputTokens: distributed.CacheCreationInputTokens,
			CacheReadInputTokens:     distributed.CacheReadInputTokens,
		}
	}

	event := MessageStartEvent{
		Type: "message_start",
		Message: MessageStartMessage{
			ID:      c.messageID,
			Type:    "message",
			Role:    "assistant",
			Model:   c.model,
			Content: []interface{}{},
			Usage:   usage,
		},
	}

	return []*SSEEvent{{Type: "message_start", Data: event}}, nil
}

func (c *Converter) convertContentBlockStart(chunk *kiro.KiroChunk) ([]*SSEEvent, error) {
	index := 0
	if chunk.Index != nil {
		index = *chunk.Index
	}

	contentStart := ContentStart{
		Type: "text",
		Text: "",
	}

	if chunk.ContentBlock != nil {
		contentStart.Type = chunk.ContentBlock.Type
		switch chunk.ContentBlock.Type {
		case "text":
			contentStart.Text = chunk.ContentBlock.Text
		case "tool_use":
			contentStart.ID = chunk.ContentBlock.ID
			contentStart.Name = chunk.ContentBlock.Name
			if chunk.ContentBlock.Input != nil {
				contentStart.Input = chunk.ContentBlock.Input
			} else {
				// Ensure input is never nil for tool_use
				contentStart.Input = json.RawMessage("{}")
			}
		case "thinking":
			contentStart.Thinking = ""
		}
	}

	event := ContentBlockStartEvent{
		Type:         "content_block_start",
		Index:        index,
		ContentBlock: contentStart,
	}

	return []*SSEEvent{{Type: "content_block_start", Data: event}}, nil
}

func (c *Converter) convertContentBlockDelta(chunk *kiro.KiroChunk) ([]*SSEEvent, error) {
	index := 0
	if chunk.Index != nil {
		index = *chunk.Index
	}

	delta := DeltaBlock{
		Type: "text_delta",
		Text: "",
	}

	if chunk.Delta != nil {
		delta.Type = chunk.Delta.Type
		delta.Text = chunk.Delta.Text
	}

	event := ContentBlockDeltaEvent{
		Type:  "content_block_delta",
		Index: index,
		Delta: delta,
	}

	return []*SSEEvent{{Type: "content_block_delta", Data: event}}, nil
}

func (c *Converter) convertContentBlockStop(chunk *kiro.KiroChunk) ([]*SSEEvent, error) {
	index := 0
	if chunk.Index != nil {
		index = *chunk.Index
	}

	event := ContentBlockStopEvent{
		Type:  "content_block_stop",
		Index: index,
	}

	return []*SSEEvent{{Type: "content_block_stop", Data: event}}, nil
}

func (c *Converter) convertMessageDelta(chunk *kiro.KiroChunk) ([]*SSEEvent, error) {
	var delta MessageDeltaData
	if chunk.StopReason != "" {
		delta.StopReason = chunk.StopReason
	}
	if chunk.StopSequence != nil {
		delta.StopSequence = chunk.StopSequence
	}

	// Also check Delta for stop_reason
	if chunk.Delta != nil && chunk.Delta.StopReason != "" {
		delta.StopReason = chunk.Delta.StopReason
	}

	// Calculate output tokens from accumulated text
	outputTokens := CountTextTokens(c.outputBuilder.String())

	// Parse usage from json.RawMessage if present
	if len(chunk.Usage) > 0 {
		var kiroUsage kiro.KiroUsage
		if err := json.Unmarshal(chunk.Usage, &kiroUsage); err == nil {
			if kiroUsage.OutputTokens > 0 {
				outputTokens = kiroUsage.OutputTokens
			}
		}
	}

	// Calculate final input tokens
	var inputTokens int
	if c.contextUsagePercentage > 0 {
		// Use percentage-based calculation if available
		calculatedTokens := CalculateInputTokensFromPercentage(c.contextUsagePercentage, outputTokens)
		if calculatedTokens > 0 {
			inputTokens = calculatedTokens
		} else {
			// Percentage calculation gave 0 (output exceeded calculated total)
			// Fall back to estimation
			inputTokens = c.estimatedInputTokens
		}
	} else {
		// Fall back to estimation
		inputTokens = c.estimatedInputTokens
	}

	// Apply token distribution
	distributed := DistributeTokens(inputTokens)

	event := FullMessageDeltaEvent{
		Type:  "message_delta",
		Delta: delta,
		Usage: SSEUsage{
			InputTokens:              distributed.InputTokens,
			OutputTokens:             outputTokens,
			CacheCreationInputTokens: distributed.CacheCreationInputTokens,
			CacheReadInputTokens:     distributed.CacheReadInputTokens,
		},
	}

	// Mark that message_delta has been emitted by the converter
	c.messageDeltaEmitted = true

	return []*SSEEvent{{Type: "message_delta", Data: event}}, nil
}

func (c *Converter) convertMessageStop(chunk *kiro.KiroChunk) ([]*SSEEvent, error) {
	event := MessageStopEvent{
		Type: "message_stop",
	}

	return []*SSEEvent{{Type: "message_stop", Data: event}}, nil
}

// ConvertUsage converts Kiro usage to Claude usage with token distribution.
func ConvertUsage(kiroUsage *kiro.KiroUsage) Usage {
	if kiroUsage == nil {
		return Usage{}
	}

	distributed := DistributeTokens(kiroUsage.InputTokens)
	return Usage{
		InputTokens:              distributed.InputTokens,
		OutputTokens:             kiroUsage.OutputTokens,
		CacheCreationInputTokens: distributed.CacheCreationInputTokens,
		CacheReadInputTokens:     distributed.CacheReadInputTokens,
	}
}

// GetFinalUsage calculates and returns the final usage at the end of streaming.
// This method should be called after all chunks have been processed to get
// the final token counts including proper distribution.
func (c *Converter) GetFinalUsage() Usage {
	// Calculate output tokens from accumulated text
	outputTokens := c.outputTokens
	outputText := c.outputBuilder.String()
	if outputTokens == 0 && outputText != "" {
		outputTokens = CountTextTokens(outputText)
	}

	// Calculate final input tokens
	var inputTokens int
	if c.contextUsagePercentage > 0 {
		// Use percentage-based calculation if available (most accurate)
		calculatedTokens := CalculateInputTokensFromPercentage(c.contextUsagePercentage, outputTokens)
		if calculatedTokens > 0 {
			inputTokens = calculatedTokens
		} else {
			// Percentage calculation gave 0 (output exceeded calculated total)
			// Fall back to estimation
			inputTokens = c.estimatedInputTokens
		}
	} else {
		// Fall back to estimation
		inputTokens = c.estimatedInputTokens
	}

	// Apply token distribution
	distributed := DistributeTokens(inputTokens)

	return Usage{
		InputTokens:              distributed.InputTokens,
		OutputTokens:             outputTokens,
		CacheCreationInputTokens: distributed.CacheCreationInputTokens,
		CacheReadInputTokens:     distributed.CacheReadInputTokens,
	}
}

// HasOpenContentBlock returns true if there's an unclosed content block that needs a stop event.
// This is used by the handler to determine if a final content_block_stop should be sent.
func (c *Converter) HasOpenContentBlock() bool {
	return c.textBlockStarted || c.inToolUse
}

// GetCurrentContentIndex returns the current content block index.
// This is used by the handler when sending final content_block_stop events.
func (c *Converter) GetCurrentContentIndex() int {
	return c.contentIndex
}

// WasMessageDeltaEmitted returns true if the converter already emitted a message_delta event.
// This prevents the handler from sending a duplicate message_delta.
func (c *Converter) WasMessageDeltaEmitted() bool {
	return c.messageDeltaEmitted
}

// MarkContentBlockClosed marks the current content block as closed.
// This is called by the handler after sending a final content_block_stop.
func (c *Converter) MarkContentBlockClosed() {
	c.textBlockStarted = false
	c.inToolUse = false
}

// ContentDelivered returns true if content was delivered to the client.
// Covers plain text streams (contentStarted) and tool_use streams (messageStartSent).
func (c *Converter) ContentDelivered() bool {
	return c.contentStarted || c.messageStartSent
}

// strPtr returns a pointer to the given string.
// Used for JSON serialization where we need to distinguish nil vs empty string.
func strPtr(s string) *string {
	return &s
}
