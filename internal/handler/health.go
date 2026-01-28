// Package handler provides HTTP handlers for the Kiro server.
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/anthropics/AIClient-2-API/internal/redis"
)

// HealthHandler handles GET /health requests.
type HealthHandler struct {
	redisClient *redis.Client
	poolManager *redis.PoolManager
}

// HealthResponse represents the health check response.
type HealthResponse struct {
	Status   string         `json:"status"`
	Redis    string         `json:"redis"`
	Accounts AccountsStatus `json:"accounts"`
}

// AccountsStatus represents account pool status.
type AccountsStatus struct {
	Total   int `json:"total"`
	Healthy int `json:"healthy"`
}

// NewHealthHandler creates a new health handler.
func NewHealthHandler(redisClient *redis.Client, poolManager *redis.PoolManager) *HealthHandler {
	return &HealthHandler{
		redisClient: redisClient,
		poolManager: poolManager,
	}
}

// ServeHTTP handles the health check request.
func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	response := HealthResponse{
		Status: "healthy",
		Redis:  "connected",
		Accounts: AccountsStatus{
			Total:   0,
			Healthy: 0,
		},
	}

	// Check Redis connection
	if err := h.redisClient.Ping(ctx); err != nil {
		response.Status = "degraded"
		response.Redis = "disconnected"
	}

	// Get account counts
	accounts, err := h.poolManager.GetAllAccounts(ctx)
	if err == nil {
		response.Accounts.Total = len(accounts)
		for _, acc := range accounts {
			if acc.IsHealthy {
				response.Accounts.Healthy++
			}
		}
	}

	// Set status based on accounts
	if response.Accounts.Healthy == 0 && response.Accounts.Total > 0 {
		response.Status = "degraded"
	}

	w.Header().Set("Content-Type", "application/json")
	if response.Status != "healthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	_ = json.NewEncoder(w).Encode(response)
}
