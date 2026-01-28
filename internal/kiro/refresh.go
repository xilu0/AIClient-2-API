// Package kiro provides HTTP client for Kiro API.
package kiro

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// RefreshURLTemplate is the Kiro token refresh endpoint template.
	RefreshURLTemplate = "https://prod.%s.auth.desktop.kiro.dev/refreshToken"
	// RefreshIDCURLTemplate is the AWS IDC token refresh endpoint template.
	RefreshIDCURLTemplate = "https://oidc.%s.amazonaws.com/token"
	// RefreshTimeout is the timeout for token refresh requests.
	RefreshTimeout = 15 * time.Second
)

// RefreshRequest represents a token refresh request for social auth.
type RefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// RefreshIDCRequest represents a token refresh request for IDC (builder-id) auth.
// IDC auth requires clientId, clientSecret, and grantType in addition to refreshToken.
type RefreshIDCRequest struct {
	RefreshToken string `json:"refreshToken"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	GrantType    string `json:"grantType"`
}

// RefreshResponse represents a token refresh response.
type RefreshResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int64  `json:"expiresIn"` // seconds
	ProfileARN   string `json:"profileArn,omitempty"`
}

// RefreshToken refreshes an OAuth token using the Kiro refresh endpoint.
// For IDC auth (builder-id), clientID and clientSecret are required.
// For social auth, only refreshToken is needed.
func (c *Client) RefreshToken(ctx context.Context, region string, refreshToken string, authMethod string, idcRegion string, clientID string, clientSecret string) (*RefreshResponse, error) {
	// Use IDC endpoint for non-social auth, otherwise use Kiro endpoint
	var refreshURL string
	var bodyBytes []byte
	var err error

	if authMethod != "" && authMethod != "social" {
		// IDC auth (builder-id): requires clientId, clientSecret, and grantType
		if idcRegion == "" {
			idcRegion = region
		}
		refreshURL = fmt.Sprintf(RefreshIDCURLTemplate, idcRegion)

		reqBody := RefreshIDCRequest{
			RefreshToken: refreshToken,
			ClientID:     clientID,
			ClientSecret: clientSecret,
			GrantType:    "refresh_token",
		}
		bodyBytes, err = json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal IDC refresh request: %w", err)
		}
		c.logger.Debug("using IDC auth for token refresh", "idcRegion", idcRegion)
	} else {
		// Social auth: only refreshToken is needed
		if region == "" {
			region = "us-east-1"
		}
		refreshURL = fmt.Sprintf(RefreshURLTemplate, region)

		reqBody := RefreshRequest{RefreshToken: refreshToken}
		bodyBytes, err = json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal refresh request: %w", err)
		}
	}

	// Create request with timeout
	ctx, cancel := context.WithTimeout(ctx, RefreshTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", refreshURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	c.logger.Debug("refreshing token", "url", refreshURL)

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read refresh response: %w", err)
	}

	// Check for errors
	if resp.StatusCode >= 400 {
		c.logger.Warn("token refresh failed",
			"status", resp.StatusCode,
			"body", string(body),
		)
		return nil, fmt.Errorf("token refresh failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var refreshResp RefreshResponse
	if err := json.Unmarshal(body, &refreshResp); err != nil {
		return nil, fmt.Errorf("failed to parse refresh response: %w", err)
	}

	c.logger.Info("token refreshed successfully")
	return &refreshResp, nil
}
