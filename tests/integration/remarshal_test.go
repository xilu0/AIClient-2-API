package integration

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/anthropics/AIClient-2-API/internal/kiro"
)

// TestKiroRemarshal reads a Kiro dump, unmarshals → re-marshals (Go serialization), then sends.
// This tests whether Go's json.Marshal produces a different result than the dump file.
//
// Usage: KIRO_REQUEST_FILE=/path/to/kiro_request.json go test ./tests/integration/... -v -run TestKiroRemarshal -timeout 120s
func TestKiroRemarshal(t *testing.T) {
	requestFilePath := os.Getenv("KIRO_REQUEST_FILE")
	if requestFilePath == "" {
		t.Skip("KIRO_REQUEST_FILE not set")
	}

	redisClient := getRedisClient(t)
	defer redisClient.Close()
	acc, token := getHealthyAccount(t, redisClient)
	t.Logf("Using account: %s (region: %s)", acc.UUID, token.IDCRegion)

	// Read and parse
	raw, err := os.ReadFile(requestFilePath)
	if err != nil {
		t.Fatal(err)
	}

	var obj interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}

	// Re-marshal with Go's standard json.Marshal
	remarshaled, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}

	// Also try MarshalWithoutHTMLEscape
	remarshaledNoEscape, err := kiro.MarshalWithoutHTMLEscape(obj)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Original (compact): would be 108739 bytes")
	t.Logf("Go json.Marshal:    %d bytes", len(remarshaled))
	t.Logf("Go NoHTMLEscape:    %d bytes", len(remarshaledNoEscape))

	// Compare with json.Compact version
	// Find first diff between remarshaled and compact
	compact := make([]byte, 0, len(raw))
	var buf json.RawMessage
	json.Unmarshal(raw, &buf)
	compact, _ = json.Marshal(buf) // This effectively compacts

	if string(compact) != string(remarshaled) {
		for i := 0; i < len(compact) && i < len(remarshaled); i++ {
			if compact[i] != remarshaled[i] {
				start := i - 30
				if start < 0 { start = 0 }
				end := i + 30
				if end > len(compact) { end = len(compact) }
				if end > len(remarshaled) { end = len(remarshaled) }
				t.Logf("First diff at byte %d:", i)
				t.Logf("  Compact:     ...%s...", string(compact[start:end]))
				t.Logf("  Remarshaled: ...%s...", string(remarshaled[start:end]))
				break
			}
		}
	}

	// Save for external inspection
	os.WriteFile("/tmp/kiro_remarshaled.json", remarshaled, 0644)
	os.WriteFile("/tmp/kiro_remarshaled_noescape.json", remarshaledNoEscape, 0644)

	// Send the re-marshaled version
	client := kiro.NewClient(kiro.ClientOptions{
		MaxConns: 10, MaxIdleConnsPerHost: 5,
		IdleConnTimeout: 90 * time.Second, Timeout: 300 * time.Second,
		Logger: slog.Default(),
	})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	// Test 1: json.Marshal (with HTML escape)
	t.Log("--- Sending json.Marshal version ---")
	kiroReq := &kiro.Request{
		Region: token.IDCRegion, ProfileARN: acc.ProfileARN,
		Token: token.AccessToken, Body: remarshaled,
	}
	respBody, err := client.SendStreamingRequest(ctx, kiroReq)
	if err != nil {
		t.Logf("json.Marshal version FAILED: %v", err)
	} else {
		t.Log("json.Marshal version SUCCEEDED")
		respBody.Close()
	}

	// Test 2: MarshalWithoutHTMLEscape
	t.Log("--- Sending MarshalWithoutHTMLEscape version ---")
	kiroReq2 := &kiro.Request{
		Region: token.IDCRegion, ProfileARN: acc.ProfileARN,
		Token: token.AccessToken, Body: remarshaledNoEscape,
	}
	respBody2, err := client.SendStreamingRequest(ctx, kiroReq2)
	if err != nil {
		t.Logf("NoHTMLEscape version FAILED: %v", err)
	} else {
		t.Log("NoHTMLEscape version SUCCEEDED")
		respBody2.Close()
	}
}
