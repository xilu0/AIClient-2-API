// Package unit contains unit tests for the Kiro server.
package unit

import (
	"testing"

	"github.com/anthropics/AIClient-2-API/internal/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	opts := redis.ClientOptions{
		URL:       "redis://localhost:6379",
		KeyPrefix: "test:",
		PoolSize:  10,
	}

	client, err := redis.NewClient(opts)
	require.NoError(t, err)
	require.NotNil(t, client)

	// Client should not be connected yet
	assert.False(t, client.IsConnected())
}

func TestClientKeyPrefix(t *testing.T) {
	opts := redis.ClientOptions{
		URL:       "redis://localhost:6379",
		KeyPrefix: "aiclient:",
		PoolSize:  10,
	}

	client, err := redis.NewClient(opts)
	require.NoError(t, err)

	// Test key prefixing
	assert.Equal(t, "aiclient:pools:claude-kiro-oauth", client.Key("pools:claude-kiro-oauth"))
	assert.Equal(t, "aiclient:tokens:uuid123", client.Key("tokens:", "uuid123"))
	assert.Equal(t, "aiclient:config", client.Key("config"))
}

func TestAccountCaching(t *testing.T) {
	opts := redis.ClientOptions{
		URL:       "redis://localhost:6379",
		KeyPrefix: "test:",
		PoolSize:  10,
	}

	client, err := redis.NewClient(opts)
	require.NoError(t, err)

	// Create test accounts
	accounts := map[string]redis.Account{
		"uuid1": {
			UUID:         "uuid1",
			ProviderType: "claude-kiro-oauth",
			Region:       "us-east-1",
			IsHealthy:    true,
		},
		"uuid2": {
			UUID:         "uuid2",
			ProviderType: "claude-kiro-oauth",
			Region:       "us-west-2",
			IsHealthy:    false,
		},
	}

	// Update cache
	client.UpdateAccountCache(accounts)

	// Verify cache
	cached, cacheTime := client.GetCachedAccounts()
	assert.Equal(t, 2, len(cached))
	assert.NotZero(t, cacheTime)
	assert.Equal(t, "uuid1", cached["uuid1"].UUID)
	assert.Equal(t, true, cached["uuid1"].IsHealthy)
	assert.Equal(t, false, cached["uuid2"].IsHealthy)
}

func TestTokenCaching(t *testing.T) {
	opts := redis.ClientOptions{
		URL:       "redis://localhost:6379",
		KeyPrefix: "test:",
		PoolSize:  10,
	}

	client, err := redis.NewClient(opts)
	require.NoError(t, err)

	// Create test token
	token := redis.Token{
		AccessToken:  "access123",
		RefreshToken: "refresh456",
		ExpiresAt:    "2026-01-27T15:23:55.517Z",
		AuthMethod:   "social",
	}

	// Update cache
	client.UpdateTokenCache("uuid1", token)

	// Verify cache
	cached, ok := client.GetCachedToken("uuid1")
	assert.True(t, ok)
	assert.Equal(t, "access123", cached.AccessToken)
	assert.Equal(t, "refresh456", cached.RefreshToken)

	// Non-existent token
	_, ok = client.GetCachedToken("nonexistent")
	assert.False(t, ok)
}

func TestConfigCaching(t *testing.T) {
	opts := redis.ClientOptions{
		URL:       "redis://localhost:6379",
		KeyPrefix: "test:",
		PoolSize:  10,
	}

	client, err := redis.NewClient(opts)
	require.NoError(t, err)

	// Initially no config
	assert.Nil(t, client.GetCachedConfig())

	// Update cache
	cfg := &redis.AppConfig{
		APIKey:        "test-key",
		ModelProvider: "claude-kiro-oauth",
	}
	client.UpdateConfigCache(cfg)

	// Verify cache
	cached := client.GetCachedConfig()
	require.NotNil(t, cached)
	assert.Equal(t, "test-key", cached.APIKey)
}

func TestClientRedisURLParsing(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "simple URL",
			url:     "redis://localhost:6379",
			wantErr: false,
		},
		{
			name:    "URL with password",
			url:     "redis://:password@localhost:6379",
			wantErr: false,
		},
		{
			name:    "URL with database",
			url:     "redis://localhost:6379/1",
			wantErr: false,
		},
		{
			name:    "URL with user and password",
			url:     "redis://user:password@localhost:6379/2",
			wantErr: false,
		},
		{
			name:    "invalid URL",
			url:     "://invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := redis.ClientOptions{
				URL:       tt.url,
				KeyPrefix: "test:",
				PoolSize:  10,
			}

			_, err := redis.NewClient(opts)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
