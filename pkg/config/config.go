package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	App           AppConfig
	ControlPlane  ControlPlaneConfig
	Gateway       GatewayConfig
	Database      DatabaseConfig
	Redis         RedisConfig
	Clerk         ClerkConfig
	RateLimit     RateLimitConfig
	Cache         CacheConfig
	LoadBalancer  LoadBalancerConfig
	Observability ObservabilityConfig
	CORS          CORSConfig
}

type AppConfig struct {
	Env  string
	Name string
}

type ControlPlaneConfig struct {
	Host     string
	Port     int
	GRPCPort int
}

type GatewayConfig struct {
	Host   string
	Port   int
	Domain string
}

type DatabaseConfig struct {
	Host               string
	Port               int
	User               string
	Password           string
	Name               string
	SSLMode            string
	MaxConnections     int
	MaxIdleConnections int
	MaxLifetime        time.Duration
}

type RedisConfig struct {
	Host       string
	Port       int
	Password   string
	DB         int
	MaxRetries int
	PoolSize   int
}

type ClerkConfig struct {
	SecretKey      string
	PublishableKey string
	APIURL         string
}

type RateLimitConfig struct {
	Enabled      bool
	DefaultRPS   int
	DefaultBurst int
}

type CacheConfig struct {
	Enabled    bool
	DefaultTTL time.Duration
	MaxSizeMB  int
}

type LoadBalancerConfig struct {
	Strategy            string
	HealthCheckInterval time.Duration
	HealthCheckTimeout  time.Duration
	MaxRetryAttempts    int
}

type ObservabilityConfig struct {
	OTELEnabled          bool
	OTELServiceName      string
	OTELExporterEndpoint string
	MetricsEnabled       bool
	MetricsPort          int
	LogLevel             string
	LogFormat            string
}

type CORSConfig struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	AllowCredentials bool
}

func Load() (*Config, error) {
	// Load .env file if it exists
	_ = godotenv.Load()

	cfg := &Config{
		App: AppConfig{
			Env:  getEnv("APP_ENV", "development"),
			Name: getEnv("APP_NAME", "VantageEdge"),
		},
		ControlPlane: ControlPlaneConfig{
			Host:     getEnv("CONTROL_PLANE_HOST", "0.0.0.0"),
			Port:     getEnvAsInt("CONTROL_PLANE_PORT", 8080),
			GRPCPort: getEnvAsInt("CONTROL_PLANE_GRPC_PORT", 9090),
		},
		Gateway: GatewayConfig{
			Host:   getEnv("GATEWAY_HOST", "0.0.0.0"),
			Port:   getEnvAsInt("GATEWAY_PORT", 8000),
			Domain: getEnv("GATEWAY_DOMAIN", "vantageedge.dev"),
		},
		Database: DatabaseConfig{
			Host:               getEnv("DB_HOST", "localhost"),
			Port:               getEnvAsInt("DB_PORT", 5432),
			User:               getEnv("DB_USER", "vantageedge"),
			Password:           getEnv("DB_PASSWORD", "changeme"),
			Name:               getEnv("DB_NAME", "vantageedge"),
			SSLMode:            getEnv("DB_SSLMODE", "disable"),
			MaxConnections:     getEnvAsInt("DB_MAX_CONNECTIONS", 25),
			MaxIdleConnections: getEnvAsInt("DB_MAX_IDLE_CONNECTIONS", 5),
			MaxLifetime:        getEnvAsDuration("DB_CONNECTION_MAX_LIFETIME", 5*time.Minute),
		},
		Redis: RedisConfig{
			Host:       getEnv("REDIS_HOST", "localhost"),
			Port:       getEnvAsInt("REDIS_PORT", 6379),
			Password:   getEnv("REDIS_PASSWORD", ""),
			DB:         getEnvAsInt("REDIS_DB", 0),
			MaxRetries: getEnvAsInt("REDIS_MAX_RETRIES", 3),
			PoolSize:   getEnvAsInt("REDIS_POOL_SIZE", 10),
		},
		Clerk: ClerkConfig{
			SecretKey:      getEnv("CLERK_SECRET_KEY", ""),
			PublishableKey: getEnv("CLERK_PUBLISHABLE_KEY", ""),
			APIURL:         getEnv("CLERK_API_URL", "https://api.clerk.com/v1"),
		},
		RateLimit: RateLimitConfig{
			Enabled:      getEnvAsBool("RATE_LIMIT_ENABLED", true),
			DefaultRPS:   getEnvAsInt("RATE_LIMIT_DEFAULT_RPS", 100),
			DefaultBurst: getEnvAsInt("RATE_LIMIT_DEFAULT_BURST", 200),
		},
		Cache: CacheConfig{
			Enabled:    getEnvAsBool("CACHE_ENABLED", true),
			DefaultTTL: getEnvAsDuration("CACHE_DEFAULT_TTL", 5*time.Minute),
			MaxSizeMB:  getEnvAsInt("CACHE_MAX_SIZE_MB", 512),
		},
		LoadBalancer: LoadBalancerConfig{
			Strategy:            getEnv("LB_STRATEGY", "round_robin"),
			HealthCheckInterval: getEnvAsDuration("LB_HEALTH_CHECK_INTERVAL", 10*time.Second),
			HealthCheckTimeout:  getEnvAsDuration("LB_HEALTH_CHECK_TIMEOUT", 5*time.Second),
			MaxRetryAttempts:    getEnvAsInt("LB_MAX_RETRY_ATTEMPTS", 3),
		},
		Observability: ObservabilityConfig{
			OTELEnabled:          getEnvAsBool("OTEL_ENABLED", true),
			OTELServiceName:      getEnv("OTEL_SERVICE_NAME", "vantageedge"),
			OTELExporterEndpoint: getEnv("OTEL_EXPORTER_ENDPOINT", "http://jaeger:14268/api/traces"),
			MetricsEnabled:       getEnvAsBool("METRICS_ENABLED", true),
			MetricsPort:          getEnvAsInt("METRICS_PORT", 9091),
			LogLevel:             getEnv("LOG_LEVEL", "info"),
			LogFormat:            getEnv("LOG_FORMAT", "json"),
		},
		CORS: func() CORSConfig {
			env := getEnv("APP_ENV", "development")
			var defaultOrigins []string
			if os.Getenv("CORS_ALLOWED_ORIGINS") != "" {
				defaultOrigins = getEnvAsSlice("CORS_ALLOWED_ORIGINS", []string{})
			} else if env == "production" {
				defaultOrigins = []string{"*"}
			} else {
				defaultOrigins = []string{"http://localhost:3000", "https://vantageedge.vercel.app"}
			}
			return CORSConfig{
				AllowedOrigins:   defaultOrigins,
				AllowedMethods:   getEnvAsSlice("CORS_ALLOWED_METHODS", []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"}),
				AllowedHeaders:   getEnvAsSlice("CORS_ALLOWED_HEADERS", []string{"Authorization", "Content-Type", "X-API-Key", "X-Tenant-ID"}),
				AllowCredentials: getEnvAsBool("CORS_ALLOW_CREDENTIALS", true),
			}
		}(),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if c.Clerk.SecretKey == "" {
		return fmt.Errorf("CLERK_SECRET_KEY is required")
	}

	if c.Database.Host == "" {
		return fmt.Errorf("DB_HOST is required")
	}

	if c.Redis.Host == "" {
		return fmt.Errorf("REDIS_HOST is required")
	}

	return nil
}

func (c *DatabaseConfig) ConnectionString() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.Name, c.SSLMode,
	)
}

func (c *RedisConfig) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// Helper functions

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvAsBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getEnvAsDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

func getEnvAsSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		var result []string
		for _, item := range splitAndTrim(value, ",") {
			if item != "" {
				result = append(result, item)
			}
		}
		return result
	}
	return defaultValue
}

func splitAndTrim(s, sep string) []string {
	var result []string
	for _, item := range split(s, sep) {
		if trimmed := trim(item); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func split(s, sep string) []string {
	var result []string
	current := ""
	for _, char := range s {
		if string(char) == sep {
			result = append(result, current)
			current = ""
		} else {
			current += string(char)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func trim(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}
