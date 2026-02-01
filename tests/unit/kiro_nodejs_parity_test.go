package unit

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/anthropics/AIClient-2-API/internal/kiro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKiroRequestParity_NodeJS tests that Go generates the same kiro_request.json structure as Node.js
func TestKiroRequestParity_NodeJS(t *testing.T) {
	// Load Node.js input (original Claude API request)
	inputData, err := os.ReadFile("../testdata/nodejs_input.json")
	if os.IsNotExist(err) {
		t.Skip("Test data not found. Run: docker cp aiclient2api:/app/kiro-debug/nodejs/<session>/request.json tests/testdata/nodejs_input.json")
	}
	require.NoError(t, err)

	// Load Node.js expected output (kiro_request.json)
	expectedData, err := os.ReadFile("../testdata/nodejs_expected.json")
	require.NoError(t, err)

	// Parse input
	var input map[string]interface{}
	require.NoError(t, json.Unmarshal(inputData, &input))

	// Parse expected
	var expected map[string]interface{}
	require.NoError(t, json.Unmarshal(expectedData, &expected))

	// Extract fields from input
	model := input["model"].(string)
	messagesJSON, err := json.Marshal(input["messages"])
	require.NoError(t, err)

	var systemStr string
	if sys, ok := input["system"]; ok {
		// System can be string or array of blocks
		switch s := sys.(type) {
		case string:
			systemStr = s
		case []interface{}:
			// Concatenate text blocks (matching Node.js getContentText behavior)
			for _, block := range s {
				if b, ok := block.(map[string]interface{}); ok {
					if b["type"] == "text" {
						if text, ok := b["text"].(string); ok {
							systemStr += text
						}
					}
				}
			}
		}
	}

	var toolsJSON []byte
	if tools, ok := input["tools"]; ok {
		toolsJSON, _ = json.Marshal(tools)
	}

	// Build Go request
	goBody, _, err := kiro.BuildRequestBody(model, messagesJSON, 8096, true, systemStr, "", toolsJSON)
	require.NoError(t, err)

	// Parse Go output
	var goResult map[string]interface{}
	require.NoError(t, json.Unmarshal(goBody, &goResult))

	// Compare structures
	t.Run("TopLevelKeys", func(t *testing.T) {
		expectedKeys := getKeys(expected)
		goKeys := getKeys(goResult)
		assert.ElementsMatch(t, expectedKeys, goKeys, "Top-level keys should match")
	})

	t.Run("ConversationStateKeys", func(t *testing.T) {
		expectedCS := expected["conversationState"].(map[string]interface{})
		goCS := goResult["conversationState"].(map[string]interface{})

		expectedKeys := getKeys(expectedCS)
		goKeys := getKeys(goCS)
		assert.ElementsMatch(t, expectedKeys, goKeys, "conversationState keys should match")
	})

	t.Run("HistoryCount", func(t *testing.T) {
		expectedCS := expected["conversationState"].(map[string]interface{})
		goCS := goResult["conversationState"].(map[string]interface{})

		var expectedHistoryCount, goHistoryCount int
		if h, ok := expectedCS["history"].([]interface{}); ok {
			expectedHistoryCount = len(h)
		}
		if h, ok := goCS["history"].([]interface{}); ok {
			goHistoryCount = len(h)
		}
		assert.Equal(t, expectedHistoryCount, goHistoryCount, "History count should match")
	})

	t.Run("HistoryStructure", func(t *testing.T) {
		expectedCS := expected["conversationState"].(map[string]interface{})
		goCS := goResult["conversationState"].(map[string]interface{})

		expectedHistory, _ := expectedCS["history"].([]interface{})
		goHistory, _ := goCS["history"].([]interface{})

		minLen := len(expectedHistory)
		if len(goHistory) < minLen {
			minLen = len(goHistory)
		}

		for i := 0; i < minLen; i++ {
			expectedItem := expectedHistory[i].(map[string]interface{})
			goItem := goHistory[i].(map[string]interface{})

			expectedType := "unknown"
			goType := "unknown"
			if _, ok := expectedItem["userInputMessage"]; ok {
				expectedType = "userInputMessage"
			} else if _, ok := expectedItem["assistantResponseMessage"]; ok {
				expectedType = "assistantResponseMessage"
			}
			if _, ok := goItem["userInputMessage"]; ok {
				goType = "userInputMessage"
			} else if _, ok := goItem["assistantResponseMessage"]; ok {
				goType = "assistantResponseMessage"
			}

			assert.Equal(t, expectedType, goType, "History[%d] type should match", i)
		}
	})

	t.Run("CurrentMessageKeys", func(t *testing.T) {
		expectedCS := expected["conversationState"].(map[string]interface{})
		goCS := goResult["conversationState"].(map[string]interface{})

		expectedCM := expectedCS["currentMessage"].(map[string]interface{})
		goCM := goCS["currentMessage"].(map[string]interface{})

		if expectedUIM, ok := expectedCM["userInputMessage"].(map[string]interface{}); ok {
			goUIM, ok := goCM["userInputMessage"].(map[string]interface{})
			require.True(t, ok, "Go should have userInputMessage")

			expectedKeys := getKeys(expectedUIM)
			goKeys := getKeys(goUIM)
			assert.ElementsMatch(t, expectedKeys, goKeys, "userInputMessage keys should match")
		}
	})

	t.Run("CurrentMessageContent", func(t *testing.T) {
		expectedCS := expected["conversationState"].(map[string]interface{})
		goCS := goResult["conversationState"].(map[string]interface{})

		expectedCM := expectedCS["currentMessage"].(map[string]interface{})
		goCM := goCS["currentMessage"].(map[string]interface{})

		expectedUIM := expectedCM["userInputMessage"].(map[string]interface{})
		goUIM := goCM["userInputMessage"].(map[string]interface{})

		expectedContent := expectedUIM["content"].(string)
		goContent := goUIM["content"].(string)

		// Check content starts with same prefix (first 100 chars)
		expectedPrefix := truncate(expectedContent, 100)
		goPrefix := truncate(goContent, 100)
		assert.Equal(t, expectedPrefix, goPrefix, "currentMessage content prefix should match")
	})

	t.Run("HistoryFirstItemContent", func(t *testing.T) {
		expectedCS := expected["conversationState"].(map[string]interface{})
		goCS := goResult["conversationState"].(map[string]interface{})

		expectedHistory, ok1 := expectedCS["history"].([]interface{})
		goHistory, ok2 := goCS["history"].([]interface{})

		if !ok1 || !ok2 || len(expectedHistory) == 0 || len(goHistory) == 0 {
			t.Skip("No history to compare")
		}

		expectedItem := expectedHistory[0].(map[string]interface{})
		goItem := goHistory[0].(map[string]interface{})

		if expectedUIM, ok := expectedItem["userInputMessage"].(map[string]interface{}); ok {
			goUIM, ok := goItem["userInputMessage"].(map[string]interface{})
			require.True(t, ok, "Go history[0] should be userInputMessage")

			expectedContent := expectedUIM["content"].(string)
			goContent := goUIM["content"].(string)

			// Check content starts with same prefix
			expectedPrefix := truncate(expectedContent, 200)
			goPrefix := truncate(goContent, 200)
			assert.Equal(t, expectedPrefix, goPrefix, "history[0] content prefix should match")
		}
	})

	t.Run("ToolsCount", func(t *testing.T) {
		expectedCS := expected["conversationState"].(map[string]interface{})
		goCS := goResult["conversationState"].(map[string]interface{})

		expectedCM := expectedCS["currentMessage"].(map[string]interface{})
		goCM := goCS["currentMessage"].(map[string]interface{})

		expectedUIM := expectedCM["userInputMessage"].(map[string]interface{})
		goUIM := goCM["userInputMessage"].(map[string]interface{})

		var expectedToolsCount, goToolsCount int
		if ctx, ok := expectedUIM["userInputMessageContext"].(map[string]interface{}); ok {
			if tools, ok := ctx["tools"].([]interface{}); ok {
				expectedToolsCount = len(tools)
			}
		}
		if ctx, ok := goUIM["userInputMessageContext"].(map[string]interface{}); ok {
			if tools, ok := ctx["tools"].([]interface{}); ok {
				goToolsCount = len(tools)
			}
		}
		assert.Equal(t, expectedToolsCount, goToolsCount, "Tools count should match")
	})

	// Print detailed diff for debugging
	t.Run("DetailedDiff", func(t *testing.T) {
		expectedJSON, _ := json.MarshalIndent(summarizeRequest(expected), "", "  ")
		goJSON, _ := json.MarshalIndent(summarizeRequest(goResult), "", "  ")

		t.Logf("Node.js summary:\n%s", string(expectedJSON))
		t.Logf("Go summary:\n%s", string(goJSON))
	})
}

func getKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// summarizeRequest creates a summary of the request for debugging
func summarizeRequest(req map[string]interface{}) map[string]interface{} {
	summary := make(map[string]interface{})

	if cs, ok := req["conversationState"].(map[string]interface{}); ok {
		csSummary := make(map[string]interface{})

		if history, ok := cs["history"].([]interface{}); ok {
			histSummary := make([]map[string]interface{}, 0)
			for i, item := range history {
				itemMap := item.(map[string]interface{})
				itemSummary := map[string]interface{}{"index": i}
				if uim, ok := itemMap["userInputMessage"].(map[string]interface{}); ok {
					itemSummary["type"] = "userInputMessage"
					if content, ok := uim["content"].(string); ok {
						itemSummary["content_len"] = len(content)
						itemSummary["content_prefix"] = truncate(content, 80)
					}
				} else if arm, ok := itemMap["assistantResponseMessage"].(map[string]interface{}); ok {
					itemSummary["type"] = "assistantResponseMessage"
					if content, ok := arm["content"].(string); ok {
						itemSummary["content"] = content
					}
				}
				histSummary = append(histSummary, itemSummary)
			}
			csSummary["history"] = histSummary
			csSummary["history_count"] = len(history)
		} else {
			csSummary["history_count"] = 0
		}

		if cm, ok := cs["currentMessage"].(map[string]interface{}); ok {
			cmSummary := make(map[string]interface{})
			if uim, ok := cm["userInputMessage"].(map[string]interface{}); ok {
				if content, ok := uim["content"].(string); ok {
					cmSummary["content_len"] = len(content)
					cmSummary["content_prefix"] = truncate(content, 80)
				}
				if ctx, ok := uim["userInputMessageContext"].(map[string]interface{}); ok {
					if tools, ok := ctx["tools"].([]interface{}); ok {
						cmSummary["tools_count"] = len(tools)
					}
				}
			}
			csSummary["currentMessage"] = cmSummary
		}

		summary["conversationState"] = csSummary
	}

	if _, ok := req["profileArn"]; ok {
		summary["has_profileArn"] = true
	} else {
		summary["has_profileArn"] = false
	}

	return summary
}
