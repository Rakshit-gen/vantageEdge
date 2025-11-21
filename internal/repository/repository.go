package repository

import (
	"github.com/vantageedge/backend/pkg/database"
)

type Repository struct {
	Tenant  TenantRepository
	User    UserRepository
	Origin  OriginRepository
	Route   RouteRepository
	APIKey  APIKeyRepository
	Request RequestLogRepository
}

func New(db *database.DB) *Repository {
	return &Repository{
		Tenant:  NewTenantRepository(db),
		User:    NewUserRepository(db),
		Origin:  NewOriginRepository(db),
		Route:   NewRouteRepository(db),
		APIKey:  NewAPIKeyRepository(db),
		Request: NewRequestLogRepository(db),
	}
}

