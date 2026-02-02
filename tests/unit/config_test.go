// Package unit contains unit tests for the Kiro server.
package unit

import (
	"os"
	"testing"
	"time"

	"github.com/anthropics/AIClient-2-API/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestConfigDefaults(t *testing.T) {
	// Clear environment
	os.Clearenv()

	cfg := config.Load()

	assert.Equal(t, 8081, cfg.Port)
	assert.Equal(t, "0.0.0.0", cfg.Host)
	assert.Equal(t, "redis://localhost:6379", cfg.RedisURL)
	assert.Equal(t, "aiclient:", cfg.RedisKeyPrefix)
	assert.Equal(t, 100, cfg.RedisPoolSize)
	assert.Equal(t, 3*time.Second, cfg.RedisTimeout)
	assert.Equal(t, 100, cfg.MaxConns)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, true, cfg.LogJSON)
	assert.Equal(t, 6*time.Second, cfg.HealthCooldown)
	assert.Equal(t, 3, cfg.MaxRetries)
	assert.Equal(t, 5*time.Minute, cfg.RefreshThreshold)
	assert.Equal(t, 5*time.Second, cfg.AccountCacheTTL)
}

func TestConfigFromEnv(t *testing.T) {
	// Set environment variables
	os.Setenv("GO_KIRO_PORT", "9000")
	os.Setenv("GO_KIRO_HOST", "127.0.0.1")
	os.Setenv("REDIS_URL", "redis://redis:6379/1")
	os.Setenv("REDIS_KEY_PREFIX", "test:")
	os.Setenv("GO_KIRO_REDIS_POOL_SIZE", "100")
	os.Setenv("GO_KIRO_API_KEY", "test-key")
	os.Setenv("GO_KIRO_MAX_CONNS", "200")
	os.Setenv("GO_KIRO_LOG_LEVEL", "debug")
	os.Setenv("GO_KIRO_LOG_JSON", "false")
	os.Setenv("GO_KIRO_HEALTH_COOLDOWN", "30s")
	defer os.Clearenv()

	cfg := config.Load()

	assert.Equal(t, 9000, cfg.Port)
	assert.Equal(t, "127.0.0.1", cfg.Host)
	assert.Equal(t, "redis://redis:6379/1", cfg.RedisURL)
	assert.Equal(t, "test:", cfg.RedisKeyPrefix)
	assert.Equal(t, 100, cfg.RedisPoolSize)
	assert.Equal(t, "test-key", cfg.APIKey)
	assert.Equal(t, 200, cfg.MaxConns)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, false, cfg.LogJSON)
	assert.Equal(t, 30*time.Second, cfg.HealthCooldown)
}

func TestConfigInvalidEnvValues(t *testing.T) {
	// Set invalid values - should fall back to defaults
	os.Setenv("GO_KIRO_PORT", "invalid")
	os.Setenv("GO_KIRO_REDIS_POOL_SIZE", "not-a-number")
	defer os.Clearenv()

	cfg := config.Load()

	// Should use defaults when parsing fails
	assert.Equal(t, 8081, cfg.Port)
	assert.Equal(t, 100, cfg.RedisPoolSize)
}
