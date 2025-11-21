package service

import (
	"github.com/vantageedge/backend/internal/repository"
	"github.com/vantageedge/backend/pkg/logger"
)

type Service struct {
	Tenant TenantService
	User   UserService
	Origin OriginService
	Route  RouteService
	APIKey APIKeyService
	Repos  *repository.Repository
	logger *logger.Logger
}

func New(repos *repository.Repository, log *logger.Logger) *Service {
	return &Service{
		Tenant: NewTenantService(repos, log),
		User:   NewUserService(repos, log),
		Origin: NewOriginService(repos, log),
		Route:  NewRouteService(repos, log),
		APIKey: NewAPIKeyService(repos, log),
		Repos:  repos,
		logger: log,
	}
}
