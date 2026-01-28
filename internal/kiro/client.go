// Package kiro provides HTTP client for Kiro API.
package kiro

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// KiroVersion simulates the Kiro IDE version for user-agent.
	KiroVersion = "1.0.0"
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

	// Set headers to match the JS implementation exactly
	// Required headers for Kiro API compatibility
	invocationID := uuid.New().String()
	osName := runtime.GOOS
	goVersion := runtime.Version()
	machineID := generateMachineID(req.ProfileARN)

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+req.Token)
	httpReq.Header.Set("amz-sdk-invocation-id", invocationID)
	httpReq.Header.Set("amz-sdk-request", "attempt=1; max=1")
	httpReq.Header.Set("x-amzn-kiro-agent-mode", "vibe")
	httpReq.Header.Set("x-amz-user-agent", fmt.Sprintf("aws-sdk-js/1.0.0 KiroIDE-%s-%s", KiroVersion, machineID))
	httpReq.Header.Set("User-Agent", fmt.Sprintf("aws-sdk-js/1.0.0 ua/2.1 os/%s lang/go md/go#%s api/codewhispererruntime#1.0.0 m/E KiroIDE-%s-%s", osName, goVersion, KiroVersion, machineID))
	httpReq.Header.Set("Connection", "close")

	c.logger.Debug("sending request to Kiro API",
		"url", url,
		"profile_arn", req.ProfileARN,
		"invocation_id", invocationID,
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

// generateMachineID generates a machine ID from the profile ARN.
func generateMachineID(profileARN string) string {
	if profileARN == "" {
		profileARN = "KIRO_DEFAULT_MACHINE"
	}
	// Use first 16 chars of a deterministic hash-like value
	b := make([]byte, 32)
	for i := 0; i < len(profileARN) && i < 32; i++ {
		b[i%32] ^= profileARN[i]
	}
	return fmt.Sprintf("%x", b[:16])
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

// KiroTool represents a tool definition in Kiro format.
type KiroTool struct {
	ToolSpecification KiroToolSpec `json:"toolSpecification"`
}

// KiroToolSpec represents the tool specification in Kiro format.
type KiroToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema KiroInputSchema `json:"inputSchema"`
}

// KiroInputSchema wraps the JSON schema for Kiro.
type KiroInputSchema struct {
	JSON json.RawMessage `json:"json"`
}

// ClaudeTool represents a Claude API tool definition.
type ClaudeTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// BuildRequestBody builds the request body for the Kiro API.
// It converts Claude API messages to Kiro's conversationState format.
// profileARN is included in the request body for social auth method (required by Kiro API).
// tools is the array of tool definitions from the Claude API request.
func BuildRequestBody(model string, messages []byte, maxTokens int, stream bool, system string, profileARN string, tools []byte) ([]byte, error) {
	// Parse Claude messages with full content block support
	var claudeMessages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(messages, &claudeMessages); err != nil {
		return nil, fmt.Errorf("failed to parse messages: %w", err)
	}

	if len(claudeMessages) == 0 {
		return nil, fmt.Errorf("no messages found")
	}

	// Map model name to Kiro model ID
	kiroModel := mapModelToKiro(model)

	// Build conversation history and current message
	var history []map[string]interface{}
	startIndex := 0

	// Handle system prompt - prepend to first user message (matching JS implementation)
	if system != "" && len(claudeMessages) > 0 {
		if claudeMessages[0].Role == "user" {
			// Prepend system prompt to first user message content
			parsed := parseMessageContent(claudeMessages[0].Content)
			userMsg := map[string]interface{}{
				"content": system + "\n\n" + parsed.Text,
				"modelId": kiroModel,
				"origin":  "AI_EDITOR",
			}
			// Add tool results if present
			if len(parsed.ToolResults) > 0 {
				userMsg["userInputMessageContext"] = map[string]interface{}{
					"toolResults": parsed.ToolResults,
				}
			}
			// Add images if present
			if len(parsed.Images) > 0 {
				userMsg["images"] = parsed.Images
			}
			history = append(history, map[string]interface{}{
				"userInputMessage": userMsg,
			})
			startIndex = 1 // Start from second message
		} else {
			// Add system prompt as standalone user message
			history = append(history, map[string]interface{}{
				"userInputMessage": map[string]interface{}{
					"content": system,
					"modelId": kiroModel,
					"origin":  "AI_EDITOR",
				},
			})
		}
	}

	// Process all messages except the last one as history
	for i := startIndex; i < len(claudeMessages)-1; i++ {
		msg := claudeMessages[i]

		switch msg.Role {
		case "user":
			parsed := parseMessageContent(msg.Content)
			userMsg := map[string]interface{}{
				"content": parsed.Text,
				"modelId": kiroModel,
				"origin":  "AI_EDITOR",
			}
			// Add tool results if present
			if len(parsed.ToolResults) > 0 {
				userMsg["userInputMessageContext"] = map[string]interface{}{
					"toolResults": parsed.ToolResults,
				}
			}
			// Add images if present
			if len(parsed.Images) > 0 {
				userMsg["images"] = parsed.Images
			}
			history = append(history, map[string]interface{}{
				"userInputMessage": userMsg,
			})
		case "assistant":
			parsed := parseAssistantContent(msg.Content)
			assistantMsg := map[string]interface{}{
				"content": parsed.Text,
			}
			// Add tool uses if present
			if len(parsed.ToolUses) > 0 {
				assistantMsg["toolUses"] = parsed.ToolUses
			}
			history = append(history, map[string]interface{}{
				"assistantResponseMessage": assistantMsg,
			})
		}
	}

	// Handle last message - it becomes currentMessage
	lastMsg := claudeMessages[len(claudeMessages)-1]

	var currentContent string
	var currentToolResults []map[string]interface{}
	var currentImages []map[string]interface{}

	// If last message is assistant, move it to history and create user currentMessage with "Continue"
	// Kiro API requires currentMessage to be userInputMessage type
	if lastMsg.Role == "assistant" {
		parsed := parseAssistantContent(lastMsg.Content)
		content := parsed.Text
		if content == "" {
			content = "Continue"
		}
		assistantMsg := map[string]interface{}{
			"content": content,
		}
		if len(parsed.ToolUses) > 0 {
			assistantMsg["toolUses"] = parsed.ToolUses
		}
		history = append(history, map[string]interface{}{
			"assistantResponseMessage": assistantMsg,
		})
		currentContent = "Continue"
	} else {
		// Last message is user
		parsed := parseMessageContent(lastMsg.Content)
		currentContent = parsed.Text
		currentToolResults = parsed.ToolResults
		currentImages = parsed.Images

		// Kiro API requires history to end with assistantResponseMessage if currentMessage is user
		if len(history) > 0 {
			lastHistoryItem := history[len(history)-1]
			if _, hasUser := lastHistoryItem["userInputMessage"]; hasUser {
				// History ends with userInputMessage, add empty assistantResponseMessage
				history = append(history, map[string]interface{}{
					"assistantResponseMessage": map[string]interface{}{
						"content": "Continue",
					},
				})
			}
		}
	}

	// Kiro API requires content to never be empty
	if currentContent == "" {
		if len(currentToolResults) > 0 {
			currentContent = "Tool results provided."
		} else {
			currentContent = "Continue"
		}
	}

	// Build the request with proper UUID format (matching JS uuidv4())
	conversationID := uuid.New().String()

	// Build currentMessage userInputMessage
	userInputMessage := map[string]interface{}{
		"content": currentContent,
		"modelId": kiroModel,
		"origin":  "AI_EDITOR",
	}

	// Add images if present
	if len(currentImages) > 0 {
		userInputMessage["images"] = currentImages
	}

	// Build userInputMessageContext with tool results and tools
	userInputMessageContext := make(map[string]interface{})

	// Add tool results if present
	if len(currentToolResults) > 0 {
		// Deduplicate tool results by toolUseId
		uniqueToolResults := deduplicateToolResults(currentToolResults)
		userInputMessageContext["toolResults"] = uniqueToolResults
	}

	// Add tools (tool definitions) if present
	if len(tools) > 0 {
		kiroTools := convertToolsToKiroFormat(tools)
		if len(kiroTools) > 0 {
			userInputMessageContext["tools"] = kiroTools
		}
	}

	// Only add userInputMessageContext if it has content
	if len(userInputMessageContext) > 0 {
		userInputMessage["userInputMessageContext"] = userInputMessageContext
	}

	request := map[string]interface{}{
		"conversationState": map[string]interface{}{
			"chatTriggerType": "MANUAL",
			"conversationId":  conversationID,
			"currentMessage": map[string]interface{}{
				"userInputMessage": userInputMessage,
			},
		},
	}

	// Add history only if not empty (API may not accept empty array)
	if len(history) > 0 {
		request["conversationState"].(map[string]interface{})["history"] = history
	}

	// Add profileArn to request body (required for social auth method)
	// This is critical - JS implementation includes profileArn in body, not as header
	if profileARN != "" {
		request["profileArn"] = profileARN
	}

	return json.Marshal(request)
}

// ParsedUserContent holds the parsed components of a user message.
type ParsedUserContent struct {
	Text        string
	ToolResults []map[string]interface{}
	Images      []map[string]interface{}
}

// ParsedAssistantContent holds the parsed components of an assistant message.
type ParsedAssistantContent struct {
	Text     string
	ToolUses []map[string]interface{}
}

// parseMessageContent parses a user message content into its components.
// Handles text, tool_result, and image content blocks.
func parseMessageContent(content json.RawMessage) ParsedUserContent {
	result := ParsedUserContent{}

	// Try as simple string first
	var str string
	if err := json.Unmarshal(content, &str); err == nil {
		result.Text = str
		return result
	}

	// Try as content blocks array
	var blocks []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text,omitempty"`
		ToolUseID string          `json:"tool_use_id,omitempty"`
		Content   json.RawMessage `json:"content,omitempty"`
		IsError   bool            `json:"is_error,omitempty"`
		Source    *struct {
			Type      string `json:"type"`
			MediaType string `json:"media_type"`
			Data      string `json:"data"`
		} `json:"source,omitempty"`
	}

	if err := json.Unmarshal(content, &blocks); err != nil {
		return result
	}

	for _, block := range blocks {
		switch block.Type {
		case "text":
			result.Text += block.Text
		case "tool_result":
			toolResult := map[string]interface{}{
				"content":   []map[string]interface{}{{"text": extractToolResultContent(block.Content)}},
				"status":    "success",
				"toolUseId": block.ToolUseID,
			}
			if block.IsError {
				toolResult["status"] = "error"
			}
			result.ToolResults = append(result.ToolResults, toolResult)
		case "image":
			if block.Source != nil {
				// Extract format from media_type (e.g., "image/png" -> "png")
				format := "png"
				if block.Source.MediaType != "" {
					parts := splitMediaType(block.Source.MediaType)
					if len(parts) > 1 {
						format = parts[1]
					}
				}
				image := map[string]interface{}{
					"format": format,
					"source": map[string]interface{}{
						"bytes": block.Source.Data,
					},
				}
				result.Images = append(result.Images, image)
			}
		}
	}

	return result
}

// parseAssistantContent parses an assistant message content into its components.
// Handles text, tool_use, and thinking content blocks.
func parseAssistantContent(content json.RawMessage) ParsedAssistantContent {
	result := ParsedAssistantContent{}

	// Try as simple string first
	var str string
	if err := json.Unmarshal(content, &str); err == nil {
		result.Text = str
		return result
	}

	// Try as content blocks array
	var blocks []struct {
		Type     string          `json:"type"`
		Text     string          `json:"text,omitempty"`
		Thinking string          `json:"thinking,omitempty"`
		ID       string          `json:"id,omitempty"`
		Name     string          `json:"name,omitempty"`
		Input    json.RawMessage `json:"input,omitempty"`
	}

	if err := json.Unmarshal(content, &blocks); err != nil {
		return result
	}

	var thinkingText string
	for _, block := range blocks {
		switch block.Type {
		case "text":
			result.Text += block.Text
		case "thinking":
			thinkingText += block.Thinking
			if thinkingText == "" {
				thinkingText = block.Text
			}
		case "tool_use":
			var input interface{}
			if len(block.Input) > 0 {
				_ = json.Unmarshal(block.Input, &input)
			}
			toolUse := map[string]interface{}{
				"toolUseId": block.ID,
				"name":      block.Name,
			}
			if input != nil {
				toolUse["input"] = input
			}
			result.ToolUses = append(result.ToolUses, toolUse)
		}
	}

	// Prepend thinking content with tags if present
	if thinkingText != "" {
		if result.Text != "" {
			result.Text = "<kiro_thinking>" + thinkingText + "</kiro_thinking>\n\n" + result.Text
		} else {
			result.Text = "<kiro_thinking>" + thinkingText + "</kiro_thinking>"
		}
	}

	return result
}

// extractToolResultContent extracts text content from tool_result content field.
func extractToolResultContent(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}

	// Try as simple string
	var str string
	if err := json.Unmarshal(content, &str); err == nil {
		return str
	}

	// Try as array of content blocks
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

// splitMediaType splits a media type string (e.g., "image/png") into parts.
func splitMediaType(mediaType string) []string {
	result := make([]string, 0, 2)
	idx := 0
	for i, c := range mediaType {
		if c == '/' {
			result = append(result, mediaType[idx:i])
			idx = i + 1
		}
	}
	if idx < len(mediaType) {
		result = append(result, mediaType[idx:])
	}
	return result
}

// deduplicateToolResults removes duplicate tool results by toolUseId.
func deduplicateToolResults(toolResults []map[string]interface{}) []map[string]interface{} {
	seen := make(map[string]bool)
	unique := make([]map[string]interface{}, 0, len(toolResults))
	for _, tr := range toolResults {
		if id, ok := tr["toolUseId"].(string); ok {
			if !seen[id] {
				seen[id] = true
				unique = append(unique, tr)
			}
		} else {
			unique = append(unique, tr)
		}
	}
	return unique
}

// extractTextContent extracts text content from Claude message content.
// Deprecated: Use parseMessageContent or parseAssistantContent instead for full content handling.
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


// convertToolsToKiroFormat converts Claude tool definitions to Kiro format.
// It filters out web_search/websearch tools and truncates long descriptions.
func convertToolsToKiroFormat(toolsJSON []byte) []map[string]interface{} {
	if len(toolsJSON) == 0 {
		return nil
	}

	var claudeTools []ClaudeTool
	if err := json.Unmarshal(toolsJSON, &claudeTools); err != nil {
		return nil
	}

	const maxDescriptionLength = 9216

	var kiroTools []map[string]interface{}
	for _, tool := range claudeTools {
		// Filter out web_search/websearch tools (Kiro doesn't support them)
		nameLower := strings.ToLower(tool.Name)
		if nameLower == "web_search" || nameLower == "websearch" {
			continue
		}

		// Truncate long descriptions
		desc := tool.Description
		if len(desc) > maxDescriptionLength {
			desc = desc[:maxDescriptionLength] + "..."
		}

		// Build Kiro tool format
		inputSchema := tool.InputSchema
		if len(inputSchema) == 0 {
			inputSchema = []byte("{}")
		}

		kiroTool := map[string]interface{}{
			"toolSpecification": map[string]interface{}{
				"name":        tool.Name,
				"description": desc,
				"inputSchema": map[string]interface{}{
					"json": json.RawMessage(inputSchema),
				},
			},
		}
		kiroTools = append(kiroTools, kiroTool)
	}

	return kiroTools
}

// Close closes the client and releases resources.
func (c *Client) Close() {
	c.httpClient.CloseIdleConnections()
}
