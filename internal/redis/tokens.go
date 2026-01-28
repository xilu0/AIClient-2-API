// Package redis provides token operations for Kiro OAuth accounts.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// TokenKeyPrefix is the key prefix for Kiro OAuth tokens.
	TokenKeyPrefix = "tokens:claude-kiro-oauth:"
)

// TokenManager handles token operations.
type TokenManager struct {
	client *Client
}

// NewTokenManager creates a new token manager.
func NewTokenManager(client *Client) *TokenManager {
	return &TokenManager{client: client}
}

// parseExpiresAt parses the ExpiresAt field which can be an ISO 8601 string.
func parseExpiresAt(expiresAt string) (time.Time, error) {
	if expiresAt == "" {
		return time.Time{}, fmt.Errorf("empty expiresAt")
	}
	// Try ISO 8601 format (Node.js stores as string)
	t, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		// Try alternative format without timezone
		t, err = time.Parse("2006-01-02T15:04:05.000Z", expiresAt)
	}
	return t, err
}

// GetToken retrieves a token for an account.
func (tm *TokenManager) GetToken(ctx context.Context, uuid string) (*Token, error) {
	key := TokenKeyPrefix + uuid
	data, err := tm.client.Get(ctx, key)
	if err != nil {
		// Try cache on error
		if cached, ok := tm.client.GetCachedToken(uuid); ok {
			tm.client.logger.Warn("using cached token due to Redis error",
				"uuid", uuid,
				"error", err,
			)
			return &cached, nil
		}
		return nil, fmt.Errorf("failed to get token for %s: %w", uuid, err)
	}

	var token Token
	if err := json.Unmarshal([]byte(data), &token); err != nil {
		return nil, fmt.Errorf("failed to parse token for %s: %w", uuid, err)
	}

	// Update cache
	tm.client.UpdateTokenCache(uuid, token)

	return &token, nil
}

// SetToken stores a token for an account.
func (tm *TokenManager) SetToken(ctx context.Context, uuid string, token *Token) error {
	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	key := TokenKeyPrefix + uuid
	if err := tm.client.Set(ctx, key, string(data), 0); err != nil {
		return fmt.Errorf("failed to set token for %s: %w", uuid, err)
	}

	// Update cache
	tm.client.UpdateTokenCache(uuid, *token)

	return nil
}

// UpdateTokenAtomic atomically updates a token using optimistic locking.
// This prevents race conditions when multiple goroutines try to refresh the same token.
func (tm *TokenManager) UpdateTokenAtomic(ctx context.Context, uuid string, token *Token) error {
	key := TokenKeyPrefix + uuid
	const maxRetries = 3

	for i := 0; i < maxRetries; i++ {
		err := tm.client.Watch(ctx, func(tx *redis.Tx) error {
			// Check if token was already updated by another goroutine
			data, err := tx.Get(ctx, tm.client.Key(key)).Result()
			if err != nil && err != redis.Nil {
				return err
			}

			if data != "" {
				var existing Token
				if err := json.Unmarshal([]byte(data), &existing); err == nil {
					// If existing token has a later expiry, skip update
					existingExpiry, err1 := parseExpiresAt(existing.ExpiresAt)
					newExpiry, err2 := parseExpiresAt(token.ExpiresAt)
					if err1 == nil && err2 == nil && existingExpiry.After(newExpiry) {
						return nil // Another goroutine already refreshed
					}
				}
			}

			// Update token
			newData, err := json.Marshal(token)
			if err != nil {
				return err
			}

			_, err = tm.client.TxPipelined(ctx, tx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, tm.client.Key(key), string(newData), 0)
				return nil
			})
			return err
		}, key)

		if err == nil {
			// Update cache
			tm.client.UpdateTokenCache(uuid, *token)
			return nil
		}

		// redis.TxFailedErr means the watched key was modified; retry
		if err == redis.TxFailedErr {
			continue
		}

		return fmt.Errorf("failed to update token for %s: %w", uuid, err)
	}

	return fmt.Errorf("failed to update token for %s after %d retries", uuid, maxRetries)
}

// IsExpiringSoon checks if a token is expiring within the given threshold.
func (tm *TokenManager) IsExpiringSoon(token *Token, threshold time.Duration) bool {
	if token == nil {
		return true
	}
	expiresAt, err := parseExpiresAt(token.ExpiresAt)
	if err != nil {
		return true // Treat parse errors as expiring
	}
	return time.Until(expiresAt) < threshold
}

// IsExpired checks if a token has already expired.
func (tm *TokenManager) IsExpired(token *Token) bool {
	if token == nil {
		return true
	}
	expiresAt, err := parseExpiresAt(token.ExpiresAt)
	if err != nil {
		return true // Treat parse errors as expired
	}
	return time.Now().After(expiresAt)
}
