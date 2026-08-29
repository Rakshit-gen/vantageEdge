// Package grpcserver implements the gRPC ConfigService the gateway uses to
// fetch tenant/route/origin configuration instead of reading Postgres
// directly. See api/proto/config.proto for the RPC contract and rationale.
package grpcserver

import (
	"context"

	"github.com/vantageedge/backend/api/proto/configpb"
	"github.com/vantageedge/backend/internal/eventbus"
	"github.com/vantageedge/backend/internal/models"
	"github.com/vantageedge/backend/internal/repository"
	"github.com/vantageedge/backend/pkg/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ConfigServer struct {
	configpb.UnimplementedConfigServiceServer
	repos  *repository.Repository
	hub    *eventbus.Hub
	logger *logger.Logger
}

func NewConfigServer(repos *repository.Repository, hub *eventbus.Hub, log *logger.Logger) *ConfigServer {
	return &ConfigServer{repos: repos, hub: hub, logger: log}
}

func (s *ConfigServer) GetTenantConfig(ctx context.Context, req *configpb.GetTenantConfigRequest) (*configpb.TenantConfig, error) {
	if req.Subdomain == "" {
		return nil, status.Error(codes.InvalidArgument, "subdomain is required")
	}

	tenant, err := s.repos.Tenant.GetBySubdomain(ctx, req.Subdomain)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "tenant not found for subdomain %q", req.Subdomain)
	}

	routes, err := s.repos.Route.ListByTenant(ctx, tenant.ID)
	if err != nil {
		s.logger.Error().Err(err).Str("tenant_id", tenant.ID.String()).Msg("Failed to list routes for gRPC config request")
		return nil, status.Error(codes.Internal, "failed to load routes")
	}

	pbRoutes := make([]*configpb.RouteConfig, 0, len(routes))
	for _, route := range routes {
		origins, err := s.repos.Route.ListOrigins(ctx, route.ID)
		if err != nil {
			s.logger.Error().Err(err).Str("route_id", route.ID.String()).Msg("Failed to list origins for gRPC config request")
			return nil, status.Error(codes.Internal, "failed to load route origins")
		}
		pbRoutes = append(pbRoutes, toRouteConfig(route, origins))
	}

	return &configpb.TenantConfig{
		TenantId:  tenant.ID.String(),
		Subdomain: tenant.Subdomain,
		Status:    tenant.Status,
		Routes:    pbRoutes,
	}, nil
}

func (s *ConfigServer) StreamConfigUpdates(_ *configpb.StreamConfigUpdatesRequest, stream configpb.ConfigService_StreamConfigUpdatesServer) error {
	ch, unsubscribe := s.hub.Subscribe()
	defer unsubscribe()

	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case tenantID, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(&configpb.ConfigUpdateEvent{TenantId: tenantID}); err != nil {
				return err
			}
		}
	}
}

func toRouteConfig(route *models.Route, origins []*models.Origin) *configpb.RouteConfig {
	pbOrigins := make([]*configpb.OriginConfig, 0, len(origins))
	for _, o := range origins {
		pbOrigins = append(pbOrigins, &configpb.OriginConfig{
			Id:              o.ID.String(),
			Url:             o.URL,
			Weight:          int32(o.Weight),
			TimeoutSeconds:  int32(o.TimeoutSeconds),
			HealthCheckPath: o.HealthCheckPath,
		})
	}

	return &configpb.RouteConfig{
		Id:                         route.ID.String(),
		Name:                       route.Name,
		PathPattern:                route.PathPattern,
		Methods:                    []string(route.Methods),
		Priority:                   int32(route.Priority),
		AuthMode:                   route.AuthMode,
		RateLimitEnabled:           route.RateLimitEnabled,
		RateLimitRequestsPerSecond: int32(route.RateLimitRequestsPerSecond),
		RateLimitBurst:             int32(route.RateLimitBurst),
		RateLimitKeyStrategy:       route.RateLimitKeyStrategy,
		CacheEnabled:               route.CacheEnabled,
		CacheTtlSeconds:            int32(route.CacheTTLSeconds),
		CacheKeyPattern:            route.CacheKeyPattern,
		TimeoutSeconds:             int32(route.TimeoutSeconds),
		RetryAttempts:              int32(route.RetryAttempts),
		LoadBalancing:              route.LoadBalancing,
		Origins:                    pbOrigins,
	}
}
