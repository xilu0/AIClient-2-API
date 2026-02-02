// Package unit contains unit tests for the Kiro server.
package unit

import (
	"testing"
	"time"

	"github.com/anthropics/AIClient-2-API/internal/account"
	"github.com/anthropics/AIClient-2-API/internal/redis"
	"github.com/stretchr/testify/assert"
)

func TestIsEligibleForRetry_HealthyAccount(t *testing.T) {
	tracker := account.NewHealthTracker(nil, 60*time.Second)

	acc := &redis.Account{
		UUID:      "test-uuid",
		IsHealthy: true,
	}

	assert.True(t, tracker.IsEligibleForRetry(acc))
}

func TestIsEligibleForRetry_UnhealthyWithCooldown(t *testing.T) {
	tracker := account.NewHealthTracker(nil, 60*time.Second)

	// Account that just errored (not eligible)
	acc := &redis.Account{
		UUID:          "test-uuid",
		IsHealthy:     false,
		LastErrorTime: time.Now().Format(time.RFC3339),
	}

	assert.False(t, tracker.IsEligibleForRetry(acc))
}

func TestIsEligibleForRetry_UnhealthyCooldownElapsed(t *testing.T) {
	tracker := account.NewHealthTracker(nil, 60*time.Second)

	// Account that errored 2 minutes ago (eligible)
	acc := &redis.Account{
		UUID:          "test-uuid",
		IsHealthy:     false,
		LastErrorTime: time.Now().Add(-2 * time.Minute).Format(time.RFC3339),
	}

	assert.True(t, tracker.IsEligibleForRetry(acc))
}

func TestIsEligibleForRetry_UnhealthyNoErrorTime(t *testing.T) {
	tracker := account.NewHealthTracker(nil, 60*time.Second)

	// Account unhealthy but no error time (treat as eligible)
	acc := &redis.Account{
		UUID:          "test-uuid",
		IsHealthy:     false,
		LastErrorTime: "",
	}

	assert.True(t, tracker.IsEligibleForRetry(acc))
}

func TestIsEligibleForRetry_NilAccount(t *testing.T) {
	tracker := account.NewHealthTracker(nil, 60*time.Second)

	assert.False(t, tracker.IsEligibleForRetry(nil))
}

func TestGetCooldownRemaining_HealthyAccount(t *testing.T) {
	tracker := account.NewHealthTracker(nil, 60*time.Second)

	acc := &redis.Account{
		UUID:      "test-uuid",
		IsHealthy: true,
	}

	remaining := tracker.GetCooldownRemaining(acc)
	assert.Equal(t, time.Duration(0), remaining)
}

func TestGetCooldownRemaining_UnhealthyWithRemaining(t *testing.T) {
	tracker := account.NewHealthTracker(nil, 60*time.Second)

	// Account that errored 30 seconds ago
	acc := &redis.Account{
		UUID:          "test-uuid",
		IsHealthy:     false,
		LastErrorTime: time.Now().Add(-30 * time.Second).Format(time.RFC3339),
	}

	remaining := tracker.GetCooldownRemaining(acc)
	// Should be approximately 30 seconds remaining
	assert.InDelta(t, 30*time.Second, remaining, float64(2*time.Second))
}

func TestGetCooldownRemaining_CooldownElapsed(t *testing.T) {
	tracker := account.NewHealthTracker(nil, 60*time.Second)

	// Account that errored 2 minutes ago
	acc := &redis.Account{
		UUID:          "test-uuid",
		IsHealthy:     false,
		LastErrorTime: time.Now().Add(-2 * time.Minute).Format(time.RFC3339),
	}

	remaining := tracker.GetCooldownRemaining(acc)
	assert.Equal(t, time.Duration(0), remaining)
}

func TestDefaultCooldownPeriod(t *testing.T) {
	// When cooldown is 0, should use 6 seconds default
	tracker := account.NewHealthTracker(nil, 0)

	acc := &redis.Account{
		UUID:          "test-uuid",
		IsHealthy:     false,
		LastErrorTime: time.Now().Add(-3 * time.Second).Format(time.RFC3339),
	}

	// 3 seconds elapsed out of 6, so should not be eligible
	assert.False(t, tracker.IsEligibleForRetry(acc))

	// 3 seconds remaining
	remaining := tracker.GetCooldownRemaining(acc)
	assert.InDelta(t, 3*time.Second, remaining, float64(2*time.Second))
}

func TestIsEligibleForRetry_InvalidTimeFormat(t *testing.T) {
	tracker := account.NewHealthTracker(nil, 60*time.Second)

	// Account with invalid time format (treat as eligible)
	acc := &redis.Account{
		UUID:          "test-uuid",
		IsHealthy:     false,
		LastErrorTime: "invalid-time-format",
	}

	assert.True(t, tracker.IsEligibleForRetry(acc))
}
