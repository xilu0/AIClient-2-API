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
	currentBlockIndex int
	currentBlockType  string
	currentBlockText  string
	currentBlockID    string
	currentBlockName  string
	currentBlockInput json.RawMessage
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
		// Start a text block if not already started
		if a.currentBlockIndex < 0 {
			a.currentBlockIndex = 0
			a.currentBlockType = "text"
		}
		a.currentBlockText += chunk.Content
		a.outputText += chunk.Content // Track for token counting
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
			a.currentBlockInput = chunk.ContentBlock.Input
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
		if a.currentBlockInput != nil {
			block.Input = a.currentBlockInput
		}
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
	a.currentBlockInput = nil
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
