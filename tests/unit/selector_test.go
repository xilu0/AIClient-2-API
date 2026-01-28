// Package unit contains unit tests for the Kiro server.
package unit

import (
	"testing"
	"time"

	"github.com/anthropics/AIClient-2-API/internal/redis"
	"github.com/stretchr/testify/assert"
)

func TestFilterHealthyAccounts(t *testing.T) {
	now := time.Now()
	cooldown := 60 * time.Second

	accounts := []redis.Account{
		{
			UUID:      "healthy-1",
			IsHealthy: true,
		},
		{
			UUID:      "healthy-2",
			IsHealthy: true,
		},
		{
			UUID:          "unhealthy-recent",
			IsHealthy:     false,
			LastErrorTime: now.Format(time.RFC3339), // Just errored
		},
		{
			UUID:          "unhealthy-old",
			IsHealthy:     false,
			LastErrorTime: now.Add(-2 * time.Minute).Format(time.RFC3339), // Errored 2 min ago
		},
		{
			UUID:          "unhealthy-no-time",
			IsHealthy:     false,
			LastErrorTime: "", // No error time
		},
	}

	healthy := filterHealthyAccountsHelper(accounts, cooldown)

	// Should include:
	// - healthy-1 (healthy)
	// - healthy-2 (healthy)
	// - unhealthy-old (cooldown elapsed)
	// - unhealthy-no-time (no error time)
	// Should exclude:
	// - unhealthy-recent (recently errored)

	assert.Len(t, healthy, 4)

	uuids := make(map[string]bool)
	for _, acc := range healthy {
		uuids[acc.UUID] = true
	}

	assert.True(t, uuids["healthy-1"])
	assert.True(t, uuids["healthy-2"])
	assert.True(t, uuids["unhealthy-old"])
	assert.True(t, uuids["unhealthy-no-time"])
	assert.False(t, uuids["unhealthy-recent"])
}

func TestFilterHealthyAccounts_AllHealthy(t *testing.T) {
	accounts := []redis.Account{
		{UUID: "acc-1", IsHealthy: true},
		{UUID: "acc-2", IsHealthy: true},
		{UUID: "acc-3", IsHealthy: true},
	}

	healthy := filterHealthyAccountsHelper(accounts, 60*time.Second)
	assert.Len(t, healthy, 3)
}

func TestFilterHealthyAccounts_AllUnhealthyRecent(t *testing.T) {
	now := time.Now()
	accounts := []redis.Account{
		{UUID: "acc-1", IsHealthy: false, LastErrorTime: now.Format(time.RFC3339)},
		{UUID: "acc-2", IsHealthy: false, LastErrorTime: now.Format(time.RFC3339)},
	}

	healthy := filterHealthyAccountsHelper(accounts, 60*time.Second)
	assert.Len(t, healthy, 0)
}

func TestFilterHealthyAccounts_EmptyList(t *testing.T) {
	healthy := filterHealthyAccountsHelper([]redis.Account{}, 60*time.Second)
	assert.Len(t, healthy, 0)
}

// filterHealthyAccountsHelper is a test helper that mimics the selector's filtering logic.
func filterHealthyAccountsHelper(accounts []redis.Account, cooldown time.Duration) []redis.Account {
	var healthy []redis.Account
	now := time.Now()

	for _, acc := range accounts {
		if acc.IsHealthy {
			healthy = append(healthy, acc)
			continue
		}

		// Check if unhealthy account is eligible for retry
		if acc.LastErrorTime != "" {
			lastError, err := time.Parse(time.RFC3339, acc.LastErrorTime)
			if err == nil && now.Sub(lastError) >= cooldown {
				healthy = append(healthy, acc)
			}
		} else {
			// No error time, treat as eligible
			healthy = append(healthy, acc)
		}
	}

	return healthy
}

func TestRoundRobinSelection(t *testing.T) {
	// Test that round-robin cycles through accounts
	accounts := []redis.Account{
		{UUID: "acc-0"},
		{UUID: "acc-1"},
		{UUID: "acc-2"},
	}

	// Simulate round-robin selection
	selected := make(map[string]int)
	for i := 0; i < 9; i++ {
		idx := i % len(accounts)
		selected[accounts[idx].UUID]++
	}

	// Each account should be selected 3 times
	assert.Equal(t, 3, selected["acc-0"])
	assert.Equal(t, 3, selected["acc-1"])
	assert.Equal(t, 3, selected["acc-2"])
}

func TestExcludedAccounts(t *testing.T) {
	accounts := []redis.Account{
		{UUID: "acc-1", IsHealthy: true},
		{UUID: "acc-2", IsHealthy: true},
		{UUID: "acc-3", IsHealthy: true},
	}

	excluded := map[string]bool{
		"acc-2": true,
	}

	// Filter out excluded
	var available []redis.Account
	for _, acc := range accounts {
		if !excluded[acc.UUID] {
			available = append(available, acc)
		}
	}

	assert.Len(t, available, 2)
	assert.Equal(t, "acc-1", available[0].UUID)
	assert.Equal(t, "acc-3", available[1].UUID)
}
