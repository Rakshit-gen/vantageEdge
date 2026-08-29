package repository

import (
	"context"
	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/models"
	"github.com/vantageedge/backend/pkg/database"
)

type RouteRepository interface {
	Create(ctx context.Context, route *models.Route) error
	GetByID(ctx context.Context, id uuid.UUID) (*models.Route, error)
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*models.Route, error)
	Update(ctx context.Context, route *models.Route) error
	Delete(ctx context.Context, id uuid.UUID) error

	// Origin pool management: a route load-balances across these origins.
	ListOrigins(ctx context.Context, routeID uuid.UUID) ([]*models.Origin, error)
	AddOrigin(ctx context.Context, routeID, originID uuid.UUID) error
	RemoveOrigin(ctx context.Context, routeID, originID uuid.UUID) error
}

type routeRepository struct {
	db *database.DB
}

func NewRouteRepository(db *database.DB) RouteRepository {
	return &routeRepository{db: db}
}

func (r *routeRepository) Create(ctx context.Context, route *models.Route) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `INSERT INTO routes (tenant_id, origin_id, name, path_pattern, methods, priority, auth_mode,
	          rate_limit_enabled, rate_limit_requests_per_second, rate_limit_burst, rate_limit_key_strategy,
	          cache_enabled, cache_ttl_seconds, cache_key_pattern, cache_bypass_rules,
	          request_headers, response_headers, timeout_seconds, retry_attempts, load_balancing, metadata)
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
	          RETURNING id, created_at, updated_at`
	if err := tx.QueryRowxContext(ctx, query,
		route.TenantID, route.OriginID, route.Name, route.PathPattern, route.Methods, route.Priority, route.AuthMode,
		route.RateLimitEnabled, route.RateLimitRequestsPerSecond, route.RateLimitBurst, route.RateLimitKeyStrategy,
		route.CacheEnabled, route.CacheTTLSeconds, route.CacheKeyPattern, route.CacheBypassRules,
		route.RequestHeaders, route.ResponseHeaders, route.TimeoutSeconds, route.RetryAttempts, route.LoadBalancing, route.Metadata).
		Scan(&route.ID, &route.CreatedAt, &route.UpdatedAt); err != nil {
		return err
	}

	// The route's primary origin is always a pool member, so a
	// newly-created route is immediately servable without a separate
	// "add origin to pool" call.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO route_origins (route_id, origin_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		route.ID, route.OriginID); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *routeRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Route, error) {
	var route models.Route
	query := `SELECT * FROM routes WHERE id = $1`
	err := r.db.GetContext(ctx, &route, query, id)
	return &route, err
}

func (r *routeRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*models.Route, error) {
	var routes []*models.Route
	query := `SELECT * FROM routes WHERE tenant_id = $1 AND is_active = true ORDER BY priority DESC`
	err := r.db.SelectContext(ctx, &routes, query, tenantID)
	return routes, err
}

func (r *routeRepository) Update(ctx context.Context, route *models.Route) error {
	// Every user-editable column must be listed here: the service layer
	// applies partial updates to the in-memory struct and hands the full
	// struct back to the caller as the "saved" result, so any column
	// missing from this statement is silently discarded (the caller is
	// told the change succeeded, but a re-read from the DB would show the
	// old value).
	query := `UPDATE routes SET name = $1, path_pattern = $2, methods = $3, priority = $4,
	          auth_mode = $5, is_active = $6, rate_limit_enabled = $7,
	          rate_limit_requests_per_second = $8, rate_limit_burst = $9, rate_limit_key_strategy = $10,
	          cache_enabled = $11, cache_ttl_seconds = $12, cache_key_pattern = $13,
	          timeout_seconds = $14, retry_attempts = $15, load_balancing = $16
	          WHERE id = $17`
	_, err := r.db.ExecContext(ctx, query,
		route.Name, route.PathPattern, route.Methods, route.Priority,
		route.AuthMode, route.IsActive, route.RateLimitEnabled,
		route.RateLimitRequestsPerSecond, route.RateLimitBurst, route.RateLimitKeyStrategy,
		route.CacheEnabled, route.CacheTTLSeconds, route.CacheKeyPattern,
		route.TimeoutSeconds, route.RetryAttempts, route.LoadBalancing, route.ID)
	return err
}

func (r *routeRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM routes WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

func (r *routeRepository) ListOrigins(ctx context.Context, routeID uuid.UUID) ([]*models.Origin, error) {
	var origins []*models.Origin
	query := `SELECT o.* FROM origins o
	          JOIN route_origins ro ON ro.origin_id = o.id
	          WHERE ro.route_id = $1
	          ORDER BY o.name`
	err := r.db.SelectContext(ctx, &origins, query, routeID)
	return origins, err
}

func (r *routeRepository) AddOrigin(ctx context.Context, routeID, originID uuid.UUID) error {
	query := `INSERT INTO route_origins (route_id, origin_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	_, err := r.db.ExecContext(ctx, query, routeID, originID)
	return err
}

func (r *routeRepository) RemoveOrigin(ctx context.Context, routeID, originID uuid.UUID) error {
	query := `DELETE FROM route_origins WHERE route_id = $1 AND origin_id = $2`
	_, err := r.db.ExecContext(ctx, query, routeID, originID)
	return err
}
