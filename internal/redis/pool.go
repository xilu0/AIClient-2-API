// Package redis provides provider pool operations for Kiro accounts.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// txFailedErr is copied from go-redis for comparison
var txFailedErr = goredis.TxFailedErr

const (
	// PoolKey is the Redis hash key for the Kiro OAuth provider pool.
	PoolKey = "pools:claude-kiro-oauth"
)

// PoolManager handles provider pool operations.
type PoolManager struct {
	client *Client
}

// NewPoolManager creates a new pool manager.
func NewPoolManager(client *Client) *PoolManager {
	return &PoolManager{client: client}
}

// GetAllAccounts retrieves all accounts from the provider pool.
func (pm *PoolManager) GetAllAccounts(ctx context.Context) ([]Account, error) {
	data, err := pm.client.HGetAll(ctx, PoolKey)
	if err != nil {
		// Try cache on error
		if cached, cacheTime := pm.client.GetCachedAccounts(); len(cached) > 0 {
			pm.client.logger.Warn("using cached accounts due to Redis error",
				"error", err,
				"cache_age", time.Since(cacheTime).String(),
			)
			accounts := make([]Account, 0, len(cached))
			for _, acc := range cached {
				accounts = append(accounts, acc)
			}
			return accounts, nil
		}
		return nil, fmt.Errorf("failed to get accounts: %w", err)
	}

	accounts := make([]Account, 0, len(data))
	accountMap := make(map[string]Account, len(data))

	for uuid, jsonStr := range data {
		var account Account
		if err := json.Unmarshal([]byte(jsonStr), &account); err != nil {
			pm.client.logger.Warn("failed to parse account", "uuid", uuid, "error", err)
			continue
		}
		accounts = append(accounts, account)
		accountMap[uuid] = account
	}

	// Update cache
	pm.client.UpdateAccountCache(accountMap)

	return accounts, nil
}

// GetAccount retrieves a single account by UUID.
func (pm *PoolManager) GetAccount(ctx context.Context, uuid string) (*Account, error) {
	data, err := pm.client.HGet(ctx, PoolKey, uuid)
	if err != nil {
		// Try cache on error
		if cached, _ := pm.client.GetCachedAccounts(); len(cached) > 0 {
			if acc, ok := cached[uuid]; ok {
				return &acc, nil
			}
		}
		return nil, fmt.Errorf("failed to get account %s: %w", uuid, err)
	}

	var account Account
	if err := json.Unmarshal([]byte(data), &account); err != nil {
		return nil, fmt.Errorf("failed to parse account %s: %w", uuid, err)
	}

	return &account, nil
}

// UpdateAccount updates an account in the provider pool.
func (pm *PoolManager) UpdateAccount(ctx context.Context, account *Account) error {
	data, err := json.Marshal(account)
	if err != nil {
		return fmt.Errorf("failed to marshal account: %w", err)
	}

	if err := pm.client.HSet(ctx, PoolKey, account.UUID, string(data)); err != nil {
		return fmt.Errorf("failed to update account %s: %w", account.UUID, err)
	}

	return nil
}

// IncrementUsage atomically increments the usage count for an account.
func (pm *PoolManager) IncrementUsage(ctx context.Context, uuid string) error {
	return pm.updateAccountField(ctx, uuid, func(acc *Account) {
		acc.UsageCount++
		acc.LastUsed = time.Now().Format(time.RFC3339)
	})
}

// IncrementError atomically increments the error count for an account.
func (pm *PoolManager) IncrementError(ctx context.Context, uuid string) error {
	return pm.updateAccountField(ctx, uuid, func(acc *Account) {
		acc.ErrorCount++
		acc.LastErrorTime = time.Now().Format(time.RFC3339)
	})
}

// MarkUnhealthy marks an account as unhealthy.
func (pm *PoolManager) MarkUnhealthy(ctx context.Context, uuid string) error {
	return pm.updateAccountField(ctx, uuid, func(acc *Account) {
		acc.IsHealthy = false
		acc.ErrorCount++
		acc.LastErrorTime = time.Now().Format(time.RFC3339)
	})
}

// MarkHealthy marks an account as healthy.
func (pm *PoolManager) MarkHealthy(ctx context.Context, uuid string) error {
	return pm.updateAccountField(ctx, uuid, func(acc *Account) {
		acc.IsHealthy = true
		acc.LastHealthCheck = time.Now().Format(time.RFC3339)
	})
}

// RecordSuccessAtomic atomically marks an account as healthy and increments usage.
// This combines two operations into one Redis transaction, reducing round-trips and contention.
func (pm *PoolManager) RecordSuccessAtomic(ctx context.Context, uuid string) error {
	now := time.Now().Format(time.RFC3339)
	return pm.updateAccountField(ctx, uuid, func(acc *Account) {
		acc.IsHealthy = true
		acc.UsageCount++
		acc.LastUsed = now
		acc.LastHealthCheck = now
	})
}

// updateAccountField performs an optimistic update on an account field.
// Uses exponential backoff with jitter to avoid thundering herd on contention.
func (pm *PoolManager) updateAccountField(ctx context.Context, uuid string, updateFn func(*Account)) error {
	const maxRetries = 3
	const baseBackoff = 5 * time.Millisecond
	key := PoolKey

	for i := 0; i < maxRetries; i++ {
		err := pm.client.Watch(ctx, func(tx *goredis.Tx) error {
			// Get current account data
			data, err := tx.HGet(ctx, pm.client.Key(key), uuid).Result()
			if err != nil {
				return err
			}

			var account Account
			if err := json.Unmarshal([]byte(data), &account); err != nil {
				return err
			}

			// Apply update
			updateFn(&account)

			// Serialize back
			updated, err := json.Marshal(account)
			if err != nil {
				return err
			}

			// Write in transaction
			_, err = pm.client.TxPipelined(ctx, tx, func(pipe goredis.Pipeliner) error {
				pipe.HSet(ctx, pm.client.Key(key), uuid, string(updated))
				return nil
			})
			return err
		}, key)

		if err == nil {
			return nil
		}

		// txFailedErr means the watched key was modified; retry with backoff
		if err == txFailedErr {
			// Exponential backoff with jitter: baseBackoff * 2^i + random jitter
			backoff := baseBackoff * time.Duration(1<<i)
			jitter := time.Duration(rand.Int63n(int64(backoff / 2))) //nolint:gosec // math/rand is fine for backoff jitter
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff + jitter):
				continue
			}
		}

		return fmt.Errorf("failed to update account %s: %w", uuid, err)
	}

	return fmt.Errorf("failed to update account %s after %d retries", uuid, maxRetries)
}
