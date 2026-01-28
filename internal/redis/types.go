// Package redis provides Redis operations for account pools and tokens.
package redis

// Account represents a Kiro OAuth credential in the provider pool.
// JSON field names match Node.js implementation exactly for compatibility.
type Account struct {
	// Identity
	UUID         string `json:"uuid"`
	ProviderType string `json:"providerType"` // always "claude-kiro-oauth"

	// Kiro-specific
	Region     string `json:"region"`     // AWS region (e.g., "us-east-1")
	ProfileARN string `json:"profileArn"` // AWS profile ARN for API calls

	// Health & Usage
	IsHealthy       bool   `json:"isHealthy"`
	UsageCount      int64  `json:"usageCount"`
	ErrorCount      int64  `json:"errorCount"`
	LastUsed        string `json:"lastUsed"`            // ISO 8601 timestamp
	LastErrorTime   string `json:"lastErrorTime"`       // ISO 8601 timestamp
	LastHealthCheck string `json:"lastHealthCheckTime"` // ISO 8601 timestamp

	// Metadata
	Description string `json:"description,omitempty"`
	AddedAt     string `json:"addedAt"`
}

// Token represents OAuth credentials for a Kiro account.
// JSON field names match Node.js implementation exactly for compatibility.
type Token struct {
	// Credentials
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    string `json:"expiresAt"` // ISO 8601 timestamp (Node.js stores as string)

	// Auth metadata
	AuthMethod   string `json:"authMethod"`             // "social" or "builder-id"
	TokenType    string `json:"tokenType,omitempty"`    // typically "Bearer"
	ClientID     string `json:"clientId,omitempty"`     // Kiro client ID
	ClientSecret string `json:"clientSecret,omitempty"` // Kiro client secret
	IDCRegion    string `json:"idcRegion,omitempty"`    // AWS IDC region

	// Refresh tracking
	LastRefreshed string `json:"lastRefreshed,omitempty"` // ISO 8601
}

// AppConfig represents the application configuration stored in Redis.
type AppConfig struct {
	APIKey          string `json:"apiKey"`
	ModelProvider   string `json:"modelProvider"`
	DefaultProvider string `json:"defaultProvider"`
}
