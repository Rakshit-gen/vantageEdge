package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/models"
	"github.com/vantageedge/backend/internal/repository"
	"github.com/vantageedge/backend/pkg/logger"
)

type OriginService interface {
	CreateOrigin(ctx context.Context, req *CreateOriginRequest) (*models.Origin, error)
	GetOrigin(ctx context.Context, id uuid.UUID) (*models.Origin, error)
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*models.Origin, error)
	UpdateOrigin(ctx context.Context, id uuid.UUID, req *UpdateOriginRequest) (*models.Origin, error)
	DeleteOrigin(ctx context.Context, id uuid.UUID) error
}

type CreateOriginRequest struct {
	TenantID            uuid.UUID `json:"tenant_id"`
	Name                string    `json:"name"`
	URL                 string    `json:"url"`
	HealthCheckPath     string    `json:"health_check_path"`
	HealthCheckInterval int       `json:"health_check_interval"`
	TimeoutSeconds      int       `json:"timeout_seconds"`
	MaxRetries          int       `json:"max_retries"`
	Weight              int       `json:"weight"`
}

type UpdateOriginRequest struct {
	Name           string `json:"name"`
	URL            string `json:"url"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	Weight         int    `json:"weight"`
}

type originService struct {
	repos  *repository.Repository
	logger *logger.Logger
}

func NewOriginService(repos *repository.Repository, log *logger.Logger) OriginService {
	return &originService{repos: repos, logger: log}
}

func (s *originService) CreateOrigin(ctx context.Context, req *CreateOriginRequest) (*models.Origin, error) {
	origin := &models.Origin{
		TenantID:            req.TenantID,
		Name:                req.Name,
		URL:                 req.URL,
		HealthCheckPath:     req.HealthCheckPath,
		HealthCheckInterval: req.HealthCheckInterval,
		TimeoutSeconds:      req.TimeoutSeconds,
		MaxRetries:          req.MaxRetries,
		Weight:              req.Weight,
		IsHealthy:           true,
	}

	if err := s.repos.Origin.Create(ctx, origin); err != nil {
		s.logger.Error().Err(err).Msg("Failed to create origin")
		return nil, err
	}

	s.logger.Info().Str("origin_id", origin.ID.String()).Str("name", origin.Name).Msg("Origin created")
	return origin, nil
}

func (s *originService) GetOrigin(ctx context.Context, id uuid.UUID) (*models.Origin, error) {
	return s.repos.Origin.GetByID(ctx, id)
}

func (s *originService) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*models.Origin, error) {
	return s.repos.Origin.ListByTenant(ctx, tenantID)
}

func (s *originService) UpdateOrigin(ctx context.Context, id uuid.UUID, req *UpdateOriginRequest) (*models.Origin, error) {
	origin, err := s.repos.Origin.GetByID(ctx, id)
	if err != nil {
		s.logger.Error().Err(err).Str("origin_id", id.String()).Msg("Origin not found")
		return nil, err
	}

	origin.Name = req.Name
	origin.URL = req.URL
	origin.TimeoutSeconds = req.TimeoutSeconds
	origin.Weight = req.Weight

	if err := s.repos.Origin.Update(ctx, origin); err != nil {
		s.logger.Error().Err(err).Str("origin_id", id.String()).Msg("Failed to update origin")
		return nil, err
	}

	s.logger.Info().Str("origin_id", id.String()).Msg("Origin updated")
	return origin, nil
}

func (s *originService) DeleteOrigin(ctx context.Context, id uuid.UUID) error {
	if err := s.repos.Origin.Delete(ctx, id); err != nil {
		s.logger.Error().Err(err).Str("origin_id", id.String()).Msg("Failed to delete origin")
		return err
	}

	s.logger.Info().Str("origin_id", id.String()).Msg("Origin deleted")
	return nil
}
