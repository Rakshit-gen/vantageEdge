//go:build integration

// Integration tests run against a real Postgres instance with migrations
// already applied (see .github/workflows/ci.yml and Makefile's
// `test-integration` target). They exercise the SQL in this package
// directly — the layer unit tests can't cover, since unit tests here would
// otherwise just be testing that a fake driver echoes back what was sent
// to it.
//
// Run locally with:
//
//	docker compose up -d postgres
//	DB_HOST=localhost DB_PASSWORD=changeme_db_password make migrate-up
//	go test -tags=integration ./internal/repository/...
package repository

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/models"
	"github.com/vantageedge/backend/pkg/config"
	"github.com/vantageedge/backend/pkg/database"
	"github.com/vantageedge/backend/pkg/logger"
)

func testDB(t *testing.T) *database.DB {
	t.Helper()

	port, _ := strconv.Atoi(envOr("DB_PORT", "5432"))
	cfg := &config.DatabaseConfig{
		Host:               envOr("DB_HOST", "localhost"),
		Port:               port,
		User:               envOr("DB_USER", "vantageedge"),
		Password:           envOr("DB_PASSWORD", "changeme_db_password"),
		Name:               envOr("DB_NAME", "vantageedge"),
		SSLMode:            envOr("DB_SSLMODE", "disable"),
		MaxConnections:     5,
		MaxIdleConnections: 2,
		MaxLifetime:        5 * time.Minute,
	}

	log := logger.New("error", "json")
	db, err := database.New(cfg, log)
	if err != nil {
		t.Fatalf("failed to connect to test database (is Postgres running with migrations applied? see file doc comment): %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func TestIntegration_TenantOriginRouteLifecycle(t *testing.T) {
	db := testDB(t)
	repos := New(db)
	ctx := context.Background()

	tenant := &models.Tenant{
		Name:      "Integration Test Tenant",
		Subdomain: "it-" + uuid.NewString()[:8],
		Status:    "active",
		Settings:  models.JSONB{},
	}
	if err := repos.Tenant.Create(ctx, tenant); err != nil {
		t.Fatalf("Tenant.Create: %v", err)
	}

	origin := &models.Origin{
		TenantID:        tenant.ID,
		Name:            "primary",
		URL:             "https://example.com",
		HealthCheckPath: "/health",
		TimeoutSeconds:  30,
		Weight:          100,
	}
	if err := repos.Origin.Create(ctx, origin); err != nil {
		t.Fatalf("Origin.Create: %v", err)
	}

	secondOrigin := &models.Origin{
		TenantID:       tenant.ID,
		Name:           "secondary",
		URL:            "https://example2.com",
		TimeoutSeconds: 30,
		Weight:         50,
	}
	if err := repos.Origin.Create(ctx, secondOrigin); err != nil {
		t.Fatalf("Origin.Create (second): %v", err)
	}

	route := &models.Route{
		TenantID:                   tenant.ID,
		OriginID:                   origin.ID,
		Name:                       "test-route",
		PathPattern:                "/api/*",
		Methods:                    models.StringArray{"GET"},
		AuthMode:                   "public",
		IsActive:                   true,
		RateLimitEnabled:           true,
		RateLimitRequestsPerSecond: 10,
		RateLimitBurst:             20,
		CacheTTLSeconds:            60,
		Metadata:                   models.JSONB{},
	}
	if err := repos.Route.Create(ctx, route); err != nil {
		t.Fatalf("Route.Create: %v", err)
	}

	// The route's primary origin must be auto-registered as a pool member.
	pool, err := repos.Route.ListOrigins(ctx, route.ID)
	if err != nil {
		t.Fatalf("Route.ListOrigins: %v", err)
	}
	if len(pool) != 1 || pool[0].ID != origin.ID {
		t.Fatalf("expected pool to contain exactly the primary origin, got %+v", pool)
	}

	// Adding a second origin grows the pool.
	if err := repos.Route.AddOrigin(ctx, route.ID, secondOrigin.ID); err != nil {
		t.Fatalf("Route.AddOrigin: %v", err)
	}
	pool, err = repos.Route.ListOrigins(ctx, route.ID)
	if err != nil {
		t.Fatalf("Route.ListOrigins after add: %v", err)
	}
	if len(pool) != 2 {
		t.Fatalf("expected pool of 2 after AddOrigin, got %d", len(pool))
	}

	// Route matching semantics: FindMatchingRoute was removed in favor of
	// app-level matching, but Update must actually persist every mutable
	// field (regression test for the silent-data-loss bug fixed in this
	// pass).
	route.RateLimitRequestsPerSecond = 999
	route.CacheEnabled = true
	if err := repos.Route.Update(ctx, route); err != nil {
		t.Fatalf("Route.Update: %v", err)
	}
	reloaded, err := repos.Route.GetByID(ctx, route.ID)
	if err != nil {
		t.Fatalf("Route.GetByID: %v", err)
	}
	if reloaded.RateLimitRequestsPerSecond != 999 || !reloaded.CacheEnabled {
		t.Fatalf("Update did not persist rate_limit_requests_per_second/cache_enabled, got %+v", reloaded)
	}

	// Cleanup (cascades to route_origins via FK ON DELETE CASCADE).
	_ = repos.Route.Delete(ctx, route.ID)
	_ = repos.Origin.Delete(ctx, origin.ID)
	_ = repos.Origin.Delete(ctx, secondOrigin.ID)
	_ = repos.Tenant.Delete(ctx, tenant.ID)
}

func TestIntegration_APIKeyHashLookup(t *testing.T) {
	db := testDB(t)
	repos := New(db)
	ctx := context.Background()

	tenant := &models.Tenant{
		Name:      "API Key Test Tenant",
		Subdomain: "it-" + uuid.NewString()[:8],
		Status:    "active",
		Settings:  models.JSONB{},
	}
	if err := repos.Tenant.Create(ctx, tenant); err != nil {
		t.Fatalf("Tenant.Create: %v", err)
	}
	t.Cleanup(func() { _ = repos.Tenant.Delete(context.Background(), tenant.ID) })

	key := &models.APIKey{
		TenantID:  tenant.ID,
		Name:      "integration-test-key",
		KeyPrefix: "ve_test_",
		KeyHash:   "deadbeef" + uuid.NewString(),
		Scopes:    models.StringArray{"read", "write"},
		IsActive:  true,
		Metadata:  models.JSONB{},
	}
	if err := repos.APIKey.Create(ctx, key); err != nil {
		t.Fatalf("APIKey.Create: %v", err)
	}

	found, err := repos.APIKey.GetByHash(ctx, key.KeyHash)
	if err != nil {
		t.Fatalf("APIKey.GetByHash: %v", err)
	}
	if found.ID != key.ID {
		t.Fatalf("GetByHash returned wrong key: got %s, want %s", found.ID, key.ID)
	}
	if len(found.Scopes) != 2 {
		t.Fatalf("expected scopes to round-trip through StringArray, got %v", found.Scopes)
	}

	if err := repos.APIKey.UpdateUsage(ctx, key.ID); err != nil {
		t.Fatalf("APIKey.UpdateUsage: %v", err)
	}
	reloaded, err := repos.APIKey.GetByID(ctx, key.ID)
	if err != nil {
		t.Fatalf("APIKey.GetByID: %v", err)
	}
	if reloaded.UsageCount != 1 {
		t.Fatalf("expected usage_count to be incremented to 1, got %d", reloaded.UsageCount)
	}
}
