package router

import (
	"testing"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/models"
)

func TestBuildTenantConfig_SortsByPriorityDescAndCarriesFields(t *testing.T) {
	tenant := &models.Tenant{ID: uuid.New(), Status: "active"}
	low := &models.Route{ID: uuid.New(), Priority: 1, AuthMode: "public"}
	high := &models.Route{ID: uuid.New(), Priority: 100, AuthMode: "apikey_required"}

	cfg := buildTenantConfig(tenant, []*models.Route{low, high}, map[uuid.UUID][]*models.Origin{
		high.ID: {{ID: uuid.New(), URL: "http://high"}},
	})

	if cfg.tenantID != tenant.ID || cfg.status != "active" {
		t.Fatalf("tenant fields not carried: %+v", cfg)
	}
	if cfg.routes[0].ID != high.ID {
		t.Errorf("expected highest-priority route first, got priority %d", cfg.routes[0].Priority)
	}
	if cfg.routes[0].AuthMode != "apikey_required" {
		t.Errorf("route fields not carried through: %+v", cfg.routes[0])
	}
	if len(cfg.origins[high.ID]) != 1 || cfg.origins[high.ID][0].URL != "http://high" {
		t.Errorf("origin pool not carried: %+v", cfg.origins)
	}
}
