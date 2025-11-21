package service

import (
	"context"
	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/models"
	"github.com/vantageedge/backend/internal/repository"
	"github.com/vantageedge/backend/pkg/logger"
)

type TenantService interface {
	CreateTenant(ctx context.Context, req *CreateTenantRequest) (*models.Tenant, error)
	GetTenant(ctx context.Context, id uuid.UUID) (*models.Tenant, error)
	ListTenants(ctx context.Context) ([]*models.Tenant, error)
	UpdateTenant(ctx context.Context, id uuid.UUID, req *UpdateTenantRequest) (*models.Tenant, error)
	DeleteTenant(ctx context.Context, id uuid.UUID) error
}

type tenantService struct {
	repos  *repository.Repository
	logger *logger.Logger
}

func NewTenantService(repos *repository.Repository, log *logger.Logger) TenantService {
	return &tenantService{repos: repos, logger: log}
}

type CreateTenantRequest struct {
	Name       string                 `json:"name"`
	Subdomain  string                 `json:"subdomain"`
	ClerkOrgID *string                `json:"clerk_org_id,omitempty"`
	Settings   map[string]interface{} `json:"settings,omitempty"`
}

type UpdateTenantRequest struct {
	Name     *string                `json:"name,omitempty"`
	Status   *string                `json:"status,omitempty"`
	Settings map[string]interface{} `json:"settings,omitempty"`
}

func (s *tenantService) CreateTenant(ctx context.Context, req *CreateTenantRequest) (*models.Tenant, error) {
	tenant := &models.Tenant{
		Name:       req.Name,
		Subdomain:  req.Subdomain,
		ClerkOrgID: req.ClerkOrgID,
		Status:     "active",
		Settings:   req.Settings,
	}

	if err := s.repos.Tenant.Create(ctx, tenant); err != nil {
		s.logger.Error().Err(err).Msg("Failed to create tenant")
		return nil, err
	}

	s.logger.Info().Str("tenant_id", tenant.ID.String()).Msg("Tenant created")
	return tenant, nil
}

func (s *tenantService) GetTenant(ctx context.Context, id uuid.UUID) (*models.Tenant, error) {
	return s.repos.Tenant.GetByID(ctx, id)
}

func (s *tenantService) ListTenants(ctx context.Context) ([]*models.Tenant, error) {
	return s.repos.Tenant.List(ctx)
}

func (s *tenantService) UpdateTenant(ctx context.Context, id uuid.UUID, req *UpdateTenantRequest) (*models.Tenant, error) {
	tenant, err := s.repos.Tenant.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		tenant.Name = *req.Name
	}
	if req.Status != nil {
		tenant.Status = *req.Status
	}
	if req.Settings != nil {
		tenant.Settings = req.Settings
	}

	if err := s.repos.Tenant.Update(ctx, tenant); err != nil {
		return nil, err
	}

	return tenant, nil
}

func (s *tenantService) DeleteTenant(ctx context.Context, id uuid.UUID) error {
	return s.repos.Tenant.Delete(ctx, id)
}
