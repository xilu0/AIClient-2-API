// Package redis provides Redis client with connection pooling and resilience.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	// ErrRedisUnavailable is returned when Redis is temporarily unavailable.
	ErrRedisUnavailable = errors.New("redis temporarily unavailable")
	// ErrNotConnected is returned when the client is not connected.
	ErrNotConnected = errors.New("redis client not connected")
)

// Client wraps the Redis client with connection pooling and resilience.
type Client struct {
	rdb       *redis.Client
	keyPrefix string
	logger    *slog.Logger

	// Connection state
	connected atomic.Bool

	// In-memory cache for resilience during Redis outages
	cacheMu      sync.RWMutex
	accountCache map[string]Account
	tokenCache   map[string]Token
	configCache  *AppConfig
	cacheUpdated time.Time
}

// ClientOptions configures the Redis client.
type ClientOptions struct {
	URL       string
	KeyPrefix string
	PoolSize  int
	Timeout   time.Duration
	Logger    *slog.Logger
}

// NewClient creates a new Redis client with connection pooling.
func NewClient(opts ClientOptions) (*Client, error) {
	// Parse Redis URL
	redisOpts, err := parseRedisURL(opts.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid redis URL: %w", err)
	}

	// Configure pool
	redisOpts.PoolSize = opts.PoolSize
	redisOpts.MinIdleConns = opts.PoolSize / 5
	redisOpts.PoolTimeout = opts.Timeout
	redisOpts.ReadTimeout = opts.Timeout
	redisOpts.WriteTimeout = opts.Timeout

	rdb := redis.NewClient(redisOpts)

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	client := &Client{
		rdb:          rdb,
		keyPrefix:    opts.KeyPrefix,
		logger:       logger,
		accountCache: make(map[string]Account),
		tokenCache:   make(map[string]Token),
	}

	return client, nil
}

// parseRedisURL parses a Redis URL into connection options.
func parseRedisURL(redisURL string) (*redis.Options, error) {
	u, err := url.Parse(redisURL)
	if err != nil {
		return nil, err
	}

	opts := &redis.Options{
		Addr: u.Host,
	}

	// Parse password if present
	if u.User != nil {
		if password, ok := u.User.Password(); ok {
			opts.Password = password
		}
	}

	// Parse database number from path
	if len(u.Path) > 1 {
		db, err := strconv.Atoi(u.Path[1:])
		if err == nil {
			opts.DB = db
		}
	}

	return opts, nil
}

// Connect establishes connection to Redis.
func (c *Client) Connect(ctx context.Context) error {
	if err := c.rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}
	c.connected.Store(true)
	c.logger.Info("connected to Redis")
	return nil
}

// Close closes the Redis connection.
func (c *Client) Close() error {
	c.connected.Store(false)
	return c.rdb.Close()
}

// IsConnected returns true if the client is connected to Redis.
func (c *Client) IsConnected() bool {
	return c.connected.Load()
}

// Ping checks Redis connectivity.
func (c *Client) Ping(ctx context.Context) error {
	if !c.connected.Load() {
		return ErrNotConnected
	}
	return c.rdb.Ping(ctx).Err()
}

// Key returns a prefixed key.
func (c *Client) Key(parts ...string) string {
	key := c.keyPrefix
	for _, part := range parts {
		key += part
	}
	return key
}

// Incr atomically increments a key and returns the new value.
func (c *Client) Incr(ctx context.Context, key string) (int64, error) {
	if !c.connected.Load() {
		return 0, ErrNotConnected
	}
	return c.rdb.Incr(ctx, c.Key(key)).Result()
}

// Get retrieves a string value.
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	if !c.connected.Load() {
		return "", ErrNotConnected
	}
	return c.rdb.Get(ctx, c.Key(key)).Result()
}

// Set stores a string value.
func (c *Client) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	if !c.connected.Load() {
		return ErrNotConnected
	}
	return c.rdb.Set(ctx, c.Key(key), value, expiration).Err()
}

// HGetAll retrieves all fields from a hash.
func (c *Client) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	if !c.connected.Load() {
		return nil, ErrNotConnected
	}
	return c.rdb.HGetAll(ctx, c.Key(key)).Result()
}

// HGet retrieves a single field from a hash.
func (c *Client) HGet(ctx context.Context, key, field string) (string, error) {
	if !c.connected.Load() {
		return "", ErrNotConnected
	}
	return c.rdb.HGet(ctx, c.Key(key), field).Result()
}

// HSet sets a field in a hash.
func (c *Client) HSet(ctx context.Context, key string, values ...interface{}) error {
	if !c.connected.Load() {
		return ErrNotConnected
	}
	return c.rdb.HSet(ctx, c.Key(key), values...).Err()
}

// Watch executes a function within a Redis transaction with optimistic locking.
func (c *Client) Watch(ctx context.Context, fn func(*redis.Tx) error, keys ...string) error {
	if !c.connected.Load() {
		return ErrNotConnected
	}
	prefixedKeys := make([]string, len(keys))
	for i, k := range keys {
		prefixedKeys[i] = c.Key(k)
	}
	return c.rdb.Watch(ctx, fn, prefixedKeys...)
}

// TxPipelined executes commands in a transaction pipeline.
func (c *Client) TxPipelined(ctx context.Context, tx *redis.Tx, fn func(redis.Pipeliner) error) ([]redis.Cmder, error) {
	return tx.TxPipelined(ctx, fn)
}

// WatchTx is a type alias for redis.Tx for use in callbacks.
type WatchTx = redis.Tx

// Pipeliner is a type alias for redis.Pipeliner for use in callbacks.
type Pipeliner = redis.Pipeliner

// UpdateAccountCache updates the in-memory account cache.
func (c *Client) UpdateAccountCache(accounts map[string]Account) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	c.accountCache = accounts
	c.cacheUpdated = time.Now()
}

// GetCachedAccounts returns the cached accounts if Redis is unavailable.
func (c *Client) GetCachedAccounts() (map[string]Account, time.Time) {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	return c.accountCache, c.cacheUpdated
}

// UpdateTokenCache updates a single token in the in-memory cache.
func (c *Client) UpdateTokenCache(uuid string, token Token) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	c.tokenCache[uuid] = token
}

// GetCachedToken returns a cached token if Redis is unavailable.
func (c *Client) GetCachedToken(uuid string) (Token, bool) {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	token, ok := c.tokenCache[uuid]
	return token, ok
}

// UpdateConfigCache updates the in-memory config cache.
func (c *Client) UpdateConfigCache(cfg *AppConfig) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	c.configCache = cfg
}

// GetCachedConfig returns the cached config if Redis is unavailable.
func (c *Client) GetCachedConfig() *AppConfig {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	return c.configCache
}

// LoadConfig loads the application configuration from Redis.
func (c *Client) LoadConfig(ctx context.Context) (*AppConfig, error) {
	data, err := c.Get(ctx, "config")
	if err != nil {
		// Try cache
		if cached := c.GetCachedConfig(); cached != nil {
			c.logger.Warn("using cached config due to Redis error", "error", err)
			return cached, nil
		}
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	var cfg AppConfig
	if err := json.Unmarshal([]byte(data), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Update cache
	c.UpdateConfigCache(&cfg)

	return &cfg, nil
}

// Raw returns the underlying Redis client for advanced operations.
func (c *Client) Raw() *redis.Client {
	return c.rdb
}
