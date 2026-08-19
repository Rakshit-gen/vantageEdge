package service

import (
	"github.com/vantageedge/backend/internal/auth/clerk"
	"github.com/vantageedge/backend/internal/eventbus"
	"github.com/vantageedge/backend/internal/repository"
	"github.com/vantageedge/backend/pkg/logger"
)

type Service struct {
	Tenant TenantService
	User   UserService
	Origin OriginService
	Route  RouteService
	APIKey APIKeyService
	Auth   AuthService
	Repos  *repository.Repository
	logger *logger.Logger
}

// New wires up every control-plane service. hub receives an event whenever
// a route or origin mutation succeeds, so the gRPC ConfigService can push
// invalidations to connected gateways (see internal/eventbus and
// internal/controlplane/grpcserver).
func New(repos *repository.Repository, clerkClient *clerk.ClerkClient, hub *eventbus.Hub, log *logger.Logger) *Service {
	return &Service{
		Tenant: NewTenantService(repos, log),
		User:   NewUserService(repos, log),
		Origin: NewOriginService(repos, hub, log),
		Route:  NewRouteService(repos, hub, log),
		APIKey: NewAPIKeyService(repos, log),
		Auth:   NewAuthService(repos, clerkClient, log),
		Repos:  repos,
		logger: log,
	}
}
