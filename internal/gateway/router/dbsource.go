package router

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/models"
	"github.com/vantageedge/backend/internal/repository"
)

// dbConfigSource reads tenant config straight from Postgres. It's the
// source the gateway uses when no control-plane gRPC address is set —
// e.g. a managed host that routes only one port per service, which makes
// the control plane's separate gRPC port unreachable from the gateway.
// The gateway already holds a DB handle for the request-log write path,
// so this adds no new connection.
//
// Trade-off vs. grpcConfigSource: no push invalidation, so a route or
// origin change takes up to ConfigCache's TTL to take effect.
type dbConfigSource struct {
	repos *repository.Repository
}

// NewDBSource builds a ConfigSource that reads tenant/route/origin config
// directly from the database.
func NewDBSource(repos *repository.Repository) ConfigSource {
	return &dbConfigSource{repos: repos}
}

func (s *dbConfigSource) Fetch(ctx context.Context, subdomain string) (*tenantConfig, error) {
	tenant, err := s.repos.Tenant.GetBySubdomain(ctx, subdomain)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTenantNotFound
		}
		return nil, fmt.Errorf("look up tenant %q: %w", subdomain, err)
	}

	// ListByTenant already filters to active routes and orders by priority;
	// buildTenantConfig re-sorts anyway so the ordering contract lives in
	// one place regardless of source (matches toTenantConfig).
	routes, err := s.repos.Route.ListByTenant(ctx, tenant.ID)
	if err != nil {
		return nil, fmt.Errorf("list routes for tenant %s: %w", tenant.ID, err)
	}

	originsByRoute := make(map[uuid.UUID][]*models.Origin, len(routes))
	for _, route := range routes {
		pool, err := s.repos.Route.ListOrigins(ctx, route.ID)
		if err != nil {
			return nil, fmt.Errorf("list origins for route %s: %w", route.ID, err)
		}
		originsByRoute[route.ID] = pool
	}

	return buildTenantConfig(tenant, routes, originsByRoute), nil
}

// buildTenantConfig assembles the gateway-local tenantConfig and enforces
// priority ordering (highest first), so ConfigCache.Match can treat every
// source identically.
func buildTenantConfig(tenant *models.Tenant, routes []*models.Route, originsByRoute map[uuid.UUID][]*models.Origin) *tenantConfig {
	cfg := &tenantConfig{
		tenantID: tenant.ID,
		status:   tenant.Status,
		routes:   make([]*models.Route, len(routes)),
		origins:  make(map[uuid.UUID][]*models.Origin, len(originsByRoute)),
	}
	copy(cfg.routes, routes)
	for routeID, pool := range originsByRoute {
		cfg.origins[routeID] = pool
	}
	sort.SliceStable(cfg.routes, func(i, j int) bool {
		return cfg.routes[i].Priority > cfg.routes[j].Priority
	})
	return cfg
}
