// Package unit contains unit tests for the Kiro server.
package unit

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/AIClient-2-API/internal/kiro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRequestBody_SimpleMessage(t *testing.T) {
	messages := []map[string]interface{}{
		{
			"role":    "user",
			"content": "Hello, Claude!",
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "", nil)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})

	assert.Equal(t, "Hello, Claude!", userInput["content"])
	assert.Equal(t, "AI_EDITOR", userInput["origin"])
}

func TestBuildRequestBody_WithSystemPrompt(t *testing.T) {
	messages := []map[string]interface{}{
		{
			"role":    "user",
			"content": "What is 2+2?",
		},
		{
			"role":    "assistant",
			"content": "4",
		},
		{
			"role":    "user",
			"content": "And 3+3?",
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "You are a math tutor.", "", nil)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	history := convState["history"].([]interface{})
	require.GreaterOrEqual(t, len(history), 2)

	// First history item should have system prompt prepended
	historyItem := history[0].(map[string]interface{})
	userInput := historyItem["userInputMessage"].(map[string]interface{})
	assert.Contains(t, userInput["content"], "You are a math tutor.")
	assert.Contains(t, userInput["content"], "What is 2+2?")
}

func TestBuildRequestBody_SingleMessageWithSystemPrompt(t *testing.T) {
	// When there's only 1 user message + system prompt, system should be merged
	// into currentMessage without creating a history entry. This prevents payload
	// duplication that can cause "Input is too long" errors.
	messages := []map[string]interface{}{
		{
			"role":    "user",
			"content": "Hello, how are you?",
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "You are a helpful assistant.", "", nil)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})

	// History should be empty (no duplication)
	_, hasHistory := convState["history"]
	if hasHistory {
		history := convState["history"].([]interface{})
		assert.Empty(t, history, "history should be empty for single message + system prompt")
	}

	// currentMessage should contain both system prompt and user content
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})
	content := userInput["content"].(string)
	assert.Contains(t, content, "You are a helpful assistant.")
	assert.Contains(t, content, "Hello, how are you?")
	assert.Equal(t, "You are a helpful assistant.\n\nHello, how are you?", content)
}

func TestBuildRequestBody_SingleMessageWithSystemPromptNoPayloadDuplication(t *testing.T) {
	// Verify that a large single message + system doesn't get duplicated.
	// Previously, system+user went to history AND user went to currentMessage,
	// roughly doubling the payload size.
	largeContent := strings.Repeat("a", 10000) // 10KB of printable content
	messages := []map[string]interface{}{
		{
			"role":    "user",
			"content": largeContent,
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	systemPrompt := "You are a helpful assistant."
	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, systemPrompt, "", nil)
	require.NoError(t, err)

	// Count occurrences of the large content in the body.
	// It should appear exactly once (in currentMessage), not twice.
	bodyStr := string(body)
	occurrences := strings.Count(bodyStr, largeContent)
	assert.Equal(t, 1, occurrences,
		"user content should appear exactly once in the request body (no duplication)")
}

func TestBuildRequestBody_WithToolResult(t *testing.T) {
	messages := []map[string]interface{}{
		{
			"role":    "user",
			"content": "Write a file",
		},
		{
			"role": "assistant",
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": "I'll write that file for you.",
				},
				{
					"type":  "tool_use",
					"id":    "toolu_01XYZ123",
					"name":  "Write",
					"input": map[string]interface{}{"file_path": "/tmp/test.txt", "content": "Hello"},
				},
			},
		},
		{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type":        "tool_result",
					"tool_use_id": "toolu_01XYZ123",
					"content":     "File written successfully.",
				},
			},
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "profile-arn-123", nil)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})

	// Should have userInputMessageContext with toolResults
	context, ok := userInput["userInputMessageContext"].(map[string]interface{})
	require.True(t, ok, "expected userInputMessageContext")

	toolResults, ok := context["toolResults"].([]interface{})
	require.True(t, ok, "expected toolResults array")
	require.Len(t, toolResults, 1)

	toolResult := toolResults[0].(map[string]interface{})
	assert.Equal(t, "toolu_01XYZ123", toolResult["toolUseId"])
	assert.Equal(t, "success", toolResult["status"])

	// Verify profileArn is included
	assert.Equal(t, "profile-arn-123", req["profileArn"])
}

func TestBuildRequestBody_WithToolUseInAssistant(t *testing.T) {
	messages := []map[string]interface{}{
		{
			"role":    "user",
			"content": "Read the config file",
		},
		{
			"role": "assistant",
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": "I'll read that file.",
				},
				{
					"type":  "tool_use",
					"id":    "toolu_read_123",
					"name":  "Read",
					"input": map[string]interface{}{"file_path": "/etc/config.json"},
				},
			},
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "", nil)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	history := convState["history"].([]interface{})

	// Assistant message should be in history with toolUses
	// Since last message is assistant, it's moved to history
	require.GreaterOrEqual(t, len(history), 2)

	// Find the assistant message in history
	var assistantMsg map[string]interface{}
	for _, h := range history {
		item := h.(map[string]interface{})
		if msg, ok := item["assistantResponseMessage"].(map[string]interface{}); ok {
			if toolUses, ok := msg["toolUses"].([]interface{}); ok && len(toolUses) > 0 {
				assistantMsg = msg
				break
			}
		}
	}
	require.NotNil(t, assistantMsg, "expected assistant message with toolUses")

	toolUses := assistantMsg["toolUses"].([]interface{})
	require.Len(t, toolUses, 1)

	toolUse := toolUses[0].(map[string]interface{})
	assert.Equal(t, "toolu_read_123", toolUse["toolUseId"])
	assert.Equal(t, "Read", toolUse["name"])
}

func TestBuildRequestBody_WithImage(t *testing.T) {
	messages := []map[string]interface{}{
		{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": "What's in this image?",
				},
				{
					"type": "image",
					"source": map[string]interface{}{
						"type":       "base64",
						"media_type": "image/png",
						"data":       "iVBORw0KGgoAAAANSUhEUg...",
					},
				},
			},
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "", nil)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})

	// Should have images array
	images, ok := userInput["images"].([]interface{})
	require.True(t, ok, "expected images array")
	require.Len(t, images, 1)

	image := images[0].(map[string]interface{})
	assert.Equal(t, "png", image["format"])

	source := image["source"].(map[string]interface{})
	assert.Equal(t, "iVBORw0KGgoAAAANSUhEUg...", source["bytes"])
}

func TestBuildRequestBody_EmptyContentWithToolResult(t *testing.T) {
	// When user sends only tool_result without text content
	messages := []map[string]interface{}{
		{
			"role":    "user",
			"content": "Do something",
		},
		{
			"role": "assistant",
			"content": []map[string]interface{}{
				{
					"type":  "tool_use",
					"id":    "toolu_abc",
					"name":  "Bash",
					"input": map[string]interface{}{"command": "ls -la"},
				},
			},
		},
		{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type":        "tool_result",
					"tool_use_id": "toolu_abc",
					"content":     "total 0\ndrwxr-xr-x  2 user user 40 Jan 28 00:00 .",
				},
			},
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "", nil)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})

	// Content should be "Tool results provided." when no text but has tool results
	assert.Equal(t, "Tool results provided.", userInput["content"])

	// Should still have tool results
	context := userInput["userInputMessageContext"].(map[string]interface{})
	toolResults := context["toolResults"].([]interface{})
	require.Len(t, toolResults, 1)
}

func TestBuildRequestBody_DuplicateToolResultIds(t *testing.T) {
	// Test deduplication of tool results
	messages := []map[string]interface{}{
		{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type":        "tool_result",
					"tool_use_id": "toolu_dup",
					"content":     "Result 1",
				},
				{
					"type":        "tool_result",
					"tool_use_id": "toolu_dup", // Duplicate ID
					"content":     "Result 2",
				},
				{
					"type":        "tool_result",
					"tool_use_id": "toolu_other",
					"content":     "Result 3",
				},
			},
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "", nil)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})

	context := userInput["userInputMessageContext"].(map[string]interface{})
	toolResults := context["toolResults"].([]interface{})

	// Should only have 2 unique tool results (deduplicated)
	assert.Len(t, toolResults, 2)

	// First one should be toolu_dup (first occurrence)
	tr1 := toolResults[0].(map[string]interface{})
	assert.Equal(t, "toolu_dup", tr1["toolUseId"])

	// Second should be toolu_other
	tr2 := toolResults[1].(map[string]interface{})
	assert.Equal(t, "toolu_other", tr2["toolUseId"])
}

func TestBuildRequestBody_ThinkingContent(t *testing.T) {
	messages := []map[string]interface{}{
		{
			"role":    "user",
			"content": "What is the meaning of life?",
		},
		{
			"role": "assistant",
			"content": []map[string]interface{}{
				{
					"type":     "thinking",
					"thinking": "Let me think about this philosophical question...",
				},
				{
					"type": "text",
					"text": "The meaning of life is subjective.",
				},
			},
		},
		{
			"role":    "user",
			"content": "Tell me more.",
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "", nil)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	history := convState["history"].([]interface{})

	// Find assistant message in history
	var assistantContent string
	for _, h := range history {
		item := h.(map[string]interface{})
		if msg, ok := item["assistantResponseMessage"].(map[string]interface{}); ok {
			content, ok := msg["content"].(string)
			if ok && content != "Continue" {
				assistantContent = content
				break
			}
		}
	}

	// Should have thinking wrapped in tags
	assert.Contains(t, assistantContent, "<kiro_thinking>")
	assert.Contains(t, assistantContent, "</kiro_thinking>")
	assert.Contains(t, assistantContent, "Let me think about this philosophical question...")
	assert.Contains(t, assistantContent, "The meaning of life is subjective.")
}

func TestBuildRequestBody_HistoryEndsWithAssistant(t *testing.T) {
	// When history ends with userInputMessage, should add empty assistantResponseMessage
	// Note: Adjacent messages are now merged, so we need User->Assistant->User to create history
	messages := []map[string]interface{}{
		{
			"role":    "user",
			"content": "Hello",
		},
		{
			"role":    "assistant",
			"content": "Hi there",
		},
		{
			"role":    "user",
			"content": "How are you?",
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "", nil)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	history := convState["history"].([]interface{})

	// History should be [User, Assistant]
	require.Len(t, history, 2)
	
	// Verify last history item is assistant
	lastHistory := history[len(history)-1].(map[string]interface{})
	_, hasAssistant := lastHistory["assistantResponseMessage"]
	assert.True(t, hasAssistant, "history should end with assistantResponseMessage")
}

func TestBuildRequestBody_ModelMapping(t *testing.T) {
	tests := []struct {
		inputModel    string
		expectedModel string
	}{
		// Sonnet models - Kiro-specific uppercase format
		{"claude-sonnet-4-5-20250929", "CLAUDE_SONNET_4_5_20250929_V1_0"},
		{"claude-sonnet-4-5", "CLAUDE_SONNET_4_5_20250929_V1_0"},
		{"claude-sonnet-4", "CLAUDE_SONNET_4_20250514_V1_0"},
		{"claude-sonnet-4-20250514", "CLAUDE_SONNET_4_20250514_V1_0"},
		{"claude-3-7-sonnet-20250219", "CLAUDE_3_7_SONNET_20250219_V1_0"},
		// Haiku models - Kiro-specific uppercase format
		{"claude-haiku-4-5", "CLAUDE_HAIKU_4_5_20251001_V1_0"},
		{"claude-haiku-4.5", "CLAUDE_HAIKU_4_5_20251001_V1_0"},
		{"claude-haiku-4-5-20251001", "CLAUDE_HAIKU_4_5_20251001_V1_0"},
		// Opus models - Kiro-specific uppercase format
		{"claude-opus-4-5", "CLAUDE_OPUS_4_5_20251101_V1_0"},
		{"claude-opus-4.5", "CLAUDE_OPUS_4_5_20251101_V1_0"},
		{"claude-opus-4-5-20251101", "CLAUDE_OPUS_4_5_20251101_V1_0"},
		// Auto and unknown
		{"auto", "CLAUDE_SONNET_4_5_20250929_V1_0"},
		{"unknown-model", "CLAUDE_SONNET_4_5_20250929_V1_0"}, // Default
	}

	for _, tt := range tests {
		t.Run(tt.inputModel, func(t *testing.T) {
			messages := []map[string]interface{}{
				{"role": "user", "content": "Hi"},
			}
			messagesJSON, _ := json.Marshal(messages)

			body, _, err := kiro.BuildRequestBody(tt.inputModel, messagesJSON, 1000, true, "", "", nil)
			require.NoError(t, err)

			var req map[string]interface{}
			json.Unmarshal(body, &req)

			convState := req["conversationState"].(map[string]interface{})
			currentMsg := convState["currentMessage"].(map[string]interface{})
			userInput := currentMsg["userInputMessage"].(map[string]interface{})

			assert.Equal(t, tt.expectedModel, userInput["modelId"])
		})
	}
}

func TestBuildRequestBody_WithTools(t *testing.T) {
	messages := []map[string]interface{}{
		{
			"role":    "user",
			"content": "Write a file",
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	// Define tools (like Claude Code's Write, Read, Bash tools)
	tools := []map[string]interface{}{
		{
			"name":        "Write",
			"description": "Write content to a file",
			"input_schema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{"type": "string"},
					"content":   map[string]interface{}{"type": "string"},
				},
				"required": []string{"file_path", "content"},
			},
		},
		{
			"name":        "Read",
			"description": "Read a file",
			"input_schema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{"type": "string"},
				},
				"required": []string{"file_path"},
			},
		},
	}
	toolsJSON, err := json.Marshal(tools)
	require.NoError(t, err)

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "", toolsJSON)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})

	// Should have userInputMessageContext with tools
	context, ok := userInput["userInputMessageContext"].(map[string]interface{})
	require.True(t, ok, "expected userInputMessageContext")

	kiroTools, ok := context["tools"].([]interface{})
	require.True(t, ok, "expected tools array")
	require.Len(t, kiroTools, 2)

	// Check first tool format (Kiro toolSpecification format)
	firstTool := kiroTools[0].(map[string]interface{})
	toolSpec, ok := firstTool["toolSpecification"].(map[string]interface{})
	require.True(t, ok, "expected toolSpecification")

	assert.Equal(t, "Write", toolSpec["name"])
	assert.Equal(t, "Write content to a file", toolSpec["description"])

	// Check inputSchema has json wrapper
	inputSchema, ok := toolSpec["inputSchema"].(map[string]interface{})
	require.True(t, ok, "expected inputSchema")
	_, hasJSON := inputSchema["json"]
	assert.True(t, hasJSON, "inputSchema should have 'json' field")
}

func TestBuildRequestBody_WithToolsFiltersWebSearch(t *testing.T) {
	messages := []map[string]interface{}{
		{
			"role":    "user",
			"content": "Search the web",
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	// Include web_search tool which should be filtered out
	tools := []map[string]interface{}{
		{
			"name":        "Read",
			"description": "Read a file",
			"input_schema": map[string]interface{}{"type": "object"},
		},
		{
			"name":        "web_search",
			"description": "Search the web",
			"input_schema": map[string]interface{}{"type": "object"},
		},
		{
			"name":        "WebSearch",
			"description": "Another web search",
			"input_schema": map[string]interface{}{"type": "object"},
		},
	}
	toolsJSON, err := json.Marshal(tools)
	require.NoError(t, err)

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "", toolsJSON)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})

	context := userInput["userInputMessageContext"].(map[string]interface{})
	kiroTools := context["tools"].([]interface{})

	// Should only have 1 tool (Read), web_search and WebSearch filtered out
	assert.Len(t, kiroTools, 1)

	firstTool := kiroTools[0].(map[string]interface{})
	toolSpec := firstTool["toolSpecification"].(map[string]interface{})
	assert.Equal(t, "Read", toolSpec["name"])
}

func TestBuildRequestBody_DuplicateToolResultIdsInHistory(t *testing.T) {
	// Test deduplication of tool results in history messages
	messages := []map[string]interface{}{
		{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type":        "tool_result",
					"tool_use_id": "toolu_hist_dup",
					"content":     "Result 1",
				},
				{
					"type":        "tool_result",
					"tool_use_id": "toolu_hist_dup", // Duplicate ID in history
					"content":     "Result 2",
				},
			},
		},
		{
			"role":    "assistant",
			"content": "Got it.",
		},
		{
			"role":    "user",
			"content": "Continue",
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "", nil)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	history := convState["history"].([]interface{})

	// Find user message with tool results in history
	var toolResults []interface{}
	for _, h := range history {
		item := h.(map[string]interface{})
		if userMsg, ok := item["userInputMessage"].(map[string]interface{}); ok {
			if ctx, ok := userMsg["userInputMessageContext"].(map[string]interface{}); ok {
				if tr, ok := ctx["toolResults"].([]interface{}); ok {
					toolResults = tr
					break
				}
			}
		}
	}

	// Should only have 1 unique tool result (deduplicated)
	require.NotNil(t, toolResults, "expected toolResults in history")
	assert.Len(t, toolResults, 1)
}

func TestBuildRequestBody_ToolResultStatusAlwaysSuccess(t *testing.T) {
	// Test that tool results always use "success" status, even with is_error flag
	messages := []map[string]interface{}{
		{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type":        "tool_result",
					"tool_use_id": "toolu_err",
					"content":     "Error: file not found",
					"is_error":    true, // This should be ignored
				},
			},
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "", nil)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})
	context := userInput["userInputMessageContext"].(map[string]interface{})
	toolResults := context["toolResults"].([]interface{})

	require.Len(t, toolResults, 1)
	tr := toolResults[0].(map[string]interface{})

	// Status should always be "success" (matching JS implementation)
	assert.Equal(t, "success", tr["status"])
}

func TestBuildRequestBody_SanitizesDollarPrefixedProperties(t *testing.T) {
	messages := []map[string]interface{}{
		{"role": "user", "content": "List permissions"},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	// Tool with $-prefixed property names (like MCP Excel tools with OData params)
	tools := []map[string]interface{}{
		{
			"name":        "list_permissions",
			"description": "List permissions",
			"input_schema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"$expand": map[string]interface{}{
						"type":    []string{"string", "null"},
						"default": nil,
					},
					"$select": map[string]interface{}{
						"type":    []string{"string", "null"},
						"default": nil,
					},
					"$skip": map[string]interface{}{
						"anyOf": []map[string]interface{}{
							{"type": "integer", "minimum": 0},
							{"type": "null"},
						},
						"default": nil,
					},
					"drive_id": map[string]interface{}{
						"type": "string",
					},
				},
				"required":            []string{"drive_id"},
				"additionalProperties": false,
				"$schema":             "http://json-schema.org/draft-07/schema#",
			},
		},
	}
	toolsJSON, err := json.Marshal(tools)
	require.NoError(t, err)

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "", toolsJSON)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})
	context := userInput["userInputMessageContext"].(map[string]interface{})
	kiroTools := context["tools"].([]interface{})
	require.Len(t, kiroTools, 1)

	toolSpec := kiroTools[0].(map[string]interface{})["toolSpecification"].(map[string]interface{})
	inputSchemaWrapper := toolSpec["inputSchema"].(map[string]interface{})

	// Parse the inner JSON schema
	schemaBytes, err := json.Marshal(inputSchemaWrapper["json"])
	require.NoError(t, err)
	var schema map[string]interface{}
	err = json.Unmarshal(schemaBytes, &schema)
	require.NoError(t, err)

	props := schema["properties"].(map[string]interface{})

	// $-prefixed properties should be removed
	_, hasExpand := props["$expand"]
	assert.False(t, hasExpand, "$expand should be removed from properties")
	_, hasSelect := props["$select"]
	assert.False(t, hasSelect, "$select should be removed from properties")
	_, hasSkip := props["$skip"]
	assert.False(t, hasSkip, "$skip should be removed from properties")

	// Regular properties should be preserved
	_, hasDriveID := props["drive_id"]
	assert.True(t, hasDriveID, "drive_id should be preserved")

	// $schema at top level should be preserved (it's a JSON Schema keyword, not a property name)
	_, hasSchema := schema["$schema"]
	assert.True(t, hasSchema, "$schema at top level should be preserved")
}

func TestBuildRequestBody_NoSanitizationWhenNoDollarProps(t *testing.T) {
	messages := []map[string]interface{}{
		{"role": "user", "content": "Write a file"},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	tools := []map[string]interface{}{
		{
			"name":        "Write",
			"description": "Write content to a file",
			"input_schema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{"type": "string"},
					"content":   map[string]interface{}{"type": "string"},
				},
				"required": []string{"file_path", "content"},
			},
		},
	}
	toolsJSON, err := json.Marshal(tools)
	require.NoError(t, err)

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "", toolsJSON)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})
	context := userInput["userInputMessageContext"].(map[string]interface{})
	kiroTools := context["tools"].([]interface{})
	require.Len(t, kiroTools, 1)

	toolSpec := kiroTools[0].(map[string]interface{})["toolSpecification"].(map[string]interface{})
	inputSchemaWrapper := toolSpec["inputSchema"].(map[string]interface{})

	schemaBytes, err := json.Marshal(inputSchemaWrapper["json"])
	require.NoError(t, err)
	var schema map[string]interface{}
	err = json.Unmarshal(schemaBytes, &schema)
	require.NoError(t, err)

	props := schema["properties"].(map[string]interface{})
	assert.Len(t, props, 2, "all properties should be preserved when no $-prefixed names")
	_, hasFilePath := props["file_path"]
	assert.True(t, hasFilePath)
	_, hasContent := props["content"]
	assert.True(t, hasContent)
}

func TestBuildRequestBody_WithToolResultsAndTools(t *testing.T) {
	// Simulate a real Claude Code scenario: tool_result with tools defined
	messages := []map[string]interface{}{
		{
			"role":    "user",
			"content": "Write hello to /tmp/test.txt",
		},
		{
			"role": "assistant",
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": "I'll write that for you.",
				},
				{
					"type":  "tool_use",
					"id":    "toolu_write_123",
					"name":  "Write",
					"input": map[string]interface{}{"file_path": "/tmp/test.txt", "content": "hello"},
				},
			},
		},
		{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type":        "tool_result",
					"tool_use_id": "toolu_write_123",
					"content":     "File written successfully.",
				},
			},
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	tools := []map[string]interface{}{
		{
			"name":         "Write",
			"description":  "Write content to a file",
			"input_schema": map[string]interface{}{"type": "object"},
		},
	}
	toolsJSON, err := json.Marshal(tools)
	require.NoError(t, err)

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "", toolsJSON)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})

	context := userInput["userInputMessageContext"].(map[string]interface{})

	// Should have both toolResults and tools
	toolResults := context["toolResults"].([]interface{})
	assert.Len(t, toolResults, 1)

	kiroTools := context["tools"].([]interface{})
	assert.Len(t, kiroTools, 1)
}
