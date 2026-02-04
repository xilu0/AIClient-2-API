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
	Metadata   map[string]string // Metadata for logging (not sent to API)
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
		"original_model", req.Metadata["original_model"],
		"kiro_model", req.Metadata["kiro_model"],
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

		// Extract model info from request metadata
		originalModel := "unknown"
		kiroModel := "unknown"
		if req.Metadata != nil {
			if orig, ok := req.Metadata["original_model"]; ok {
				originalModel = orig
			}
			if kiro, ok := req.Metadata["kiro_model"]; ok {
				kiroModel = kiro
			}
		}

		// Extract summary info from request body for logging
		var reqBody map[string]interface{}
		modelIdInRequest := "unknown"
		messageCount := 0
		hasTools := false
		hasToolResults := false
		hasImages := false

		if err := json.Unmarshal(req.Body, &reqBody); err == nil {
			if convState, ok := reqBody["conversationState"].(map[string]interface{}); ok {
				if history, ok := convState["history"].([]interface{}); ok {
					messageCount = len(history)
				}
				if currentMsg, ok := convState["currentMessage"].(map[string]interface{}); ok {
					if userInput, ok := currentMsg["userInputMessage"].(map[string]interface{}); ok {
						if modelId, ok := userInput["modelId"].(string); ok {
							modelIdInRequest = modelId
						}
						if images, ok := userInput["images"].([]interface{}); ok && len(images) > 0 {
							hasImages = true
						}
						if ctx, ok := userInput["userInputMessageContext"].(map[string]interface{}); ok {
							if tools, ok := ctx["tools"].([]interface{}); ok && len(tools) > 0 {
								hasTools = true
							}
							if results, ok := ctx["toolResults"].([]interface{}); ok && len(results) > 0 {
								hasToolResults = true
							}
						}
					}
				}
			}
		}

		// Log error with summary info (full request/response dumped to file by handler)
		c.logger.Error("Kiro API error",
			"status", resp.StatusCode,
			"profile_arn", req.ProfileARN,
			"original_model", originalModel,
			"kiro_model", kiroModel,
			"model_id_in_request", modelIdInRequest,
			"history_count", messageCount,
			"has_tools", hasTools,
			"has_tool_results", hasToolResults,
			"has_images", hasImages,
			"response_body", string(body),
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

// IsRateLimited returns true if this is a rate limit error (429 or 529).
// 529 is a Kiro-specific overloaded error code.
func (e *APIError) IsRateLimited() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode == 529
}

// IsOverloaded returns true if this is an overloaded error (529).
// This is a Kiro-specific error code indicating the service is overloaded.
func (e *APIError) IsOverloaded() bool {
	return e.StatusCode == 529
}

// IsForbidden returns true if this is an authorization error (403).
func (e *APIError) IsForbidden() bool {
	return e.StatusCode == http.StatusForbidden
}

// IsPaymentRequired returns true if this is a payment required error (402).
// This typically indicates the account has reached its usage limit.
func (e *APIError) IsPaymentRequired() bool {
	return e.StatusCode == http.StatusPaymentRequired
}

// IsBadRequest returns true if this is a bad request error (400).
// This typically indicates the request format is invalid or the model is not supported.
func (e *APIError) IsBadRequest() bool {
	return e.StatusCode == http.StatusBadRequest
}

// IsContextTooLong returns true if the error indicates the input context is too long.
// This checks for Kiro-specific error messages that indicate context length limits.
func (e *APIError) IsContextTooLong() bool {
	if e.StatusCode != http.StatusBadRequest {
		return false
	}
	bodyStr := string(e.Body)
	// Check for known Kiro context length error messages
	return strings.Contains(bodyStr, "Input is too long") ||
		strings.Contains(bodyStr, "CONTENT_LENGTH_EXCEEDS_THRESHOLD")
}

// IsImproperlyFormedRequest returns true if the error is "Improperly formed request".
// This typically indicates missing tools definition when history contains toolUses.
func (e *APIError) IsImproperlyFormedRequest() bool {
	if e.StatusCode != http.StatusBadRequest {
		return false
	}
	return strings.Contains(string(e.Body), "Improperly formed request")
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
// Returns the request body and metadata for logging.
func BuildRequestBody(model string, messages []byte, maxTokens int, stream bool, system string, profileARN string, tools []byte) ([]byte, map[string]string, error) {
	// Parse Claude messages with full content block support
	var claudeMessages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(messages, &claudeMessages); err != nil {
		return nil, nil, fmt.Errorf("failed to parse messages: %w", err)
	}

	if len(claudeMessages) == 0 {
		return nil, nil, fmt.Errorf("no messages found")
	}

	// Map model name to Kiro model ID
	kiroModel, originalModel := mapModelToKiroWithOriginal(model)

	// Get tools that don't require parameters (can have empty input)
	// This is used to filter invalid toolUses from history
	toolsNoRequiredParams := getToolsWithNoRequiredParams(tools)

	// Collect all toolUseIds that were removed due to empty input.
	// These will be used to also remove corresponding toolResults to avoid orphan references.
	allRemovedToolUseIds := make(map[string]bool)

	// Step 1: Parse and merge adjacent messages with same role (matching JS implementation)
	type MergedMessage struct {
		Role             string
		UserContent      ParsedUserContent
		AssistantContent ParsedAssistantContent
	}
	var mergedMessages []MergedMessage

	// Track all tool names from toolUses in history (for auto-generating tools definition)
	toolNamesFromHistory := make(map[string]bool)

	for _, msg := range claudeMessages {
		var newMsg MergedMessage
		if msg.Role == "user" {
			newMsg.Role = "user"
			newMsg.UserContent = parseMessageContent(msg.Content)
		} else {
			newMsg.Role = "assistant"
			newMsg.AssistantContent = parseAssistantContent(msg.Content)
			// Collect tool names from toolUses
			for _, toolUse := range newMsg.AssistantContent.ToolUses {
				if name, ok := toolUse["name"].(string); ok && name != "" {
					toolNamesFromHistory[name] = true
				}
			}
		}

		if len(mergedMessages) > 0 {
			lastMsg := &mergedMessages[len(mergedMessages)-1]
			if lastMsg.Role == newMsg.Role {
				// Merge with previous message
				if newMsg.Role == "user" {
					if lastMsg.UserContent.Text != "" && newMsg.UserContent.Text != "" {
						lastMsg.UserContent.Text += "\n" + newMsg.UserContent.Text
					} else {
						lastMsg.UserContent.Text += newMsg.UserContent.Text
					}
					lastMsg.UserContent.ToolResults = append(lastMsg.UserContent.ToolResults, newMsg.UserContent.ToolResults...)
					lastMsg.UserContent.Images = append(lastMsg.UserContent.Images, newMsg.UserContent.Images...)
				} else {
					if lastMsg.AssistantContent.Text != "" && newMsg.AssistantContent.Text != "" {
						lastMsg.AssistantContent.Text += "\n" + newMsg.AssistantContent.Text
					} else {
						lastMsg.AssistantContent.Text += newMsg.AssistantContent.Text
					}
					lastMsg.AssistantContent.ToolUses = append(lastMsg.AssistantContent.ToolUses, newMsg.AssistantContent.ToolUses...)
				}
				continue
			}
		}
		mergedMessages = append(mergedMessages, newMsg)
	}

	// Step 1.5: Collect all toolResultIds from ALL user messages
	// This is used to filter orphan toolUses (toolUses without matching toolResults)
	allToolResultIds := make(map[string]bool)
	for _, msg := range mergedMessages {
		if msg.Role == "user" {
			for _, tr := range msg.UserContent.ToolResults {
				if id, ok := tr["toolUseId"].(string); ok && id != "" {
					allToolResultIds[id] = true
				}
			}
		}
	}

	// Step 2: Build history and current message
	// Match Node.js behavior: if there's a system prompt and first message is user,
	// add system+user to history first, then process remaining messages
	var history []map[string]interface{}
	startIndex := 0

	if system != "" {
		if len(mergedMessages) == 1 && mergedMessages[0].Role == "user" {
			// Single user message: prepend system to the message content directly.
			// This avoids creating a history entry that duplicates the user content,
			// which can cause "Input is too long" errors when payloads are large.
			// The combined content will end up in currentMessage only.
			userText := mergedMessages[0].UserContent.Text
			if userText == "" {
				userText = "Continue"
			}
			mergedMessages[0].UserContent.Text = system + "\n\n" + userText
		} else if len(mergedMessages) > 0 && mergedMessages[0].Role == "user" {
			// Multiple messages: system+first_user → history
			firstUserContent := mergedMessages[0].UserContent.Text
			if firstUserContent == "" {
				firstUserContent = "Continue"
			}
			combinedContent := system + "\n\n" + firstUserContent
			history = append(history, map[string]interface{}{
				"userInputMessage": map[string]interface{}{
					"content": combinedContent,
					"modelId": kiroModel,
					"origin":  "AI_EDITOR",
				},
			})
			startIndex = 1 // Start processing from the second message
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

	// Process messages from startIndex to second-to-last as history
	for i := startIndex; i < len(mergedMessages)-1; i++ {
		msg := mergedMessages[i]
		if msg.Role == "user" {
			content := msg.UserContent.Text
			// Kiro API requires content to never be empty, even in history
			if content == "" {
				if len(msg.UserContent.ToolResults) > 0 {
					content = "Tool results provided."
				} else if len(msg.UserContent.Images) > 0 {
					content = "Image provided."
				} else {
					content = "Continue"
				}
			}

			userMsg := map[string]interface{}{
				"content": content,
				"modelId": kiroModel,
				"origin":  "AI_EDITOR",
			}
			if len(msg.UserContent.ToolResults) > 0 {
				uniqueToolResults := deduplicateToolResults(msg.UserContent.ToolResults)
				userMsg["userInputMessageContext"] = map[string]interface{}{
					"toolResults": uniqueToolResults,
				}
			}
			if len(msg.UserContent.Images) > 0 {
				userMsg["images"] = msg.UserContent.Images
			}
			history = append(history, map[string]interface{}{
				"userInputMessage": userMsg,
			})
		} else {
			content := msg.AssistantContent.Text
			if content == "" {
				content = "Continue"
			}
			assistantMsg := map[string]interface{}{
				"content": content,
			}
			// Filter toolUses:
			// 1. Remove empty input for tools that require parameters
			// 2. Remove orphan toolUses (no matching toolResult)
			filteredToolUses, removedIds := filterToolUsesWithEmptyInput(msg.AssistantContent.ToolUses, toolsNoRequiredParams)
			for id := range removedIds {
				allRemovedToolUseIds[id] = true
			}
			filteredToolUses, orphanRemovedIds := filterOrphanToolUses(filteredToolUses, allToolResultIds)
			for id := range orphanRemovedIds {
				allRemovedToolUseIds[id] = true
			}
			if len(filteredToolUses) > 0 {
				assistantMsg["toolUses"] = filteredToolUses
			}
			history = append(history, map[string]interface{}{
				"assistantResponseMessage": assistantMsg,
			})
		}
	}

	// Handle last message (CurrentMessage)
	lastMsg := mergedMessages[len(mergedMessages)-1]

	var currentContent string
	var currentToolResults []map[string]interface{}
	var currentImages []map[string]interface{}

	if lastMsg.Role == "assistant" {
		// Move to history
		assistantMsg := map[string]interface{}{
			"content": lastMsg.AssistantContent.Text,
		}
		if assistantMsg["content"] == "" {
			assistantMsg["content"] = "Continue" // Fallback content
		}
		// Filter toolUses:
		// 1. Remove empty input for tools that require parameters
		// 2. Remove orphan toolUses (no matching toolResult)
		// Note: For the last assistant message, toolResults may be in currentMessage,
		// but those would already be included in allToolResultIds from mergedMessages
		filteredToolUses, removedIds := filterToolUsesWithEmptyInput(lastMsg.AssistantContent.ToolUses, toolsNoRequiredParams)
		for id := range removedIds {
			allRemovedToolUseIds[id] = true
		}
		filteredToolUses, orphanRemovedIds := filterOrphanToolUses(filteredToolUses, allToolResultIds)
		for id := range orphanRemovedIds {
			allRemovedToolUseIds[id] = true
		}
		if len(filteredToolUses) > 0 {
			assistantMsg["toolUses"] = filteredToolUses
		}
		history = append(history, map[string]interface{}{
			"assistantResponseMessage": assistantMsg,
		})
		currentContent = "Continue"
	} else {
		// User message is current
		currentContent = lastMsg.UserContent.Text
		currentToolResults = lastMsg.UserContent.ToolResults
		currentImages = lastMsg.UserContent.Images

		// Ensure history ends with assistant
		if len(history) > 0 {
			lastHistoryItem := history[len(history)-1]
			if _, hasUser := lastHistoryItem["userInputMessage"]; hasUser {
				history = append(history, map[string]interface{}{
					"assistantResponseMessage": map[string]interface{}{
						"content": "Continue",
					},
				})
			}
		}
	}

	// Post-process: filter out toolResults that reference removed toolUses
	// This must happen after all toolUses have been processed to collect all removedIds
	if len(allRemovedToolUseIds) > 0 {
		// Filter toolResults in history
		for i, historyItem := range history {
			if uim, ok := historyItem["userInputMessage"].(map[string]interface{}); ok {
				if ctx, ok := uim["userInputMessageContext"].(map[string]interface{}); ok {
					if trs, ok := ctx["toolResults"].([]map[string]interface{}); ok {
						filtered := filterToolResults(trs, allRemovedToolUseIds)
						if len(filtered) == 0 {
							delete(ctx, "toolResults")
							if len(ctx) == 0 {
								delete(uim, "userInputMessageContext")
							}
						} else {
							ctx["toolResults"] = filtered
						}
						history[i]["userInputMessage"] = uim
					}
				}
			}
		}
		// Filter currentToolResults
		currentToolResults = filterToolResults(currentToolResults, allRemovedToolUseIds)
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

	// Prepare metadata for logging (not sent to API)
	metadata := map[string]string{
		"original_model": originalModel,
		"kiro_model":     kiroModel,
	}

	body, err := MarshalWithoutHTMLEscape(request)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	return body, metadata, nil
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
			// Note: JS implementation always uses "success" status, ignoring is_error flag
			// Kiro API may not accept "error" status, so we match JS behavior
			toolResult := map[string]interface{}{
				"content":   []map[string]interface{}{{"text": extractToolResultContent(block.Content)}},
				"status":    "success",
				"toolUseId": block.ToolUseID,
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
// Also handles fragmented tool_use blocks (from streaming) by aggregating raw_arguments.
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

	// Collect tool_use blocks, grouping by ID to handle fragmented streaming data
	// Claude Code may send history with fragmented tool_use entries where each has
	// input.raw_arguments containing a JSON fragment
	type toolUseFragment struct {
		Name         string
		RawArguments []string // Ordered fragments to concatenate
		CompleteInput interface{} // Complete input if not fragmented
	}
	toolUseMap := make(map[string]*toolUseFragment)
	toolUseOrder := []string{} // Preserve order of first occurrence

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

			// Check if this is a fragmented tool_use (has raw_arguments)
			inputMap, isMap := input.(map[string]interface{})
			rawArg, hasRawArg := "", false
			if isMap {
				if ra, ok := inputMap["raw_arguments"].(string); ok {
					rawArg = ra
					hasRawArg = true
				}
			}

			if existing, ok := toolUseMap[block.ID]; ok {
				// Append fragment to existing entry
				if hasRawArg {
					existing.RawArguments = append(existing.RawArguments, rawArg)
				}
			} else {
				// New tool_use entry
				fragment := &toolUseFragment{
					Name: block.Name,
				}
				if hasRawArg {
					fragment.RawArguments = []string{rawArg}
				} else if isMap && len(inputMap) == 0 {
					// Empty input {} - might be followed by fragments, or might be valid
					fragment.RawArguments = []string{}
				} else {
					// Complete input (not fragmented)
					fragment.CompleteInput = input
				}
				toolUseMap[block.ID] = fragment
				toolUseOrder = append(toolUseOrder, block.ID)
			}
		}
	}

	// Build final toolUses from aggregated fragments
	for _, id := range toolUseOrder {
		fragment := toolUseMap[id]
		var input interface{}

		if fragment.CompleteInput != nil {
			// Not fragmented, use as-is
			input = fragment.CompleteInput
		} else if len(fragment.RawArguments) > 0 {
			// Aggregate raw_arguments fragments and parse as JSON
			merged := strings.Join(fragment.RawArguments, "")
			var parsedInput interface{}
			if err := json.Unmarshal([]byte(merged), &parsedInput); err == nil {
				input = parsedInput
			} else {
				// Failed to parse merged JSON, use empty object
				// This shouldn't happen with valid data, but be defensive
				input = map[string]interface{}{}
			}
		} else {
			// No fragments and no complete input, use empty object
			input = map[string]interface{}{}
		}

		toolUse := map[string]interface{}{
			"toolUseId": id,
			"name":      fragment.Name,
			"input":     input,
		}
		result.ToolUses = append(result.ToolUses, toolUse)
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


// generateToolsFromHistory generates minimal tool definitions from tool names found in history.
// This is used when the request doesn't include tools but history contains toolUses.
// Kiro API requires tools definition when history contains toolUses.
func generateToolsFromHistory(toolNames map[string]bool) []map[string]interface{} {
	if len(toolNames) == 0 {
		return nil
	}

	var kiroTools []map[string]interface{}
	for name := range toolNames {
		// Generate minimal tool definition with empty schema
		// The actual schema doesn't matter for history replay, only the name is required
		kiroTool := map[string]interface{}{
			"toolSpecification": map[string]interface{}{
				"name":        name,
				"description": "Tool used in conversation history",
				"inputSchema": map[string]interface{}{
					"json": json.RawMessage(`{"type":"object"}`),
				},
			},
		}
		kiroTools = append(kiroTools, kiroTool)
	}
	return kiroTools
}

// InjectToolsFromHistory extracts tool names from history in the request body
// and injects minimal tools definitions. This is used to fix "Improperly formed request"
// errors when history contains toolUses but no tools definition was provided.
// Returns the modified request body, or the original if no modification needed.
func InjectToolsFromHistory(reqBody []byte) ([]byte, bool) {
	var request map[string]interface{}
	if err := json.Unmarshal(reqBody, &request); err != nil {
		return reqBody, false
	}

	convState, ok := request["conversationState"].(map[string]interface{})
	if !ok {
		return reqBody, false
	}

	// Check if tools already exist in currentMessage
	if currentMsg, ok := convState["currentMessage"].(map[string]interface{}); ok {
		if userInput, ok := currentMsg["userInputMessage"].(map[string]interface{}); ok {
			if ctx, ok := userInput["userInputMessageContext"].(map[string]interface{}); ok {
				if _, hasTools := ctx["tools"]; hasTools {
					// Tools already present, no need to inject
					return reqBody, false
				}
			}
		}
	}

	// Extract tool names from history
	history, ok := convState["history"].([]interface{})
	if !ok || len(history) == 0 {
		return reqBody, false
	}

	toolNames := make(map[string]bool)
	for _, item := range history {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		assistantMsg, ok := itemMap["assistantResponseMessage"].(map[string]interface{})
		if !ok {
			continue
		}
		toolUses, ok := assistantMsg["toolUses"].([]interface{})
		if !ok {
			continue
		}
		for _, tu := range toolUses {
			tuMap, ok := tu.(map[string]interface{})
			if !ok {
				continue
			}
			if name, ok := tuMap["name"].(string); ok && name != "" {
				toolNames[name] = true
			}
		}
	}

	if len(toolNames) == 0 {
		return reqBody, false
	}

	// Generate and inject tools
	kiroTools := generateToolsFromHistory(toolNames)
	if len(kiroTools) == 0 {
		return reqBody, false
	}

	// Inject tools into userInputMessageContext
	currentMsg, ok := convState["currentMessage"].(map[string]interface{})
	if !ok {
		currentMsg = make(map[string]interface{})
		convState["currentMessage"] = currentMsg
	}
	userInput, ok := currentMsg["userInputMessage"].(map[string]interface{})
	if !ok {
		userInput = make(map[string]interface{})
		currentMsg["userInputMessage"] = userInput
	}
	ctx, ok := userInput["userInputMessageContext"].(map[string]interface{})
	if !ok {
		ctx = make(map[string]interface{})
		userInput["userInputMessageContext"] = ctx
	}
	ctx["tools"] = kiroTools

	// Marshal back to JSON without HTML escaping (Kiro API rejects \u003c etc.)
	modifiedBody, err := MarshalWithoutHTMLEscape(request)
	if err != nil {
		return reqBody, false
	}

	return modifiedBody, true
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
// Haiku/Opus use standard Claude API format, Sonnet uses Kiro-specific uppercase format.
func mapModelToKiro(model string) string {
	kiroModel, _ := mapModelToKiroWithOriginal(model)
	return kiroModel
}

// mapModelToKiroWithOriginal maps Claude model names to Kiro model IDs and returns both.
// Returns (kiroModelID, originalModelName).
func mapModelToKiroWithOriginal(model string) (string, string) {
	modelMapping := map[string]string{
		// Opus models - Kiro-specific uppercase format (same as Sonnet)
		"claude-opus-4-5":          "CLAUDE_OPUS_4_5_20251101_V1_0",
		"claude-opus-4.5":          "CLAUDE_OPUS_4_5_20251101_V1_0",
		"claude-opus-4-5-20251101": "CLAUDE_OPUS_4_5_20251101_V1_0",
		// Haiku models - Kiro-specific uppercase format
		"claude-haiku-4-5":          "CLAUDE_HAIKU_4_5_20251001_V1_0",
		"claude-haiku-4.5":          "CLAUDE_HAIKU_4_5_20251001_V1_0",
		"claude-haiku-4-5-20251001": "CLAUDE_HAIKU_4_5_20251001_V1_0",
		// Sonnet models - Kiro-specific uppercase format
		"claude-sonnet-4-5":          "CLAUDE_SONNET_4_5_20250929_V1_0",
		"claude-sonnet-4.5":          "CLAUDE_SONNET_4_5_20250929_V1_0",
		"claude-sonnet-4-5-20250929": "CLAUDE_SONNET_4_5_20250929_V1_0",
		"claude-sonnet-4":            "CLAUDE_SONNET_4_20250514_V1_0",
		"claude-sonnet-4-20250514":   "CLAUDE_SONNET_4_20250514_V1_0",
		"claude-3-7-sonnet-20250219": "CLAUDE_3_7_SONNET_20250219_V1_0",
		// Auto defaults to sonnet 4.5
		"auto": "CLAUDE_SONNET_4_5_20250929_V1_0",
	}

	if kiroModel, ok := modelMapping[model]; ok {
		return kiroModel, model
	}
	// Default to sonnet 4.5 if unknown (use uppercase format)
	return "CLAUDE_SONNET_4_5_20250929_V1_0", model
}


// getToolsWithNoRequiredParams returns a set of tool names that have no required parameters.
// Tools in this set can have empty input {} without causing "Improperly formed request" errors.
func getToolsWithNoRequiredParams(toolsJSON []byte) map[string]bool {
	result := make(map[string]bool)
	if len(toolsJSON) == 0 {
		return result
	}

	var claudeTools []ClaudeTool
	if err := json.Unmarshal(toolsJSON, &claudeTools); err != nil {
		return result
	}

	for _, tool := range claudeTools {
		// Parse input_schema to check for required fields
		var schema struct {
			Required []string `json:"required"`
		}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			continue
		}

		// If no required fields, this tool can have empty input
		if len(schema.Required) == 0 {
			result[tool.Name] = true
		}
	}

	return result
}

// filterToolUsesWithEmptyInput filters out toolUses with empty input for tools that require parameters.
// Tools without required parameters (like ExitPlanMode, EnterPlanMode) are kept even with empty input.
// Returns the filtered toolUses and the set of removed toolUseIds.
// The removed toolUseIds should be used to filter out corresponding toolResults to avoid orphan references.
func filterToolUsesWithEmptyInput(toolUses []map[string]interface{}, noRequiredParams map[string]bool) ([]map[string]interface{}, map[string]bool) {
	removedIds := make(map[string]bool)
	if len(toolUses) == 0 {
		return toolUses, removedIds
	}

	var filtered []map[string]interface{}
	for _, tu := range toolUses {
		name, _ := tu["name"].(string)
		toolUseId, _ := tu["toolUseId"].(string)
		input := tu["input"]

		// Check if input is empty
		isEmptyInput := false
		if input == nil {
			isEmptyInput = true
		} else if inputMap, ok := input.(map[string]interface{}); ok && len(inputMap) == 0 {
			isEmptyInput = true
		}

		// Keep if: input is not empty, OR tool has no required params
		// NOTE: We no longer keep toolUses just because they're referenced by toolResults.
		// Instead, we remove both the invalid toolUse AND the corresponding toolResult.
		if !isEmptyInput || noRequiredParams[name] {
			filtered = append(filtered, tu)
		} else {
			// Track removed toolUseId so we can also remove the corresponding toolResult
			if toolUseId != "" {
				removedIds[toolUseId] = true
			}
		}
	}

	return filtered, removedIds
}

// filterOrphanToolUses removes toolUses that don't have matching toolResults.
// This prevents "Improperly formed request" errors from orphan toolUse references.
// A toolUse is orphan if its toolUseId is NOT in the allToolResultIds set.
func filterOrphanToolUses(toolUses []map[string]interface{}, allToolResultIds map[string]bool) ([]map[string]interface{}, map[string]bool) {
	removedIds := make(map[string]bool)
	if len(toolUses) == 0 || len(allToolResultIds) == 0 {
		// If no toolResults exist at all, we can't determine orphans,
		// so we keep all toolUses (this handles edge cases)
		return toolUses, removedIds
	}

	var filtered []map[string]interface{}
	for _, tu := range toolUses {
		toolUseId, _ := tu["toolUseId"].(string)

		// Keep if there's a matching toolResult
		if allToolResultIds[toolUseId] {
			filtered = append(filtered, tu)
		} else {
			// Track removed orphan toolUseId
			if toolUseId != "" {
				removedIds[toolUseId] = true
			}
		}
	}

	return filtered, removedIds
}

// filterToolResults removes toolResults that reference removed toolUses.
// This prevents "Improperly formed request" errors from orphan toolResult references.
func filterToolResults(toolResults []map[string]interface{}, removedToolUseIds map[string]bool) []map[string]interface{} {
	if len(toolResults) == 0 || len(removedToolUseIds) == 0 {
		return toolResults
	}

	var filtered []map[string]interface{}
	for _, tr := range toolResults {
		toolUseId, _ := tr["toolUseId"].(string)
		if !removedToolUseIds[toolUseId] {
			filtered = append(filtered, tr)
		}
	}

	return filtered
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
		// Ensure inputSchema is a valid JSON object (not null, empty, or invalid)
		if len(inputSchema) == 0 || string(inputSchema) == "null" || string(inputSchema) == "" {
			inputSchema = []byte("{}")
		}

		// Sanitize schema: remove $-prefixed property names that Kiro API rejects
		inputSchema = sanitizeToolSchema(inputSchema)

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

// sanitizeToolSchema removes $-prefixed property names from JSON Schema properties.
// The Kiro API rejects tool schemas containing $-prefixed property names (e.g., $expand,
// $select, $skip, $top) as it treats them as reserved JSON Schema keywords.
// Only modifies the schema if $-prefixed properties are found; returns original bytes otherwise.
func sanitizeToolSchema(schemaJSON json.RawMessage) json.RawMessage {
	var schema map[string]interface{}
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		return schemaJSON
	}

	if !sanitizeSchemaProperties(schema) {
		return schemaJSON // No changes needed
	}

	result, err := MarshalWithoutHTMLEscape(schema)
	if err != nil {
		return schemaJSON
	}
	return result
}

// sanitizeSchemaProperties recursively removes $-prefixed keys from properties objects.
// Returns true if any modifications were made.
func sanitizeSchemaProperties(schema map[string]interface{}) bool {
	modified := false

	// Remove $-prefixed keys from properties
	if props, ok := schema["properties"].(map[string]interface{}); ok {
		for key := range props {
			if strings.HasPrefix(key, "$") {
				delete(props, key)
				modified = true
			}
		}

		// Clean up required array to remove references to deleted properties
		if modified {
			if required, ok := schema["required"].([]interface{}); ok {
				var cleaned []interface{}
				for _, r := range required {
					if s, ok := r.(string); ok && strings.HasPrefix(s, "$") {
						continue
					}
					cleaned = append(cleaned, r)
				}
				if len(cleaned) != len(required) {
					schema["required"] = cleaned
				}
			}
		}

		// Recurse into remaining property schemas
		for _, propSchema := range props {
			if nested, ok := propSchema.(map[string]interface{}); ok {
				if sanitizeSchemaProperties(nested) {
					modified = true
				}
			}
		}
	}

	// Recurse into nested schemas
	for _, key := range []string{"items", "additionalProperties"} {
		if nested, ok := schema[key].(map[string]interface{}); ok {
			if sanitizeSchemaProperties(nested) {
				modified = true
			}
		}
	}

	// Recurse into array schemas (anyOf, allOf, oneOf)
	for _, key := range []string{"anyOf", "allOf", "oneOf"} {
		if arr, ok := schema[key].([]interface{}); ok {
			for _, item := range arr {
				if nested, ok := item.(map[string]interface{}); ok {
					if sanitizeSchemaProperties(nested) {
						modified = true
					}
				}
			}
		}
	}

	return modified
}

// Close closes the client and releases resources.
func (c *Client) Close() {
	c.httpClient.CloseIdleConnections()
}

// MarshalWithoutHTMLEscape marshals v to JSON without HTML escaping.
// Go's json.Marshal escapes <, >, & by default; this function preserves them as-is.
func MarshalWithoutHTMLEscape(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// Encode appends a trailing newline; trim it
	b := buf.Bytes()
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	return b, nil
}
