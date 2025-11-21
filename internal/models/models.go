package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Tenant represents a multi-tenant organization
type Tenant struct {
	ID          uuid.UUID       `json:"id" db:"id"`
	Name        string          `json:"name" db:"name"`
	Subdomain   string          `json:"subdomain" db:"subdomain"`
	ClerkOrgID  *string         `json:"clerk_org_id,omitempty" db:"clerk_org_id"`
	Status      string          `json:"status" db:"status"`
	Settings    JSONB           `json:"settings" db:"settings"`
	CreatedAt   time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at" db:"updated_at"`
}

// User represents a user within a tenant
type User struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	TenantID    uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	ClerkUserID string     `json:"clerk_user_id" db:"clerk_user_id"`
	Email       string     `json:"email" db:"email"`
	FirstName   *string    `json:"first_name,omitempty" db:"first_name"`
	LastName    *string    `json:"last_name,omitempty" db:"last_name"`
	Role        string     `json:"role" db:"role"`
	Status      string     `json:"status" db:"status"`
	Metadata    JSONB      `json:"metadata" db:"metadata"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty" db:"last_login_at"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
}

// Origin represents a backend service/API
type Origin struct {
	ID                  uuid.UUID  `json:"id" db:"id"`
	TenantID            uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	Name                string     `json:"name" db:"name"`
	URL                 string     `json:"url" db:"url"`
	HealthCheckPath     string     `json:"health_check_path" db:"health_check_path"`
	HealthCheckInterval int        `json:"health_check_interval" db:"health_check_interval"`
	TimeoutSeconds      int        `json:"timeout_seconds" db:"timeout_seconds"`
	MaxRetries          int        `json:"max_retries" db:"max_retries"`
	Weight              int        `json:"weight" db:"weight"`
	IsHealthy           bool       `json:"is_healthy" db:"is_healthy"`
	LastHealthCheck     *time.Time `json:"last_health_check,omitempty" db:"last_health_check"`
	Metadata            JSONB      `json:"metadata" db:"metadata"`
	CreatedAt           time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at" db:"updated_at"`
}

// Route represents a routing rule
type Route struct {
	ID                      uuid.UUID      `json:"id" db:"id"`
	TenantID                uuid.UUID      `json:"tenant_id" db:"tenant_id"`
	OriginID                uuid.UUID      `json:"origin_id" db:"origin_id"`
	Name                    string         `json:"name" db:"name"`
	PathPattern             string         `json:"path_pattern" db:"path_pattern"`
	Methods                 StringArray    `json:"methods" db:"methods"`
	Priority                int            `json:"priority" db:"priority"`
	AuthMode                string         `json:"auth_mode" db:"auth_mode"`
	IsActive                bool           `json:"is_active" db:"is_active"`
	
	// Rate limiting
	RateLimitEnabled            bool   `json:"rate_limit_enabled" db:"rate_limit_enabled"`
	RateLimitRequestsPerSecond  int    `json:"rate_limit_requests_per_second" db:"rate_limit_requests_per_second"`
	RateLimitBurst              int    `json:"rate_limit_burst" db:"rate_limit_burst"`
	RateLimitKeyStrategy        string `json:"rate_limit_key_strategy" db:"rate_limit_key_strategy"`
	
	// Caching
	CacheEnabled      bool   `json:"cache_enabled" db:"cache_enabled"`
	CacheTTLSeconds   int    `json:"cache_ttl_seconds" db:"cache_ttl_seconds"`
	CacheKeyPattern   string `json:"cache_key_pattern" db:"cache_key_pattern"`
	CacheBypassRules  JSONB  `json:"cache_bypass_rules" db:"cache_bypass_rules"`
	
	// Transformation
	RequestHeaders      JSONB   `json:"request_headers" db:"request_headers"`
	ResponseHeaders     JSONB   `json:"response_headers" db:"response_headers"`
	PathRewritePattern  *string `json:"path_rewrite_pattern,omitempty" db:"path_rewrite_pattern"`
	PathRewriteTarget   *string `json:"path_rewrite_target,omitempty" db:"path_rewrite_target"`
	
	// Advanced
	TimeoutSeconds          int  `json:"timeout_seconds" db:"timeout_seconds"`
	RetryAttempts           int  `json:"retry_attempts" db:"retry_attempts"`
	CircuitBreakerEnabled   bool `json:"circuit_breaker_enabled" db:"circuit_breaker_enabled"`
	CircuitBreakerThreshold int  `json:"circuit_breaker_threshold" db:"circuit_breaker_threshold"`
	
	Metadata  JSONB     `json:"metadata" db:"metadata"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// APIKey represents an API key for authentication
type APIKey struct {
	ID                uuid.UUID  `json:"id" db:"id"`
	TenantID          uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	UserID            *uuid.UUID `json:"user_id,omitempty" db:"user_id"`
	Name              string     `json:"name" db:"name"`
	KeyPrefix         string     `json:"key_prefix" db:"key_prefix"`
	KeyHash           string     `json:"-" db:"key_hash"`
	Scopes            StringArray `json:"scopes" db:"scopes"`
	RateLimitOverride *int       `json:"rate_limit_override,omitempty" db:"rate_limit_override"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty" db:"expires_at"`
	LastUsedAt        *time.Time `json:"last_used_at,omitempty" db:"last_used_at"`
	UsageCount        int64      `json:"usage_count" db:"usage_count"`
	IsActive          bool       `json:"is_active" db:"is_active"`
	Metadata          JSONB      `json:"metadata" db:"metadata"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at" db:"updated_at"`
}

// RequestLog represents a logged API request for analytics
type RequestLog struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	TenantID        uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	RouteID         *uuid.UUID `json:"route_id,omitempty" db:"route_id"`
	UserID          *uuid.UUID `json:"user_id,omitempty" db:"user_id"`
	Method          string     `json:"method" db:"method"`
	Path            string     `json:"path" db:"path"`
	QueryString     *string    `json:"query_string,omitempty" db:"query_string"`
	UserAgent       *string    `json:"user_agent,omitempty" db:"user_agent"`
	IPAddress       *string    `json:"ip_address,omitempty" db:"ip_address"`
	StatusCode      int        `json:"status_code" db:"status_code"`
	ResponseTimeMs  int        `json:"response_time_ms" db:"response_time_ms"`
	ResponseSizeBytes *int     `json:"response_size_bytes,omitempty" db:"response_size_bytes"`
	CacheHit        bool       `json:"cache_hit" db:"cache_hit"`
	CacheKey        *string    `json:"cache_key,omitempty" db:"cache_key"`
	OriginURL       *string    `json:"origin_url,omitempty" db:"origin_url"`
	RateLimited     bool       `json:"rate_limited" db:"rate_limited"`
	AuthMethod      *string    `json:"auth_method,omitempty" db:"auth_method"`
	APIKeyID        *uuid.UUID `json:"api_key_id,omitempty" db:"api_key_id"`
	ErrorMessage    *string    `json:"error_message,omitempty" db:"error_message"`
	ErrorCode       *string    `json:"error_code,omitempty" db:"error_code"`
	TraceID         *string    `json:"trace_id,omitempty" db:"trace_id"`
	SpanID          *string    `json:"span_id,omitempty" db:"span_id"`
	Metadata        JSONB      `json:"metadata" db:"metadata"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
}

// Custom types for database compatibility

// JSONB represents a PostgreSQL JSONB field
type JSONB map[string]interface{}

func (j JSONB) Value() (driver.Value, error) {
	if j == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(j)
}

func (j *JSONB) Scan(value interface{}) error {
	if value == nil {
		*j = make(JSONB)
		return nil
	}
	
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal JSONB value: %v", value)
	}
	
	return json.Unmarshal(bytes, j)
}

// StringArray represents a PostgreSQL TEXT[] field
type StringArray []string

func (a StringArray) Value() (driver.Value, error) {
	if a == nil {
		return "{}", nil
	}
	
	// PostgreSQL array format: {val1,val2,val3}
	result := "{"
	for i, s := range a {
		if i > 0 {
			result += ","
		}
		result += s
	}
	result += "}"
	return result, nil
}

func (a *StringArray) Scan(value interface{}) error {
	if value == nil {
		*a = []string{}
		return nil
	}
	
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal StringArray value: %v", value)
	}
	
	// Parse PostgreSQL array format
	str := string(bytes)
	if str == "{}" {
		*a = []string{}
		return nil
	}
	
	// Remove braces and split
	str = str[1 : len(str)-1]
	*a = splitArray(str)
	return nil
}

func splitArray(s string) []string {
	if s == "" {
		return []string{}
	}
	
	var result []string
	current := ""
	inQuotes := false
	
	for i := 0; i < len(s); i++ {
		char := s[i]
		
		if char == '"' {
			inQuotes = !inQuotes
			continue
		}
		
		if char == ',' && !inQuotes {
			result = append(result, current)
			current = ""
			continue
		}
		
		current += string(char)
	}
	
	if current != "" {
		result = append(result, current)
	}
	
	return result
}
