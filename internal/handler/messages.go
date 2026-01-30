// Package handler provides HTTP handlers for the Kiro server.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/anthropics/AIClient-2-API/internal/account"
	"github.com/anthropics/AIClient-2-API/internal/claude"
	"github.com/anthropics/AIClient-2-API/internal/debug"
	"github.com/anthropics/AIClient-2-API/internal/kiro"
	"github.com/anthropics/AIClient-2-API/internal/redis"
	"github.com/google/uuid"
)

// MessagesHandler handles POST /v1/messages requests.
type MessagesHandler struct {
	selector     *account.Selector
	poolManager  *redis.PoolManager
	tokenManager *redis.TokenManager
	kiroClient   *kiro.Client
	logger       *slog.Logger
	maxRetries   int
	debugDumper  *debug.Dumper
}

// MessagesHandlerOptions configures the messages handler.
type MessagesHandlerOptions struct {
	Selector     *account.Selector
	PoolManager  *redis.PoolManager
	TokenManager *redis.TokenManager
	KiroClient   *kiro.Client
	Logger       *slog.Logger
	MaxRetries   int
}

// NewMessagesHandler creates a new messages handler.
func NewMessagesHandler(opts MessagesHandlerOptions) *MessagesHandler {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	maxRetries := opts.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}

	debugDumper := debug.NewDumper()
	if debugDumper.Enabled() {
		logger.Info("debug dumper enabled", "dir", "/tmp/kiro-debug")
	}

	return &MessagesHandler{
		selector:     opts.Selector,
		poolManager:  opts.PoolManager,
		tokenManager: opts.TokenManager,
		kiroClient:   opts.KiroClient,
		logger:       logger,
		maxRetries:   maxRetries,
		debugDumper:  debugDumper,
	}
}

// ServeHTTP handles the messages request.
func (h *MessagesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Generate session ID for debugging (use request ID if available)
	sessionID := r.Header.Get("x-request-id")
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	// Create debug session (nil if disabled)
	debugSession := h.debugDumper.NewSession(sessionID)
	defer func() {
		if debugSession != nil {
			debugSession.Close()
		}
	}()

	// Parse request body
	var req claude.MessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, claude.NewInvalidRequestError("Invalid JSON: "+err.Error()))
		return
	}

	// Dump request for debugging
	if debugSession != nil {
		debugSession.SetModel(req.Model)
		debugSession.DumpRequestJSON(&req)
	}

	// Log received model for debugging
	h.logger.Debug("received request", "model", req.Model, "session_id", sessionID)

	// Validate request
	if err := h.validateRequest(&req); err != nil {
		h.writeError(w, err)
		return
	}

	// Handle streaming vs non-streaming
	if req.Stream {
		h.handleStreaming(ctx, w, &req, debugSession)
	} else {
		h.handleNonStreaming(ctx, w, &req, debugSession)
	}
}

// validateRequest validates the message request.
func (h *MessagesHandler) validateRequest(req *claude.MessageRequest) *claude.APIError {
	// Required fields
	if req.Model == "" {
		return claude.NewInvalidRequestError("model: field is required")
	}
	if len(req.Messages) == 0 {
		return claude.NewInvalidRequestError("messages: field is required and must contain at least one message")
	}
	if req.MaxTokens <= 0 {
		return claude.NewInvalidRequestError("max_tokens: must be a positive integer greater than 0")
	}

	// Validate max_tokens range
	if req.MaxTokens > 200000 {
		return claude.NewInvalidRequestError("max_tokens: exceeds maximum allowed value of 200000")
	}

	// Validate messages
	for i, msg := range req.Messages {
		if msg.Role == "" {
			return claude.NewInvalidRequestError(fmt.Sprintf("messages[%d].role: field is required", i))
		}
		if msg.Role != "user" && msg.Role != "assistant" {
			return claude.NewInvalidRequestError(fmt.Sprintf("messages[%d].role: must be 'user' or 'assistant', got '%s'", i, msg.Role))
		}
		if msg.Content == nil {
			return claude.NewInvalidRequestError(fmt.Sprintf("messages[%d].content: field is required", i))
		}
	}

	// Validate conversation starts with user
	if len(req.Messages) > 0 && req.Messages[0].Role != "user" {
		return claude.NewInvalidRequestError("messages: first message must have role 'user'")
	}

	// Validate temperature range if provided
	if req.Temperature != nil {
		if *req.Temperature < 0.0 || *req.Temperature > 1.0 {
			return claude.NewInvalidRequestError("temperature: must be between 0.0 and 1.0")
		}
	}

	// Validate top_p range if provided
	if req.TopP != nil {
		if *req.TopP < 0.0 || *req.TopP > 1.0 {
			return claude.NewInvalidRequestError("top_p: must be between 0.0 and 1.0")
		}
	}

	// Validate top_k if provided
	if req.TopK != nil && *req.TopK < 0 {
		return claude.NewInvalidRequestError("top_k: must be a non-negative integer")
	}

	return nil
}

// handleStreaming handles streaming requests.
func (h *MessagesHandler) handleStreaming(ctx context.Context, w http.ResponseWriter, req *claude.MessageRequest, debugSession *debug.Session) {
	startTime := time.Now()

	// Estimate input tokens before making the request
	estimatedInputTokens := claude.EstimateInputTokens(req)

	// Setup SSE writer
	sseWriter := claude.NewSSEWriter(w)
	sseWriter.WriteHeaders()

	// Try to get a working account with retries
	excluded := make(map[string]bool)
	var lastErr error
	var lastAccountUUID string    // Track the last account UUID for error reporting
	var triedAccounts []string    // Track all tried accounts for debugging

	for attempt := 0; attempt < h.maxRetries; attempt++ {
		// Select account
		acc, err := h.selector.SelectWithRetry(ctx, h.maxRetries-attempt, excluded)
		if err != nil {
			if errors.Is(err, account.ErrNoHealthyAccounts) {
				if debugSession != nil {
					debugSession.SetError(err)
					debugSession.Fail(err)
				}
				_ = sseWriter.WriteError(claude.ErrNoHealthyAccounts)
				return
			}
			lastErr = err
			continue
		}

		// Track this account for error reporting
		lastAccountUUID = acc.UUID
		triedAccounts = append(triedAccounts, acc.UUID)
		if debugSession != nil {
			debugSession.AddTriedAccount(acc.UUID)
			debugSession.SetAccountUUID(acc.UUID)
		}

		// Get token
		token, err := h.tokenManager.GetToken(ctx, acc.UUID)
		if err != nil {
			h.logger.Warn("failed to get token", "uuid", acc.UUID, "error", err)
			excluded[acc.UUID] = true
			lastErr = err
			continue
		}

		// Check if token is expired and try to refresh
		if h.tokenManager.IsExpired(token) {
			h.logger.Warn("token expired, attempting refresh", "uuid", acc.UUID)
			newToken, refreshErr := h.refreshToken(ctx, acc, token)
			if refreshErr != nil {
				h.logger.Error("token refresh failed", "uuid", acc.UUID, "error", refreshErr)
				excluded[acc.UUID] = true
				lastErr = refreshErr
				continue
			}
			token = newToken
		}

		// Build request body - include profileARN for social auth method and tools
		messagesJSON, _ := json.Marshal(req.Messages)
		toolsJSON, _ := json.Marshal(req.Tools)
		reqBody, metadata, err := kiro.BuildRequestBody(req.Model, messagesJSON, req.MaxTokens, true, req.GetSystemString(), acc.ProfileARN, toolsJSON)
		if err != nil {
			if debugSession != nil {
				debugSession.SetError(err)
				debugSession.Fail(err)
			}
			h.writeError(w, claude.NewAPIError("Failed to build request"))
			return
		}

		// Dump Kiro request for debugging
		if debugSession != nil {
			debugSession.DumpKiroRequest(reqBody)
		}

		// Get region from token (idcRegion), default to us-east-1
		region := token.IDCRegion
		if region == "" {
			region = "us-east-1"
		}

		// Send to Kiro
		kiroReq := &kiro.Request{
			Region:     region,
			ProfileARN: acc.ProfileARN,
			Token:      token.AccessToken,
			Body:       reqBody,
			Metadata:   metadata,
		}

		body, err := h.kiroClient.SendStreamingRequest(ctx, kiroReq)
		if err != nil {
			var apiErr *kiro.APIError
			if errors.As(err, &apiErr) {
				// Dump error response for debugging
				if debugSession != nil {
					debugSession.SetStatusCode(apiErr.StatusCode)
					debugSession.DumpKiroResponse(apiErr.Body)
				}

				// Always dump errors to file for troubleshooting (even if debug mode is off)
				if debugSession == nil && h.debugDumper.ErrorDumpEnabled() {
					errorSessionID := fmt.Sprintf("error-%d-%s", time.Now().UnixMilli(), acc.UUID[:8])
					errorSession := h.debugDumper.NewErrorSession(errorSessionID)
					if errorSession != nil {
						errorSession.SetModel(req.Model)
						errorSession.SetAccountUUID(acc.UUID)
						errorSession.SetStatusCode(apiErr.StatusCode)
						errorSession.SetErrorType(getErrorType(apiErr))
						errorSession.DumpRequestJSON(req)
						errorSession.DumpKiroRequest(reqBody)
						errorSession.DumpKiroResponse(apiErr.Body)
						errorSession.Fail(err)
					}
				}

				if apiErr.IsPaymentRequired() {
					// 402 Payment Required - quota exhausted, set recovery time to next month
					nextMonth := getNextMonthFirstDay()
					_ = h.poolManager.MarkUnhealthyWithRecovery(ctx, acc.UUID, nextMonth)
					excluded[acc.UUID] = true
					lastErr = err
					h.logger.Warn("Account quota exhausted, recovery scheduled",
						"uuid", acc.UUID,
						"profile_arn", acc.ProfileARN,
						"recovery_time", nextMonth.Format(time.RFC3339))
					continue
				}
				if apiErr.IsRateLimited() || apiErr.IsForbidden() {
					// Mark account unhealthy and retry
					_ = h.poolManager.MarkUnhealthy(ctx, acc.UUID)
					excluded[acc.UUID] = true
					lastErr = err
					continue
				}
				// Check for context too long error BEFORE generic IsBadRequest
				// Return 503 to trigger client-side compaction
				if apiErr.IsContextTooLong() {
					h.logger.Warn("Context too long, returning 503 to trigger compaction",
						"uuid", acc.UUID,
						"profile_arn", acc.ProfileARN,
						"model", req.Model)
					if debugSession != nil {
						debugSession.SetError(err)
						debugSession.Fail(err)
					}
					_ = sseWriter.WriteError(claude.NewOverloadedError(
						"Input context is too long. Please compact or reduce your conversation history to continue. " +
							"Consider using /compact command or starting a new conversation."))
					return
				}
				if apiErr.IsBadRequest() {
					// 400 Bad Request - likely model not supported by this account
					// Mark account unhealthy and retry with another account
					_ = h.poolManager.MarkUnhealthy(ctx, acc.UUID)
					excluded[acc.UUID] = true
					lastErr = err
					h.logger.Warn("Account returned 400, may not support this model",
						"uuid", acc.UUID,
						"profile_arn", acc.ProfileARN,
						"model", req.Model,
						"region", acc.Region)
					continue
				}
			}
			h.logger.Error("Kiro API error", "error", err, "uuid", acc.UUID, "profile_arn", acc.ProfileARN)
			if debugSession != nil {
				debugSession.SetError(err)
				debugSession.Fail(err)
			}
			_ = sseWriter.WriteError(claude.NewAPIError("Upstream error"))
			return
		}

		// Increment usage
		_ = h.poolManager.IncrementUsage(ctx, acc.UUID)

		// Stream the response with estimated tokens
		h.streamResponse(ctx, body, sseWriter, req.Model, estimatedInputTokens, acc.UUID, startTime, debugSession)
		if err := body.Close(); err != nil {
			h.logger.Warn("failed to close response body", "error", err)
		}

		// Mark debug session as success
		if debugSession != nil {
			debugSession.Success()
		}
		return
	}

	// All retries failed - pass through the original error for debugging
	h.logger.Error("all retries failed", "error", lastErr, "tried_accounts", triedAccounts)

	// Mark debug session as failed
	if debugSession != nil {
		debugSession.SetError(lastErr)
		debugSession.Fail(lastErr)
	}

	// Return appropriate error based on the last error type, preserving original message
	var apiErr *kiro.APIError
	if errors.As(lastErr, &apiErr) {
		if apiErr.IsOverloaded() {
			_ = sseWriter.WriteError(claude.NewOverloadedError(fmt.Sprintf("Service overloaded (account: %s): %s", lastAccountUUID, string(apiErr.Body))))
			return
		}
		// Pass through the original error message from Kiro API with account info
		_ = sseWriter.WriteError(claude.NewAPIErrorWithStatus(
			fmt.Sprintf("Upstream error (account: %s, status %d): %s", lastAccountUUID, apiErr.StatusCode, string(apiErr.Body)),
			apiErr.StatusCode,
		))
		return
	}
	_ = sseWriter.WriteError(claude.NewAPIError(fmt.Sprintf("All accounts failed (tried: %v): %v", triedAccounts, lastErr)))
}

// streamResponse reads from Kiro and writes SSE events.
func (h *MessagesHandler) streamResponse(ctx context.Context, body io.Reader, sseWriter *claude.SSEWriter, model string, estimatedInputTokens int, accountUUID string, startTime time.Time, debugSession *debug.Session) {
	// Use pooled parser to reduce GC pressure under high concurrency
	parser := kiro.GetEventStreamParser()
	defer kiro.ReleaseEventStreamParser(parser)

	converter := claude.NewConverterWithEstimate(model, estimatedInputTokens)

	buf := make([]byte, 4096)

	// Read and process chunks
	for {
		select {
		case <-ctx.Done():
			// Send final events on context cancellation
			h.sendFinalStreamEvents(sseWriter, converter, model, accountUUID, startTime)
			return
		default:
		}

		n, err := body.Read(buf)
		if err != nil {
			if err == io.EOF {
				// End of stream - send final events
				h.sendFinalStreamEvents(sseWriter, converter, model, accountUUID, startTime)
			} else {
				h.logger.Error("error reading response", "error", err)
			}
			return
		}

		if n == 0 {
			continue
		}

		// Parse AWS event stream messages
		messages, parseErr := parser.Parse(buf[:n])
		if parseErr != nil {
			h.logger.Error("error parsing event stream", "error", parseErr)
			continue
		}

		for _, msg := range messages {
			if !msg.IsEvent() {
				if msg.IsException() {
					h.logger.Error("received exception", "payload", string(msg.Payload))
					// Dump exception for debugging
					if debugSession != nil {
						debugSession.AppendKiroChunk(msg.Payload)
					}
				}
				continue
			}

			// Dump chunk for debugging
			if debugSession != nil {
				debugSession.AppendKiroChunk(msg.Payload)
			}

			// Parse Kiro chunk
			var chunk kiro.KiroChunk
			if err := json.Unmarshal(msg.Payload, &chunk); err != nil {
				h.logger.Warn("failed to parse chunk", "error", err)
				continue
			}

			// Convert to Claude format (returns multiple events)
			events, err := converter.Convert(&chunk)
			if err != nil {
				h.logger.Warn("failed to convert chunk", "error", err)
				continue
			}

			// Write all events returned by the converter
			for _, event := range events {
				if event == nil {
					continue
				}

				// Dump Claude event for debugging
				if debugSession != nil {
					debugSession.AppendClaudeChunk(event.Type, event.Data)
				}

				if err := sseWriter.WriteEvent(event.Type, event.Data); err != nil {
					h.logger.Error("failed to write SSE event", "error", err)
					return
				}
			}
		}
	}
}

// sendFinalStreamEvents sends the final SSE events at the end of a stream.
// Uses the converter's state to avoid sending duplicate events.
func (h *MessagesHandler) sendFinalStreamEvents(sseWriter *claude.SSEWriter, converter *claude.Converter, model string, accountUUID string, startTime time.Time) {
	// Get final usage from converter
	finalUsage := converter.GetFinalUsage()

	// Log usage information for monitoring
	h.logger.Info("request completed",
		"model", model,
		"account_uuid", accountUUID,
		"input_tokens", finalUsage.InputTokens,
		"output_tokens", finalUsage.OutputTokens,
		"cache_creation_tokens", finalUsage.CacheCreationInputTokens,
		"cache_read_tokens", finalUsage.CacheReadInputTokens,
		"total_input_tokens", finalUsage.InputTokens+finalUsage.CacheCreationInputTokens+finalUsage.CacheReadInputTokens,
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	// Send content_block_stop only if there's an unclosed content block
	// The converter tracks this state and handles closing text blocks before tool_use
	if converter.HasOpenContentBlock() {
		if err := sseWriter.WriteContentBlockStop(converter.GetCurrentContentIndex()); err != nil {
			h.logger.Error("failed to write content_block_stop", "error", err)
		}
		converter.MarkContentBlockClosed()
	}

	// Only send message_delta if the converter hasn't already sent one
	// This prevents duplicate message_delta events which can confuse clients
	if !converter.WasMessageDeltaEmitted() {
		// Get the appropriate stop_reason based on what was processed
		// If tool_use blocks were emitted, use "tool_use", otherwise "end_turn"
		stopReason := converter.GetStopReason()

		// Send message_delta with final usage (using typed struct for efficiency)
		// Note: SSEUsage has different json tags than Usage, so explicit copy is intentional
		messageDeltaEvent := claude.FullMessageDeltaEvent{
			Type: "message_delta",
			Delta: claude.MessageDeltaData{
				StopReason: stopReason,
			},
			Usage: claude.SSEUsage(finalUsage),
		}
		if err := sseWriter.WriteEvent("message_delta", messageDeltaEvent); err != nil {
			h.logger.Error("failed to write message_delta", "error", err)
		}
	}

	// Send message_stop
	if err := sseWriter.WriteMessageStop(); err != nil {
		h.logger.Error("failed to write message_stop", "error", err)
	}
}

// handleNonStreaming handles non-streaming requests.
func (h *MessagesHandler) handleNonStreaming(ctx context.Context, w http.ResponseWriter, req *claude.MessageRequest, debugSession *debug.Session) {
	startTime := time.Now()

	// Estimate input tokens before making the request
	estimatedInputTokens := claude.EstimateInputTokens(req)

	// Try to get a working account with retries
	excluded := make(map[string]bool)
	var lastErr error
	var lastAccountUUID string    // Track the last account UUID for error reporting
	var triedAccounts []string    // Track all tried accounts for debugging

	for attempt := 0; attempt < h.maxRetries; attempt++ {
		// Select account
		acc, err := h.selector.SelectWithRetry(ctx, h.maxRetries-attempt, excluded)
		if err != nil {
			if errors.Is(err, account.ErrNoHealthyAccounts) {
				if debugSession != nil {
					debugSession.SetError(err)
					debugSession.Fail(err)
				}
				h.writeError(w, claude.ErrNoHealthyAccounts)
				return
			}
			lastErr = err
			continue
		}

		// Track this account for error reporting
		lastAccountUUID = acc.UUID
		triedAccounts = append(triedAccounts, acc.UUID)
		if debugSession != nil {
			debugSession.AddTriedAccount(acc.UUID)
			debugSession.SetAccountUUID(acc.UUID)
		}

		// Get token
		token, err := h.tokenManager.GetToken(ctx, acc.UUID)
		if err != nil {
			h.logger.Warn("failed to get token", "uuid", acc.UUID, "error", err)
			excluded[acc.UUID] = true
			lastErr = err
			continue
		}

		// Check if token is expired and try to refresh
		if h.tokenManager.IsExpired(token) {
			h.logger.Warn("token expired, attempting refresh", "uuid", acc.UUID)
			newToken, refreshErr := h.refreshToken(ctx, acc, token)
			if refreshErr != nil {
				h.logger.Error("token refresh failed", "uuid", acc.UUID, "error", refreshErr)
				excluded[acc.UUID] = true
				lastErr = refreshErr
				continue
			}
			token = newToken
		}

		// Build request body - use stream=true internally to receive chunks
		// Include profileARN for social auth method and tools
		messagesJSON, _ := json.Marshal(req.Messages)
		toolsJSON, _ := json.Marshal(req.Tools)
		reqBody, metadata, err := kiro.BuildRequestBody(req.Model, messagesJSON, req.MaxTokens, true, req.GetSystemString(), acc.ProfileARN, toolsJSON)
		if err != nil {
			if debugSession != nil {
				debugSession.SetError(err)
				debugSession.Fail(err)
			}
			h.writeError(w, claude.NewAPIError("Failed to build request"))
			return
		}

		// Dump Kiro request for debugging
		if debugSession != nil {
			debugSession.DumpKiroRequest(reqBody)
		}

		// Get region from token (idcRegion), default to us-east-1
		region := token.IDCRegion
		if region == "" {
			region = "us-east-1"
		}

		// Send to Kiro
		kiroReq := &kiro.Request{
			Region:     region,
			ProfileARN: acc.ProfileARN,
			Token:      token.AccessToken,
			Body:       reqBody,
			Metadata:   metadata,
		}

		body, err := h.kiroClient.SendStreamingRequest(ctx, kiroReq)
		if err != nil {
			var apiErr *kiro.APIError
			if errors.As(err, &apiErr) {
				// Dump error response for debugging
				if debugSession != nil {
					debugSession.SetStatusCode(apiErr.StatusCode)
					debugSession.DumpKiroResponse(apiErr.Body)
				}

				// Always dump errors to file for troubleshooting (even if debug mode is off)
				if debugSession == nil && h.debugDumper.ErrorDumpEnabled() {
					errorSessionID := fmt.Sprintf("error-%d-%s", time.Now().UnixMilli(), acc.UUID[:8])
					errorSession := h.debugDumper.NewErrorSession(errorSessionID)
					if errorSession != nil {
						errorSession.SetModel(req.Model)
						errorSession.SetAccountUUID(acc.UUID)
						errorSession.SetStatusCode(apiErr.StatusCode)
						errorSession.SetErrorType(getErrorType(apiErr))
						errorSession.DumpRequestJSON(req)
						errorSession.DumpKiroRequest(reqBody)
						errorSession.DumpKiroResponse(apiErr.Body)
						errorSession.Fail(err)
					}
				}

				if apiErr.IsPaymentRequired() {
					// 402 Payment Required - quota exhausted, set recovery time to next month
					nextMonth := getNextMonthFirstDay()
					_ = h.poolManager.MarkUnhealthyWithRecovery(ctx, acc.UUID, nextMonth)
					excluded[acc.UUID] = true
					lastErr = err
					h.logger.Warn("Account quota exhausted, recovery scheduled",
						"uuid", acc.UUID,
						"profile_arn", acc.ProfileARN,
						"recovery_time", nextMonth.Format(time.RFC3339))
					continue
				}
				if apiErr.IsRateLimited() || apiErr.IsForbidden() {
					// Mark account unhealthy and retry
					_ = h.poolManager.MarkUnhealthy(ctx, acc.UUID)
					excluded[acc.UUID] = true
					lastErr = err
					continue
				}
				// Check for context too long error BEFORE generic IsBadRequest
				// Return 503 to trigger client-side compaction
				if apiErr.IsContextTooLong() {
					h.logger.Warn("Context too long, returning 503 to trigger compaction",
						"uuid", acc.UUID,
						"profile_arn", acc.ProfileARN,
						"model", req.Model)
					if debugSession != nil {
						debugSession.SetError(err)
						debugSession.Fail(err)
					}
					h.writeError(w, claude.NewOverloadedError(
						"Input context is too long. Please compact or reduce your conversation history to continue. "+
							"Consider using /compact command or starting a new conversation."))
					return
				}
				if apiErr.IsBadRequest() {
					// 400 Bad Request - likely model not supported by this account
					// Mark account unhealthy and retry with another account
					_ = h.poolManager.MarkUnhealthy(ctx, acc.UUID)
					excluded[acc.UUID] = true
					lastErr = err
					h.logger.Warn("Account returned 400, may not support this model",
						"uuid", acc.UUID,
						"profile_arn", acc.ProfileARN,
						"model", req.Model,
						"region", acc.Region)
					continue
				}
			}
			h.logger.Error("Kiro API error", "error", err, "uuid", acc.UUID, "profile_arn", acc.ProfileARN)
			if debugSession != nil {
				debugSession.SetError(err)
				debugSession.Fail(err)
			}
			h.writeError(w, claude.NewAPIError("Upstream error"))
			return
		}

		// Increment usage
		_ = h.poolManager.IncrementUsage(ctx, acc.UUID)

		// Aggregate the response with estimated tokens
		response := h.aggregateResponse(ctx, body, req.Model, estimatedInputTokens, acc.UUID, startTime, debugSession)
		if err := body.Close(); err != nil {
			h.logger.Warn("failed to close response body", "error", err)
		}

		if response == nil {
			if debugSession != nil {
				debugSession.SetError(fmt.Errorf("failed to aggregate response"))
				debugSession.Fail(fmt.Errorf("failed to aggregate response"))
			}
			h.writeError(w, claude.NewAPIError("Failed to aggregate response"))
			return
		}

		// Mark debug session as success
		if debugSession != nil {
			debugSession.Success()
		}

		// Write JSON response with proper Content-Type
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			h.logger.Error("failed to write response", "error", err)
		}
		return
	}

	// All retries failed
	h.logger.Error("all retries failed", "error", lastErr, "tried_accounts", triedAccounts)

	// Mark debug session as failed
	if debugSession != nil {
		debugSession.SetError(lastErr)
		debugSession.Fail(lastErr)
	}

	// Return appropriate error based on the last error type
	var apiErr *kiro.APIError
	if errors.As(lastErr, &apiErr) {
		if apiErr.IsOverloaded() {
			h.writeError(w, claude.NewOverloadedError(fmt.Sprintf("Service overloaded (account: %s): %s", lastAccountUUID, string(apiErr.Body))))
			return
		}
		// Pass through the original error message from Kiro API with account info
		h.writeError(w, claude.NewAPIErrorWithStatus(
			fmt.Sprintf("Upstream error (account: %s, status %d): %s", lastAccountUUID, apiErr.StatusCode, string(apiErr.Body)),
			apiErr.StatusCode,
		))
		return
	}
	h.writeError(w, claude.NewAPIError(fmt.Sprintf("All accounts failed (tried: %v): %v", triedAccounts, lastErr)))
}

// aggregateResponse reads all chunks and builds a complete response.
func (h *MessagesHandler) aggregateResponse(ctx context.Context, body io.Reader, model string, estimatedInputTokens int, accountUUID string, startTime time.Time, debugSession *debug.Session) *claude.MessageResponse {
	// Use pooled parser to reduce GC pressure under high concurrency
	parser := kiro.GetEventStreamParser()
	defer kiro.ReleaseEventStreamParser(parser)

	aggregator := claude.NewAggregatorWithEstimate(model, estimatedInputTokens)

	buf := make([]byte, 4096)

	for {
		select {
		case <-ctx.Done():
			resp := aggregator.Build()
			h.logUsage(model, accountUUID, &resp.Usage, startTime)
			return resp
		default:
		}

		n, err := body.Read(buf)
		if err != nil {
			if err == io.EOF {
				// End of stream, return aggregated response
				resp := aggregator.Build()
				h.logUsage(model, accountUUID, &resp.Usage, startTime)
				return resp
			}
			h.logger.Error("error reading response", "error", err)
			resp := aggregator.Build()
			h.logUsage(model, accountUUID, &resp.Usage, startTime)
			return resp
		}

		if n == 0 {
			continue
		}

		// Parse AWS event stream messages
		messages, err := parser.Parse(buf[:n])
		if err != nil {
			h.logger.Error("error parsing event stream", "error", err)
			continue
		}

		for _, msg := range messages {
			if !msg.IsEvent() {
				if msg.IsException() {
					h.logger.Error("received exception", "payload", string(msg.Payload))
					// Dump exception for debugging
					if debugSession != nil {
						debugSession.AppendKiroChunk(msg.Payload)
					}
				}
				continue
			}

			// Dump chunk for debugging
			if debugSession != nil {
				debugSession.AppendKiroChunk(msg.Payload)
			}

			// Parse Kiro chunk
			var chunk kiro.KiroChunk
			if err := json.Unmarshal(msg.Payload, &chunk); err != nil {
				h.logger.Warn("failed to parse chunk", "error", err)
				continue
			}

			// Add to aggregator
			if err := aggregator.Add(&chunk); err != nil {
				h.logger.Warn("failed to aggregate chunk", "error", err)
			}
		}
	}
}

// writeError writes an error response.
func (h *MessagesHandler) writeError(w http.ResponseWriter, err *claude.APIError) {
	err.WriteError(w)
}

// refreshToken attempts to refresh an expired token.
func (h *MessagesHandler) refreshToken(ctx context.Context, acc *redis.Account, token *redis.Token) (*redis.Token, error) {
	if token.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}

	// Get region for refresh endpoint
	region := token.IDCRegion
	if region == "" {
		region = acc.Region
	}
	if region == "" {
		region = "us-east-1"
	}

	h.logger.Info("refreshing expired token", "uuid", acc.UUID, "region", region)

	// Call Kiro refresh endpoint
	// For IDC auth (builder-id), clientID and clientSecret are required
	refreshResp, err := h.kiroClient.RefreshToken(ctx, region, token.RefreshToken, token.AuthMethod, token.IDCRegion, token.ClientID, token.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("token refresh failed: %w", err)
	}

	// Calculate new expiry time
	expiresAt := time.Now().Add(time.Duration(refreshResp.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)

	// Build updated token
	newToken := &redis.Token{
		AccessToken:   refreshResp.AccessToken,
		RefreshToken:  refreshResp.RefreshToken,
		ExpiresAt:     expiresAt,
		AuthMethod:    token.AuthMethod,
		TokenType:     token.TokenType,
		ClientID:      token.ClientID,
		ClientSecret:  token.ClientSecret,
		IDCRegion:     token.IDCRegion,
		LastRefreshed: time.Now().UTC().Format(time.RFC3339),
	}

	// Update ProfileARN if returned
	if refreshResp.ProfileARN != "" {
		// Update account's ProfileARN in pool
		acc.ProfileARN = refreshResp.ProfileARN
	}

	// Save to Redis
	if err := h.tokenManager.SetToken(ctx, acc.UUID, newToken); err != nil {
		h.logger.Warn("failed to save refreshed token", "uuid", acc.UUID, "error", err)
		// Continue anyway, token is valid
	}

	h.logger.Info("token refreshed successfully", "uuid", acc.UUID, "expires_at", expiresAt)
	return newToken, nil
}

// getNextMonthFirstDay returns the first day of next month at 00:00:00 UTC.
// Used for scheduling recovery time for quota exhaustion (402 errors).
// Matches Node.js implementation: _getNextMonthFirstDay.
func getNextMonthFirstDay() time.Time {
	now := time.Now().UTC()
	year, month, _ := now.Date()
	// Add one month
	nextMonth := month + 1
	nextYear := year
	if nextMonth > 12 {
		nextMonth = 1
		nextYear++
	}
	return time.Date(nextYear, nextMonth, 1, 0, 0, 0, 0, time.UTC)
}

// getErrorType returns a human-readable error type string for the API error.
func getErrorType(apiErr *kiro.APIError) string {
	if apiErr == nil {
		return "unknown"
	}
	switch {
	case apiErr.IsBadRequest():
		return "bad_request"
	case apiErr.IsRateLimited():
		return "rate_limit"
	case apiErr.IsOverloaded():
		return "overloaded"
	case apiErr.IsForbidden():
		return "forbidden"
	case apiErr.IsPaymentRequired():
		return "payment_required"
	default:
		return fmt.Sprintf("http_%d", apiErr.StatusCode)
	}
}

// logUsage logs the token usage information for a completed request.
func (h *MessagesHandler) logUsage(model string, accountUUID string, usage *claude.Usage, startTime time.Time) {
	if usage == nil {
		return
	}
	h.logger.Info("request completed",
		"model", model,
		"account_uuid", accountUUID,
		"input_tokens", usage.InputTokens,
		"output_tokens", usage.OutputTokens,
		"cache_creation_tokens", usage.CacheCreationInputTokens,
		"cache_read_tokens", usage.CacheReadInputTokens,
		"total_input_tokens", usage.InputTokens+usage.CacheCreationInputTokens+usage.CacheReadInputTokens,
		"duration_ms", time.Since(startTime).Milliseconds(),
	)
}
