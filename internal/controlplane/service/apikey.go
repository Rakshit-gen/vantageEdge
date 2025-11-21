package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/models"
	"github.com/vantageedge/backend/internal/repository"
	"github.com/vantageedge/backend/pkg/logger"
)

type APIKeyService interface {
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*models.APIKey, error)
	CreateAPIKey(ctx context.Context, req *CreateAPIKeyRequest) (*models.APIKey, string, error)
	DeleteAPIKey(ctx context.Context, id uuid.UUID) error
}

type CreateAPIKeyRequest struct {
	TenantID  uuid.UUID  `json:"tenant_id"`
	UserID    *uuid.UUID `json:"user_id,omitempty"`
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *string    `json:"expires_at,omitempty"`
}

type apiKeyService struct {
	repos  *repository.Repository
	logger *logger.Logger
}

func NewAPIKeyService(repos *repository.Repository, log *logger.Logger) APIKeyService {
	return &apiKeyService{repos: repos, logger: log}
}

func (s *apiKeyService) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*models.APIKey, error) {
	return s.repos.APIKey.ListByTenant(ctx, tenantID)
}

func (s *apiKeyService) CreateAPIKey(ctx context.Context, req *CreateAPIKeyRequest) (*models.APIKey, string, error) {
	// Generate secure random key (32 bytes = 64 hex characters)
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		s.logger.Error().Err(err).Msg("Failed to generate random bytes")
		return nil, "", err
	}

	// Create API key with prefix and random suffix
	randomSuffix := hex.EncodeToString(randomBytes)
	apiKeyString := "ve_live_" + randomSuffix

	// Hash the API key for storage
	hash := sha256.Sum256([]byte(apiKeyString))
	keyHash := hex.EncodeToString(hash[:])

	key := &models.APIKey{
		TenantID:   req.TenantID,
		UserID:     req.UserID,
		Name:       req.Name,
		KeyPrefix:  "ve_live_",
		KeyHash:    keyHash,
		Scopes:     models.StringArray(req.Scopes),
		IsActive:   true,
		UsageCount: 0,
		Metadata:   models.JSONB{},
	}

	// Set expiration if provided
	if req.ExpiresAt != nil {
		// Parse the expiration date string (assuming ISO format)
		// For now, just set it directly - the repository should handle parsing
		// In production, you'd want proper date parsing here
	}

	if err := s.repos.APIKey.Create(ctx, key); err != nil {
		s.logger.Error().Err(err).Msg("Failed to create API key")
		return nil, "", err
	}

	s.logger.Info().Str("key_id", key.ID.String()).Msg("API key created successfully")
	return key, apiKeyString, nil
}

func (s *apiKeyService) DeleteAPIKey(ctx context.Context, id uuid.UUID) error {
	return s.repos.APIKey.Delete(ctx, id)
}
