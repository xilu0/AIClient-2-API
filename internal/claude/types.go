// Package claude provides Claude API types and converters.
package claude

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
)

// MessageRequest represents a Claude-compatible request payload.
type MessageRequest struct {
	// Required
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`

	// Optional
	Stream        bool              `json:"stream,omitempty"`
	System        json.RawMessage   `json:"system,omitempty"` // Can be string or []ContentBlock
	Temperature   *float64          `json:"temperature,omitempty"`
	TopP          *float64          `json:"top_p,omitempty"`
	TopK          *int              `json:"top_k,omitempty"`
	StopSequences []string          `json:"stop_sequences,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`

	// Extended thinking
	Thinking *ThinkingConfig `json:"thinking,omitempty"`

	// Tools (optional)
	Tools      []Tool      `json:"tools,omitempty"`
	ToolChoice *ToolChoice `json:"tool_choice,omitempty"`
}

// Message represents a message in the conversation.
type Message struct {
	Role    string          `json:"role"`    // "user" or "assistant"
	Content json.RawMessage `json:"content"` // string or []ContentBlock
}

// ContentBlock represents a content block in a message.
type ContentBlock struct {
	Type string `json:"type"` // "text", "image", "tool_use", "tool_result", "thinking"

	// For type=text
	Text string `json:"text,omitempty"`

	// For type=image
	Source *ImageSource `json:"source,omitempty"`

	// For type=tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// For type=tool_result
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"` // Can be string or nested content blocks
	IsError   bool            `json:"is_error,omitempty"`

	// For type=thinking
	Thinking string `json:"thinking,omitempty"`
}

// ImageSource represents an image source.
type ImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/jpeg", "image/png", etc.
	Data      string `json:"data"`       // base64-encoded data
}

// ThinkingConfig configures extended thinking.
type ThinkingConfig struct {
	Type         string `json:"type"` // "enabled"
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

// Tool represents a tool definition.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ToolChoice represents tool selection preference.
type ToolChoice struct {
	Type string `json:"type"` // "auto", "any", "tool"
	Name string `json:"name,omitempty"`
}

// MessageResponse represents a complete response for non-streaming requests.
type MessageResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"` // "message"
	Role         string         `json:"role"` // "assistant"
	Content      []ContentBlock `json:"content"`
	Model        string         `json:"model"`
	StopReason   string         `json:"stop_reason"`
	StopSequence *string        `json:"stop_sequence,omitempty"`
	Usage        Usage          `json:"usage"`
}

// Usage represents token usage information.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// Delta represents a content delta in streaming responses.
type Delta struct {
	Type string `json:"type,omitempty"` // "text_delta", "thinking_delta"
	Text string `json:"text,omitempty"`
}

// StopDelta represents the stop reason delta.
type StopDelta struct {
	StopReason   string  `json:"stop_reason,omitempty"`
	StopSequence *string `json:"stop_sequence,omitempty"`
}

// GenerateMessageID generates a unique message ID in Claude format.
func GenerateMessageID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "msg_" + hex.EncodeToString(b)
}

// GetSystemString extracts text from a system field (which can be string or []ContentBlock).
func (req *MessageRequest) GetSystemString() string {
	if len(req.System) == 0 {
		return ""
	}

	// Try as simple string first
	var str string
	if err := json.Unmarshal(req.System, &str); err == nil {
		return str
	}

	// Try as content blocks (array of TextBlockParam)
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(req.System, &blocks); err == nil {
		var result string
		for _, block := range blocks {
			if block.Type == "text" {
				result += block.Text
			}
		}
		return result
	}

	return ""
}

// GetContentString extracts the string content from a message.
func (m *Message) GetContentString() string {
	// Try as simple string first
	var str string
	if err := json.Unmarshal(m.Content, &str); err == nil {
		return str
	}

	// Try as content blocks
	var blocks []ContentBlock
	if err := json.Unmarshal(m.Content, &blocks); err == nil {
		var result string
		for _, block := range blocks {
			if block.Type == "text" {
				result += block.Text
			}
		}
		return result
	}

	return ""
}

// ===========================================================================
// SSE Event Types - Strongly typed structs to eliminate map[string]interface{} allocations
// ===========================================================================

// MessageStartEvent represents a message_start SSE event.
type MessageStartEvent struct {
	Type    string              `json:"type"` // Always "message_start"
	Message MessageStartMessage `json:"message"`
}

// MessageStartMessage is the message object in message_start events.
type MessageStartMessage struct {
	ID      string        `json:"id"`
	Type    string        `json:"type"` // Always "message"
	Role    string        `json:"role"` // Always "assistant"
	Model   string        `json:"model"`
	Content []interface{} `json:"content"` // Empty array
	Usage   SSEUsage      `json:"usage"`
}

// SSEUsage represents usage in SSE events (with all cache fields).
type SSEUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// ContentBlockStartEvent represents a content_block_start SSE event.
type ContentBlockStartEvent struct {
	Type         string       `json:"type"` // Always "content_block_start"
	Index        int          `json:"index"`
	ContentBlock ContentStart `json:"content_block"`
}

// ContentStart is the content_block object in content_block_start events.
type ContentStart struct {
	Type     string `json:"type"` // "text", "tool_use", "thinking"
	Text     string `json:"text,omitempty"`
	ID       string `json:"id,omitempty"`       // For tool_use
	Name     string `json:"name,omitempty"`     // For tool_use
	Input    any    `json:"input,omitempty"`    // For tool_use
	Thinking string `json:"thinking,omitempty"` // For thinking
}

// ContentBlockDeltaEvent represents a content_block_delta SSE event.
type ContentBlockDeltaEvent struct {
	Type  string     `json:"type"` // Always "content_block_delta"
	Index int        `json:"index"`
	Delta DeltaBlock `json:"delta"`
}

// DeltaBlock is the delta object in content_block_delta events.
type DeltaBlock struct {
	Type        string `json:"type"`                   // "text_delta", "thinking_delta", "input_json_delta"
	Text        string `json:"text,omitempty"`         // For text_delta and thinking_delta
	PartialJSON string `json:"partial_json,omitempty"` // For input_json_delta (tool inputs)
}

// ContentBlockStopEvent represents a content_block_stop SSE event.
type ContentBlockStopEvent struct {
	Type  string `json:"type"` // Always "content_block_stop"
	Index int    `json:"index"`
}

// MessageDeltaEvent represents a message_delta SSE event (simple version with output_tokens only).
type MessageDeltaEvent struct {
	Type  string           `json:"type"` // Always "message_delta"
	Delta MessageDeltaData `json:"delta"`
	Usage OutputUsage      `json:"usage"`
}

// FullMessageDeltaEvent represents a message_delta SSE event with full usage (for converter).
type FullMessageDeltaEvent struct {
	Type  string           `json:"type"` // Always "message_delta"
	Delta MessageDeltaData `json:"delta"`
	Usage SSEUsage         `json:"usage"`
}

// MessageDeltaData is the delta object in message_delta events.
type MessageDeltaData struct {
	StopReason   string  `json:"stop_reason,omitempty"`
	StopSequence *string `json:"stop_sequence,omitempty"`
}

// OutputUsage represents output-only usage in message_delta events.
type OutputUsage struct {
	OutputTokens int `json:"output_tokens"`
}

// MessageStopEvent represents a message_stop SSE event.
type MessageStopEvent struct {
	Type string `json:"type"` // Always "message_stop"
}

// PingEvent represents a ping SSE event.
type PingEvent struct {
	Type string `json:"type"` // Always "ping"
}

// ErrorEvent represents an error SSE event.
type ErrorEvent struct {
	Type  string     `json:"type"` // Always "error"
	Error ErrorBlock `json:"error"`
}

// ErrorBlock is the error object in error events.
type ErrorBlock struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
