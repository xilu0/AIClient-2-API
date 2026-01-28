// Package benchmark contains performance benchmark tests.
package benchmark

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func BenchmarkConcurrentConnections(b *testing.B) {
	// Simulate a handler that returns SSE events
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		// Write SSE events
		events := []string{
			`event: message_start` + "\ndata: " + `{"type":"message_start"}` + "\n\n",
			`event: content_block_start` + "\ndata: " + `{"type":"content_block_start","index":0}` + "\n\n",
			`event: content_block_delta` + "\ndata: " + `{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}` + "\n\n",
			`event: content_block_stop` + "\ndata: " + `{"type":"content_block_stop","index":0}` + "\n\n",
			`event: message_delta` + "\ndata: " + `{"type":"message_delta","stop_reason":"end_turn"}` + "\n\n",
			`event: message_stop` + "\ndata: " + `{"type":"message_stop"}` + "\n\n",
		}

		for _, event := range events {
			w.Write([]byte(event))
			flusher.Flush()
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			body := `{"model":"claude-sonnet-4","max_tokens":1024,"stream":true,"messages":[{"role":"user","content":"Hello"}]}`
			resp, err := http.Post(server.URL, "application/json", bytes.NewBufferString(body))
			if err != nil {
				b.Fatal(err)
			}
			resp.Body.Close()
		}
	})
}

func BenchmarkConcurrentRequestParsing(b *testing.B) {
	type messageReq struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		Stream    bool   `json:"stream"`
		Messages  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}

	body := []byte(`{"model":"claude-sonnet-4","max_tokens":1024,"stream":true,"messages":[{"role":"user","content":"Hello, world!"}]}`)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			var req messageReq
			if err := json.Unmarshal(body, &req); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkConcurrent500Connections(b *testing.B) {
	// Test 500+ concurrent connections
	var activeConns atomic.Int64
	var maxConns atomic.Int64

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := activeConns.Add(1)
		defer activeConns.Add(-1)

		// Track max concurrent
		for {
			old := maxConns.Load()
			if current <= old || maxConns.CompareAndSwap(old, current) {
				break
			}
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"type":"message","content":[{"type":"text","text":"ok"}]}`))
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	const targetConcurrency = 500

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(targetConcurrency)

		for j := 0; j < targetConcurrency; j++ {
			go func() {
				defer wg.Done()
				resp, err := http.Get(server.URL)
				if err != nil {
					return
				}
				resp.Body.Close()
			}()
		}

		wg.Wait()
	}

	b.ReportMetric(float64(maxConns.Load()), "max_concurrent")
}

func BenchmarkSSEEventWriting(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			w := httptest.NewRecorder()
			w.Header().Set("Content-Type", "text/event-stream")

			events := []struct {
				event string
				data  string
			}{
				{"message_start", `{"type":"message_start"}`},
				{"content_block_start", `{"type":"content_block_start","index":0}`},
				{"content_block_delta", `{"type":"content_block_delta"}`},
				{"content_block_stop", `{"type":"content_block_stop"}`},
				{"message_delta", `{"type":"message_delta"}`},
				{"message_stop", `{"type":"message_stop"}`},
			}

			for _, e := range events {
				w.Write([]byte("event: " + e.event + "\ndata: " + e.data + "\n\n"))
			}
		}
	})
}
