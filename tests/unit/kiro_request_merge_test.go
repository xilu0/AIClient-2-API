package unit

import (
	"encoding/json"
	"testing"

	"github.com/anthropics/AIClient-2-API/internal/kiro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRequestBody_MergeAdjacentUserMessages(t *testing.T) {
	// Test merging of adjacent User messages (e.g. Context + Prompt)
	messages := []map[string]interface{}{
		{
			"role":    "user",
			"content": "Context: file.txt content...",
		},
		{
			"role":    "user",
			"content": "Please analyze this file.",
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	body, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "", nil)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	
	// History should be empty because we merged everything into currentMessage (since it's the last block)
	// Wait, if we have 2 messages, and they merge, we have 1 merged message.
	// Since it's the last (and only) message, it becomes currentMessage.
	
	if history, ok := convState["history"]; ok {
		assert.Empty(t, history, "History should be empty for single merged user message")
	}

	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})
	content := userInput["content"].(string)

	assert.Contains(t, content, "Context: file.txt content...")
	assert.Contains(t, content, "Please analyze this file.")
	// Check newlines
	assert.Contains(t, content, "\n")
}

func TestBuildRequestBody_MergeAdjacentToolResults(t *testing.T) {
	// Test merging of split tool results in history
	messages := []map[string]interface{}{
		{
			"role":    "user",
			"content": "Run tools",
		},
		{
			"role": "assistant",
			"content": []map[string]interface{}{
				{
					"type": "tool_use",
					"id":   "tool_1",
					"name": "tool1",
					"input": map[string]string{},
				},
				{
					"type": "tool_use",
					"id":   "tool_2",
					"name": "tool2",
					"input": map[string]string{},
				},
			},
		},
		{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type":        "tool_result",
					"tool_use_id": "tool_1",
					"content":     "Result 1",
				},
			},
		},
		{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type":        "tool_result",
					"tool_use_id": "tool_2",
					"content":     "Result 2",
				},
			},
		},
		{
			"role": "assistant",
			"content": "Next step...",
		},
		{
			"role":    "user",
			"content": "Continue",
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	body, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "", nil)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	history := convState["history"].([]interface{})

	// Expected structure:
	// 1. User: "Run tools"
	// 2. Assistant: ToolUse 1 & 2
	// 3. User: ToolResult 1 & 2 (Merged)
	// 4. Assistant: "Next step..."
	
	// We expect 4 items in history
	assert.Len(t, history, 4)

	// Check item 3 (index 2) - should be the merged User message
	mergedUserMsg := history[2].(map[string]interface{})
	userInput := mergedUserMsg["userInputMessage"].(map[string]interface{})
	context := userInput["userInputMessageContext"].(map[string]interface{})
	toolResults := context["toolResults"].([]interface{})

	assert.Len(t, toolResults, 2)
	
tr1 := toolResults[0].(map[string]interface{})
tr2 := toolResults[1].(map[string]interface{})
	
	// Order should be preserved
	assert.Equal(t, "tool_1", tr1["toolUseId"])
	assert.Equal(t, "tool_2", tr2["toolUseId"])
}

func TestBuildRequestBody_MergeWithSystemPrompt(t *testing.T) {
	// Test system prompt prepending with merging
	messages := []map[string]interface{}{
		{
			"role":    "user",
			"content": "User 1",
		},
		{
			"role":    "user",
			"content": "User 2",
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	body, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "System Prompt", "", nil)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})
	content := userInput["content"].(string)

	// Should contain System Prompt + User 1 + User 2
	assert.Contains(t, content, "System Prompt")
	assert.Contains(t, content, "User 1")
	assert.Contains(t, content, "User 2")
}

func TestBuildRequestBody_EmptyContentFallback(t *testing.T) {
	// Test fallback for empty content in history
	messages := []map[string]interface{}{
		{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type":        "tool_result",
					"tool_use_id": "tool_1",
					"content":     "Result",
				},
			},
		},
		{
			"role":    "assistant",
			"content": "Ok",
		},
		{
			"role":    "user",
			"content": "Next",
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	body, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "", nil)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	history := convState["history"].([]interface{})

	// 1. User (Tool Result) -> Should have fallback text
	// 2. Assistant ("Ok")
	
	require.Len(t, history, 2)
	
	userMsg := history[0].(map[string]interface{})["userInputMessage"].(map[string]interface{})
	assert.Equal(t, "Tool results provided.", userMsg["content"])
}