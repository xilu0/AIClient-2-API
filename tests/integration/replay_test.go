// Package integration provides integration tests for the Kiro service.
// KIRO_ERRORS_DIR=/root/src/AIClient-2-API/kiro-debug/errors/b0a8b1ac-4671-42af-ba30-26cc04a05ead go test ./tests/integration/... -v -run TestReplayAllErrors -timeout 600s
package integration

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	// Command line flags
	dumpDir    = flag.String("dump-dir", "", "Directory containing debug dump (e.g., /tmp/kiro-debug/errors/xxx)")
	dumpFile   = flag.String("dump-file", "", "Path to a specific request.json file to replay")
	serviceURL = flag.String("service-url", "http://localhost:8081", "Kiro service URL")
	apiKey     = flag.String("api-key", "AI_club2026", "API key for authentication")
	timeout    = flag.Duration("timeout", 60*time.Second, "Request timeout")
	verbose    = flag.Bool("verbose", false, "Print verbose output")
)

// ReplayResult contains the result of replaying a request
type ReplayResult struct {
	DumpPath     string
	OriginalErr  string
	ReplayStatus int
	ReplayErr    string
	Success      bool
	ResponseBody string
}

// TestReplayDump replays a single debug dump directory
func TestReplayDump(t *testing.T) {
	if *dumpDir == "" && *dumpFile == "" {
		t.Skip("No dump specified. Use -dump-dir or -dump-file flag")
	}

	var requestPath string
	var metadataPath string

	if *dumpFile != "" {
		requestPath = *dumpFile
		metadataPath = filepath.Join(filepath.Dir(*dumpFile), "metadata.json")
	} else {
		requestPath = filepath.Join(*dumpDir, "request.json")
		metadataPath = filepath.Join(*dumpDir, "metadata.json")
	}

	// Load original metadata if exists
	var originalErr string
	if metaData, err := os.ReadFile(metadataPath); err == nil {
		var meta map[string]interface{}
		if json.Unmarshal(metaData, &meta) == nil {
			if errStr, ok := meta["error"].(string); ok {
				originalErr = errStr
			}
		}
		if *verbose {
			t.Logf("Original error: %s", originalErr)
		}
	}

	// Load request
	reqData, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("Failed to read request file %s: %v", requestPath, err)
	}

	// Replay the request
	result := replayRequest(t, reqData, originalErr)

	if result.Success {
		t.Logf("✅ Replay successful! Original error was: %s", originalErr)
	} else {
		t.Errorf("❌ Replay failed with status %d: %s", result.ReplayStatus, result.ReplayErr)
		if *verbose && result.ResponseBody != "" {
			t.Logf("Response body: %s", truncate(result.ResponseBody, 500))
		}
	}
}

// TestReplayAllErrors replays all error dumps in a directory
func TestReplayAllErrors(t *testing.T) {
	errorsDir := os.Getenv("KIRO_ERRORS_DIR")
	if errorsDir == "" {
		t.Skip("Set KIRO_ERRORS_DIR environment variable to replay all errors")
	}

	entries, err := os.ReadDir(errorsDir)
	if err != nil {
		t.Fatalf("Failed to read errors directory: %v", err)
	}

	var results []ReplayResult
	successCount := 0
	failCount := 0

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dumpPath := filepath.Join(errorsDir, entry.Name())
		requestPath := filepath.Join(dumpPath, "request.json")
		metadataPath := filepath.Join(dumpPath, "metadata.json")

		// Skip if no request.json
		if _, err := os.Stat(requestPath); os.IsNotExist(err) {
			continue
		}

		t.Run(entry.Name(), func(t *testing.T) {
			// Load original metadata
			var originalErr string
			if metaData, err := os.ReadFile(metadataPath); err == nil {
				var meta map[string]interface{}
				if json.Unmarshal(metaData, &meta) == nil {
					if errStr, ok := meta["error"].(string); ok {
						originalErr = errStr
					}
				}
			}

			// Load request
			t.Logf("request file %s", requestPath)
			reqData, err := os.ReadFile(requestPath)
			if err != nil {
				t.Errorf("Failed to read request: %v", err)
				return
			}

			result := replayRequest(t, reqData, originalErr)
			result.DumpPath = dumpPath
			results = append(results, result)

			if result.Success {
				successCount++
				t.Logf("✅ Fixed: %s", entry.Name())
			} else {
				failCount++
				t.Errorf("❌ Still failing: %s - %s", entry.Name(), result.ReplayErr)
			}
		})
	}

	t.Logf("\n=== Summary ===")
	t.Logf("Total: %d, Success: %d, Failed: %d", len(results), successCount, failCount)
}

// replayRequest sends a request to the service and checks the result
func replayRequest(t *testing.T, reqData []byte, originalErr string) ReplayResult {
	result := ReplayResult{
		OriginalErr: originalErr,
	}

	// Parse request to check model
	var req map[string]interface{}
	if err := json.Unmarshal(reqData, &req); err != nil {
		result.ReplayErr = fmt.Sprintf("Failed to parse request: %v", err)
		return result
	}

	if *verbose {
		model, _ := req["model"].(string)
		messages, _ := req["messages"].([]interface{})
		t.Logf("Replaying request: model=%s, messages=%d", model, len(messages))
	}

	// Create HTTP request
	httpReq, err := http.NewRequest("POST", *serviceURL+"/v1/messages", bytes.NewReader(reqData))
	if err != nil {
		result.ReplayErr = fmt.Sprintf("Failed to create request: %v", err)
		return result
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", *apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	// Send request
	client := &http.Client{Timeout: *timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		result.ReplayErr = fmt.Sprintf("Request failed: %v", err)
		return result
	}
	defer resp.Body.Close()

	result.ReplayStatus = resp.StatusCode

	// Read response body
	bodyBytes, _ := io.ReadAll(resp.Body)
	result.ResponseBody = string(bodyBytes)

	// Check for success
	// For streaming responses, check if we get event: message_start
	// For non-streaming, check for 200 status
	if resp.StatusCode == http.StatusOK {
		// Check if it's a streaming response with actual content
		if strings.Contains(result.ResponseBody, "event: message_start") ||
			strings.Contains(result.ResponseBody, `"type":"message"`) {
			result.Success = true
			return result
		}
		// Check for error events in SSE stream
		if strings.Contains(result.ResponseBody, "event: error") {
			// Extract error message
			lines := strings.Split(result.ResponseBody, "\n")
			for i, line := range lines {
				if line == "event: error" && i+1 < len(lines) {
					dataLine := lines[i+1]
					if strings.HasPrefix(dataLine, "data: ") {
						var errData map[string]interface{}
						if json.Unmarshal([]byte(strings.TrimPrefix(dataLine, "data: ")), &errData) == nil {
							if errObj, ok := errData["error"].(map[string]interface{}); ok {
								if msg, ok := errObj["message"].(string); ok {
									result.ReplayErr = msg
								}
							}
						}
					}
				}
			}
			if result.ReplayErr == "" {
				result.ReplayErr = "Unknown error in SSE stream"
			}
			return result
		}
		result.Success = true
	} else {
		result.ReplayErr = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncate(result.ResponseBody, 200))
	}

	return result
}

// truncate truncates a string to maxLen characters
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// TestReplaySpecificError is a helper to replay a specific error by ID
func TestReplaySpecificError(t *testing.T) {
	errorID := os.Getenv("KIRO_ERROR_ID")
	baseDir := os.Getenv("KIRO_DEBUG_DIR")
	if errorID == "" {
		t.Skip("Set KIRO_ERROR_ID environment variable")
	}
	if baseDir == "" {
		baseDir = "/tmp/kiro-debug"
	}

	dumpPath := filepath.Join(baseDir, "errors", errorID)
	requestPath := filepath.Join(dumpPath, "request.json")
	metadataPath := filepath.Join(dumpPath, "metadata.json")

	// Load original metadata
	var originalErr string
	if metaData, err := os.ReadFile(metadataPath); err == nil {
		var meta map[string]interface{}
		if json.Unmarshal(metaData, &meta) == nil {
			if errStr, ok := meta["error"].(string); ok {
				originalErr = errStr
			}
		}
		t.Logf("Original error: %s", originalErr)
	}

	// Load request
	reqData, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("Failed to read request: %v", err)
	}

	result := replayRequest(t, reqData, originalErr)

	if result.Success {
		t.Logf("✅ Error %s is now fixed!", errorID)
	} else {
		t.Errorf("❌ Error %s still fails: %s", errorID, result.ReplayErr)
	}
}
