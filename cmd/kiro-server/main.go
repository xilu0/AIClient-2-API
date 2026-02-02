// Package main is the entry point for the Kiro server.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/anthropics/AIClient-2-API/internal/account"
	"github.com/anthropics/AIClient-2-API/internal/config"
	"github.com/anthropics/AIClient-2-API/internal/handler"
	"github.com/anthropics/AIClient-2-API/internal/kiro"
	"github.com/anthropics/AIClient-2-API/internal/redis"
	"github.com/anthropics/AIClient-2-API/pkg/middleware"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Setup logger
	logger := setupLogger(cfg)
	logger.Info("starting Kiro server",
		"port", cfg.Port,
		"redis_url", cfg.RedisURL,
	)

	// Create Redis client
	redisClient, err := redis.NewClient(redis.ClientOptions{
		URL:       cfg.RedisURL,
		KeyPrefix: cfg.RedisKeyPrefix,
		PoolSize:  cfg.RedisPoolSize,
		Timeout:   cfg.RedisTimeout,
		Logger:    logger,
	})
	if err != nil {
		logger.Error("failed to create Redis client", "error", err)
		os.Exit(1)
	}

	// Connect to Redis
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := redisClient.Connect(ctx); err != nil {
		logger.Error("failed to connect to Redis", "error", err)
		os.Exit(1)
	}
	cancel()

	// Load API key from config if not provided
	apiKey := cfg.APIKey
	if apiKey == "" {
		appConfig, err := redisClient.LoadConfig(context.Background())
		if err != nil {
			logger.Warn("failed to load config from Redis, API key validation disabled", "error", err)
		} else if appConfig.APIKey != "" {
			apiKey = appConfig.APIKey
			logger.Info("loaded API key from Redis config")
		}
	}

	// Create managers
	poolManager := redis.NewPoolManager(redisClient)
	tokenManager := redis.NewTokenManager(redisClient)

	// Create account selector
	selector := account.NewSelector(account.SelectorOptions{
		RedisClient:    redisClient,
		PoolManager:    poolManager,
		Logger:         logger,
		CacheTTL:       cfg.AccountCacheTTL,
		HealthCooldown: cfg.HealthCooldown,
	})

	// Create Kiro client
	kiroClient := kiro.NewClient(kiro.ClientOptions{
		MaxConns:            cfg.MaxConns,
		MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:     cfg.IdleConnTimeout,
		Timeout:             cfg.KiroAPITimeout,
		Logger:              logger,
	})

	// Create handlers
	messagesHandler := handler.NewMessagesHandler(handler.MessagesHandlerOptions{
		Selector:        selector,
		PoolManager:     poolManager,
		TokenManager:    tokenManager,
		KiroClient:      kiroClient,
		Logger:          logger,
		MaxRetries:      cfg.MaxRetries,
		MaxKiroBodySize: cfg.MaxKiroRequestBody,
	})

	countTokensHandler := handler.NewCountTokensHandler(handler.CountTokensHandlerOptions{
		Logger: logger,
	})

	// Create API key validator
	validateAPIKey := func(key string) bool {
		if apiKey == "" {
			return true // No API key configured, allow all
		}
		return key == apiKey
	}

	// Create router
	mux := http.NewServeMux()

	// Health endpoint (no auth required)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		status := "healthy"
		redisStatus := "connected"
		if err := redisClient.Ping(r.Context()); err != nil {
			status = "degraded"
			redisStatus = "disconnected"
		}

		total, healthy, _ := selector.GetAccountCount(r.Context())

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"status":"%s","redis":"%s","accounts":{"total":%d,"healthy":%d}}`,
			status, redisStatus, total, healthy)
	})

	// Event logging stub endpoint (no-op, returns 200)
	mux.HandleFunc("POST /api/event_logging/batch", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Messages endpoint
	mux.Handle("POST /v1/messages", messagesHandler)

	// Count tokens endpoint (local estimation, no API call)
	mux.Handle("POST /v1/messages/count_tokens", countTokensHandler)

	// Apply middleware
	var httpHandler http.Handler = mux
	httpHandler = middleware.Auth(validateAPIKey, logger)(httpHandler)
	httpHandler = middleware.Logging(logger)(httpHandler)

	// Create server
	server := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:      httpHandler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // No timeout for streaming
		IdleTimeout:  120 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Info("server listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server...")

	// Graceful shutdown
	ctx, cancel = context.WithTimeout(context.Background(), cfg.GracefulTimeout)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("server forced to shutdown", "error", err)
	}

	// Close connections
	kiroClient.Close()
	if err := redisClient.Close(); err != nil {
		logger.Error("failed to close Redis connection", "error", err)
	}

	logger.Info("server stopped")
}

func setupLogger(cfg *config.Config) *slog.Logger {
	var handler slog.Handler

	level := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{Level: level}

	if cfg.LogJSON {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}
