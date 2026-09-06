package router

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/api/proto/configpb"
	"github.com/vantageedge/backend/internal/models"
)

// ErrNoMatchingRoute is returned when no active route matches the request.
var ErrNoMatchingRoute = fmt.Errorf("no matching route")

// ErrTenantSuspended is returned when a tenant exists but its status
// isn't "active". Previously nothing checked tenant status at all in the
// gateway, so a suspended tenant's traffic kept being served exactly as
// before suspension.
var ErrTenantSuspended = fmt.Errorf("tenant is suspended")

// ErrTenantNotFound is returned when the subdomain doesn't match any
// tenant.
var ErrTenantNotFound = fmt.Errorf("tenant not found")

// tenantConfig is the gateway-local, already-parsed form of a
// configpb.TenantConfig: routes and origins converted to the same
// *models.Route / *models.Origin types the rest of the gateway (proxy,
// authenticator, load balancer) already works with, so this is the only
// place that deals with the wire format.
type tenantConfig struct {
	tenantID uuid.UUID
	status   string
	routes   []*models.Route
	origins  map[uuid.UUID][]*models.Origin // routeID -> pool
}

// ConfigCache fetches tenant/route/origin config from a ConfigSource and
// caches it per subdomain with a short TTL, so the gateway isn't hitting
// the source on every request. When the source supports push invalidation
// (the gRPC source does), Invalidate drops a stale entry ahead of its TTL.
type ConfigCache struct {
	source ConfigSource
	ttl    time.Duration

	mu             sync.RWMutex
	bySubdomain    map[string]configCacheEntry
	subdomainByTID map[string]string    // tenantID string -> subdomain, for invalidation lookups
	inflight       map[string]*cfgFetch // subdomain -> in-progress fetch, for single-flighting
}

type configCacheEntry struct {
	config    *tenantConfig
	expiresAt time.Time
}

// cfgFetch collapses concurrent refetches of the same subdomain into one
// upstream call: without it, every in-flight request for a tenant fires
// its own source.Fetch the instant the TTL lapses (a thundering herd of
// identical RPCs against the control plane, once per TTL, scaling with
// request rate).
type cfgFetch struct {
	done chan struct{}
	cfg  *tenantConfig
	err  error
}

func NewConfigCache(source ConfigSource, ttl time.Duration) *ConfigCache {
	if ttl <= 0 {
		ttl = 5 * time.Second
	}
	return &ConfigCache{
		source:         source,
		ttl:            ttl,
		bySubdomain:    make(map[string]configCacheEntry),
		subdomainByTID: make(map[string]string),
		inflight:       make(map[string]*cfgFetch),
	}
}

// Invalidate drops the cached entry for tenantID, if any is held. Called
// from the control plane's push stream (see configclient.WatchInvalidations)
// so a route/origin change is reflected on the very next request instead
// of waiting out the TTL.
func (c *ConfigCache) Invalidate(tenantID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if subdomain, ok := c.subdomainByTID[tenantID]; ok {
		delete(c.bySubdomain, subdomain)
		delete(c.subdomainByTID, tenantID)
	}
}

func (c *ConfigCache) get(ctx context.Context, subdomain string) (*tenantConfig, error) {
	c.mu.RLock()
	entry, ok := c.bySubdomain[subdomain]
	c.mu.RUnlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return entry.config, nil
	}

	c.mu.Lock()
	// Re-check under the write lock: another goroutine may have refreshed
	// while we waited for it.
	if entry, ok := c.bySubdomain[subdomain]; ok && time.Now().Before(entry.expiresAt) {
		c.mu.Unlock()
		return entry.config, nil
	}
	if fl, ok := c.inflight[subdomain]; ok {
		// A refetch for this subdomain is already running — wait for it
		// instead of starting a second one.
		c.mu.Unlock()
		select {
		case <-fl.done:
			return c.resultOrStale(subdomain, fl)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	fl := &cfgFetch{done: make(chan struct{})}
	c.inflight[subdomain] = fl
	c.mu.Unlock()

	// Detach from this one request's context: the shared fetch must not be
	// cancelled just because the caller that happened to start it went
	// away. Other waiters still honour their own ctx above.
	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	cfg, err := c.source.Fetch(fetchCtx, subdomain)
	cancel()

	c.mu.Lock()
	delete(c.inflight, subdomain)
	if err == nil {
		c.bySubdomain[subdomain] = configCacheEntry{config: cfg, expiresAt: time.Now().Add(c.ttl)}
		c.subdomainByTID[cfg.tenantID.String()] = subdomain
	}
	c.mu.Unlock()

	fl.cfg, fl.err = cfg, err
	close(fl.done)

	return c.resultOrStale(subdomain, fl)
}

// resultOrStale returns a completed fetch's result, falling back to a
// still-cached (expired) entry when the refetch failed — a control-plane
// blip should briefly serve stale config, not 5xx a tenant's traffic.
func (c *ConfigCache) resultOrStale(subdomain string, fl *cfgFetch) (*tenantConfig, error) {
	if fl.err == nil {
		return fl.cfg, nil
	}
	c.mu.RLock()
	entry, ok := c.bySubdomain[subdomain]
	c.mu.RUnlock()
	if ok {
		return entry.config, nil
	}
	return nil, fl.err
}

// Match resolves subdomain to a tenant, then returns the highest-priority
// active route matching path and method plus that route's origin pool.
func (c *ConfigCache) Match(ctx context.Context, subdomain, path, method string) (*models.Route, []*models.Origin, uuid.UUID, error) {
	cfg, err := c.get(ctx, subdomain)
	if err != nil {
		return nil, nil, uuid.Nil, err
	}
	if cfg.status != "active" {
		return nil, nil, cfg.tenantID, ErrTenantSuspended
	}

	for _, route := range cfg.routes {
		if MatchMethod(route.Methods, method) && MatchPath(route.PathPattern, path) {
			return route, cfg.origins[route.ID], cfg.tenantID, nil
		}
	}
	return nil, nil, cfg.tenantID, ErrNoMatchingRoute
}

func toTenantConfig(pb *configpb.TenantConfig) (*tenantConfig, error) {
	tenantID, err := uuid.Parse(pb.TenantId)
	if err != nil {
		return nil, fmt.Errorf("control plane returned invalid tenant_id %q: %w", pb.TenantId, err)
	}

	cfg := &tenantConfig{
		tenantID: tenantID,
		status:   pb.Status,
		routes:   make([]*models.Route, 0, len(pb.Routes)),
		origins:  make(map[uuid.UUID][]*models.Origin, len(pb.Routes)),
	}

	for _, r := range pb.Routes {
		routeID, err := uuid.Parse(r.Id)
		if err != nil {
			continue // defensive: skip a malformed record rather than fail the whole tenant
		}

		cfg.routes = append(cfg.routes, &models.Route{
			ID:                         routeID,
			TenantID:                   tenantID,
			Name:                       r.Name,
			PathPattern:                r.PathPattern,
			Methods:                    models.StringArray(r.Methods),
			Priority:                   int(r.Priority),
			AuthMode:                   r.AuthMode,
			IsActive:                   true, // the control plane only ever includes active routes
			RateLimitEnabled:           r.RateLimitEnabled,
			RateLimitRequestsPerSecond: int(r.RateLimitRequestsPerSecond),
			RateLimitBurst:             int(r.RateLimitBurst),
			RateLimitKeyStrategy:       r.RateLimitKeyStrategy,
			CacheEnabled:               r.CacheEnabled,
			CacheTTLSeconds:            int(r.CacheTtlSeconds),
			CacheKeyPattern:            r.CacheKeyPattern,
			TimeoutSeconds:             int(r.TimeoutSeconds),
			RetryAttempts:              int(r.RetryAttempts),
			LoadBalancing:              r.LoadBalancing,
		})

		origins := make([]*models.Origin, 0, len(r.Origins))
		for _, o := range r.Origins {
			originID, err := uuid.Parse(o.Id)
			if err != nil {
				continue
			}
			origins = append(origins, &models.Origin{
				ID:              originID,
				TenantID:        tenantID,
				URL:             o.Url,
				Weight:          int(o.Weight),
				TimeoutSeconds:  int(o.TimeoutSeconds),
				HealthCheckPath: o.HealthCheckPath,
			})
		}
		cfg.origins[routeID] = origins
	}

	// Match walks cfg.routes in order and returns the first structural
	// match, so priority ordering must be enforced here rather than
	// assumed from the wire: relying on the control plane's SQL query
	// having ordered them is an implicit contract nothing enforces if the
	// RPC's response ever changes shape (e.g. a future caching layer on
	// the control-plane side, or a test double, that doesn't happen to
	// preserve order).
	sort.SliceStable(cfg.routes, func(i, j int) bool {
		return cfg.routes[i].Priority > cfg.routes[j].Priority
	})

	return cfg, nil
}
