// Package claude provides Claude API error types.
package claude

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// ErrorType represents Claude API error types.
type ErrorType string

const (
	// ErrorTypeInvalidRequest indicates a malformed request.
	ErrorTypeInvalidRequest ErrorType = "invalid_request_error"
	// ErrorTypeAuthentication indicates an authentication failure.
	ErrorTypeAuthentication ErrorType = "authentication_error"
	// ErrorTypePermissionDenied indicates insufficient permissions.
	ErrorTypePermissionDenied ErrorType = "permission_denied_error"
	// ErrorTypeNotFound indicates a resource was not found.
	ErrorTypeNotFound ErrorType = "not_found_error"
	// ErrorTypeRateLimit indicates rate limiting.
	ErrorTypeRateLimit ErrorType = "rate_limit_error"
	// ErrorTypeAPI indicates an internal API error.
	ErrorTypeAPI ErrorType = "api_error"
	// ErrorTypeOverloaded indicates the service is overloaded.
	ErrorTypeOverloaded ErrorType = "overloaded_error"
)

// ErrorResponse represents a Claude API error response.
type ErrorResponse struct {
	Type  string    `json:"type"` // always "error"
	Error ErrorBody `json:"error"`
}

// ErrorBody contains the error details.
type ErrorBody struct {
	Type    ErrorType `json:"type"`
	Message string    `json:"message"`
}

// APIError is an error type that can be converted to a Claude API error response.
type APIError struct {
	Type       ErrorType
	Message    string
	StatusCode int
}

// Error implements the error interface.
func (e *APIError) Error() string {
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

// ToResponse converts the error to a Claude API error response.
func (e *APIError) ToResponse() *ErrorResponse {
	return &ErrorResponse{
		Type: "error",
		Error: ErrorBody{
			Type:    e.Type,
			Message: e.Message,
		},
	}
}

// WriteError writes a Claude API error response to the response writer.
func (e *APIError) WriteError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.StatusCode)
	_ = json.NewEncoder(w).Encode(e.ToResponse())
}

// NewInvalidRequestError creates a new invalid request error.
func NewInvalidRequestError(message string) *APIError {
	return &APIError{
		Type:       ErrorTypeInvalidRequest,
		Message:    message,
		StatusCode: http.StatusBadRequest,
	}
}

// NewAuthenticationError creates a new authentication error.
func NewAuthenticationError(message string) *APIError {
	return &APIError{
		Type:       ErrorTypeAuthentication,
		Message:    message,
		StatusCode: http.StatusUnauthorized,
	}
}

// NewPermissionDeniedError creates a new permission denied error.
func NewPermissionDeniedError(message string) *APIError {
	return &APIError{
		Type:       ErrorTypePermissionDenied,
		Message:    message,
		StatusCode: http.StatusForbidden,
	}
}

// NewNotFoundError creates a new not found error.
func NewNotFoundError(message string) *APIError {
	return &APIError{
		Type:       ErrorTypeNotFound,
		Message:    message,
		StatusCode: http.StatusNotFound,
	}
}

// NewRateLimitError creates a new rate limit error.
func NewRateLimitError(message string) *APIError {
	return &APIError{
		Type:       ErrorTypeRateLimit,
		Message:    message,
		StatusCode: http.StatusTooManyRequests,
	}
}

// NewAPIError creates a new internal API error.
func NewAPIError(message string) *APIError {
	return &APIError{
		Type:       ErrorTypeAPI,
		Message:    message,
		StatusCode: http.StatusInternalServerError,
	}
}

// NewOverloadedError creates a new overloaded error.
func NewOverloadedError(message string) *APIError {
	return &APIError{
		Type:       ErrorTypeOverloaded,
		Message:    message,
		StatusCode: http.StatusServiceUnavailable,
	}
}

// ErrNoHealthyAccounts is returned when no healthy accounts are available.
var ErrNoHealthyAccounts = NewOverloadedError("No healthy accounts available")
