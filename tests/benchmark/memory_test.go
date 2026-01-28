// Package benchmark contains performance benchmark tests.
package benchmark

import (
	"encoding/json"
	"runtime"
	"testing"

	"github.com/anthropics/AIClient-2-API/internal/claude"
	"github.com/anthropics/AIClient-2-API/internal/kiro"
)

func BenchmarkMemoryAggregator(b *testing.B) {
	// Measures memory allocation of response aggregation
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		agg := claude.NewAggregator("claude-sonnet-4")

		chunks := []*kiro.KiroChunk{
			{Type: "message_start", Message: &kiro.KiroMessage{Role: "assistant", Usage: &kiro.KiroUsage{InputTokens: 1000}}},
			{Type: "content_block_start", Index: intPtr(0), ContentBlock: &kiro.KiroContentBlock{Type: "text"}},
		}

		// Add 100 deltas to simulate real response
		for j := 0; j < 100; j++ {
			chunks = append(chunks, &kiro.KiroChunk{
				Type:  "content_block_delta",
				Index: intPtr(0),
				Delta: &kiro.KiroDelta{Type: "text_delta", Text: "Hello world, this is a test response. "},
			})
		}

		chunks = append(chunks,
			&kiro.KiroChunk{Type: "content_block_stop", Index: intPtr(0)},
			&kiro.KiroChunk{Type: "message_delta", StopReason: "end_turn", Usage: []byte(`{"output_tokens": 500}`)},
			&kiro.KiroChunk{Type: "message_stop"},
		)

		for _, chunk := range chunks {
			agg.Add(chunk)
		}

		resp := agg.Build()
		_ = resp
	}
}

func BenchmarkMemoryStabilityUnderLoad(b *testing.B) {
	// Track memory stability across iterations
	var memBefore, memAfter runtime.MemStats

	runtime.GC()
	runtime.ReadMemStats(&memBefore)

	for i := 0; i < b.N; i++ {
		// Simulate processing a request
		agg := claude.NewAggregator("claude-sonnet-4")

		chunks := createTestChunks(50) // 50 content deltas
		for _, chunk := range chunks {
			agg.Add(chunk)
		}
		resp := agg.Build()

		// Simulate JSON serialization (as in non-streaming response)
		data, _ := json.Marshal(resp)
		_ = data
	}

	runtime.GC()
	runtime.ReadMemStats(&memAfter)

	// Report memory growth
	b.ReportMetric(float64(memAfter.TotalAlloc-memBefore.TotalAlloc)/float64(b.N), "bytes/op_total")
	b.ReportMetric(float64(memAfter.HeapInuse-memBefore.HeapInuse), "heap_growth_bytes")
}

func BenchmarkMemoryTokenDistribution(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		usage := claude.DistributeTokens(10000)
		_ = usage
	}
}

func BenchmarkMemorySSEEventSerialization(b *testing.B) {
	b.ReportAllocs()

	event := claude.MessageResponse{
		ID:         "msg_test123",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-sonnet-4",
		StopReason: "end_turn",
		Content: []claude.ContentBlock{
			{Type: "text", Text: "Hello, world! This is a test response."},
		},
		Usage: claude.Usage{
			InputTokens:              357,
			OutputTokens:             142,
			CacheCreationInputTokens: 714,
			CacheReadInputTokens:     8929,
		},
	}

	for i := 0; i < b.N; i++ {
		data, _ := json.Marshal(event)
		_ = data
	}
}

// createTestChunks creates a sequence of Kiro chunks simulating a response.
func createTestChunks(deltaCount int) []*kiro.KiroChunk {
	chunks := []*kiro.KiroChunk{
		{Type: "message_start", Message: &kiro.KiroMessage{Role: "assistant", Usage: &kiro.KiroUsage{InputTokens: 500}}},
		{Type: "content_block_start", Index: intPtr(0), ContentBlock: &kiro.KiroContentBlock{Type: "text"}},
	}

	for i := 0; i < deltaCount; i++ {
		chunks = append(chunks, &kiro.KiroChunk{
			Type:  "content_block_delta",
			Index: intPtr(0),
			Delta: &kiro.KiroDelta{Type: "text_delta", Text: "word "},
		})
	}

	chunks = append(chunks,
		&kiro.KiroChunk{Type: "content_block_stop", Index: intPtr(0)},
		&kiro.KiroChunk{Type: "message_delta", StopReason: "end_turn", Usage: []byte(`{"output_tokens": 200}`)},
		&kiro.KiroChunk{Type: "message_stop"},
	)

	return chunks
}

func intPtr(i int) *int { return &i }
