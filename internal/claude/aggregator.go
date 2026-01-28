// Package claude provides response aggregation for non-streaming requests.
package claude

import (
	"encoding/json"

	"github.com/anthropics/AIClient-2-API/internal/kiro"
)

// Aggregator collects streaming chunks into a complete response.
type Aggregator struct {
	model        string
	messageID    string
	role         string
	content      []ContentBlock
	stopReason   string
	stopSeq      *string
	inputTokens  int
	outputTokens int

	// Token tracking
	estimatedInputTokens   int
	contextUsagePercentage float64
	outputText             string // Accumulated output for token counting

	// Current content block being built
	currentBlockIndex    int
	currentBlockType     string
	currentBlockText     string
	currentBlockID       string
	currentBlockName     string
	currentBlockInputStr string // Accumulate input as string first, validate JSON later
}

// NewAggregator creates a new response aggregator.
func NewAggregator(model string) *Aggregator {
	return &Aggregator{
		model:             model,
		messageID:         GenerateMessageID(),
		role:              "assistant",
		currentBlockIndex: -1,
	}
}

// NewAggregatorWithEstimate creates an aggregator with pre-estimated input tokens.
func NewAggregatorWithEstimate(model string, estimatedInputTokens int) *Aggregator {
	return &Aggregator{
		model:                model,
		messageID:            GenerateMessageID(),
		role:                 "assistant",
		currentBlockIndex:    -1,
		estimatedInputTokens: estimatedInputTokens,
	}
}

// Add processes a Kiro chunk and adds it to the aggregated response.
func (a *Aggregator) Add(chunk *kiro.KiroChunk) error {
	if chunk == nil {
		return nil
	}

	// Track context usage percentage if present (sent at end of stream)
	if chunk.ContextUsagePercentage != nil {
		a.contextUsagePercentage = *chunk.ContextUsagePercentage
		// Debug: Log when we receive context usage percentage
		// fmt.Printf("[DEBUG] Received contextUsagePercentage: %.4f\n", *chunk.ContextUsagePercentage)
	}

	// Handle Kiro's simple content format
	if chunk.Content != "" && chunk.FollowupPrompt == nil {
		// If current block is not text, finish it and start a new text block
		if a.currentBlockType != "" && a.currentBlockType != "text" {
			a.finishCurrentBlock()
			// After finish, currentBlockIndex is reset to -1
			// Start new text block at next index
			a.currentBlockIndex = len(a.content)
			a.currentBlockType = "text"
			a.currentBlockText = ""
		}
		// If no block started yet, start a text block
		if a.currentBlockIndex < 0 {
			a.currentBlockIndex = len(a.content)
			a.currentBlockType = "text"
		}
		a.currentBlockText += chunk.Content
		a.outputText += chunk.Content // Track for token counting
		return nil
	}

	// Handle Kiro's native tool use events (name + toolUseId format)
	if chunk.Name != "" && chunk.ToolUseID != "" {
		// Finish any previous block
		a.finishCurrentBlock()

		// Start new tool_use block at next index
		a.currentBlockIndex = len(a.content)
		a.currentBlockType = "tool_use"
		a.currentBlockID = chunk.ToolUseID
		a.currentBlockName = chunk.Name

		// Accumulate input as string (Kiro sends input as string chunks)
		if chunk.Input != "" {
			a.currentBlockInputStr += chunk.Input
			a.outputText += chunk.Input // Track for token counting
		}

		// If stop is true, finish this tool block
		if chunk.Stop {
			a.finishCurrentBlock()
		}

		return nil
	}

	// Handle tool input continuation (input without name/toolUseId)
	if chunk.Input != "" && chunk.Name == "" && a.currentBlockType == "tool_use" {
		a.currentBlockInputStr += chunk.Input
		a.outputText += chunk.Input

		// If stop is true, finish this tool block
		if chunk.Stop {
			a.finishCurrentBlock()
		}

		return nil
	}

	// Handle legacy Claude-style events
	switch chunk.Type {
	case "message_start", kiro.EventTypeMessageStart:
		if chunk.Message != nil {
			if chunk.Message.Usage != nil {
				a.inputTokens = chunk.Message.Usage.InputTokens
			}
		}

	case "content_block_start", kiro.EventTypeContentBlockStart:
		a.finishCurrentBlock()
		if chunk.Index != nil {
			a.currentBlockIndex = *chunk.Index
		}
		if chunk.ContentBlock != nil {
			a.currentBlockType = chunk.ContentBlock.Type
			a.currentBlockText = chunk.ContentBlock.Text
			a.currentBlockID = chunk.ContentBlock.ID
			a.currentBlockName = chunk.ContentBlock.Name
			if chunk.ContentBlock.Input != nil {
				a.currentBlockInputStr = string(chunk.ContentBlock.Input)
			}
		}

	case "content_block_delta", kiro.EventTypeContentBlockDelta:
		if chunk.Delta != nil {
			a.currentBlockText += chunk.Delta.Text
			a.outputText += chunk.Delta.Text // Track for token counting
		}

	case "content_block_stop", kiro.EventTypeContentBlockStop:
		a.finishCurrentBlock()

	case "message_delta", kiro.EventTypeMessageDelta:
		if chunk.StopReason != "" {
			a.stopReason = chunk.StopReason
		}
		if chunk.StopSequence != nil {
			a.stopSeq = chunk.StopSequence
		}
		if chunk.Delta != nil {
			if chunk.Delta.StopReason != "" {
				a.stopReason = chunk.Delta.StopReason
			}
			if chunk.Delta.StopSequence != nil {
				a.stopSeq = chunk.Delta.StopSequence
			}
		}
		// Parse usage from json.RawMessage
		if len(chunk.Usage) > 0 {
			var usage kiro.KiroUsage
			if err := json.Unmarshal(chunk.Usage, &usage); err == nil {
				if usage.OutputTokens > 0 {
					a.outputTokens = usage.OutputTokens
				}
				if usage.InputTokens > 0 {
					a.inputTokens = usage.InputTokens
				}
			}
		}

	case "message_stop", "message_complete", kiro.EventTypeMessageStop, kiro.EventTypeMessageComplete:
		a.finishCurrentBlock()
	}

	return nil
}

// finishCurrentBlock adds the current block to content if valid.
func (a *Aggregator) finishCurrentBlock() {
	if a.currentBlockIndex < 0 {
		return
	}

	block := ContentBlock{
		Type: a.currentBlockType,
	}

	switch a.currentBlockType {
	case "text":
		block.Text = a.currentBlockText
	case "tool_use":
		block.ID = a.currentBlockID
		block.Name = a.currentBlockName
		// Validate and set input JSON
		block.Input = a.validateAndGetInput()
	case "thinking":
		block.Thinking = a.currentBlockText
	}

	// Extend content slice if needed
	for len(a.content) <= a.currentBlockIndex {
		a.content = append(a.content, ContentBlock{})
	}
	a.content[a.currentBlockIndex] = block

	// Reset
	a.currentBlockIndex = -1
	a.currentBlockType = ""
	a.currentBlockText = ""
	a.currentBlockID = ""
	a.currentBlockName = ""
	a.currentBlockInputStr = ""
}

// validateAndGetInput validates the accumulated input string as JSON.
// If invalid, wraps it in {"raw_arguments": "..."} as a fallback.
// Returns empty object {} if input is empty.
func (a *Aggregator) validateAndGetInput() json.RawMessage {
	if a.currentBlockInputStr == "" {
		// Return empty object for empty input
		return json.RawMessage("{}")
	}

	// Try to validate as JSON
	var js json.RawMessage
	if err := json.Unmarshal([]byte(a.currentBlockInputStr), &js); err == nil {
		// Valid JSON, use as-is
		return js
	}

	// Invalid JSON - wrap in raw_arguments as fallback (matching JS implementation)
	wrapped := map[string]string{"raw_arguments": a.currentBlockInputStr}
	result, err := json.Marshal(wrapped)
	if err != nil {
		// Should never happen, but return empty object as ultimate fallback
		return json.RawMessage("{}")
	}
	return result
}

// Build creates the final MessageResponse.
func (a *Aggregator) Build() *MessageResponse {
	a.finishCurrentBlock()

	// Calculate output tokens from accumulated text if not already set
	outputTokens := a.outputTokens
	if outputTokens == 0 && a.outputText != "" {
		outputTokens = CountTextTokens(a.outputText)
	}

	// Calculate final input tokens
	var inputTokens int
	if a.contextUsagePercentage > 0 {
		// Use percentage-based calculation if available
		calculatedTokens := CalculateInputTokensFromPercentage(a.contextUsagePercentage, outputTokens)
		if calculatedTokens > 0 {
			inputTokens = calculatedTokens
		} else {
			// Percentage calculation gave 0 (output exceeded calculated total)
			// Fall back to estimation
			inputTokens = a.estimatedInputTokens
		}
	} else if a.inputTokens > 0 {
		// Use tokens from Kiro if provided
		inputTokens = a.inputTokens
	} else {
		// Fall back to estimation
		inputTokens = a.estimatedInputTokens
	}

	// Apply token distribution
	distributed := DistributeTokens(inputTokens)

	return &MessageResponse{
		ID:           a.messageID,
		Type:         "message",
		Role:         a.role,
		Content:      a.content,
		Model:        a.model,
		StopReason:   a.stopReason,
		StopSequence: a.stopSeq,
		Usage: Usage{
			InputTokens:              distributed.InputTokens,
			OutputTokens:             outputTokens,
			CacheCreationInputTokens: distributed.CacheCreationInputTokens,
			CacheReadInputTokens:     distributed.CacheReadInputTokens,
		},
	}
}

// GetMessageID returns the generated message ID.
func (a *Aggregator) GetMessageID() string {
	return a.messageID
}
