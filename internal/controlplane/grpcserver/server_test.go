package grpcserver

import (
	"testing"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/models"
)

func TestToRouteConfig_ConvertsAllFields(t *testing.T) {
	routeID := uuid.New()
	originID := uuid.New()

	route := &models.Route{
		ID:                         routeID,
		Name:                       "test-route",
		PathPattern:                "/api/*",
		Methods:                    models.StringArray{"GET", "POST"},
		Priority:                   42,
		AuthMode:                   "jwt_required",
		RateLimitEnabled:           true,
		RateLimitRequestsPerSecond: 100,
		RateLimitBurst:             200,
		RateLimitKeyStrategy:       "tenant_user",
		CacheEnabled:               true,
		CacheTTLSeconds:            300,
		CacheKeyPattern:            "path+query",
		TimeoutSeconds:             30,
		RetryAttempts:              2,
	}
	origins := []*models.Origin{
		{ID: originID, URL: "https://backend.example.com", Weight: 100, TimeoutSeconds: 10, HealthCheckPath: "/health"},
	}

	pb := toRouteConfig(route, origins)

	if pb.Id != routeID.String() {
		t.Errorf("Id = %q, want %q", pb.Id, routeID.String())
	}
	if pb.Name != "test-route" || pb.PathPattern != "/api/*" {
		t.Errorf("unexpected name/pattern: %+v", pb)
	}
	if len(pb.Methods) != 2 || pb.Methods[0] != "GET" || pb.Methods[1] != "POST" {
		t.Errorf("Methods = %v, want [GET POST]", pb.Methods)
	}
	if pb.Priority != 42 {
		t.Errorf("Priority = %d, want 42", pb.Priority)
	}
	if !pb.RateLimitEnabled || pb.RateLimitRequestsPerSecond != 100 || pb.RateLimitBurst != 200 {
		t.Errorf("rate limit fields not converted correctly: %+v", pb)
	}
	if !pb.CacheEnabled || pb.CacheTtlSeconds != 300 || pb.CacheKeyPattern != "path+query" {
		t.Errorf("cache fields not converted correctly: %+v", pb)
	}
	if len(pb.Origins) != 1 {
		t.Fatalf("expected 1 origin, got %d", len(pb.Origins))
	}
	if pb.Origins[0].Id != originID.String() || pb.Origins[0].Url != "https://backend.example.com" || pb.Origins[0].Weight != 100 {
		t.Errorf("origin not converted correctly: %+v", pb.Origins[0])
	}
}

func TestToRouteConfig_EmptyOriginsList(t *testing.T) {
	pb := toRouteConfig(&models.Route{ID: uuid.New()}, nil)
	if pb.Origins == nil {
		t.Error("expected a non-nil (possibly empty) Origins slice, not nil — a nil slice serializes fine over gRPC but is easy to accidentally treat differently from an empty one downstream")
	}
	if len(pb.Origins) != 0 {
		t.Errorf("expected 0 origins, got %d", len(pb.Origins))
	}
}
