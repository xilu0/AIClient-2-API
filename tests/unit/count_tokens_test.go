// Package unit contains unit tests for the Kiro server.
package unit

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/AIClient-2-API/internal/claude"
	"github.com/anthropics/AIClient-2-API/internal/handler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCountTokensHandler_ValidRequest(t *testing.T) {
	h := handler.NewCountTokensHandler(handler.CountTokensHandlerOptions{})

	req := claude.MessageRequest{
		Model: "claude-sonnet-4-5-20250929",
		Messages: []claude.Message{
			{Role: "user", Content: json.RawMessage(`"Hello, world!"`)},
		},
	}

	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v1/messages/count_tokens", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp map[string]int
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Greater(t, resp["input_tokens"], 0)
}

func TestCountTokensHandler_WithSystemPrompt(t *testing.T) {
	h := handler.NewCountTokensHandler(handler.CountTokensHandlerOptions{})

	req := claude.MessageRequest{
		Model:  "claude-sonnet-4-5-20250929",
		System: json.RawMessage(`"You are a helpful assistant."`),
		Messages: []claude.Message{
			{Role: "user", Content: json.RawMessage(`"Hello!"`)},
		},
	}

	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v1/messages/count_tokens", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]int
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	// System prompt adds tokens
	assert.Greater(t, resp["input_tokens"], 5)
}

func TestCountTokensHandler_WithTools(t *testing.T) {
	h := handler.NewCountTokensHandler(handler.CountTokensHandlerOptions{})

	req := claude.MessageRequest{
		Model: "claude-sonnet-4-5-20250929",
		Messages: []claude.Message{
			{Role: "user", Content: json.RawMessage(`"What's the weather?"`)},
		},
		Tools: []claude.Tool{
			{
				Name:        "get_weather",
				Description: "Get the current weather for a location",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}}}`),
			},
		},
	}

	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v1/messages/count_tokens", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]int
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	// Tools add significant tokens
	assert.Greater(t, resp["input_tokens"], 20)
}

func TestCountTokensHandler_MissingModel(t *testing.T) {
	h := handler.NewCountTokensHandler(handler.CountTokensHandlerOptions{})

	req := claude.MessageRequest{
		Messages: []claude.Message{
			{Role: "user", Content: json.RawMessage(`"Hello"`)},
		},
	}

	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v1/messages/count_tokens", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "error", resp["type"])
}

func TestCountTokensHandler_MissingMessages(t *testing.T) {
	h := handler.NewCountTokensHandler(handler.CountTokensHandlerOptions{})

	req := claude.MessageRequest{
		Model:    "claude-sonnet-4-5-20250929",
		Messages: []claude.Message{},
	}

	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v1/messages/count_tokens", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCountTokensHandler_InvalidRole(t *testing.T) {
	h := handler.NewCountTokensHandler(handler.CountTokensHandlerOptions{})

	req := claude.MessageRequest{
		Model: "claude-sonnet-4-5-20250929",
		Messages: []claude.Message{
			{Role: "system", Content: json.RawMessage(`"Hello"`)}, // Invalid role
		},
	}

	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v1/messages/count_tokens", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCountTokensHandler_FirstMessageNotUser(t *testing.T) {
	h := handler.NewCountTokensHandler(handler.CountTokensHandlerOptions{})

	req := claude.MessageRequest{
		Model: "claude-sonnet-4-5-20250929",
		Messages: []claude.Message{
			{Role: "assistant", Content: json.RawMessage(`"Hello"`)},
		},
	}

	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v1/messages/count_tokens", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCountTokensHandler_InvalidJSON(t *testing.T) {
	h := handler.NewCountTokensHandler(handler.CountTokensHandlerOptions{})

	httpReq := httptest.NewRequest("POST", "/v1/messages/count_tokens", bytes.NewReader([]byte(`{invalid json}`)))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCountTokensHandler_WithContentBlocks(t *testing.T) {
	h := handler.NewCountTokensHandler(handler.CountTokensHandlerOptions{})

	// Content as array of blocks
	content := []map[string]interface{}{
		{"type": "text", "text": "Hello, this is a test message."},
	}
	contentJSON, _ := json.Marshal(content)

	req := claude.MessageRequest{
		Model: "claude-sonnet-4-5-20250929",
		Messages: []claude.Message{
			{Role: "user", Content: contentJSON},
		},
	}

	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v1/messages/count_tokens", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]int
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Greater(t, resp["input_tokens"], 0)
}

func TestCountTokensHandler_WithImage(t *testing.T) {
	h := handler.NewCountTokensHandler(handler.CountTokensHandlerOptions{})

	// Content with image block
	content := []map[string]interface{}{
		{"type": "text", "text": "What's in this image?"},
		{
			"type": "image",
			"source": map[string]string{
				"type":       "base64",
				"media_type": "image/png",
				"data":       "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
			},
		},
	}
	contentJSON, _ := json.Marshal(content)

	req := claude.MessageRequest{
		Model: "claude-sonnet-4-5-20250929",
		Messages: []claude.Message{
			{Role: "user", Content: contentJSON},
		},
	}

	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v1/messages/count_tokens", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]int
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	// Image should add significant tokens (conservative estimate: 2500)
	assert.Greater(t, resp["input_tokens"], 2000)
}

func TestCountTokensHandler_MultipleMessages(t *testing.T) {
	h := handler.NewCountTokensHandler(handler.CountTokensHandlerOptions{})

	req := claude.MessageRequest{
		Model: "claude-sonnet-4-5-20250929",
		Messages: []claude.Message{
			{Role: "user", Content: json.RawMessage(`"Hello"`)},
			{Role: "assistant", Content: json.RawMessage(`"Hi there! How can I help you?"`)},
			{Role: "user", Content: json.RawMessage(`"What's the weather like?"`)},
		},
	}

	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v1/messages/count_tokens", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]int
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	// Multiple messages should have more tokens
	assert.Greater(t, resp["input_tokens"], 15)
}
