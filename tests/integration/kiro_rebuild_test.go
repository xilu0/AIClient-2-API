package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/anthropics/AIClient-2-API/internal/kiro"
	"github.com/stretchr/testify/require"
)

// TestKiroRebuildAndSend reads the original Claude API request.json, rebuilds
// the Kiro request using Go's BuildRequestBody (like the Go service does),
// and sends it directly to Kiro API.
//
// This confirms whether BuildRequestBody produces a valid request.
// Compare with TestKiroRequestFromFile which sends the pre-built kiro_request.json dump.
//
// Usage:
//
//	KIRO_ORIGINAL_REQUEST=/path/to/request.json go test ./tests/integration/... -v -run TestKiroRebuildAndSend -timeout 120s
func TestKiroRebuildAndSend(t *testing.T) {
	requestFilePath := os.Getenv("KIRO_ORIGINAL_REQUEST")
	if requestFilePath == "" {
		t.Skip("KIRO_ORIGINAL_REQUEST not set")
	}

	// Read original Claude API request
	rawData, err := os.ReadFile(requestFilePath)
	require.NoError(t, err)

	var claudeReq struct {
		Model     string            `json:"model"`
		Messages  json.RawMessage   `json:"messages"`
		MaxTokens int               `json:"max_tokens"`
		Stream    bool              `json:"stream"`
		System    json.RawMessage   `json:"system"`
		Tools     json.RawMessage   `json:"tools"`
		Thinking  json.RawMessage   `json:"thinking"`
	}
	require.NoError(t, json.Unmarshal(rawData, &claudeReq))
	t.Logf("Original request: model=%s max_tokens=%d stream=%v", claudeReq.Model, claudeReq.MaxTokens, claudeReq.Stream)

	// Count tools
	if claudeReq.Tools != nil {
		var tools []interface{}
		json.Unmarshal(claudeReq.Tools, &tools)
		t.Logf("Tools count: %d", len(tools))
	}

	// Get system prompt as string
	system := ""
	if claudeReq.System != nil {
		// Try as string first
		var sysStr string
		if err := json.Unmarshal(claudeReq.System, &sysStr); err != nil {
			// Try as array of content blocks
			var sysBlocks []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(claudeReq.System, &sysBlocks); err == nil {
				for _, block := range sysBlocks {
					if block.Type == "text" {
						if system != "" {
							system += "\n"
						}
						system += block.Text
					}
				}
			}
		} else {
			system = sysStr
		}
		t.Logf("System prompt: %d chars", len(system))
	}

	// Get account
	redisClient := getRedisClient(t)
	defer redisClient.Close()
	acc, token := getHealthyAccount(t, redisClient)
	t.Logf("Using account: %s (region: %s)", acc.UUID, token.IDCRegion)

	// Build Kiro request using Go's BuildRequestBody (same as Go service)
	body, metadata, err := kiro.BuildRequestBody(
		claudeReq.Model,
		claudeReq.Messages,
		claudeReq.MaxTokens,
		claudeReq.Stream,
		system,
		acc.ProfileARN,
		claudeReq.Tools,
	)
	require.NoError(t, err, "BuildRequestBody failed")
	t.Logf("Built Kiro request: %d bytes", len(body))
	t.Logf("Metadata: %v", metadata)

	// Save rebuilt request for comparison
	os.WriteFile("/tmp/kiro_rebuilt_request.json", body, 0644)
	t.Log("Saved rebuilt request to /tmp/kiro_rebuilt_request.json")

	// Also save a pretty-printed version
	var pretty bytes.Buffer
	json.Indent(&pretty, body, "", "  ")
	os.WriteFile("/tmp/kiro_rebuilt_request_pretty.json", pretty.Bytes(), 0644)

	// Send to Kiro API
	client := kiro.NewClient(kiro.ClientOptions{
		MaxConns: 10, MaxIdleConnsPerHost: 5,
		IdleConnTimeout: 90 * time.Second, Timeout: 300 * time.Second,
		Logger: slog.Default(),
	})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	kiroReq := &kiro.Request{
		Region:     token.IDCRegion,
		ProfileARN: acc.ProfileARN,
		Token:      token.AccessToken,
		Body:       body,
	}

	t.Log("Sending rebuilt request to Kiro API...")
	respBody, err := client.SendStreamingRequest(ctx, kiroReq)
	if err != nil {
		t.Logf("❌ Rebuilt request FAILED: %v", err)

		// Now also check if the dump file exists and compare
		dumpDir := os.Getenv("KIRO_DUMP_DIR")
		if dumpDir != "" {
			dumpPath := dumpDir + "/kiro_request.json"
			dumpData, readErr := os.ReadFile(dumpPath)
			if readErr == nil {
				// Compact dump for comparison
				var compacted bytes.Buffer
				json.Compact(&compacted, dumpData)

				t.Logf("Dump size (compact): %d bytes", compacted.Len())
				t.Logf("Rebuilt size:        %d bytes", len(body))

				if bytes.Equal(compacted.Bytes(), body) {
					t.Log("Rebuilt request is IDENTICAL to dump")
				} else {
					t.Log("Rebuilt request DIFFERS from dump — finding first diff...")
					findFirstDiff(t, compacted.Bytes(), body, "dump", "rebuilt")
				}
			}
		}
		t.Fatal("Request failed")
	}
	defer respBody.Close()

	t.Log("✅ Rebuilt request SUCCEEDED")
	parseSSEResponse(t, respBody)
}

func findFirstDiff(t *testing.T, a, b []byte, nameA, nameB string) {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}

	for i := 0; i < minLen; i++ {
		if a[i] != b[i] {
			start := i - 50
			if start < 0 {
				start = 0
			}
			end := i + 50
			if end > len(a) {
				end = len(a)
			}
			endB := i + 50
			if endB > len(b) {
				endB = len(b)
			}
			t.Logf("First diff at byte %d:", i)
			t.Logf("  %s: ...%s...", nameA, string(a[start:end]))
			t.Logf("  %s: ...%s...", nameB, string(b[start:end]))
			return
		}
	}

	if len(a) != len(b) {
		t.Logf("Same content for first %d bytes, but sizes differ: %s=%d, %s=%d", minLen, nameA, len(a), nameB, len(b))
	}
}
