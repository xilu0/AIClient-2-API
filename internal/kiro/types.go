// Package kiro provides Kiro API client and types.
package kiro

import "encoding/json"

// KiroChunk represents a parsed Kiro API response chunk.
// Kiro returns a simpler format than Claude API.
type KiroChunk struct {
	// Content field - the main text content
	Content string `json:"content,omitempty"`

	// Tool use fields
	Name      string `json:"name,omitempty"`
	ToolUseID string `json:"toolUseId,omitempty"`
	Input     string `json:"input,omitempty"`
	Stop      bool   `json:"stop,omitempty"`

	// Followup prompt (should be ignored)
	FollowupPrompt interface{} `json:"followupPrompt,omitempty"`

	// Usage information (can be a number or object)
	Usage json.RawMessage `json:"usage,omitempty"`

	// Context usage percentage - sent at end of stream
	// Used to calculate total tokens: TOTAL_CONTEXT_TOKENS * percentage / 100
	ContextUsagePercentage *float64 `json:"contextUsagePercentage,omitempty"`

	// Legacy Claude-style fields (for compatibility)
	Type         string            `json:"type,omitempty"`
	Message      *KiroMessage      `json:"message,omitempty"`
	Index        *int              `json:"index,omitempty"`
	ContentBlock *KiroContentBlock `json:"content_block,omitempty"`
	Delta        *KiroDelta        `json:"delta,omitempty"`
	StopReason   string            `json:"stop_reason,omitempty"`
	StopSequence *string           `json:"stop_sequence,omitempty"`
	Thinking     *string           `json:"thinking,omitempty"`
}

// KiroMessage represents the message in a messageStart chunk.
type KiroMessage struct {
	ID    string     `json:"id,omitempty"`
	Type  string     `json:"type,omitempty"`
	Role  string     `json:"role,omitempty"`
	Model string     `json:"model,omitempty"`
	Usage *KiroUsage `json:"usage,omitempty"`
}

// KiroContentBlock represents a content block from Kiro.
type KiroContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`

	// For tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// KiroDelta represents a delta in a Kiro chunk.
type KiroDelta struct {
	Type         string  `json:"type,omitempty"` // "text_delta", "thinking_delta"
	Text         string  `json:"text,omitempty"`
	StopReason   string  `json:"stop_reason,omitempty"`
	StopSequence *string `json:"stop_sequence,omitempty"`
}

// KiroUsage represents usage information from Kiro.
type KiroUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// AWSEventMessage represents the binary structure of an AWS event stream message.
type AWSEventMessage struct {
	// Prelude (12 bytes)
	TotalLength   uint32
	HeadersLength uint32
	PreludeCRC    uint32

	// Headers (variable)
	Headers map[string]HeaderValue

	// Payload (variable)
	Payload []byte

	// CRC (4 bytes)
	MessageCRC uint32
}

// HeaderValue represents a header value in AWS event stream format.
type HeaderValue struct {
	Type  byte // 7 = string
	Value string
}

// AWS Event Stream header types
const (
	HeaderTypeString = 7
)

// Common AWS Event Stream headers
const (
	HeaderMessageType = ":message-type"
	HeaderEventType   = ":event-type"
	HeaderContentType = ":content-type"
)

// Message types
const (
	MessageTypeEvent     = "event"
	MessageTypeException = "exception"
)

// Event types from Kiro
const (
	EventTypeChunk             = "chunk"
	EventTypeMessageStart      = "messageStart"
	EventTypeContentBlockStart = "contentBlockStart"
	EventTypeContentBlockDelta = "contentBlockDelta"
	EventTypeContentBlockStop  = "contentBlockStop"
	EventTypeMessageDelta      = "messageDelta"
	EventTypeMessageComplete   = "messageComplete"
	EventTypeMessageStop       = "messageStop"
)
