// Package integration provides integration tests for the Kiro service.
package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/AIClient-2-API/internal/kiro"
	"github.com/stretchr/testify/require"
)

// TestKiroRequestFromFile loads a Kiro request JSON file and sends it directly to the Kiro API
// This test bypasses the Go service transformation and sends the request body as-is.
//
// Usage:
//
//	KIRO_REQUEST_FILE=/path/to/kiro_request.json go test ./tests/integration/... -v -run TestKiroRequestFromFile -timeout 120s
//
// Example with debug dump:
//
//	KIRO_REQUEST_FILE=/root/src/AIClient-2-API/kiro-debug/errors/1b04cb5a-9fdc-4f9c-8a24-8fcb59a11a02/kiro_request.json \
//	  go test ./tests/integration/... -v -run TestKiroRequestFromFile -timeout 120s
//
// With custom Redis URL:
//
//	REDIS_URL=redis://localhost:6379 \
//	KIRO_REQUEST_FILE=/path/to/request.json \
//	  go test ./tests/integration/... -v -run TestKiroRequestFromFile
func TestKiroRequestFromFile(t *testing.T) {
	// 1. Get request file path from environment
	requestFilePath := os.Getenv("KIRO_REQUEST_FILE")
	if requestFilePath == "" {
		t.Skip("KIRO_REQUEST_FILE not set, skipping test")
	}

	// 2. Connect to Redis and get healthy account
	redisClient := getRedisClient(t)
	defer redisClient.Close()

	acc, token := getHealthyAccount(t, redisClient)
	t.Logf("Using account: %s (region: %s)", acc.UUID, token.IDCRegion)

	// 3. Read request file
	reqData, err := os.ReadFile(requestFilePath)
	require.NoError(t, err, "Failed to read request file")

	// Compact JSON - dump files are pretty-printed but Kiro API may have size limits
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, reqData); err != nil {
		t.Logf("JSON compact failed (using raw): %v", err)
	} else {
		t.Logf("Compacted JSON: %d → %d bytes (%.0f%% reduction)",
			len(reqData), compacted.Len(),
			float64(len(reqData)-compacted.Len())/float64(len(reqData))*100)
		reqData = compacted.Bytes()
	}
	t.Logf("Loaded request file: %s (%d bytes)", requestFilePath, len(reqData))

	// Log request summary
	logRequestSummary(t, reqData)

	// 4. Create Kiro client with extended timeout for large requests
	client := kiro.NewClient(kiro.ClientOptions{
		MaxConns:            10,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
		Timeout:             300 * time.Second, // 5 minute timeout for large requests
		Logger:              slog.Default(),
	})
	defer client.Close()

	// 5. Build and send request
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	kiroReq := &kiro.Request{
		Region:     token.IDCRegion,
		ProfileARN: acc.ProfileARN,
		Token:      token.AccessToken,
		Body:       reqData,
	}

	t.Log("Sending request to Kiro API...")
	respBody, err := client.SendStreamingRequest(ctx, kiroReq)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer respBody.Close()

	// 6. Parse and display SSE events
	t.Log("--- SSE Response Events ---")
	parseSSEResponse(t, respBody)
}

// logRequestSummary logs a summary of the request without printing the entire body
func logRequestSummary(t *testing.T, reqData []byte) {
	var req map[string]interface{}
	if err := json.Unmarshal(reqData, &req); err != nil {
		t.Logf("Request is not valid JSON: %v", err)
		return
	}

	// Extract conversationState info
	if cs, ok := req["conversationState"].(map[string]interface{}); ok {
		if cm, ok := cs["currentMessage"].(map[string]interface{}); ok {
			if uim, ok := cm["userInputMessage"].(map[string]interface{}); ok {
				if modelId, ok := uim["modelId"].(string); ok {
					t.Logf("Model ID: %s", modelId)
				}
			}
		}

		// Count messages in conversation
		if messages, ok := cs["messages"].([]interface{}); ok {
			t.Logf("Conversation messages: %d", len(messages))
		}
	}

	// Check for tools
	if tools, ok := req["tools"].([]interface{}); ok {
		t.Logf("Tools count: %d", len(tools))
		// Log tool names
		for i, tool := range tools {
			if toolMap, ok := tool.(map[string]interface{}); ok {
				if name, ok := toolMap["name"].(string); ok {
					// Check tool description length
					descLen := 0
					if desc, ok := toolMap["description"].(string); ok {
						descLen = len(desc)
					}
					t.Logf("  Tool %d: %s (description: %d chars)", i+1, name, descLen)
				}
			}
		}
	}
}

// parseSSEResponse parses SSE events from the response and logs them
func parseSSEResponse(t *testing.T, respBody io.ReadCloser) {
	scanner := bufio.NewScanner(respBody)
	// Increase buffer size for large SSE events
	const maxScanTokenSize = 1024 * 1024 // 1MB
	buf := make([]byte, maxScanTokenSize)
	scanner.Buffer(buf, maxScanTokenSize)

	eventCount := 0
	var currentEvent string
	var currentData string
	var textContent strings.Builder
	var errorOccurred bool

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			currentData = strings.TrimPrefix(line, "data: ")
		} else if line == "" && currentEvent != "" {
			// End of event, process it
			eventCount++
			processSSEEvent(t, currentEvent, currentData, &textContent, &errorOccurred)
			currentEvent = ""
			currentData = ""
		}
	}

	if err := scanner.Err(); err != nil {
		t.Errorf("Error reading SSE stream: %v", err)
	}

	t.Log("--- End of SSE Response ---")
	t.Logf("Total events received: %d", eventCount)

	// Print accumulated text content
	if textContent.Len() > 0 {
		t.Log("--- Accumulated Text Content ---")
		content := textContent.String()
		if len(content) > 2000 {
			t.Logf("%s\n... (truncated, total %d chars)", content[:2000], len(content))
		} else {
			t.Log(content)
		}
	}

	if errorOccurred {
		t.Error("Error event detected in response")
	}
}

// processSSEEvent processes a single SSE event and logs relevant information
func processSSEEvent(t *testing.T, eventType, data string, textContent *strings.Builder, errorOccurred *bool) {
	switch eventType {
	case "message_start":
		t.Logf("[%s] Message started", eventType)
		// Parse message_start to get model info
		var msg map[string]interface{}
		if json.Unmarshal([]byte(data), &msg) == nil {
			if message, ok := msg["message"].(map[string]interface{}); ok {
				if model, ok := message["model"].(string); ok {
					t.Logf("  Model: %s", model)
				}
				if id, ok := message["id"].(string); ok {
					t.Logf("  Message ID: %s", id)
				}
			}
		}

	case "content_block_start":
		t.Logf("[%s] Content block started", eventType)
		var block map[string]interface{}
		if json.Unmarshal([]byte(data), &block) == nil {
			if cb, ok := block["content_block"].(map[string]interface{}); ok {
				if cbType, ok := cb["type"].(string); ok {
					t.Logf("  Block type: %s", cbType)
					if cbType == "tool_use" {
						if name, ok := cb["name"].(string); ok {
							t.Logf("  Tool name: %s", name)
						}
					}
				}
			}
		}

	case "content_block_delta":
		// Extract text deltas silently and accumulate
		var delta map[string]interface{}
		if json.Unmarshal([]byte(data), &delta) == nil {
			if d, ok := delta["delta"].(map[string]interface{}); ok {
				if text, ok := d["text"].(string); ok {
					textContent.WriteString(text)
				}
			}
		}

	case "content_block_stop":
		// Silent, just marks end of block

	case "message_delta":
		t.Logf("[%s] Message delta", eventType)
		var delta map[string]interface{}
		if json.Unmarshal([]byte(data), &delta) == nil {
			if d, ok := delta["delta"].(map[string]interface{}); ok {
				if stopReason, ok := d["stop_reason"].(string); ok {
					t.Logf("  Stop reason: %s", stopReason)
				}
			}
			if usage, ok := delta["usage"].(map[string]interface{}); ok {
				t.Logf("  Usage: %v", usage)
			}
		}

	case "message_stop":
		t.Logf("[%s] Message completed", eventType)

	case "error":
		*errorOccurred = true
		t.Logf("[%s] ERROR received", eventType)
		var errData map[string]interface{}
		if json.Unmarshal([]byte(data), &errData) == nil {
			if errObj, ok := errData["error"].(map[string]interface{}); ok {
				if errType, ok := errObj["type"].(string); ok {
					t.Logf("  Error type: %s", errType)
				}
				if errMsg, ok := errObj["message"].(string); ok {
					t.Logf("  Error message: %s", errMsg)
				}
			}
		} else {
			t.Logf("  Raw error data: %s", data)
		}

	case "ping":
		// Silent, just a keep-alive

	default:
		t.Logf("[%s] Unknown event type", eventType)
		if len(data) > 200 {
			t.Logf("  Data: %s... (truncated)", data[:200])
		} else {
			t.Logf("  Data: %s", data)
		}
	}
}
