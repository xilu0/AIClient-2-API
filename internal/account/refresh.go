// Package account provides account selection and health tracking.
package account

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/anthropics/AIClient-2-API/internal/redis"
)

const (
	// DefaultRefreshThreshold is the default time before expiry to trigger refresh.
	DefaultRefreshThreshold = 5 * time.Minute
)

// TokenRefresher handles background token refresh with deduplication.
type TokenRefresher struct {
	tokenManager     *redis.TokenManager
	logger           *slog.Logger
	refreshThreshold time.Duration

	// Singleflight for deduplication
	sfGroup singleflight.Group

	// Track in-flight refreshes
	mu          sync.RWMutex
	inFlight    map[string]bool
	lastRefresh map[string]time.Time
}

// TokenRefresherOptions configures the token refresher.
type TokenRefresherOptions struct {
	TokenManager     *redis.TokenManager
	Logger           *slog.Logger
	RefreshThreshold time.Duration
}

// NewTokenRefresher creates a new token refresher.
func NewTokenRefresher(opts TokenRefresherOptions) *TokenRefresher {
	threshold := opts.RefreshThreshold
	if threshold == 0 {
		threshold = DefaultRefreshThreshold
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &TokenRefresher{
		tokenManager:     opts.TokenManager,
		logger:           logger,
		refreshThreshold: threshold,
		inFlight:         make(map[string]bool),
		lastRefresh:      make(map[string]time.Time),
	}
}

// NeedsRefresh checks if a token needs refreshing.
func (r *TokenRefresher) NeedsRefresh(token *redis.Token) bool {
	if token == nil {
		return true
	}

	// ExpiresAt is ISO 8601 timestamp string
	expiresAt, err := time.Parse(time.RFC3339, token.ExpiresAt)
	if err != nil {
		// Try alternative format
		expiresAt, err = time.Parse("2006-01-02T15:04:05.000Z", token.ExpiresAt)
		if err != nil {
			return true // Treat parse errors as needing refresh
		}
	}
	return time.Until(expiresAt) <= r.refreshThreshold
}

// TriggerBackgroundRefresh starts a background refresh if not already in progress.
// This method returns immediately without blocking.
func (r *TokenRefresher) TriggerBackgroundRefresh(ctx context.Context, uuid string, refreshFn func() error) {
	r.mu.RLock()
	if r.inFlight[uuid] {
		r.mu.RUnlock()
		return
	}
	r.mu.RUnlock()

	// Mark as in-flight
	r.mu.Lock()
	if r.inFlight[uuid] {
		r.mu.Unlock()
		return
	}
	r.inFlight[uuid] = true
	r.mu.Unlock()

	// Start background refresh
	go func() {
		defer func() {
			r.mu.Lock()
			delete(r.inFlight, uuid)
			r.lastRefresh[uuid] = time.Now()
			r.mu.Unlock()
		}()

		// Use singleflight to deduplicate concurrent calls for same UUID
		_, err, shared := r.sfGroup.Do(uuid, func() (interface{}, error) {
			r.logger.Debug("starting token refresh", "uuid", uuid)
			if err := refreshFn(); err != nil {
				r.logger.Error("token refresh failed", "uuid", uuid, "error", err)
				return nil, err
			}
			r.logger.Info("token refresh completed", "uuid", uuid)
			return nil, nil
		})

		if shared {
			r.logger.Debug("token refresh deduplicated", "uuid", uuid)
		}

		if err != nil {
			r.logger.Warn("background refresh failed", "uuid", uuid, "error", err)
		}
	}()
}

// RefreshSync performs a synchronous refresh with deduplication.
// Use this when you need the token immediately.
func (r *TokenRefresher) RefreshSync(ctx context.Context, uuid string, refreshFn func() error) error {
	_, err, shared := r.sfGroup.Do(uuid, func() (interface{}, error) {
		r.logger.Debug("starting synchronous token refresh", "uuid", uuid)
		if err := refreshFn(); err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.lastRefresh[uuid] = time.Now()
		r.mu.Unlock()
		return nil, nil
	})

	if shared {
		r.logger.Debug("synchronous refresh deduplicated", "uuid", uuid)
	}

	return err
}

// IsRefreshInProgress checks if a refresh is currently in progress for an account.
func (r *TokenRefresher) IsRefreshInProgress(uuid string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.inFlight[uuid]
}

// GetLastRefreshTime returns when the token was last refreshed.
func (r *TokenRefresher) GetLastRefreshTime(uuid string) (time.Time, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.lastRefresh[uuid]
	return t, ok
}

// ClearInFlight clears the in-flight status for an account.
// Useful for cleanup after errors.
func (r *TokenRefresher) ClearInFlight(uuid string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.inFlight, uuid)
}
