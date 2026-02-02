// Package config provides configuration loading from environment variables and flags.
package config

import (
	"flag"
	"os"
	"strconv"
	"time"

	"github.com/anthropics/AIClient-2-API/internal/claude"
)

// Config holds all configuration for the Kiro server.
type Config struct {
	// Server settings
	Port            int
	Host            string
	GracefulTimeout time.Duration

	// Redis settings
	RedisURL       string
	RedisKeyPrefix string
	RedisPoolSize  int
	RedisTimeout   time.Duration

	// API settings
	APIKey string

	// HTTP client settings
	MaxConns            int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
	RequestTimeout      time.Duration

	// Kiro API settings
	KiroAPITimeout time.Duration

	// Logging
	LogLevel string
	LogJSON  bool

	// Health check
	HealthCooldown time.Duration
	MaxRetries     int

	// Token refresh
	RefreshThreshold time.Duration

	// Cache settings
	AccountCacheTTL time.Duration

	// Request size limits
	MaxKiroRequestBody int
}

// Load reads configuration from environment variables and command-line flags.
// Environment variables take precedence over defaults.
// Command-line flags take precedence over environment variables.
func Load() *Config {
	cfg := &Config{
		// Defaults
		Port:                8081,
		Host:                "0.0.0.0",
		GracefulTimeout:     30 * time.Second,
		RedisURL:            "redis://localhost:6379",
		RedisKeyPrefix:      "aiclient:",
		RedisPoolSize:       100, // Increased for 500+ concurrent connections
		RedisTimeout:        3 * time.Second,
		MaxConns:            100,
		MaxIdleConnsPerHost: 50,
		IdleConnTimeout:     90 * time.Second,
		RequestTimeout:      0, // No timeout for streaming
		KiroAPITimeout:      5 * time.Minute,
		LogLevel:            "info",
		LogJSON:             true,
		HealthCooldown:      6 * time.Second,
		MaxRetries:          3,
		RefreshThreshold:    5 * time.Minute,
		AccountCacheTTL:     5 * time.Second,
		MaxKiroRequestBody:  claude.MaxKiroRequestBodyDefault,
	}

	// Load from environment
	cfg.loadFromEnv()

	// Parse command-line flags (override env)
	cfg.parseFlags()

	return cfg
}

func (c *Config) loadFromEnv() {
	if v := os.Getenv("GO_KIRO_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			c.Port = port
		}
	}
	if v := os.Getenv("GO_KIRO_HOST"); v != "" {
		c.Host = v
	}
	if v := os.Getenv("REDIS_URL"); v != "" {
		c.RedisURL = v
	}
	if v := os.Getenv("REDIS_KEY_PREFIX"); v != "" {
		c.RedisKeyPrefix = v
	}
	if v := os.Getenv("GO_KIRO_REDIS_POOL_SIZE"); v != "" {
		if size, err := strconv.Atoi(v); err == nil {
			c.RedisPoolSize = size
		}
	}
	if v := os.Getenv("GO_KIRO_API_KEY"); v != "" {
		c.APIKey = v
	}
	if v := os.Getenv("GO_KIRO_MAX_CONNS"); v != "" {
		if conns, err := strconv.Atoi(v); err == nil {
			c.MaxConns = conns
		}
	}
	if v := os.Getenv("GO_KIRO_LOG_LEVEL"); v != "" {
		c.LogLevel = v
	}
	if v := os.Getenv("GO_KIRO_LOG_JSON"); v != "" {
		c.LogJSON = v == "true" || v == "1"
	}
	if v := os.Getenv("GO_KIRO_HEALTH_COOLDOWN"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.HealthCooldown = d
		}
	}
	if v := os.Getenv("GO_KIRO_GRACEFUL_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.GracefulTimeout = d
		}
	}
	if v := os.Getenv("GO_KIRO_MAX_REQUEST_BODY"); v != "" {
		if size, err := strconv.Atoi(v); err == nil {
			c.MaxKiroRequestBody = size
		}
	}
}

var flagsParsed bool

func (c *Config) parseFlags() {
	// Only parse flags once to avoid "flag redefined" panic in tests
	if flagsParsed {
		return
	}
	flagsParsed = true

	flag.IntVar(&c.Port, "port", c.Port, "Server port")
	flag.StringVar(&c.Host, "host", c.Host, "Server host")
	flag.StringVar(&c.RedisURL, "redis-url", c.RedisURL, "Redis URL")
	flag.StringVar(&c.RedisKeyPrefix, "redis-prefix", c.RedisKeyPrefix, "Redis key prefix")
	flag.StringVar(&c.APIKey, "api-key", c.APIKey, "API key for authentication")
	flag.StringVar(&c.LogLevel, "log-level", c.LogLevel, "Log level (debug, info, warn, error)")
	flag.Parse()
}
