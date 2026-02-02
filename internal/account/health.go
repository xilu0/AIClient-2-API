// Package account provides health tracking for Kiro accounts.
package account

import (
	"context"
	"time"

	"github.com/anthropics/AIClient-2-API/internal/redis"
)

// HealthTracker manages account health status.
type HealthTracker struct {
	poolManager    *redis.PoolManager
	cooldownPeriod time.Duration
}

// NewHealthTracker creates a new health tracker.
func NewHealthTracker(poolManager *redis.PoolManager, cooldownPeriod time.Duration) *HealthTracker {
	if cooldownPeriod == 0 {
		cooldownPeriod = 6 * time.Second
	}
	return &HealthTracker{
		poolManager:    poolManager,
		cooldownPeriod: cooldownPeriod,
	}
}

// MarkUnhealthy marks an account as unhealthy due to an error.
func (h *HealthTracker) MarkUnhealthy(ctx context.Context, uuid string) error {
	return h.poolManager.MarkUnhealthy(ctx, uuid)
}

// MarkHealthy marks an account as healthy after a successful request.
func (h *HealthTracker) MarkHealthy(ctx context.Context, uuid string) error {
	return h.poolManager.MarkHealthy(ctx, uuid)
}

// IsEligibleForRetry checks if an unhealthy account is eligible for retry.
// An account is eligible if:
// - It is healthy, OR
// - It is unhealthy but the cooldown period has elapsed
func (h *HealthTracker) IsEligibleForRetry(account *redis.Account) bool {
	if account == nil {
		return false
	}

	// Healthy accounts are always eligible
	if account.IsHealthy {
		return true
	}

	// Check cooldown for unhealthy accounts
	if account.LastErrorTime == "" {
		// No error time recorded, treat as eligible
		return true
	}

	lastError, err := time.Parse(time.RFC3339, account.LastErrorTime)
	if err != nil {
		// Can't parse time, treat as eligible
		return true
	}

	return time.Since(lastError) >= h.cooldownPeriod
}

// GetCooldownRemaining returns the remaining cooldown time for an account.
// Returns 0 if the account is healthy or the cooldown has elapsed.
func (h *HealthTracker) GetCooldownRemaining(account *redis.Account) time.Duration {
	if account == nil || account.IsHealthy {
		return 0
	}

	if account.LastErrorTime == "" {
		return 0
	}

	lastError, err := time.Parse(time.RFC3339, account.LastErrorTime)
	if err != nil {
		return 0
	}

	elapsed := time.Since(lastError)
	if elapsed >= h.cooldownPeriod {
		return 0
	}

	return h.cooldownPeriod - elapsed
}

// RecordSuccess records a successful request for an account.
// This marks the account as healthy and increments usage in a single atomic operation.
func (h *HealthTracker) RecordSuccess(ctx context.Context, uuid string) error {
	// Use atomic operation to reduce Redis round-trips and contention
	return h.poolManager.RecordSuccessAtomic(ctx, uuid)
}

// RecordError records a failed request for an account.
// This marks the account as unhealthy and increments error count.
func (h *HealthTracker) RecordError(ctx context.Context, uuid string) error {
	return h.poolManager.MarkUnhealthy(ctx, uuid)
}
