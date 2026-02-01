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

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "", nil)
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
					"type":  "tool_use",
					"id":    "tool_1",
					"name":  "tool1",
					"input": map[string]string{"arg": "value1"}, // Non-empty input required
				},
				{
					"type":  "tool_use",
					"id":    "tool_2",
					"name":  "tool2",
					"input": map[string]string{"arg": "value2"}, // Non-empty input required
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
			"role":    "assistant",
			"content": "Next step...",
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
	// New behavior (matching Node.js): system+firstUser goes to history[0],
	// currentMessage contains the merged user content without system
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

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "System Prompt", "", nil)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})

	// Check history[0] contains system prompt + merged user content
	history := convState["history"].([]interface{})
	require.GreaterOrEqual(t, len(history), 1, "history should have at least 1 entry")
	historyItem := history[0].(map[string]interface{})
	historyUserMsg := historyItem["userInputMessage"].(map[string]interface{})
	historyContent := historyUserMsg["content"].(string)
	assert.Contains(t, historyContent, "System Prompt")
	assert.Contains(t, historyContent, "User 1")
	assert.Contains(t, historyContent, "User 2")

	// currentMessage should contain merged user content (without system)
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})
	content := userInput["content"].(string)
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

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "", nil)
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

func TestBuildRequestBody_PreservesEmptyInputToolUseAndResult(t *testing.T) {
	// Test that tool_use with empty input is preserved (not filtered).
	// Tools like TaskList have input: {} which is valid for Kiro API.
	messages := []map[string]interface{}{
		{
			"role":    "user",
			"content": "Run tools",
		},
		{
			"role": "assistant",
			"content": []map[string]interface{}{
				{
					"type":  "tool_use",
					"id":    "tool_valid",
					"name":  "valid_tool",
					"input": map[string]string{"arg": "value"},
				},
				{
					"type":  "tool_use",
					"id":    "tool_empty",
					"name":  "empty_tool",
					"input": map[string]string{}, // Empty input - should be preserved
				},
			},
		},
		{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type":        "tool_result",
					"tool_use_id": "tool_valid",
					"content":     "Valid result",
				},
				{
					"type":        "tool_result",
					"tool_use_id": "tool_empty",
					"content":     "Empty tool result - should be preserved",
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

	// Expected structure:
	// 1. User: "Run tools"
	// 2. Assistant: Both tool_valid and tool_empty preserved

	require.Len(t, history, 2)

	// Check assistant message - should have both tool_uses
	assistantMsg := history[1].(map[string]interface{})["assistantResponseMessage"].(map[string]interface{})
	toolUses := assistantMsg["toolUses"].([]interface{})
	assert.Len(t, toolUses, 2)
	assert.Equal(t, "tool_valid", toolUses[0].(map[string]interface{})["toolUseId"])
	assert.Equal(t, "tool_empty", toolUses[1].(map[string]interface{})["toolUseId"])

	// Check currentMessage - should have both tool_results
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})
	context := userInput["userInputMessageContext"].(map[string]interface{})
	toolResults := context["toolResults"].([]interface{})

	assert.Len(t, toolResults, 2)
	assert.Equal(t, "tool_valid", toolResults[0].(map[string]interface{})["toolUseId"])
	assert.Equal(t, "tool_empty", toolResults[1].(map[string]interface{})["toolUseId"])
}

func TestBuildRequestBody_PreservesAllEmptyInputToolUses(t *testing.T) {
	// Test when ALL tool_uses have empty input - they should all be preserved (not filtered).
	// This matches Node.js behavior where TaskList with input: {} works correctly.
	messages := []map[string]interface{}{
		{
			"role":    "user",
			"content": "Run tools",
		},
		{
			"role": "assistant",
			"content": []map[string]interface{}{
				{
					"type":  "tool_use",
					"id":    "tool_empty_1",
					"name":  "TaskList",
					"input": map[string]string{}, // Empty input - should be preserved
				},
			},
		},
		{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type":        "tool_result",
					"tool_use_id": "tool_empty_1",
					"content":     "Result for TaskList",
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

	// Expected structure:
	// 1. User: "Run tools"
	// 2. Assistant: toolUses with TaskList preserved

	require.Len(t, history, 2)

	// Check assistant message - should have toolUses with the empty-input tool
	assistantMsg := history[1].(map[string]interface{})["assistantResponseMessage"].(map[string]interface{})
	toolUses, hasToolUses := assistantMsg["toolUses"].([]interface{})
	assert.True(t, hasToolUses, "toolUses should be present for empty-input tools")
	require.Len(t, toolUses, 1)
	assert.Equal(t, "tool_empty_1", toolUses[0].(map[string]interface{})["toolUseId"])
	assert.Equal(t, "TaskList", toolUses[0].(map[string]interface{})["name"])

	// Check currentMessage - should have tool results
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})
	context, hasContext := userInput["userInputMessageContext"].(map[string]interface{})
	assert.True(t, hasContext, "userInputMessageContext should be present with tool results")
	toolResults := context["toolResults"].([]interface{})
	require.Len(t, toolResults, 1)
	assert.Equal(t, "tool_empty_1", toolResults[0].(map[string]interface{})["toolUseId"])

	// Content should be "Tool results provided." since there are tool results
	assert.Equal(t, "Tool results provided.", userInput["content"])
}
func TestInjectToolsFromHistory_NoToolsNeeded(t *testing.T) {
	// Test when history has no toolUses - should not modify
	reqBody := []byte(`{
		"conversationState": {
			"history": [
				{"userInputMessage": {"content": "Hello"}},
				{"assistantResponseMessage": {"content": "Hi there"}}
			],
			"currentMessage": {
				"userInputMessage": {"content": "How are you?"}
			}
		}
	}`)

	modified, changed := kiro.InjectToolsFromHistory(reqBody)
	assert.False(t, changed, "Should not modify when no toolUses in history")
	assert.Equal(t, reqBody, modified)
}

func TestInjectToolsFromHistory_ToolsAlreadyPresent(t *testing.T) {
	// Test when tools already present - should not modify
	reqBody := []byte(`{
		"conversationState": {
			"history": [
				{"userInputMessage": {"content": "Use tool"}},
				{"assistantResponseMessage": {"content": "OK", "toolUses": [{"name": "Test", "toolUseId": "t1", "input": {"x": "1"}}]}}
			],
			"currentMessage": {
				"userInputMessage": {
					"content": "Result",
					"userInputMessageContext": {
						"tools": [{"toolSpecification": {"name": "Test"}}],
						"toolResults": [{"toolUseId": "t1", "content": [{"text": "Done"}]}]
					}
				}
			}
		}
	}`)

	_, changed := kiro.InjectToolsFromHistory(reqBody)
	assert.False(t, changed, "Should not modify when tools already present")
}

func TestInjectToolsFromHistory_InjectsTools(t *testing.T) {
	// Test when history has toolUses but no tools definition - should inject
	reqBody := []byte(`{
		"conversationState": {
			"history": [
				{"userInputMessage": {"content": "Use tool", "modelId": "TEST", "origin": "AI_EDITOR"}},
				{"assistantResponseMessage": {"content": "OK", "toolUses": [{"name": "TestTool", "toolUseId": "t1", "input": {"x": "1"}}]}}
			],
			"currentMessage": {
				"userInputMessage": {
					"content": "Tool results provided.",
					"modelId": "TEST",
					"origin": "AI_EDITOR",
					"userInputMessageContext": {
						"toolResults": [{"toolUseId": "t1", "content": [{"text": "Done"}], "status": "success"}]
					}
				}
			}
		}
	}`)

	modified, changed := kiro.InjectToolsFromHistory(reqBody)
	assert.True(t, changed, "Should modify when toolUses in history but no tools")

	var result map[string]interface{}
	err := json.Unmarshal(modified, &result)
	require.NoError(t, err)

	// Check that tools were injected
	convState := result["conversationState"].(map[string]interface{})
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})
	ctx := userInput["userInputMessageContext"].(map[string]interface{})
	tools := ctx["tools"].([]interface{})

	assert.Len(t, tools, 1)
	tool := tools[0].(map[string]interface{})
	spec := tool["toolSpecification"].(map[string]interface{})
	assert.Equal(t, "TestTool", spec["name"])
}

func TestInjectToolsFromHistory_MultipleTools(t *testing.T) {
	// Test with multiple different tools in history
	reqBody := []byte(`{
		"conversationState": {
			"history": [
				{"userInputMessage": {"content": "Use tools"}},
				{"assistantResponseMessage": {"content": "OK", "toolUses": [
					{"name": "Tool1", "toolUseId": "t1", "input": {"a": "1"}},
					{"name": "Tool2", "toolUseId": "t2", "input": {"b": "2"}}
				]}},
				{"userInputMessage": {"content": "Results", "userInputMessageContext": {"toolResults": [
					{"toolUseId": "t1", "content": [{"text": "R1"}]},
					{"toolUseId": "t2", "content": [{"text": "R2"}]}
				]}}},
				{"assistantResponseMessage": {"content": "Done"}}
			],
			"currentMessage": {
				"userInputMessage": {"content": "What next?"}
			}
		}
	}`)

	modified, changed := kiro.InjectToolsFromHistory(reqBody)
	assert.True(t, changed, "Should modify when toolUses in history")

	var result map[string]interface{}
	err := json.Unmarshal(modified, &result)
	require.NoError(t, err)

	convState := result["conversationState"].(map[string]interface{})
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})
	ctx := userInput["userInputMessageContext"].(map[string]interface{})
	tools := ctx["tools"].([]interface{})

	// Should have 2 unique tools
	assert.Len(t, tools, 2)

	// Collect tool names
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		spec := tool.(map[string]interface{})["toolSpecification"].(map[string]interface{})
		toolNames[spec["name"].(string)] = true
	}
	assert.True(t, toolNames["Tool1"])
	assert.True(t, toolNames["Tool2"])
}
