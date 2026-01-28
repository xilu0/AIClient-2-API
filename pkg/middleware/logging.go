// Package middleware provides HTTP middleware for the Kiro server.
package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/anthropics/AIClient-2-API/internal/claude"
)

// contextKey is a type for context keys to avoid collisions.
type contextKey string

const (
	// RequestIDKey is the context key for request ID.
	RequestIDKey contextKey = "request_id"
	// StartTimeKey is the context key for request start time.
	StartTimeKey contextKey = "start_time"
)

// responseWriter wraps http.ResponseWriter to capture status code and size.
type responseWriter struct {
	http.ResponseWriter
	status int
	size   int
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.size += n
	return n, err
}

// Unwrap returns the underlying ResponseWriter for http.Flusher support.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// Flush implements http.Flusher for SSE streaming.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Logging creates a structured JSON logging middleware.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip logging for event logging endpoint (high frequency, no-op)
			if r.URL.Path == "/api/event_logging/batch" {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			requestID := generateRequestID()

			// Add request info to context
			ctx := context.WithValue(r.Context(), RequestIDKey, requestID)
			ctx = context.WithValue(ctx, StartTimeKey, start)
			r = r.WithContext(ctx)

			// Wrap response writer
			wrapped := &responseWriter{
				ResponseWriter: w,
				status:         http.StatusOK,
			}

			// Log request start
			logger.Info("request started",
				"request_id", requestID,
				"method", r.Method,
				"path", r.URL.Path,
				"remote_addr", r.RemoteAddr,
				"user_agent", r.UserAgent(),
			)

			// Serve request
			next.ServeHTTP(wrapped, r)

			// Log request completion
			duration := time.Since(start)
			logger.Info("request completed",
				"request_id", requestID,
				"method", r.Method,
				"path", r.URL.Path,
				"status", wrapped.status,
				"size", wrapped.size,
				"duration_ms", duration.Milliseconds(),
			)
		})
	}
}

// generateRequestID generates a unique request ID.
func generateRequestID() string {
	return claude.GenerateMessageID()
}

// GetRequestID retrieves the request ID from context.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(RequestIDKey).(string); ok {
		return id
	}
	return ""
}

// GetStartTime retrieves the request start time from context.
func GetStartTime(ctx context.Context) time.Time {
	if t, ok := ctx.Value(StartTimeKey).(time.Time); ok {
		return t
	}
	return time.Now()
}
