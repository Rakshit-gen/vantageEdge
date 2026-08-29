//go:build integration

// End-to-end test of the full gateway pipeline (tenant resolution -> route
// match -> auth -> rate limit -> cache -> proxy -> origin pool selection)
// against real Postgres and Redis, proving the pieces wired together in
// this pass actually work as a system rather than only in isolation.
//
// Run locally with:
//
//	docker compose up -d postgres redis
//	DB_HOST=localhost DB_PASSWORD=changeme_db_password make migrate-up
//	DB_HOST=localhost DB_PASSWORD=changeme_db_password REDIS_HOST=localhost \
//	  go test -tags=integration ./internal/gateway/router/...
package router

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/auth/apikey"
	authjwt "github.com/vantageedge/backend/internal/auth/jwt"
	redispkg "github.com/vantageedge/backend/internal/cache/redis"
	"github.com/vantageedge/backend/internal/controlplane/grpcserver"
	"github.com/vantageedge/backend/internal/eventbus"
	"github.com/vantageedge/backend/internal/gateway/configclient"
	"github.com/vantageedge/backend/internal/gateway/middleware"
	"github.com/vantageedge/backend/internal/gateway/proxy"
	"github.com/vantageedge/backend/internal/loadbalancer"
	"github.com/vantageedge/backend/internal/models"
	"github.com/vantageedge/backend/internal/observability"
	"github.com/vantageedge/backend/internal/ratelimit"
	"github.com/vantageedge/backend/internal/repository"
	"github.com/vantageedge/backend/pkg/config"
	"github.com/vantageedge/backend/pkg/database"
	"github.com/vantageedge/backend/pkg/logger"
	"google.golang.org/grpc"

	"github.com/vantageedge/backend/api/proto/configpb"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// testJWKSServer mirrors internal/auth/jwt's test helper; duplicated
// rather than shared across package boundaries to keep each test package
// self-contained. It's unused by the public-route tests below but wired in
// so the harness is realistic (a jwt_required route would exercise it).
func testJWKSServer(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	jwk := map[string]interface{}{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "test-key-1",
		"n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"keys": []interface{}{jwk}})
	}))
	t.Cleanup(server.Close)
	return server.URL
}

type testHarness struct {
	repos   *repository.Repository
	gateway http.Handler
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()

	dbPort, _ := strconv.Atoi(envOr("DB_PORT", "5432"))
	dbCfg := &config.DatabaseConfig{
		Host: envOr("DB_HOST", "localhost"), Port: dbPort,
		User: envOr("DB_USER", "vantageedge"), Password: envOr("DB_PASSWORD", "changeme_db_password"),
		Name: envOr("DB_NAME", "vantageedge"), SSLMode: "disable",
		MaxConnections: 5, MaxIdleConnections: 2, MaxLifetime: 5 * time.Minute,
	}
	log := logger.New("error", "json")
	db, err := database.New(dbCfg, log)
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	redisClient, err := redispkg.NewClient(fmt.Sprintf("redis://%s:%s/0", envOr("REDIS_HOST", "localhost"), envOr("REDIS_PORT", "6379")))
	if err != nil {
		t.Fatalf("failed to connect to test Redis: %v", err)
	}
	t.Cleanup(func() { redisClient.Close() })

	repos := repository.New(db)

	jwksURL := testJWKSServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	jwtValidator, err := authjwt.NewJWTValidator(ctx, jwksURL, "", "")
	if err != nil {
		t.Fatalf("failed to build JWT validator: %v", err)
	}
	authenticator := middleware.NewAuthenticator(jwtValidator, apikey.NewValidator(repos))

	// The gateway no longer reads route/origin config from Postgres
	// directly — it calls the control plane's gRPC ConfigService, so the
	// harness has to run a real (if minimal) instance of that service
	// in-process for the gateway-under-test to dial.
	grpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen for test gRPC server: %v", err)
	}
	grpcServer := grpc.NewServer()
	configpb.RegisterConfigServiceServer(grpcServer, grpcserver.NewConfigServer(repos, eventbus.NewHub(), log))
	go grpcServer.Serve(grpcListener)
	t.Cleanup(grpcServer.Stop)

	configClient, err := configclient.NewClient(grpcListener.Addr().String(), log)
	if err != nil {
		t.Fatalf("failed to create config client: %v", err)
	}
	t.Cleanup(func() { configClient.Close() })
	// Short TTL: tests seed data via the repository directly and then hit
	// the gateway immediately, so the cache must not serve a pre-seed miss
	// for the default 5s window.
	configCache := NewConfigCache(NewGRPCSource(configClient), 50*time.Millisecond)

	cfg := &config.Config{
		RateLimit:    config.RateLimitConfig{DefaultRPS: 100, DefaultBurst: 200},
		LoadBalancer: config.LoadBalancerConfig{HealthCheckInterval: time.Minute},
	}

	deps := Deps{
		ConfigCache:   configCache,
		Authenticator: authenticator,
		Limiter:       ratelimit.NewRedisLimiter(redisClient),
		Cache:         middleware.NewResponseCache(middleware.NewRedisStore(redisClient)),
		Proxy:         proxy.NewReverseProxy(5 * time.Second),
		Metrics:       observability.NewMetrics("test_" + uuid.NewString()[:8]),
		HealthChecker: loadbalancer.NewHealthChecker(log, time.Minute),
	}

	return &testHarness{
		repos:   repos,
		gateway: New(cfg, repos, deps, log),
	}
}

// seedTenantWithRoute creates a tenant, one or more origins pointing at
// origins (already-running httptest servers), and a route whose pool
// contains every one of them. Returns the tenant's subdomain.
func (h *testHarness) seedTenantWithRoute(t *testing.T, route *models.Route, originURLs ...string) string {
	t.Helper()
	ctx := context.Background()

	subdomain := "it-" + uuid.NewString()[:8]
	tenant := &models.Tenant{Name: "Integration Test", Subdomain: subdomain, Status: "active", Settings: models.JSONB{}}
	if err := h.repos.Tenant.Create(ctx, tenant); err != nil {
		t.Fatalf("Tenant.Create: %v", err)
	}
	t.Cleanup(func() { _ = h.repos.Tenant.Delete(context.Background(), tenant.ID) })

	var originIDs []uuid.UUID
	for i, url := range originURLs {
		origin := &models.Origin{TenantID: tenant.ID, Name: fmt.Sprintf("origin-%d", i), URL: url, TimeoutSeconds: 5, Weight: 100}
		if err := h.repos.Origin.Create(ctx, origin); err != nil {
			t.Fatalf("Origin.Create: %v", err)
		}
		originIDs = append(originIDs, origin.ID)
	}

	route.TenantID = tenant.ID
	route.OriginID = originIDs[0]
	route.IsActive = true
	route.Metadata = models.JSONB{}
	if err := h.repos.Route.Create(ctx, route); err != nil {
		t.Fatalf("Route.Create: %v", err)
	}
	for _, id := range originIDs[1:] {
		if err := h.repos.Route.AddOrigin(ctx, route.ID, id); err != nil {
			t.Fatalf("Route.AddOrigin: %v", err)
		}
	}

	return subdomain
}

func fakeOrigin(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func doRequest(t *testing.T, gateway http.Handler, subdomain, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = subdomain + ".vantageedge.test"
	rec := httptest.NewRecorder()
	gateway.ServeHTTP(rec, req)
	return rec
}

func TestIntegration_PublicRoute_ProxiesToOrigin(t *testing.T) {
	h := newTestHarness(t)
	origin := fakeOrigin(t, "hello from origin")

	route := &models.Route{
		Name: "public", PathPattern: "/api/*", Methods: models.StringArray{"GET"},
		AuthMode: "public", RateLimitEnabled: true, RateLimitRequestsPerSecond: 1000, RateLimitBurst: 1000,
	}
	subdomain := h.seedTenantWithRoute(t, route, origin.URL)

	rec := doRequest(t, h.gateway, subdomain, "/api/hello")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "hello from origin" {
		t.Fatalf("expected proxied body from origin, got %q", rec.Body.String())
	}
}

func TestIntegration_RateLimit_BlocksAfterBurst(t *testing.T) {
	h := newTestHarness(t)
	origin := fakeOrigin(t, "ok")

	route := &models.Route{
		Name: "limited", PathPattern: "/api/*", Methods: models.StringArray{"GET"},
		AuthMode: "public", RateLimitEnabled: true, RateLimitRequestsPerSecond: 1, RateLimitBurst: 3,
	}
	subdomain := h.seedTenantWithRoute(t, route, origin.URL)

	sawRateLimited := false
	for i := 0; i < 10; i++ {
		rec := doRequest(t, h.gateway, subdomain, "/api/hello")
		if rec.Code == http.StatusTooManyRequests {
			sawRateLimited = true
			if rec.Header().Get("Retry-After") == "" {
				t.Error("expected Retry-After header on a 429 response")
			}
			break
		}
	}
	if !sawRateLimited {
		t.Fatal("expected at least one request beyond burst capacity to be rate limited")
	}
}

func TestIntegration_Cache_SecondRequestIsHit(t *testing.T) {
	h := newTestHarness(t)
	callCount := 0
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Write([]byte("cached-body"))
	}))
	t.Cleanup(origin.Close)

	route := &models.Route{
		Name: "cached", PathPattern: "/api/*", Methods: models.StringArray{"GET"},
		AuthMode: "public", RateLimitEnabled: true, RateLimitRequestsPerSecond: 1000, RateLimitBurst: 1000,
		CacheEnabled: true, CacheTTLSeconds: 60, CacheKeyPattern: "path+query",
	}
	subdomain := h.seedTenantWithRoute(t, route, origin.URL)

	first := doRequest(t, h.gateway, subdomain, "/api/cacheme")
	if first.Header().Get("X-Cache") == "HIT" {
		t.Fatal("expected first request to be a cache miss")
	}
	second := doRequest(t, h.gateway, subdomain, "/api/cacheme")
	if second.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("expected second request to be served from cache, got X-Cache=%q", second.Header().Get("X-Cache"))
	}
	if callCount != 1 {
		t.Fatalf("expected origin to be hit exactly once, got %d calls", callCount)
	}
}

func TestIntegration_OriginPool_DistributesAcrossOrigins(t *testing.T) {
	h := newTestHarness(t)
	originA := fakeOrigin(t, "origin-a")
	originB := fakeOrigin(t, "origin-b")

	route := &models.Route{
		Name: "pooled", PathPattern: "/api/*", Methods: models.StringArray{"GET"},
		AuthMode: "public", RateLimitEnabled: true, RateLimitRequestsPerSecond: 1000, RateLimitBurst: 1000,
	}
	subdomain := h.seedTenantWithRoute(t, route, originA.URL, originB.URL)

	seenA, seenB := false, false
	for i := 0; i < 40 && !(seenA && seenB); i++ {
		rec := doRequest(t, h.gateway, subdomain, "/api/hello")
		switch rec.Body.String() {
		case "origin-a":
			seenA = true
		case "origin-b":
			seenB = true
		}
	}
	if !seenA || !seenB {
		t.Fatalf("expected requests to be distributed across both pool members within 40 tries, saw A=%v B=%v", seenA, seenB)
	}
}
