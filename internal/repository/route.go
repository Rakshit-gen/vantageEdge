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
	FindMatchingRoute(ctx context.Context, tenantID uuid.UUID, path, method string) (*models.Route, error)
}

type routeRepository struct {
	db *database.DB
}

func NewRouteRepository(db *database.DB) RouteRepository {
	return &routeRepository{db: db}
}

func (r *routeRepository) Create(ctx context.Context, route *models.Route) error {
	query := `INSERT INTO routes (tenant_id, origin_id, name, path_pattern, methods, priority, auth_mode,
	          rate_limit_enabled, rate_limit_requests_per_second, rate_limit_burst, rate_limit_key_strategy,
	          cache_enabled, cache_ttl_seconds, cache_key_pattern, cache_bypass_rules,
	          request_headers, response_headers, timeout_seconds, retry_attempts, metadata) 
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20) 
	          RETURNING id, created_at, updated_at`
	return r.db.QueryRowContext(ctx, query,
		route.TenantID, route.OriginID, route.Name, route.PathPattern, route.Methods, route.Priority, route.AuthMode,
		route.RateLimitEnabled, route.RateLimitRequestsPerSecond, route.RateLimitBurst, route.RateLimitKeyStrategy,
		route.CacheEnabled, route.CacheTTLSeconds, route.CacheKeyPattern, route.CacheBypassRules,
		route.RequestHeaders, route.ResponseHeaders, route.TimeoutSeconds, route.RetryAttempts, route.Metadata).
		Scan(&route.ID, &route.CreatedAt, &route.UpdatedAt)
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
	query := `UPDATE routes SET name = $1, path_pattern = $2, methods = $3, priority = $4,
	          auth_mode = $5, is_active = $6 WHERE id = $7`
	_, err := r.db.ExecContext(ctx, query,
		route.Name, route.PathPattern, route.Methods, route.Priority,
		route.AuthMode, route.IsActive, route.ID)
	return err
}

func (r *routeRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM routes WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

func (r *routeRepository) FindMatchingRoute(ctx context.Context, tenantID uuid.UUID, path, method string) (*models.Route, error) {
	var route models.Route
	query := `SELECT * FROM routes 
	          WHERE tenant_id = $1 AND is_active = true 
	          AND $2 LIKE path_pattern AND $3 = ANY(methods)
	          ORDER BY priority DESC LIMIT 1`
	err := r.db.GetContext(ctx, &route, query, tenantID, path, method)
	return &route, err
}
