// Package handler provides HTTP handlers for the Kiro server.
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/anthropics/AIClient-2-API/internal/claude"
)

// CountTokensHandler handles POST /v1/messages/count_tokens requests.
// This endpoint estimates input tokens without making an API call.
type CountTokensHandler struct {
	logger *slog.Logger
}

// CountTokensHandlerOptions contains options for creating a CountTokensHandler.
type CountTokensHandlerOptions struct {
	Logger *slog.Logger
}

// NewCountTokensHandler creates a new CountTokensHandler.
func NewCountTokensHandler(opts CountTokensHandlerOptions) *CountTokensHandler {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &CountTokensHandler{logger: logger}
}

// ServeHTTP handles the count_tokens request.
func (h *CountTokensHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var req claude.MessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, claude.NewInvalidRequestError("Invalid JSON: "+err.Error()))
		return
	}

	// Validate request
	if err := h.validateRequest(&req); err != nil {
		h.writeError(w, err)
		return
	}

	// Count tokens using conservative estimation
	inputTokens, details := claude.EstimateInputTokensWithDetails(&req)

	h.logger.Info("count_tokens",
		"model", req.Model,
		"messages_count", len(req.Messages),
		"tools_count", len(req.Tools),
		"has_system", len(req.System) > 0,
		"has_thinking", req.Thinking != nil,
		"system_tokens", details.SystemTokens,
		"messages_tokens", details.MessagesTokens,
		"tools_tokens", details.ToolsTokens,
		"thinking_overhead", details.ThinkingOverhead,
		"total_input_tokens", inputTokens,
	)

	// Write response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]int{"input_tokens": inputTokens})
}

// validateRequest validates the count_tokens request.
func (h *CountTokensHandler) validateRequest(req *claude.MessageRequest) *claude.APIError {
	// Model is required
	if req.Model == "" {
		return claude.NewInvalidRequestError("model: field is required")
	}

	// Messages is required and must not be empty
	if len(req.Messages) == 0 {
		return claude.NewInvalidRequestError("messages: field is required and must contain at least one message")
	}

	// Validate each message
	for i, msg := range req.Messages {
		if msg.Role == "" {
			return claude.NewInvalidRequestError("messages[" + strconv.Itoa(i) + "].role: field is required")
		}
		if msg.Role != "user" && msg.Role != "assistant" {
			return claude.NewInvalidRequestError("messages[" + strconv.Itoa(i) + "].role: must be 'user' or 'assistant'")
		}
		if len(msg.Content) == 0 {
			return claude.NewInvalidRequestError("messages[" + strconv.Itoa(i) + "].content: field is required")
		}
	}

	// First message must be from user
	if req.Messages[0].Role != "user" {
		return claude.NewInvalidRequestError("messages: first message must have role 'user'")
	}

	return nil
}

// writeError writes an error response.
func (h *CountTokensHandler) writeError(w http.ResponseWriter, err *claude.APIError) {
	err.WriteError(w)
}
