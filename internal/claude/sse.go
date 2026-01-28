// Package claude provides SSE (Server-Sent Events) writing for Claude API streaming.
package claude

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sync"
)

// bufferPool provides reusable buffers for JSON encoding to reduce GC pressure.
var bufferPool = sync.Pool{
	New: func() interface{} {
		return bytes.NewBuffer(make([]byte, 0, 512))
	},
}

// SSEWriter writes Server-Sent Events to an HTTP response.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewSSEWriter creates a new SSE writer.
func NewSSEWriter(w http.ResponseWriter) *SSEWriter {
	flusher, _ := w.(http.Flusher)
	return &SSEWriter{
		w:       w,
		flusher: flusher,
	}
}

// WriteHeaders sets the appropriate headers for SSE streaming.
func (s *SSEWriter) WriteHeaders() {
	s.w.Header().Set("Content-Type", "text/event-stream")
	s.w.Header().Set("Cache-Control", "no-cache")
	s.w.Header().Set("Connection", "keep-alive")
	s.w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
}

// WriteEvent writes an SSE event with the given type and data.
func (s *SSEWriter) WriteEvent(eventType string, data interface{}) error {
	// Get buffer from pool to reduce allocations
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufferPool.Put(buf)

	// Write event type directly
	buf.WriteString("event: ")
	buf.WriteString(eventType)
	buf.WriteString("\ndata: ")

	// Encode JSON directly to buffer (avoids intermediate allocation)
	encoder := json.NewEncoder(buf)
	encoder.SetEscapeHTML(false) // Avoid extra allocations for HTML escaping
	if err := encoder.Encode(data); err != nil {
		return err
	}

	// json.Encoder.Encode adds a newline, so we just need one more for SSE format
	buf.WriteByte('\n')

	// Write entire buffer at once
	if _, err := s.w.Write(buf.Bytes()); err != nil {
		return err
	}

	// Flush immediately
	s.flush()
	return nil
}

// WriteMessageStart writes a message_start event.
func (s *SSEWriter) WriteMessageStart(messageID, model string, inputTokens int) error {
	// Apply token distribution
	usage := DistributeTokens(inputTokens)

	event := MessageStartEvent{
		Type: "message_start",
		Message: MessageStartMessage{
			ID:      messageID,
			Type:    "message",
			Role:    "assistant",
			Model:   model,
			Content: []interface{}{},
			Usage: SSEUsage{
				InputTokens:              usage.InputTokens,
				CacheCreationInputTokens: usage.CacheCreationInputTokens,
				CacheReadInputTokens:     usage.CacheReadInputTokens,
			},
		},
	}

	return s.WriteEvent("message_start", event)
}

// WriteContentBlockStart writes a content_block_start event.
func (s *SSEWriter) WriteContentBlockStart(index int, blockType string) error {
	event := ContentBlockStartEvent{
		Type:  "content_block_start",
		Index: index,
		ContentBlock: ContentStart{
			Type: blockType,
			Text: "",
		},
	}

	return s.WriteEvent("content_block_start", event)
}

// WriteContentBlockDelta writes a content_block_delta event with text.
func (s *SSEWriter) WriteContentBlockDelta(index int, text string) error {
	event := ContentBlockDeltaEvent{
		Type:  "content_block_delta",
		Index: index,
		Delta: DeltaBlock{
			Type: "text_delta",
			Text: text,
		},
	}

	return s.WriteEvent("content_block_delta", event)
}

// WriteThinkingDelta writes a content_block_delta event with thinking content.
func (s *SSEWriter) WriteThinkingDelta(index int, text string) error {
	event := ContentBlockDeltaEvent{
		Type:  "content_block_delta",
		Index: index,
		Delta: DeltaBlock{
			Type: "thinking_delta",
			Text: text,
		},
	}

	return s.WriteEvent("content_block_delta", event)
}

// WriteContentBlockStop writes a content_block_stop event.
func (s *SSEWriter) WriteContentBlockStop(index int) error {
	event := ContentBlockStopEvent{
		Type:  "content_block_stop",
		Index: index,
	}

	return s.WriteEvent("content_block_stop", event)
}

// WriteMessageDelta writes a message_delta event with stop reason and usage.
func (s *SSEWriter) WriteMessageDelta(stopReason string, outputTokens int) error {
	event := MessageDeltaEvent{
		Type: "message_delta",
		Delta: MessageDeltaData{
			StopReason: stopReason,
		},
		Usage: OutputUsage{
			OutputTokens: outputTokens,
		},
	}

	return s.WriteEvent("message_delta", event)
}

// WriteMessageStop writes a message_stop event.
func (s *SSEWriter) WriteMessageStop() error {
	event := MessageStopEvent{
		Type: "message_stop",
	}

	return s.WriteEvent("message_stop", event)
}

// WritePing writes a ping event for keep-alive.
func (s *SSEWriter) WritePing() error {
	event := PingEvent{
		Type: "ping",
	}

	return s.WriteEvent("ping", event)
}

// WriteError writes an error event.
func (s *SSEWriter) WriteError(apiErr *APIError) error {
	event := ErrorEvent{
		Type: "error",
		Error: ErrorBlock{
			Type:    string(apiErr.Type),
			Message: apiErr.Message,
		},
	}

	return s.WriteEvent("error", event)
}

// flush flushes the response writer if it supports flushing.
func (s *SSEWriter) flush() {
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

// SSEEvent represents an SSE event to be written.
type SSEEvent struct {
	Type string
	Data interface{}
}
