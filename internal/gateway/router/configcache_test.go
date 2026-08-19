package router

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/api/proto/configpb"
	"github.com/vantageedge/backend/internal/gateway/configclient"
	"github.com/vantageedge/backend/pkg/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// fakeConfigServer is a hand-rolled ConfigServiceServer stub — no
// database, no real network — so ConfigCache's own logic (caching,
// wire-format conversion, route/method matching, tenant-status handling)
// can be tested without standing up Postgres.
type fakeConfigServer struct {
	configpb.UnimplementedConfigServiceServer
	configs map[string]*configpb.TenantConfig // subdomain -> config
}

func (f *fakeConfigServer) GetTenantConfig(_ context.Context, req *configpb.GetTenantConfigRequest) (*configpb.TenantConfig, error) {
	cfg, ok := f.configs[req.Subdomain]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "tenant not found")
	}
	return cfg, nil
}

func newTestConfigCache(t *testing.T, configs map[string]*configpb.TenantConfig) *ConfigCache {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	configpb.RegisterConfigServiceServer(server, &fakeConfigServer{configs: configs})
	go server.Serve(lis)
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to dial bufconn: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	// configclient.NewClient dials by address; build the Client struct
	// directly here instead, reusing the same bufconn connection, since
	// there's no real address to dial in-memory.
	client := configclient.NewClientFromConn(conn, logger.New("error", "json"))

	return NewConfigCache(client, time.Minute)
}

func TestConfigCache_MatchesHighestPriorityRoute(t *testing.T) {
	tenantID := uuid.New().String()
	routeIDLow := uuid.New().String()
	routeIDHigh := uuid.New().String()
	originID := uuid.New().String()

	configs := map[string]*configpb.TenantConfig{
		"acme": {
			TenantId:  tenantID,
			Subdomain: "acme",
			Status:    "active",
			Routes: []*configpb.RouteConfig{
				{
					Id: routeIDLow, Name: "low-priority", PathPattern: "/api/*",
					Methods: []string{"GET"}, Priority: 1, AuthMode: "public",
					Origins: []*configpb.OriginConfig{{Id: originID, Url: "http://low.example.com", Weight: 100}},
				},
				{
					Id: routeIDHigh, Name: "high-priority", PathPattern: "/api/*",
					Methods: []string{"GET"}, Priority: 100, AuthMode: "public",
					Origins: []*configpb.OriginConfig{{Id: originID, Url: "http://high.example.com", Weight: 100}},
				},
			},
		},
	}
	cache := newTestConfigCache(t, configs)

	route, pool, gotTenantID, err := cache.Match(context.Background(), "acme", "/api/hello", "GET")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if route.Name != "high-priority" {
		t.Errorf("expected the higher-priority route to win when both match, got %q", route.Name)
	}
	if gotTenantID.String() != tenantID {
		t.Errorf("tenant ID mismatch: got %s, want %s", gotTenantID, tenantID)
	}
	if len(pool) != 1 || pool[0].URL != "http://high.example.com" {
		t.Errorf("expected pool from the matched route, got %+v", pool)
	}
}

func TestConfigCache_UnknownTenant(t *testing.T) {
	cache := newTestConfigCache(t, map[string]*configpb.TenantConfig{})

	_, _, _, err := cache.Match(context.Background(), "nonexistent", "/api/hello", "GET")
	if err != ErrTenantNotFound {
		t.Fatalf("expected ErrTenantNotFound, got %v", err)
	}
}

func TestConfigCache_SuspendedTenant(t *testing.T) {
	configs := map[string]*configpb.TenantConfig{
		"suspended-co": {TenantId: uuid.New().String(), Subdomain: "suspended-co", Status: "suspended"},
	}
	cache := newTestConfigCache(t, configs)

	_, _, _, err := cache.Match(context.Background(), "suspended-co", "/api/hello", "GET")
	if err != ErrTenantSuspended {
		t.Fatalf("expected ErrTenantSuspended, got %v", err)
	}
}

func TestConfigCache_NoMatchingRoute(t *testing.T) {
	configs := map[string]*configpb.TenantConfig{
		"acme": {
			TenantId: uuid.New().String(), Subdomain: "acme", Status: "active",
			Routes: []*configpb.RouteConfig{
				{Id: uuid.New().String(), PathPattern: "/only-this/*", Methods: []string{"GET"}},
			},
		},
	}
	cache := newTestConfigCache(t, configs)

	_, _, _, err := cache.Match(context.Background(), "acme", "/somewhere/else", "GET")
	if err != ErrNoMatchingRoute {
		t.Fatalf("expected ErrNoMatchingRoute, got %v", err)
	}
}

func TestConfigCache_Invalidate_ForcesRefetch(t *testing.T) {
	tenantID := uuid.New().String()
	server := &fakeConfigServer{configs: map[string]*configpb.TenantConfig{
		"acme": {TenantId: tenantID, Subdomain: "acme", Status: "active"},
	}}

	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	configpb.RegisterConfigServiceServer(grpcServer, server)
	go grpcServer.Serve(lis)
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to dial bufconn: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	client := configclient.NewClientFromConn(conn, logger.New("error", "json"))

	// Long TTL: without invalidation, a second Match within the TTL window
	// must NOT see the route added below.
	cache := NewConfigCache(client, time.Hour)

	if _, _, _, err := cache.Match(context.Background(), "acme", "/api/hello", "GET"); err != ErrNoMatchingRoute {
		t.Fatalf("expected no route on first fetch, got %v", err)
	}

	// Mutate the "backend" and invalidate — without calling Invalidate,
	// the long TTL would keep serving the stale (routeless) config.
	newRouteID := uuid.New().String()
	server.configs["acme"].Routes = []*configpb.RouteConfig{
		{Id: newRouteID, PathPattern: "/api/*", Methods: []string{"GET"}, AuthMode: "public"},
	}
	cache.Invalidate(tenantID)

	route, _, _, err := cache.Match(context.Background(), "acme", "/api/hello", "GET")
	if err != nil {
		t.Fatalf("expected the newly-added route to be visible after Invalidate, got error: %v", err)
	}
	if route.ID.String() != newRouteID {
		t.Errorf("expected route %s, got %s", newRouteID, route.ID)
	}
}
