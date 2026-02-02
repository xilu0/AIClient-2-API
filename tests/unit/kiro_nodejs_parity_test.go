package unit

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/anthropics/AIClient-2-API/internal/kiro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKiroRequestParity_NodeJS tests that Go generates a compatible kiro_request.json structure.
// Note: For single-message + system prompt cases, Go intentionally diverges from Node.js by
// merging system into currentMessage instead of duplicating user content across history and
// currentMessage. This prevents "Input is too long" errors from payload duplication.
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
		goCS := goResult["conversationState"].(map[string]interface{})
		goKeys := getKeys(goCS)

		// Go always has conversationId, currentMessage, chatTriggerType.
		// For single-message + system cases, Go omits history (no duplication).
		assert.Contains(t, goKeys, "conversationId")
		assert.Contains(t, goKeys, "currentMessage")
		assert.Contains(t, goKeys, "chatTriggerType")
	})

	t.Run("HistoryCount", func(t *testing.T) {
		goCS := goResult["conversationState"].(map[string]interface{})

		var goHistoryCount int
		if h, ok := goCS["history"].([]interface{}); ok {
			goHistoryCount = len(h)
		}

		// For single-message + system prompt, Go intentionally produces no history
		// to avoid duplicating user content (fixes "Input is too long" errors).
		// Node.js puts system+user in history AND user in currentMessage (2x payload).
		assert.Equal(t, 0, goHistoryCount, "Single-message case should have no history (avoids duplication)")
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

		goCM := goCS["currentMessage"].(map[string]interface{})
		goUIM := goCM["userInputMessage"].(map[string]interface{})
		goContent := goUIM["content"].(string)

		// For single-message + system: Go merges system into currentMessage.
		// Node.js puts user content in currentMessage and system+user in history[0].
		// Go puts system+user in currentMessage only (no duplication).
		// Verify Go's currentMessage matches the full system+user content from Node.js history[0].
		expectedHistoryArr, hasHistory := expectedCS["history"].([]interface{})
		if hasHistory && len(expectedHistoryArr) > 0 {
			histItem := expectedHistoryArr[0].(map[string]interface{})
			if uim, ok := histItem["userInputMessage"].(map[string]interface{}); ok {
				histContent := uim["content"].(string)
				// Go's currentMessage should start with the same content as Node.js history[0]
				assert.Equal(t, truncate(histContent, 100), truncate(goContent, 100),
					"Go currentMessage should contain system+user content that Node.js puts in history[0]")
			}
		} else {
			// No history in Node.js output — compare currentMessage directly
			expectedCM := expectedCS["currentMessage"].(map[string]interface{})
			expectedUIM := expectedCM["userInputMessage"].(map[string]interface{})
			expectedContent := expectedUIM["content"].(string)
			assert.Equal(t, truncate(expectedContent, 100), truncate(goContent, 100),
				"currentMessage content prefix should match")
		}
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
