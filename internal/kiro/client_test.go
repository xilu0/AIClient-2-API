package kiro

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseAssistantContent_EmptyToolInput(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		expectedText   string
		expectedTools  int
		description    string
	}{
		{
			name: "empty tool input object should be filtered",
			content: `[
				{
					"type": "thinking",
					"thinking": ""
				},
				{
					"type": "tool_use",
					"id": "tooluse_123",
					"name": "AskUserQuestion",
					"input": {}
				}
			]`,
			expectedText:  "",
			expectedTools: 0,
			description:   "Empty tool input {} should be filtered out",
		},
		{
			name: "null tool input should be filtered",
			content: `[
				{
					"type": "tool_use",
					"id": "tooluse_456",
					"name": "Read",
					"input": null
				}
			]`,
			expectedText:  "",
			expectedTools: 0,
			description:   "Null tool input should be filtered out",
		},
		{
			name: "valid tool input should be kept",
			content: `[
				{
					"type": "tool_use",
					"id": "tooluse_789",
					"name": "Read",
					"input": {"file_path": "/test/file.txt"}
				}
			]`,
			expectedText:  "",
			expectedTools: 1,
			description:   "Valid tool input should be preserved",
		},
		{
			name: "empty thinking should not appear in text",
			content: `[
				{
					"type": "thinking",
					"thinking": ""
				},
				{
					"type": "text",
					"text": "Hello"
				}
			]`,
			expectedText:  "Hello",
			expectedTools: 0,
			description:   "Empty thinking should not add kiro_thinking tags",
		},
		{
			name: "non-empty thinking should appear in text",
			content: `[
				{
					"type": "thinking",
					"thinking": "Let me think..."
				},
				{
					"type": "text",
					"text": "Hello"
				}
			]`,
			expectedText:  "<kiro_thinking>Let me think...</kiro_thinking>\n\nHello",
			expectedTools: 0,
			description:   "Non-empty thinking should be wrapped in tags",
		},
		{
			name: "mixed empty and valid tools",
			content: `[
				{
					"type": "tool_use",
					"id": "tooluse_empty",
					"name": "AskUserQuestion",
					"input": {}
				},
				{
					"type": "tool_use",
					"id": "tooluse_valid",
					"name": "Read",
					"input": {"file_path": "/test/file.txt"}
				}
			]`,
			expectedText:  "",
			expectedTools: 1,
			description:   "Only valid tool uses should be kept",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseAssistantContent(json.RawMessage(tt.content))

			assert.Equal(t, tt.expectedText, result.Text, tt.description)
			assert.Equal(t, tt.expectedTools, len(result.ToolUses),
				"Expected %d tool uses, got %d", tt.expectedTools, len(result.ToolUses))

			// Verify that no tool use has empty input
			for _, toolUse := range result.ToolUses {
				input, ok := toolUse["input"]
				assert.True(t, ok, "Tool use should have input field")
				assert.NotNil(t, input, "Tool use input should not be nil")

				if inputMap, ok := input.(map[string]interface{}); ok {
					assert.NotEmpty(t, inputMap, "Tool use input map should not be empty")
				}
			}
		})
	}
}

func TestParseAssistantContent_RealWorldErrorCase(t *testing.T) {
	// This is the actual error case from the debug samples
	content := `[
		{
			"thinking": "",
			"type": "thinking"
		},
		{
			"type": "tool_use",
			"id": "tooluse_Bu424BYoS5u_De2WllpHKw",
			"name": "AskUserQuestion",
			"input": {},
			"cache_control": {
				"type": "ephemeral"
			}
		}
	]`

	result := parseAssistantContent(json.RawMessage(content))

	// Should have no tool uses (empty input filtered out)
	assert.Equal(t, 0, len(result.ToolUses),
		"Empty tool input should be filtered, preventing 'Improperly formed request' error")

	// Should have empty text (empty thinking filtered out)
	assert.Equal(t, "", result.Text,
		"Empty thinking should not add kiro_thinking tags")
}
