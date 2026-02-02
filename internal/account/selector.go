// Package account provides account selection and management.
package account

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/anthropics/AIClient-2-API/internal/redis"
)

var (
	// ErrNoHealthyAccounts is returned when no healthy accounts are available.
	ErrNoHealthyAccounts = errors.New("no healthy accounts available")
)

// Selector provides lock-free round-robin account selection.
type Selector struct {
	redisClient *redis.Client
	poolManager *redis.PoolManager
	logger      *slog.Logger

	// In-memory cache
	cacheMu        sync.RWMutex
	cachedAccounts []redis.Account
	cacheUpdated   time.Time
	cacheTTL       time.Duration

	// Health cooldown
	healthCooldown time.Duration
}

// SelectorOptions configures the account selector.
type SelectorOptions struct {
	RedisClient    *redis.Client
	PoolManager    *redis.PoolManager
	Logger         *slog.Logger
	CacheTTL       time.Duration
	HealthCooldown time.Duration
}

// NewSelector creates a new account selector.
func NewSelector(opts SelectorOptions) *Selector {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	cacheTTL := opts.CacheTTL
	if cacheTTL == 0 {
		cacheTTL = 5 * time.Second
	}

	healthCooldown := opts.HealthCooldown
	if healthCooldown == 0 {
		healthCooldown = 6 * time.Second
	}

	return &Selector{
		redisClient:    opts.RedisClient,
		poolManager:    opts.PoolManager,
		logger:         logger,
		cacheTTL:       cacheTTL,
		healthCooldown: healthCooldown,
	}
}

// Select selects a healthy account using lock-free round-robin.
func (s *Selector) Select(ctx context.Context) (*redis.Account, error) {
	// Get healthy accounts (cached with TTL)
	healthy, err := s.getHealthyAccounts(ctx)
	if err != nil {
		return nil, err
	}

	if len(healthy) == 0 {
		return nil, ErrNoHealthyAccounts
	}

	// Atomic increment for round-robin
	counter, err := s.redisClient.Incr(ctx, "kiro:round-robin-counter")
	if err != nil {
		// Fall back to first healthy account on Redis error
		s.logger.Warn("failed to increment counter, using first account", "error", err)
		return &healthy[0], nil
	}

	// Round-robin selection
	idx := counter % int64(len(healthy))
	selected := healthy[idx]

	s.logger.Debug("selected account",
		"uuid", selected.UUID,
		"index", idx,
		"total_healthy", len(healthy),
	)

	return &selected, nil
}

// SelectWithRetry selects an account, retrying up to maxRetries times on failure.
func (s *Selector) SelectWithRetry(ctx context.Context, maxRetries int, excluded map[string]bool) (*redis.Account, error) {
	// Get healthy accounts
	healthy, err := s.getHealthyAccounts(ctx)
	if err != nil {
		return nil, err
	}

	// Filter out excluded accounts
	var available []redis.Account
	for _, acc := range healthy {
		if !excluded[acc.UUID] {
			available = append(available, acc)
		}
	}

	if len(available) == 0 {
		return nil, ErrNoHealthyAccounts
	}

	// Atomic increment for round-robin
	counter, err := s.redisClient.Incr(ctx, "kiro:round-robin-counter")
	if err != nil {
		return &available[0], nil
	}

	idx := counter % int64(len(available))
	return &available[idx], nil
}

// getHealthyAccounts returns accounts that are healthy or eligible for retry.
func (s *Selector) getHealthyAccounts(ctx context.Context) ([]redis.Account, error) {
	// Check cache
	s.cacheMu.RLock()
	if time.Since(s.cacheUpdated) < s.cacheTTL && len(s.cachedAccounts) > 0 {
		accounts := s.filterHealthyAccounts(s.cachedAccounts)
		s.cacheMu.RUnlock()
		return accounts, nil
	}
	s.cacheMu.RUnlock()

	// Refresh cache
	accounts, err := s.poolManager.GetAllAccounts(ctx)
	if err != nil {
		// Try using stale cache on error
		s.cacheMu.RLock()
		if len(s.cachedAccounts) > 0 {
			s.logger.Warn("using stale cache due to Redis error", "error", err)
			accounts := s.filterHealthyAccounts(s.cachedAccounts)
			s.cacheMu.RUnlock()
			return accounts, nil
		}
		s.cacheMu.RUnlock()
		return nil, err
	}

	// Update cache
	s.cacheMu.Lock()
	s.cachedAccounts = accounts
	s.cacheUpdated = time.Now()
	s.cacheMu.Unlock()

	return s.filterHealthyAccounts(accounts), nil
}

// filterHealthyAccounts returns accounts that are healthy or eligible for retry.
// If no accounts pass the health filter, all non-disabled accounts are returned
// (all unhealthy == all healthy) to maximize availability.
func (s *Selector) filterHealthyAccounts(accounts []redis.Account) []redis.Account {
	var healthy []redis.Account
	var nonDisabled []redis.Account
	now := time.Now()

	for _, acc := range accounts {
		// Skip disabled accounts
		if acc.IsDisabled {
			continue
		}

		nonDisabled = append(nonDisabled, acc)

		if acc.IsHealthy {
			healthy = append(healthy, acc)
			continue
		}

		// Check if account has scheduled recovery time (e.g., quota reset)
		if acc.ScheduledRecoveryTime != "" {
			recoveryTime, err := time.Parse(time.RFC3339, acc.ScheduledRecoveryTime)
			if err == nil && now.Before(recoveryTime) {
				// Still before recovery time, skip this account
				continue
			}
			// Recovery time has passed, account is eligible for retry
			healthy = append(healthy, acc)
			continue
		}

		// Check if unhealthy account is eligible for retry (cooldown elapsed)
		if acc.LastErrorTime != "" {
			lastError, err := time.Parse(time.RFC3339, acc.LastErrorTime)
			if err == nil && now.Sub(lastError) >= s.healthCooldown {
				// Eligible for retry
				healthy = append(healthy, acc)
			}
		}
	}

	// If no healthy accounts found, use all non-disabled accounts
	// All unhealthy == all healthy: maximize availability
	if len(healthy) == 0 && len(nonDisabled) > 0 {
		s.logger.Warn("no healthy accounts after filtering, using all non-disabled accounts as fallback",
			"total", len(accounts),
			"non_disabled", len(nonDisabled),
		)
		return nonDisabled
	}

	return healthy
}

// RefreshCache forces a cache refresh.
func (s *Selector) RefreshCache(ctx context.Context) error {
	accounts, err := s.poolManager.GetAllAccounts(ctx)
	if err != nil {
		return err
	}

	s.cacheMu.Lock()
	s.cachedAccounts = accounts
	s.cacheUpdated = time.Now()
	s.cacheMu.Unlock()

	return nil
}

// GetAccountCount returns the total number of accounts.
func (s *Selector) GetAccountCount(ctx context.Context) (total int, healthy int, err error) {
	accounts, err := s.getHealthyAccounts(ctx)
	if err != nil {
		return 0, 0, err
	}

	s.cacheMu.RLock()
	total = len(s.cachedAccounts)
	s.cacheMu.RUnlock()

	healthy = len(accounts)
	return total, healthy, nil
}
