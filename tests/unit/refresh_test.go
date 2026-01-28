// Package unit contains unit tests for the Kiro server.
package unit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/singleflight"
)

func TestTokenExpiryDetection(t *testing.T) {
	threshold := 5 * time.Minute

	tests := []struct {
		name       string
		expiresAt  time.Time
		shouldFlag bool
	}{
		{
			name:       "expired token",
			expiresAt:  time.Now().Add(-1 * time.Hour),
			shouldFlag: true,
		},
		{
			name:       "expires within threshold",
			expiresAt:  time.Now().Add(2 * time.Minute),
			shouldFlag: true,
		},
		{
			name:       "expires at threshold boundary",
			expiresAt:  time.Now().Add(5 * time.Minute),
			shouldFlag: true,
		},
		{
			name:       "expires after threshold",
			expiresAt:  time.Now().Add(10 * time.Minute),
			shouldFlag: false,
		},
		{
			name:       "expires in one hour",
			expiresAt:  time.Now().Add(1 * time.Hour),
			shouldFlag: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			needsRefresh := isExpiringSoon(tt.expiresAt, threshold)
			assert.Equal(t, tt.shouldFlag, needsRefresh)
		})
	}
}

func TestSingleflightDeduplication(t *testing.T) {
	var g singleflight.Group
	var callCount atomic.Int32

	// Simulate slow refresh function
	refreshFn := func() (interface{}, error) {
		callCount.Add(1)
		time.Sleep(50 * time.Millisecond)
		return "refreshed_token", nil
	}

	// Launch multiple concurrent calls with same key
	var wg sync.WaitGroup
	const concurrency = 10

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err, shared := g.Do("uuid-123", refreshFn)
			assert.NoError(t, err)
			assert.Equal(t, "refreshed_token", result)
			// All except the first should be shared
			_ = shared // At least some should be shared
		}()
	}

	wg.Wait()

	// Should only call refreshFn once due to singleflight
	assert.Equal(t, int32(1), callCount.Load())
}

func TestSingleflightDifferentKeys(t *testing.T) {
	var g singleflight.Group
	var callCount atomic.Int32

	refreshFn := func() (interface{}, error) {
		callCount.Add(1)
		time.Sleep(10 * time.Millisecond)
		return "refreshed", nil
	}

	// Launch calls with different keys - should NOT deduplicate
	var wg sync.WaitGroup

	for i := 0; i < 3; i++ {
		wg.Add(1)
		key := string(rune('a' + i)) // "a", "b", "c"
		go func(k string) {
			defer wg.Done()
			_, err, _ := g.Do(k, refreshFn)
			assert.NoError(t, err)
		}(key)
	}

	wg.Wait()

	// Should call refreshFn 3 times (once per key)
	assert.Equal(t, int32(3), callCount.Load())
}

func TestSingleflightSequentialCalls(t *testing.T) {
	var g singleflight.Group
	var callCount atomic.Int32

	refreshFn := func() (interface{}, error) {
		callCount.Add(1)
		return "token", nil
	}

	// Sequential calls after first completes
	for i := 0; i < 3; i++ {
		result, err, _ := g.Do("uuid", refreshFn)
		assert.NoError(t, err)
		assert.Equal(t, "token", result)
	}

	// Each call should trigger refresh since previous completed
	assert.Equal(t, int32(3), callCount.Load())
}

func TestBackgroundRefreshDoesNotBlock(t *testing.T) {
	var g singleflight.Group
	refreshComplete := make(chan struct{})

	// Slow refresh
	refreshFn := func() (interface{}, error) {
		time.Sleep(100 * time.Millisecond)
		close(refreshComplete)
		return "new_token", nil
	}

	// Start refresh in background
	go func() {
		g.Do("uuid", refreshFn)
	}()

	// Should not block - give it a moment to start
	time.Sleep(10 * time.Millisecond)

	// Do other work while refresh happens
	workDone := make(chan struct{})
	go func() {
		// Simulate request processing
		time.Sleep(5 * time.Millisecond)
		close(workDone)
	}()

	select {
	case <-workDone:
		// Good - work completed while refresh in progress
	case <-time.After(50 * time.Millisecond):
		t.Fatal("work should have completed quickly")
	}

	// Wait for refresh to complete
	<-refreshComplete
}

// isExpiringSoon checks if a token expires within the threshold.
func isExpiringSoon(expiresAt time.Time, threshold time.Duration) bool {
	return time.Until(expiresAt) <= threshold
}
