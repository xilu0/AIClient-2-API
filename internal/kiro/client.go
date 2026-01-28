// Package kiro provides HTTP client for Kiro API.
package kiro

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Client is an HTTP client for the Kiro API.
type Client struct {
	httpClient *http.Client
	logger     *slog.Logger
}

// ClientOptions configures the Kiro HTTP client.
type ClientOptions struct {
	MaxConns            int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
	Timeout             time.Duration
	Logger              *slog.Logger
}

// NewClient creates a new Kiro API client with connection pooling.
func NewClient(opts ClientOptions) *Client {
	transport := &http.Transport{
		MaxIdleConns:        opts.MaxConns,
		MaxIdleConnsPerHost: opts.MaxIdleConnsPerHost,
		MaxConnsPerHost:     opts.MaxConns,
		IdleConnTimeout:     opts.IdleConnTimeout,
		DisableKeepAlives:   false,
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Client{
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   opts.Timeout, // 0 for streaming
		},
		logger: logger,
	}
}

// Request represents a request to the Kiro API.
type Request struct {
	Region     string
	ProfileARN string
	Token      string
	Body       []byte
}

// SendStreamingRequest sends a streaming request to the Kiro API.
// It returns a reader for the response body that must be closed by the caller.
func (c *Client) SendStreamingRequest(ctx context.Context, req *Request) (io.ReadCloser, error) {
	// Build Kiro API URL
	url := buildKiroURL(req.Region)

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(req.Body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/vnd.amazon.eventstream")
	httpReq.Header.Set("Authorization", "Bearer "+req.Token)
	httpReq.Header.Set("x-amz-profile-arn", req.ProfileARN)

	c.logger.Debug("sending request to Kiro API",
		"url", url,
		"profile_arn", req.ProfileARN,
	)

	// Send request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	// Check for error responses
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)

		c.logger.Warn("Kiro API error",
			"status", resp.StatusCode,
			"body", string(body),
		)

		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Body:       body,
		}
	}

	return resp.Body, nil
}

// APIError represents an error from the Kiro API.
type APIError struct {
	StatusCode int
	Body       []byte
}

// Error implements the error interface.
func (e *APIError) Error() string {
	return fmt.Sprintf("Kiro API error: status %d, body: %s", e.StatusCode, string(e.Body))
}

// IsRateLimited returns true if this is a rate limit error (429).
func (e *APIError) IsRateLimited() bool {
	return e.StatusCode == http.StatusTooManyRequests
}

// IsForbidden returns true if this is an authorization error (403).
func (e *APIError) IsForbidden() bool {
	return e.StatusCode == http.StatusForbidden
}

// buildKiroURL builds the Kiro API URL for the given region.
func buildKiroURL(region string) string {
	// Default to us-east-1 if region is empty
	if region == "" {
		region = "us-east-1"
	}
	// Kiro uses AWS Q endpoint
	return fmt.Sprintf("https://q.%s.amazonaws.com/generateAssistantResponse", region)
}

// BuildRequestBody builds the request body for the Kiro API.
// It converts Claude API messages to Kiro's conversationState format.
func BuildRequestBody(model string, messages []byte, maxTokens int, stream bool, system string) ([]byte, error) {
	// Parse Claude messages
	var claudeMessages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(messages, &claudeMessages); err != nil {
		return nil, fmt.Errorf("failed to parse messages: %w", err)
	}

	// Map model name to Kiro model ID
	kiroModel := mapModelToKiro(model)

	// Build conversation history and current message
	var history []map[string]interface{}
	var currentContent string

	// Process all messages except the last one as history
	for i := 0; i < len(claudeMessages)-1; i++ {
		msg := claudeMessages[i]
		content := extractTextContent(msg.Content)

		switch msg.Role {
		case "user":
			history = append(history, map[string]interface{}{
				"userInputMessage": map[string]interface{}{
					"content": content,
					"modelId": kiroModel,
					"origin":  "AI_EDITOR",
				},
			})
		case "assistant":
			history = append(history, map[string]interface{}{
				"assistantResponseMessage": map[string]interface{}{
					"content": content,
				},
			})
		}
	}

	// Last message is the current message
	if len(claudeMessages) > 0 {
		lastMsg := claudeMessages[len(claudeMessages)-1]
		currentContent = extractTextContent(lastMsg.Content)
	}

	// Build the request
	conversationID := generateConversationID()
	request := map[string]interface{}{
		"conversationState": map[string]interface{}{
			"chatTriggerType": "MANUAL",
			"conversationId":  conversationID,
			"currentMessage": map[string]interface{}{
				"userInputMessage": map[string]interface{}{
					"content": currentContent,
					"modelId": kiroModel,
					"origin":  "AI_EDITOR",
				},
			},
		},
	}

	// Add history if not empty
	if len(history) > 0 {
		request["conversationState"].(map[string]interface{})["history"] = history
	}

	return json.Marshal(request)
}

// extractTextContent extracts text content from Claude message content.
func extractTextContent(content json.RawMessage) string {
	// Try as simple string first
	var str string
	if err := json.Unmarshal(content, &str); err == nil {
		return str
	}

	// Try as content blocks
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	}
	if err := json.Unmarshal(content, &blocks); err == nil {
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

// mapModelToKiro maps Claude model names to Kiro model IDs.
// Haiku/Opus use lowercase dot format, Sonnet uses uppercase format.
func mapModelToKiro(model string) string {
	modelMapping := map[string]string{
		// Haiku models - lowercase dot format
		"claude-haiku-4-5":          "claude-haiku-4.5",
		"claude-haiku-4-5-20251001": "claude-haiku-4.5",
		// Opus models - lowercase dot format
		"claude-opus-4-5":          "claude-opus-4.5",
		"claude-opus-4-5-20251101": "claude-opus-4.5",
		// Sonnet models - uppercase format
		"claude-sonnet-4-5":          "CLAUDE_SONNET_4_5_20250929_V1_0",
		"claude-sonnet-4-5-20250929": "CLAUDE_SONNET_4_5_20250929_V1_0",
		"claude-sonnet-4-20250514":   "CLAUDE_SONNET_4_20250514_V1_0",
		"claude-3-7-sonnet-20250219": "CLAUDE_3_7_SONNET_20250219_V1_0",
	}

	if kiroModel, ok := modelMapping[model]; ok {
		return kiroModel
	}
	// Default to sonnet if unknown
	return "CLAUDE_SONNET_4_5_20250929_V1_0"
}

// generateConversationID generates a unique conversation ID.
func generateConversationID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Close closes the client and releases resources.
func (c *Client) Close() {
	c.httpClient.CloseIdleConnections()
}
