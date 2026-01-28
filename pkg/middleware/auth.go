// Package middleware provides HTTP middleware for the Kiro server.
package middleware

import (
	"log/slog"
	"net/http"
)

// APIKeyValidator is a function that validates an API key.
type APIKeyValidator func(key string) bool

// Auth creates an authentication middleware that validates API keys.
func Auth(validate APIKeyValidator, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for health and event logging endpoints
			if r.URL.Path == "/health" || r.URL.Path == "/api/event_logging/batch" {
				next.ServeHTTP(w, r)
				return
			}

			// Get API key from header
			apiKey := r.Header.Get("x-api-key")
			if apiKey == "" {
				// Also check Authorization header (Bearer token)
				auth := r.Header.Get("Authorization")
				if len(auth) > 7 && auth[:7] == "Bearer " {
					apiKey = auth[7:]
				}
			}

			if apiKey == "" {
				logger.Warn("missing API key",
					"path", r.URL.Path,
					"remote_addr", r.RemoteAddr,
				)
				writeAuthError(w, "Missing API key")
				return
			}

			if !validate(apiKey) {
				logger.Warn("invalid API key",
					"path", r.URL.Path,
					"remote_addr", r.RemoteAddr,
				)
				writeAuthError(w, "Invalid API key")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// writeAuthError writes an authentication error response in Claude API format.
func writeAuthError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	// Claude API error format
	_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"` + message + `"}}`))
}
