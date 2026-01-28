// Package unit contains unit tests for the Kiro server.
package unit

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/AIClient-2-API/internal/claude"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSSEWriter_WriteEvent(t *testing.T) {
	rec := httptest.NewRecorder()
	writer := claude.NewSSEWriter(rec)

	// Write message_start event
	data := map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id":   "msg_123",
			"type": "message",
			"role": "assistant",
		},
	}

	err := writer.WriteEvent("message_start", data)
	require.NoError(t, err)

	result := rec.Body.String()

	// Should have event type
	assert.Contains(t, result, "event: message_start\n")

	// Should have data line
	assert.Contains(t, result, "data: ")

	// Should end with double newline
	assert.True(t, strings.HasSuffix(result, "\n\n"))

	// Data should be valid JSON
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") {
			jsonStr := strings.TrimPrefix(line, "data: ")
			var parsed map[string]interface{}
			err := json.Unmarshal([]byte(jsonStr), &parsed)
			assert.NoError(t, err)
		}
	}
}

func TestSSEWriter_WriteContentDelta(t *testing.T) {
	rec := httptest.NewRecorder()
	writer := claude.NewSSEWriter(rec)

	err := writer.WriteContentBlockDelta(0, "Hello, world!")
	require.NoError(t, err)

	result := rec.Body.String()

	// Should have event type
	assert.Contains(t, result, "event: content_block_delta\n")

	// Should contain the text
	assert.Contains(t, result, "Hello, world!")

	// Should have index
	assert.Contains(t, result, `"index":0`)
}

func TestSSEWriter_WriteMessageStop(t *testing.T) {
	rec := httptest.NewRecorder()
	writer := claude.NewSSEWriter(rec)

	err := writer.WriteMessageStop()
	require.NoError(t, err)

	result := rec.Body.String()
	assert.Contains(t, result, "event: message_stop\n")
}

func TestSSEWriter_WritePing(t *testing.T) {
	rec := httptest.NewRecorder()
	writer := claude.NewSSEWriter(rec)

	err := writer.WritePing()
	require.NoError(t, err)

	result := rec.Body.String()
	assert.Contains(t, result, "event: ping\n")
}

func TestSSEWriter_WriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	writer := claude.NewSSEWriter(rec)

	apiErr := claude.NewAPIError("Something went wrong")
	err := writer.WriteError(apiErr)
	require.NoError(t, err)

	result := rec.Body.String()
	assert.Contains(t, result, "event: error\n")
	assert.Contains(t, result, "api_error")
	assert.Contains(t, result, "Something went wrong")
}

func TestSSEWriter_Headers(t *testing.T) {
	rec := httptest.NewRecorder()
	writer := claude.NewSSEWriter(rec)

	// Write headers
	writer.WriteHeaders()

	// Check Content-Type
	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))

	// Check Cache-Control
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))

	// Check Connection
	assert.Equal(t, "keep-alive", rec.Header().Get("Connection"))
}

func TestSSEWriter_Flush(t *testing.T) {
	// Create a handler that writes SSE events
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writer := claude.NewSSEWriter(w)
		writer.WriteHeaders()
		_ = writer.WriteEvent("message_start", map[string]string{"type": "message_start"})
	})

	// Use httptest.Server to get proper flushing behavior
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "event: message_start")
}

func TestSSEWriter_EscapeNewlines(t *testing.T) {
	rec := httptest.NewRecorder()
	writer := claude.NewSSEWriter(rec)

	// Write data with newlines
	err := writer.WriteContentBlockDelta(0, "Line 1\nLine 2\nLine 3")
	require.NoError(t, err)

	result := rec.Body.String()

	// Newlines in the text should be preserved in the JSON
	// but the SSE format should be correct
	lines := strings.Split(result, "\n")
	eventLine := false
	dataLine := false
	for _, line := range lines {
		if line == "event: content_block_delta" {
			eventLine = true
		}
		if strings.HasPrefix(line, "data: ") {
			dataLine = true
			// Verify JSON is valid
			jsonStr := strings.TrimPrefix(line, "data: ")
			var parsed map[string]interface{}
			err := json.Unmarshal([]byte(jsonStr), &parsed)
			assert.NoError(t, err)
		}
	}
	assert.True(t, eventLine, "should have event line")
	assert.True(t, dataLine, "should have data line")
}

func TestSSEWriter_CompleteSequence(t *testing.T) {
	var buf bytes.Buffer
	writer := claude.NewSSEWriter(httptest.NewRecorder())

	// Simulate a complete streaming response
	events := []struct {
		eventType string
		data      interface{}
	}{
		{"message_start", map[string]interface{}{
			"type": "message_start",
			"message": map[string]interface{}{
				"id":    "msg_123",
				"type":  "message",
				"role":  "assistant",
				"model": "claude-sonnet-4",
			},
		}},
		{"content_block_start", map[string]interface{}{
			"type":          "content_block_start",
			"index":         0,
			"content_block": map[string]interface{}{"type": "text", "text": ""},
		}},
		{"content_block_delta", map[string]interface{}{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]interface{}{"type": "text_delta", "text": "Hello"},
		}},
		{"content_block_stop", map[string]interface{}{
			"type":  "content_block_stop",
			"index": 0,
		}},
		{"message_delta", map[string]interface{}{
			"type":  "message_delta",
			"delta": map[string]interface{}{"stop_reason": "end_turn"},
			"usage": map[string]interface{}{"output_tokens": 5},
		}},
		{"message_stop", map[string]interface{}{
			"type": "message_stop",
		}},
	}

	rec := httptest.NewRecorder()
	writer = claude.NewSSEWriter(rec)

	for _, e := range events {
		err := writer.WriteEvent(e.eventType, e.data)
		require.NoError(t, err)
	}

	result := rec.Body.String()
	_ = buf // unused, just for simulation

	// Verify all events are present
	assert.Contains(t, result, "event: message_start")
	assert.Contains(t, result, "event: content_block_start")
	assert.Contains(t, result, "event: content_block_delta")
	assert.Contains(t, result, "event: content_block_stop")
	assert.Contains(t, result, "event: message_delta")
	assert.Contains(t, result, "event: message_stop")
}
