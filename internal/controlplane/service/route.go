package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/models"
	"github.com/vantageedge/backend/internal/repository"
	"github.com/vantageedge/backend/pkg/logger"
)

type RouteService interface {
	CreateRoute(ctx context.Context, req *CreateRouteRequest) (*models.Route, error)
	GetRoute(ctx context.Context, id uuid.UUID) (*models.Route, error)
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*models.Route, error)
	UpdateRoute(ctx context.Context, id uuid.UUID, req *UpdateRouteRequest) (*models.Route, error)
	DeleteRoute(ctx context.Context, id uuid.UUID) error
}

type CreateRouteRequest struct {
	TenantID                      uuid.UUID `json:"tenant_id"`
	OriginID                      uuid.UUID `json:"origin_id"`
	Name                          string    `json:"name"`
	PathPattern                   string    `json:"path_pattern"`
	Methods                       []string  `json:"methods"`
	Priority                      int       `json:"priority"`
	AuthMode                      string    `json:"auth_mode"`
	IsActive                      bool      `json:"is_active"`
	RateLimitEnabled              bool      `json:"rate_limit_enabled"`
	RateLimitRequestsPerSecond    int       `json:"rate_limit_requests_per_second"`
	RateLimitBurst                int       `json:"rate_limit_burst"`
	RateLimitKeyStrategy          string    `json:"rate_limit_key_strategy"`
	CacheEnabled                  bool      `json:"cache_enabled"`
	CacheTTLSeconds               int       `json:"cache_ttl_seconds"`
	CacheKeyPattern               string    `json:"cache_key_pattern"`
	TimeoutSeconds                int       `json:"timeout_seconds"`
	RetryAttempts                 int       `json:"retry_attempts"`
}

type UpdateRouteRequest struct {
	Name                       string   `json:"name"`
	PathPattern                string   `json:"path_pattern"`
	Methods                    []string `json:"methods"`
	Priority                   int      `json:"priority"`
	AuthMode                   string   `json:"auth_mode"`
	IsActive                   bool     `json:"is_active"`
	RateLimitEnabled           bool     `json:"rate_limit_enabled"`
	RateLimitRequestsPerSecond int      `json:"rate_limit_requests_per_second"`
	CacheEnabled               bool     `json:"cache_enabled"`
	CacheTTLSeconds            int      `json:"cache_ttl_seconds"`
}

type routeService struct {
	repos  *repository.Repository
	logger *logger.Logger
}

func NewRouteService(repos *repository.Repository, log *logger.Logger) RouteService {
	return &routeService{repos: repos, logger: log}
}

func (s *routeService) CreateRoute(ctx context.Context, req *CreateRouteRequest) (*models.Route, error) {
	route := &models.Route{
		TenantID:                      req.TenantID,
		OriginID:                      req.OriginID,
		Name:                          req.Name,
		PathPattern:                   req.PathPattern,
		Methods:                       models.StringArray(req.Methods),
		Priority:                      req.Priority,
		AuthMode:                      req.AuthMode,
		IsActive:                      req.IsActive,
		RateLimitEnabled:              req.RateLimitEnabled,
		RateLimitRequestsPerSecond:    req.RateLimitRequestsPerSecond,
		RateLimitBurst:                req.RateLimitBurst,
		RateLimitKeyStrategy:          req.RateLimitKeyStrategy,
		CacheEnabled:                  req.CacheEnabled,
		CacheTTLSeconds:               req.CacheTTLSeconds,
		CacheKeyPattern:               req.CacheKeyPattern,
		TimeoutSeconds:                req.TimeoutSeconds,
		RetryAttempts:                 req.RetryAttempts,
		Metadata:                      models.JSONB{},
	}

	if err := s.repos.Route.Create(ctx, route); err != nil {
		s.logger.Error().Err(err).Msg("Failed to create route")
		return nil, err
	}

	s.logger.Info().Str("route_id", route.ID.String()).Str("name", route.Name).Msg("Route created")
	return route, nil
}

func (s *routeService) GetRoute(ctx context.Context, id uuid.UUID) (*models.Route, error) {
	return s.repos.Route.GetByID(ctx, id)
}

func (s *routeService) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*models.Route, error) {
	return s.repos.Route.ListByTenant(ctx, tenantID)
}

func (s *routeService) UpdateRoute(ctx context.Context, id uuid.UUID, req *UpdateRouteRequest) (*models.Route, error) {
	route, err := s.repos.Route.GetByID(ctx, id)
	if err != nil {
		s.logger.Error().Err(err).Str("route_id", id.String()).Msg("Route not found")
		return nil, err
	}

	route.Name = req.Name
	route.PathPattern = req.PathPattern
	route.Methods = models.StringArray(req.Methods)
	route.Priority = req.Priority
	route.AuthMode = req.AuthMode
	route.IsActive = req.IsActive
	route.RateLimitEnabled = req.RateLimitEnabled
	route.RateLimitRequestsPerSecond = req.RateLimitRequestsPerSecond
	route.CacheEnabled = req.CacheEnabled
	route.CacheTTLSeconds = req.CacheTTLSeconds

	if err := s.repos.Route.Update(ctx, route); err != nil {
		s.logger.Error().Err(err).Str("route_id", id.String()).Msg("Failed to update route")
		return nil, err
	}

	s.logger.Info().Str("route_id", id.String()).Msg("Route updated")
	return route, nil
}

func (s *routeService) DeleteRoute(ctx context.Context, id uuid.UUID) error {
	if err := s.repos.Route.Delete(ctx, id); err != nil {
		s.logger.Error().Err(err).Str("route_id", id.String()).Msg("Failed to delete route")
		return err
	}

	s.logger.Info().Str("route_id", id.String()).Msg("Route deleted")
	return nil
}
