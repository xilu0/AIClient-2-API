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
	// Test system prompt prepending with merging.
	// When adjacent user messages merge into a single message, system should be
	// prepended to currentMessage only (no history entry), avoiding payload duplication.
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

	// History should be empty — single merged message + system goes to currentMessage only
	if historyRaw, hasHistory := convState["history"]; hasHistory {
		if history, ok := historyRaw.([]interface{}); ok {
			assert.Empty(t, history, "history should be empty for single merged message + system")
		}
	}

	// currentMessage should contain system prompt + merged user content
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})
	content := userInput["content"].(string)
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

func TestBuildRequestBody_FiltersEmptyInputToolUseWithoutToolResult(t *testing.T) {
	// Test that tool_use with empty input is filtered ONLY when there's no corresponding toolResult.
	// If a tool requires params but input is {}, and there's NO toolResult referencing it,
	// the toolUse should be filtered to prevent "Improperly formed request" errors.
	// However, if there IS a toolResult referencing it, the toolUse must be preserved
	// to avoid creating orphan toolResults.
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
					"id":    "tool_empty_unreferenced",
					"name":  "empty_tool",
					"input": map[string]string{}, // Empty input, NO toolResult - should be filtered
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
				// Note: NO tool_result for tool_empty_unreferenced
			},
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	// No tools passed - empty input toolUses without toolResult are filtered
	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "", nil)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	history := convState["history"].([]interface{})

	// Expected structure:
	// 1. User: "Run tools"
	// 2. Assistant: Only valid_tool preserved (tool_empty_unreferenced filtered)

	require.Len(t, history, 2)

	// Check assistant message - should have only valid_tool
	assistantMsg := history[1].(map[string]interface{})["assistantResponseMessage"].(map[string]interface{})
	toolUses := assistantMsg["toolUses"].([]interface{})
	assert.Len(t, toolUses, 1)
	assert.Equal(t, "tool_valid", toolUses[0].(map[string]interface{})["toolUseId"])

	// Check currentMessage - only 1 tool_result (for tool_valid)
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})
	context := userInput["userInputMessageContext"].(map[string]interface{})
	toolResults := context["toolResults"].([]interface{})
	assert.Len(t, toolResults, 1)
}

func TestBuildRequestBody_RemovesEmptyInputToolUseAndToolResult(t *testing.T) {
	// Test that tool_use with empty input AND its corresponding toolResult are BOTH removed.
	// This prevents "Improperly formed request" errors from Kiro API.
	// The old behavior was to preserve empty-input toolUses if they had a toolResult,
	// but this still caused API errors because the empty input violates the tool schema.
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
					"id":    "tool_empty_referenced",
					"name":  "empty_tool",
					"input": map[string]string{}, // Empty input - should be REMOVED along with its toolResult
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
					"tool_use_id": "tool_empty_referenced",
					"content":     "Empty tool error result", // Should be REMOVED
				},
			},
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	// Define tools with required params (empty_tool requires params)
	tools := []map[string]interface{}{
		{
			"name": "valid_tool",
			"input_schema": map[string]interface{}{
				"type":     "object",
				"required": []string{"arg"},
			},
		},
		{
			"name": "empty_tool",
			"input_schema": map[string]interface{}{
				"type":     "object",
				"required": []string{"required_param"}, // Has required param, so empty input is invalid
			},
		},
	}
	toolsJSON, _ := json.Marshal(tools)

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4", messagesJSON, 1000, true, "", "", toolsJSON)
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	convState := req["conversationState"].(map[string]interface{})
	history := convState["history"].([]interface{})

	// Expected structure:
	// 1. User: "Run tools"
	// 2. Assistant: Only valid_tool preserved (empty_tool removed)

	require.Len(t, history, 2)

	// Check assistant message - should have only ONE toolUse (valid_tool)
	assistantMsg := history[1].(map[string]interface{})["assistantResponseMessage"].(map[string]interface{})
	toolUses := assistantMsg["toolUses"].([]interface{})
	assert.Len(t, toolUses, 1, "Only valid_tool should remain, empty_tool should be removed")
	assert.Equal(t, "tool_valid", toolUses[0].(map[string]interface{})["toolUseId"])

	// Check currentMessage - only valid tool_result present
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInput := currentMsg["userInputMessage"].(map[string]interface{})
	context := userInput["userInputMessageContext"].(map[string]interface{})
	toolResults := context["toolResults"].([]interface{})
	assert.Len(t, toolResults, 1, "Only tool_valid's toolResult should remain")
	assert.Equal(t, "tool_valid", toolResults[0].(map[string]interface{})["toolUseId"])
}

func TestBuildRequestBody_PreservesEmptyInputForToolsWithNoRequiredParams(t *testing.T) {
	// Test that tool_use with empty input is preserved for tools that have NO required params.
	// Tools like TaskList don't require any parameters, so input: {} is valid.
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

	// Tool definition for TaskList with NO required params
	tools := []map[string]interface{}{
		{
			"name":        "TaskList",
			"description": "List all tasks",
			"input_schema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				// No "required" field - empty input is valid
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
	history := convState["history"].([]interface{})

	// Expected structure:
	// 1. User: "Run tools"
	// 2. Assistant: toolUses with TaskList preserved

	require.Len(t, history, 2)

	// Check assistant message - should have toolUses with the empty-input tool
	assistantMsg := history[1].(map[string]interface{})["assistantResponseMessage"].(map[string]interface{})
	toolUses, hasToolUses := assistantMsg["toolUses"].([]interface{})
	assert.True(t, hasToolUses, "toolUses should be present for tools with no required params")
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

// TestBuildRequestBody_FilterEmptyInputToolUses tests that toolUses with empty input
// are filtered out along with their corresponding toolResults.
// This prevents "Improperly formed request" errors from Kiro API.
func TestBuildRequestBody_FilterEmptyInputToolUses(t *testing.T) {
	// Simulate a conversation where Claude made a tool call with empty input,
	// then user provided a tool result for it.
	// The Read tool requires file_path parameter.
	messages := []map[string]interface{}{
		{
			"role":    "user",
			"content": "Read a file for me",
		},
		{
			"role": "assistant",
			"content": []map[string]interface{}{
				{"type": "text", "text": "I'll read the file."},
				{
					"type":       "tool_use",
					"id":         "tooluse_empty_input_123",
					"name":       "Read",
					"input":      map[string]interface{}{}, // Empty input - invalid!
				},
			},
		},
		{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type":        "tool_result",
					"tool_use_id": "tooluse_empty_input_123",
					"content":     "Error: file_path is required",
					"is_error":    true,
				},
			},
		},
		{
			"role":    "user",
			"content": "Please try again with the correct path",
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	// Define Read tool with required file_path
	tools := []map[string]interface{}{
		{
			"name":        "Read",
			"description": "Read a file",
			"input_schema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{
						"type":        "string",
						"description": "The file path",
					},
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
	history := convState["history"].([]interface{})

	// Check that the invalid toolUse and its toolResult have been filtered out
	for i, item := range history {
		historyItem := item.(map[string]interface{})

		// Check assistant messages don't have the empty-input toolUse
		if arm, ok := historyItem["assistantResponseMessage"].(map[string]interface{}); ok {
			if toolUses, ok := arm["toolUses"].([]interface{}); ok {
				for _, tu := range toolUses {
					tuMap := tu.(map[string]interface{})
					if tuMap["toolUseId"] == "tooluse_empty_input_123" {
						t.Errorf("history[%d]: Found toolUse with empty input that should have been filtered: %v", i, tuMap)
					}
				}
			}
		}

		// Check user messages don't have toolResults for the removed toolUse
		if uim, ok := historyItem["userInputMessage"].(map[string]interface{}); ok {
			if ctx, ok := uim["userInputMessageContext"].(map[string]interface{}); ok {
				if trs, ok := ctx["toolResults"].([]interface{}); ok {
					for _, tr := range trs {
						trMap := tr.(map[string]interface{})
						if trMap["toolUseId"] == "tooluse_empty_input_123" {
							t.Errorf("history[%d]: Found toolResult for removed toolUse: %v", i, trMap)
						}
					}
				}
			}
		}
	}
}

// TestBuildRequestBody_KeepEmptyInputForNoRequiredParams tests that toolUses with empty input
// are kept for tools that have no required parameters (like ExitPlanMode).
func TestBuildRequestBody_KeepEmptyInputForNoRequiredParams(t *testing.T) {
	messages := []map[string]interface{}{
		{
			"role":    "user",
			"content": "Exit plan mode",
		},
		{
			"role": "assistant",
			"content": []map[string]interface{}{
				{"type": "text", "text": "Exiting plan mode."},
				{
					"type":  "tool_use",
					"id":    "tooluse_exit_plan_456",
					"name":  "ExitPlanMode",
					"input": map[string]interface{}{}, // Empty input - valid for this tool!
				},
			},
		},
		{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type":        "tool_result",
					"tool_use_id": "tooluse_exit_plan_456",
					"content":     "Plan mode exited",
				},
			},
		},
		{
			"role":    "user",
			"content": "Continue with implementation",
		},
	}
	messagesJSON, err := json.Marshal(messages)
	require.NoError(t, err)

	// Define ExitPlanMode tool with NO required parameters
	tools := []map[string]interface{}{
		{
			"name":        "ExitPlanMode",
			"description": "Exit plan mode",
			"input_schema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []string{}, // No required params
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
	history := convState["history"].([]interface{})

	// The ExitPlanMode toolUse should be KEPT because it has no required params
	foundToolUse := false
	foundToolResult := false

	// Check history for toolUse
	for _, item := range history {
		historyItem := item.(map[string]interface{})

		if arm, ok := historyItem["assistantResponseMessage"].(map[string]interface{}); ok {
			if toolUses, ok := arm["toolUses"].([]interface{}); ok {
				for _, tu := range toolUses {
					tuMap := tu.(map[string]interface{})
					if tuMap["toolUseId"] == "tooluse_exit_plan_456" {
						foundToolUse = true
					}
				}
			}
		}

		if uim, ok := historyItem["userInputMessage"].(map[string]interface{}); ok {
			if ctx, ok := uim["userInputMessageContext"].(map[string]interface{}); ok {
				if trs, ok := ctx["toolResults"].([]interface{}); ok {
					for _, tr := range trs {
						trMap := tr.(map[string]interface{})
						if trMap["toolUseId"] == "tooluse_exit_plan_456" {
							foundToolResult = true
						}
					}
				}
			}
		}
	}

	// Also check currentMessage for toolResult (it may be merged there)
	currentMsg := convState["currentMessage"].(map[string]interface{})
	if uim, ok := currentMsg["userInputMessage"].(map[string]interface{}); ok {
		if ctx, ok := uim["userInputMessageContext"].(map[string]interface{}); ok {
			if trs, ok := ctx["toolResults"].([]interface{}); ok {
				for _, tr := range trs {
					trMap := tr.(map[string]interface{})
					if trMap["toolUseId"] == "tooluse_exit_plan_456" {
						foundToolResult = true
					}
				}
			}
		}
	}

	assert.True(t, foundToolUse, "ExitPlanMode toolUse should be kept (no required params)")
	assert.True(t, foundToolResult, "ExitPlanMode toolResult should be kept")
}
