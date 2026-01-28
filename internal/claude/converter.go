// Package claude provides Kiro to Claude format conversion.
package claude

import (
	"encoding/json"
	"strings"

	"github.com/anthropics/AIClient-2-API/internal/kiro"
)

// Converter converts Kiro API responses to Claude API format.
type Converter struct {
	model          string
	messageID      string
	contentStarted bool
	contentIndex   int

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

// Convert converts a Kiro chunk to a Claude SSE event.
// Kiro returns simple {content: "..."} chunks, which we convert to Claude format.
func (c *Converter) Convert(chunk *kiro.KiroChunk) (*SSEEvent, error) {
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

	// Handle tool use events
	if chunk.Name != "" && chunk.ToolUseID != "" {
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
func (c *Converter) convertKiroContent(chunk *kiro.KiroChunk) (*SSEEvent, error) {
	// If this is the first content, we need to send message_start and content_block_start first
	if !c.contentStarted {
		c.contentStarted = true
		// Return message_start first - the handler will call Convert again for the content
		return c.createMessageStart(), nil
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

	return &SSEEvent{Type: "content_block_delta", Data: event}, nil
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
func (c *Converter) convertToolUse(chunk *kiro.KiroChunk) (*SSEEvent, error) {
	// Tool use handling - simplified for now
	return nil, nil
}

func (c *Converter) convertMessageStart(chunk *kiro.KiroChunk) (*SSEEvent, error) {
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

	return &SSEEvent{Type: "message_start", Data: event}, nil
}

func (c *Converter) convertContentBlockStart(chunk *kiro.KiroChunk) (*SSEEvent, error) {
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

	return &SSEEvent{Type: "content_block_start", Data: event}, nil
}

func (c *Converter) convertContentBlockDelta(chunk *kiro.KiroChunk) (*SSEEvent, error) {
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

	return &SSEEvent{Type: "content_block_delta", Data: event}, nil
}

func (c *Converter) convertContentBlockStop(chunk *kiro.KiroChunk) (*SSEEvent, error) {
	index := 0
	if chunk.Index != nil {
		index = *chunk.Index
	}

	event := ContentBlockStopEvent{
		Type:  "content_block_stop",
		Index: index,
	}

	return &SSEEvent{Type: "content_block_stop", Data: event}, nil
}

func (c *Converter) convertMessageDelta(chunk *kiro.KiroChunk) (*SSEEvent, error) {
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

	return &SSEEvent{Type: "message_delta", Data: event}, nil
}

func (c *Converter) convertMessageStop(chunk *kiro.KiroChunk) (*SSEEvent, error) {
	event := MessageStopEvent{
		Type: "message_stop",
	}

	return &SSEEvent{Type: "message_stop", Data: event}, nil
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
