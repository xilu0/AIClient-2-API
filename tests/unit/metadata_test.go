// Package unit contains unit tests for the Kiro server.
package unit

import (
	"encoding/json"
	"testing"

	"github.com/anthropics/AIClient-2-API/internal/kiro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMetadataNotInRequestBody verifies that _modelMapping is not sent to Kiro API.
func TestMetadataNotInRequestBody(t *testing.T) {
	messages := []map[string]interface{}{
		{"role": "user", "content": "Hello"},
	}
	messagesJSON, _ := json.Marshal(messages)

	body, metadata, err := kiro.BuildRequestBody("claude-sonnet-4-5", messagesJSON, 1000, true, "", "test-arn", nil)
	require.NoError(t, err)

	// Parse body to check for _modelMapping
	var reqBody map[string]interface{}
	err = json.Unmarshal(body, &reqBody)
	require.NoError(t, err)

	// Verify _modelMapping is NOT in request body
	_, exists := reqBody["_modelMapping"]
	assert.False(t, exists, "_modelMapping should not be in request body sent to Kiro API")

	// Verify metadata contains model info
	assert.NotNil(t, metadata, "metadata should not be nil")
	assert.Equal(t, "claude-sonnet-4-5", metadata["original_model"])
	assert.Equal(t, "CLAUDE_SONNET_4_5_20250929_V1_0", metadata["kiro_model"])
}

// TestMetadataForDifferentModels verifies metadata is correct for various models.
func TestMetadataForDifferentModels(t *testing.T) {
	tests := []struct {
		name          string
		inputModel    string
		expectedOrig  string
		expectedKiro  string
	}{
		{
			name:         "Sonnet 4.5",
			inputModel:   "claude-sonnet-4-5",
			expectedOrig: "claude-sonnet-4-5",
			expectedKiro: "CLAUDE_SONNET_4_5_20250929_V1_0",
		},
		{
			name:         "Haiku 4.5",
			inputModel:   "claude-haiku-4-5",
			expectedOrig: "claude-haiku-4-5",
			expectedKiro: "claude-haiku-4.5",
		},
		{
			name:         "Opus 4.5",
			inputModel:   "claude-opus-4-5",
			expectedOrig: "claude-opus-4-5",
			expectedKiro: "claude-opus-4.5",
		},
		{
			name:         "Unknown model",
			inputModel:   "unknown-model",
			expectedOrig: "unknown-model",
			expectedKiro: "claude-sonnet-4.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages := []map[string]interface{}{
				{"role": "user", "content": "Test"},
			}
			messagesJSON, _ := json.Marshal(messages)

			_, metadata, err := kiro.BuildRequestBody(tt.inputModel, messagesJSON, 1000, true, "", "", nil)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedOrig, metadata["original_model"])
			assert.Equal(t, tt.expectedKiro, metadata["kiro_model"])
		})
	}
}

// TestRequestBodyStructure verifies the request body only contains expected fields.
func TestRequestBodyStructure(t *testing.T) {
	messages := []map[string]interface{}{
		{"role": "user", "content": "Hello"},
	}
	messagesJSON, _ := json.Marshal(messages)

	body, _, err := kiro.BuildRequestBody("claude-sonnet-4-5", messagesJSON, 1000, true, "", "test-arn", nil)
	require.NoError(t, err)

	var reqBody map[string]interface{}
	err = json.Unmarshal(body, &reqBody)
	require.NoError(t, err)

	// Expected fields
	expectedFields := []string{"conversationState", "profileArn"}
	for _, field := range expectedFields {
		assert.Contains(t, reqBody, field, "request body should contain %s", field)
	}

	// Fields that should NOT be present
	forbiddenFields := []string{"_modelMapping", "_metadata", "_debug"}
	for _, field := range forbiddenFields {
		assert.NotContains(t, reqBody, field, "request body should NOT contain %s", field)
	}
}
