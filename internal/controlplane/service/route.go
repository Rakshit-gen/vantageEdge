package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/eventbus"
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

	// Origin pool management: the gateway load-balances a route's traffic
	// across every origin in its pool (see migration 000007).
	ListRouteOrigins(ctx context.Context, routeID uuid.UUID) ([]*models.Origin, error)
	AddRouteOrigin(ctx context.Context, routeID, originID uuid.UUID) error
	RemoveRouteOrigin(ctx context.Context, routeID, originID uuid.UUID) error
}

type CreateRouteRequest struct {
	TenantID                   uuid.UUID `json:"tenant_id"`
	OriginID                   uuid.UUID `json:"origin_id"`
	Name                       string    `json:"name"`
	PathPattern                string    `json:"path_pattern"`
	Methods                    []string  `json:"methods"`
	Priority                   int       `json:"priority"`
	AuthMode                   string    `json:"auth_mode"`
	IsActive                   bool      `json:"is_active"`
	RateLimitEnabled           bool      `json:"rate_limit_enabled"`
	RateLimitRequestsPerSecond int       `json:"rate_limit_requests_per_second"`
	RateLimitBurst             int       `json:"rate_limit_burst"`
	RateLimitKeyStrategy       string    `json:"rate_limit_key_strategy"`
	CacheEnabled               bool      `json:"cache_enabled"`
	CacheTTLSeconds            int       `json:"cache_ttl_seconds"`
	CacheKeyPattern            string    `json:"cache_key_pattern"`
	TimeoutSeconds             int       `json:"timeout_seconds"`
	RetryAttempts              int       `json:"retry_attempts"`
}

// UpdateRouteRequest fields are pointers so PUT/PATCH only overwrites
// fields the caller actually sent (see UpdateOriginRequest for why).
type UpdateRouteRequest struct {
	Name                       *string  `json:"name"`
	PathPattern                *string  `json:"path_pattern"`
	Methods                    []string `json:"methods"`
	Priority                   *int     `json:"priority"`
	AuthMode                   *string  `json:"auth_mode"`
	IsActive                   *bool    `json:"is_active"`
	RateLimitEnabled           *bool    `json:"rate_limit_enabled"`
	RateLimitRequestsPerSecond *int     `json:"rate_limit_requests_per_second"`
	RateLimitBurst             *int     `json:"rate_limit_burst"`
	RateLimitKeyStrategy       *string  `json:"rate_limit_key_strategy"`
	CacheEnabled               *bool    `json:"cache_enabled"`
	CacheTTLSeconds            *int     `json:"cache_ttl_seconds"`
	CacheKeyPattern            *string  `json:"cache_key_pattern"`
	TimeoutSeconds             *int     `json:"timeout_seconds"`
	RetryAttempts              *int     `json:"retry_attempts"`
}

type routeService struct {
	repos  *repository.Repository
	hub    *eventbus.Hub
	logger *logger.Logger
}

func NewRouteService(repos *repository.Repository, hub *eventbus.Hub, log *logger.Logger) RouteService {
	return &routeService{repos: repos, hub: hub, logger: log}
}

func (s *routeService) CreateRoute(ctx context.Context, req *CreateRouteRequest) (*models.Route, error) {
	route := &models.Route{
		TenantID:                   req.TenantID,
		OriginID:                   req.OriginID,
		Name:                       req.Name,
		PathPattern:                req.PathPattern,
		Methods:                    models.StringArray(req.Methods),
		Priority:                   req.Priority,
		AuthMode:                   req.AuthMode,
		IsActive:                   req.IsActive,
		RateLimitEnabled:           req.RateLimitEnabled,
		RateLimitRequestsPerSecond: req.RateLimitRequestsPerSecond,
		RateLimitBurst:             req.RateLimitBurst,
		RateLimitKeyStrategy:       req.RateLimitKeyStrategy,
		CacheEnabled:               req.CacheEnabled,
		CacheTTLSeconds:            req.CacheTTLSeconds,
		CacheKeyPattern:            req.CacheKeyPattern,
		TimeoutSeconds:             req.TimeoutSeconds,
		RetryAttempts:              req.RetryAttempts,
		Metadata:                   models.JSONB{},
	}

	if err := s.repos.Route.Create(ctx, route); err != nil {
		s.logger.Error().Err(err).Msg("Failed to create route")
		return nil, err
	}

	s.logger.Info().Str("route_id", route.ID.String()).Str("name", route.Name).Msg("Route created")
	s.hub.Publish(route.TenantID.String())
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

	if req.Name != nil {
		route.Name = *req.Name
	}
	if req.PathPattern != nil {
		route.PathPattern = *req.PathPattern
	}
	if req.Methods != nil {
		route.Methods = models.StringArray(req.Methods)
	}
	if req.Priority != nil {
		route.Priority = *req.Priority
	}
	if req.AuthMode != nil {
		route.AuthMode = *req.AuthMode
	}
	if req.IsActive != nil {
		route.IsActive = *req.IsActive
	}
	if req.RateLimitEnabled != nil {
		route.RateLimitEnabled = *req.RateLimitEnabled
	}
	if req.RateLimitRequestsPerSecond != nil {
		route.RateLimitRequestsPerSecond = *req.RateLimitRequestsPerSecond
	}
	if req.RateLimitBurst != nil {
		route.RateLimitBurst = *req.RateLimitBurst
	}
	if req.RateLimitKeyStrategy != nil {
		route.RateLimitKeyStrategy = *req.RateLimitKeyStrategy
	}
	if req.CacheEnabled != nil {
		route.CacheEnabled = *req.CacheEnabled
	}
	if req.CacheTTLSeconds != nil {
		route.CacheTTLSeconds = *req.CacheTTLSeconds
	}
	if req.CacheKeyPattern != nil {
		route.CacheKeyPattern = *req.CacheKeyPattern
	}
	if req.TimeoutSeconds != nil {
		route.TimeoutSeconds = *req.TimeoutSeconds
	}
	if req.RetryAttempts != nil {
		route.RetryAttempts = *req.RetryAttempts
	}

	if err := s.repos.Route.Update(ctx, route); err != nil {
		s.logger.Error().Err(err).Str("route_id", id.String()).Msg("Failed to update route")
		return nil, err
	}

	s.logger.Info().Str("route_id", id.String()).Msg("Route updated")
	s.hub.Publish(route.TenantID.String())
	return route, nil
}

func (s *routeService) ListRouteOrigins(ctx context.Context, routeID uuid.UUID) ([]*models.Origin, error) {
	return s.repos.Route.ListOrigins(ctx, routeID)
}

func (s *routeService) AddRouteOrigin(ctx context.Context, routeID, originID uuid.UUID) error {
	route, err := s.repos.Route.GetByID(ctx, routeID)
	if err != nil {
		return err
	}
	if err := s.repos.Route.AddOrigin(ctx, routeID, originID); err != nil {
		return err
	}
	s.hub.Publish(route.TenantID.String())
	return nil
}

func (s *routeService) RemoveRouteOrigin(ctx context.Context, routeID, originID uuid.UUID) error {
	// A route with an empty pool can never be proxied, so refuse to remove
	// the last origin rather than silently leaving the route unservable.
	route, err := s.repos.Route.GetByID(ctx, routeID)
	if err != nil {
		return err
	}
	origins, err := s.repos.Route.ListOrigins(ctx, routeID)
	if err != nil {
		return err
	}
	if len(origins) <= 1 {
		return fmt.Errorf("cannot remove the last origin from a route's pool")
	}
	if err := s.repos.Route.RemoveOrigin(ctx, routeID, originID); err != nil {
		return err
	}
	s.hub.Publish(route.TenantID.String())
	return nil
}

func (s *routeService) DeleteRoute(ctx context.Context, id uuid.UUID) error {
	route, err := s.repos.Route.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if err := s.repos.Route.Delete(ctx, id); err != nil {
		s.logger.Error().Err(err).Str("route_id", id.String()).Msg("Failed to delete route")
		return err
	}

	s.logger.Info().Str("route_id", id.String()).Msg("Route deleted")
	s.hub.Publish(route.TenantID.String())
	return nil
}
