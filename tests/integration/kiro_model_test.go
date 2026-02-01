package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/anthropics/AIClient-2-API/internal/kiro"
	"github.com/anthropics/AIClient-2-API/internal/redis"
)

// getRedisClient creates a Redis client from environment
func getRedisClient(t *testing.T) *redis.Client {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}

	client, err := redis.NewClient(redis.ClientOptions{
		URL:       redisURL,
		KeyPrefix: "aiclient:",
		PoolSize:  10,
		Timeout:   10 * time.Second,
		Logger:    slog.Default(),
	})
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}

	// Connect to Redis (required before any operations)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect to Redis: %v", err)
	}

	return client
}

// getHealthyAccount gets a healthy account and its token from Redis
func getHealthyAccount(t *testing.T, redisClient *redis.Client) (*redis.Account, *redis.Token) {
	poolManager := redis.NewPoolManager(redisClient)
	tokenManager := redis.NewTokenManager(redisClient)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	accounts, err := poolManager.GetAllAccounts(ctx)
	if err != nil {
		t.Fatalf("Failed to get accounts: %v", err)
	}

	if len(accounts) == 0 {
		t.Fatal("No accounts available")
	}

	var acc *redis.Account
	for i := range accounts {
		if accounts[i].IsHealthy && !accounts[i].IsDisabled {
			acc = &accounts[i]
			break
		}
	}
	if acc == nil {
		t.Fatal("No healthy accounts available")
	}

	token, err := tokenManager.GetToken(ctx, acc.UUID)
	if err != nil {
		t.Fatalf("Failed to get token for account %s: %v", acc.UUID, err)
	}

	return acc, token
}

// TestKiroModelNames tests which model names are accepted by Kiro API
// Run with: INTEGRATION_TEST=true REDIS_URL=redis://localhost:6379 go test ./tests/integration/... -v -run TestKiroModelNames -timeout 120s
func TestKiroModelNames(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("Skipping integration test. Set INTEGRATION_TEST=true to run.")
	}

	redisClient := getRedisClient(t)
	defer redisClient.Close()

	acc, token := getHealthyAccount(t, redisClient)
	t.Logf("Using account: %s, region: %s", acc.UUID, token.IDCRegion)

	// Model names to test
	modelNames := []string{
		// Sonnet variants
		"CLAUDE_SONNET_4_5_20250929_V1_0",
		"claude-sonnet-4.5",
		"claude-sonnet-4-5",
		"claude-sonnet-4-5-20250929",

		// Haiku variants
		"CLAUDE_HAIKU_4_5_20251001_V1_0",
		"claude-haiku-4.5",
		"claude-haiku-4-5",
		"claude-haiku-4-5-20251001",

		// Opus variants
		"CLAUDE_OPUS_4_5_20251101_V1_0",
		"claude-opus-4.5",
		"claude-opus-4-5",
		"claude-opus-4-5-20251101",

		// Other formats
		"CLAUDE_3_7_SONNET_20250219_V1_0",
		"claude-3-7-sonnet-20250219",
	}

	// Simple test message
	messages := []map[string]interface{}{
		{
			"role":    "user",
			"content": "Say 'test ok' and nothing else.",
		},
	}
	messagesJSON, _ := json.Marshal(messages)

	client := kiro.NewClient(kiro.ClientOptions{
		MaxConns:            10,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
		Timeout:             60 * time.Second,
		Logger:              slog.Default(),
	})
	defer client.Close()

	for _, modelName := range modelNames {
		t.Run(modelName, func(t *testing.T) {
			// Build request with this model name
			body, _, err := kiro.BuildRequestBody(modelName, messagesJSON, 100, false, "", acc.ProfileARN, nil)
			if err != nil {
				t.Fatalf("Failed to build request: %v", err)
			}

			// Log the modelId being sent
			var req map[string]interface{}
			json.Unmarshal(body, &req)
			cs := req["conversationState"].(map[string]interface{})
			cm := cs["currentMessage"].(map[string]interface{})
			uim := cm["userInputMessage"].(map[string]interface{})
			t.Logf("Sending modelId: %v", uim["modelId"])

			// Send request
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			kiroReq := &kiro.Request{
				Region:     token.IDCRegion,
				ProfileARN: acc.ProfileARN,
				Token:      token.AccessToken,
				Body:       body,
			}

			respBody, err := client.SendStreamingRequest(ctx, kiroReq)
			if err != nil {
				t.Logf("Model %s: ✗ REJECTED: %v", modelName, err)
				return
			}
			defer respBody.Close()

			// Read first chunk to check if successful
			buf := make([]byte, 1024)
			n, err := respBody.Read(buf)
			if err != nil && err != io.EOF {
				t.Logf("Model %s: ✗ Read error: %v", modelName, err)
				return
			}

			response := string(buf[:n])
			if len(response) > 100 {
				response = response[:100] + "..."
			}
			t.Logf("Model %s: ✓ ACCEPTED, response: %s", modelName, response)
		})
	}
}

// TestKiroSingleModel tests a single model name
// Run with: INTEGRATION_TEST=true REDIS_URL=redis://localhost:6379 TEST_MODEL="claude-sonnet-4.5" go test ./tests/integration/... -v -run TestKiroSingleModel -timeout 60s
func TestKiroSingleModel(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("Skipping integration test. Set INTEGRATION_TEST=true to run.")
	}

	modelName := os.Getenv("TEST_MODEL")
	if modelName == "" {
		modelName = "CLAUDE_SONNET_4_5_20250929_V1_0"
	}

	redisClient := getRedisClient(t)
	defer redisClient.Close()

	acc, token := getHealthyAccount(t, redisClient)
	t.Logf("Using account: %s, region: %s", acc.UUID, token.IDCRegion)

	messages := []map[string]interface{}{
		{"role": "user", "content": "Say 'hello' only."},
	}
	messagesJSON, _ := json.Marshal(messages)

	body, _, err := kiro.BuildRequestBody(modelName, messagesJSON, 100, false, "", acc.ProfileARN, nil)
	if err != nil {
		t.Fatalf("Failed to build request: %v", err)
	}

	// Print the request for debugging
	var req map[string]interface{}
	json.Unmarshal(body, &req)
	prettyReq, _ := json.MarshalIndent(req, "", "  ")
	t.Logf("Request:\n%s", string(prettyReq))

	client := kiro.NewClient(kiro.ClientOptions{
		MaxConns:            10,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
		Timeout:             60 * time.Second,
		Logger:              slog.Default(),
	})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	kiroReq := &kiro.Request{
		Region:     token.IDCRegion,
		ProfileARN: acc.ProfileARN,
		Token:      token.AccessToken,
		Body:       body,
	}

	respBody, err := client.SendStreamingRequest(ctx, kiroReq)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer respBody.Close()

	// Read full response
	data, err := io.ReadAll(respBody)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	t.Logf("Response (%d bytes):\n%s", len(data), string(data))
}

// TestKiroRawRequest sends a raw request to Kiro API for debugging
// Run with: INTEGRATION_TEST=true REDIS_URL=redis://localhost:6379 KIRO_REQUEST_FILE=/path/to/kiro_request.json go test ./tests/integration/... -v -run TestKiroRawRequest
func TestKiroRawRequest(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("Skipping integration test. Set INTEGRATION_TEST=true to run.")
	}

	dumpFile := os.Getenv("KIRO_REQUEST_FILE")
	if dumpFile == "" {
		t.Skip("Set KIRO_REQUEST_FILE to a kiro_request.json file to test")
	}

	reqData, err := os.ReadFile(dumpFile)
	if err != nil {
		t.Fatalf("Failed to read dump file: %v", err)
	}

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

	redisClient := getRedisClient(t)
	defer redisClient.Close()

	acc, token := getHealthyAccount(t, redisClient)
	t.Logf("Using account: %s, region: %s", acc.UUID, token.IDCRegion)
	t.Logf("Request size: %d bytes", len(reqData))

	client := kiro.NewClient(kiro.ClientOptions{
		MaxConns:            10,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
		Timeout:             60 * time.Second,
		Logger:              slog.Default(),
	})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	kiroReq := &kiro.Request{
		Region:     token.IDCRegion,
		ProfileARN: acc.ProfileARN,
		Token:      token.AccessToken,
		Body:       reqData,
	}

	respBody, err := client.SendStreamingRequest(ctx, kiroReq)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer respBody.Close()

	data, err := io.ReadAll(respBody)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	fmt.Printf("Response (%d bytes):\n%s\n", len(data), string(data))
}
