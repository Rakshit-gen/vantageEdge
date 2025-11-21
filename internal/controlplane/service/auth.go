package service

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/models"
	"github.com/vantageedge/backend/internal/repository"
	"github.com/vantageedge/backend/pkg/logger"
)

type AuthService interface {
	SyncUser(ctx context.Context, req *SyncUserRequest) (*models.User, error)
	SyncTenant(ctx context.Context, req *SyncTenantRequest) (*models.Tenant, error)
	GetCurrentUser(ctx context.Context, clerkUserID string) (*models.User, error)
	GetUserTenant(ctx context.Context, clerkUserID string) (*models.Tenant, error)
}

type SyncUserRequest struct {
	ClerkUserID string  `json:"clerk_user_id"`
	Email       string  `json:"email"`
	FirstName   *string `json:"first_name,omitempty"`
	LastName    *string `json:"last_name,omitempty"`
	ClerkOrgID  *string `json:"clerk_org_id,omitempty"`
}

type SyncTenantRequest struct {
	ClerkUserID string `json:"clerk_user_id"`
	TenantName  string `json:"tenant_name"`
	ClerkOrgID  string `json:"clerk_org_id"`
}

type authService struct {
	repos  *repository.Repository
	logger *logger.Logger
}

func NewAuthService(repos *repository.Repository, log *logger.Logger) AuthService {
	return &authService{repos: repos, logger: log}
}

func (s *authService) SyncUser(ctx context.Context, req *SyncUserRequest) (*models.User, error) {
	// Try to get existing user
	existingUser, err := s.repos.User.GetByClerkID(ctx, req.ClerkUserID)
	if err == nil {
		// User exists, update if needed
		existingUser.Email = req.Email
		if req.FirstName != nil {
			existingUser.FirstName = req.FirstName
		}
		if req.LastName != nil {
			existingUser.LastName = req.LastName
		}
		if err := s.repos.User.Update(ctx, existingUser); err != nil {
			s.logger.Error().Err(err).Str("clerk_user_id", req.ClerkUserID).Msg("Failed to update user")
			return nil, err
		}
		return existingUser, nil
	}

	// User doesn't exist, create a new tenant and user
	tenantName := req.Email
	if req.FirstName != nil && *req.FirstName != "" {
		tenantName = *req.FirstName
		if req.LastName != nil && *req.LastName != "" {
			tenantName += " " + *req.LastName + " Workspace"
		} else {
			tenantName += " Workspace"
		}
	}

	// Create tenant with a unique subdomain based on email
	tenantID := uuid.New()
	// Generate simple subdomain from email username part
	emailParts := strings.Split(req.Email, "@")
	emailUsername := emailParts[0]
	// Keep only first 20 chars of email username to avoid subdomain being too long
	if len(emailUsername) > 20 {
		emailUsername = emailUsername[:20]
	}
	// Remove any invalid characters and replace with hyphen
	subdomain := strings.ToLower(strings.ReplaceAll(emailUsername, ".", "-"))
	// Add UUID suffix for uniqueness (first 8 chars)
	subdomain = subdomain + "-" + tenantID.String()[:8]

	tenant := &models.Tenant{
		ID:        tenantID,
		Name:      tenantName,
		Subdomain: subdomain,
		Status:    "active",
	}
	if err := s.repos.Tenant.Create(ctx, tenant); err != nil {
		s.logger.Error().Err(err).Msg("Failed to create tenant during user sync")
		return nil, err
	}

	// Create user
	firstName := (*string)(nil)
	lastName := (*string)(nil)
	if req.FirstName != nil {
		firstName = req.FirstName
	}
	if req.LastName != nil {
		lastName = req.LastName
	}

	user := &models.User{
		ID:          uuid.New(),
		TenantID:    tenant.ID,
		ClerkUserID: req.ClerkUserID,
		Email:       req.Email,
		FirstName:   firstName,
		LastName:    lastName,
		Role:        "admin", // First user is admin
		Status:      "active",
	}

	if err := s.repos.User.Create(ctx, user); err != nil {
		s.logger.Error().Err(err).Str("clerk_user_id", req.ClerkUserID).Msg("Failed to create user during sync")
		return nil, err
	}

	s.logger.Info().Str("user_id", user.ID.String()).Str("tenant_id", tenant.ID.String()).Msg("User synced successfully")
	return user, nil
}

func (s *authService) SyncTenant(ctx context.Context, req *SyncTenantRequest) (*models.Tenant, error) {
	// Get user to find their tenant
	user, err := s.repos.User.GetByClerkID(ctx, req.ClerkUserID)
	if err != nil {
		s.logger.Error().Err(err).Str("clerk_user_id", req.ClerkUserID).Msg("User not found for tenant sync")
		return nil, err
	}

	// Get or create tenant
	tenant, err := s.repos.Tenant.GetByID(ctx, user.TenantID)
	if err != nil {
		s.logger.Error().Err(err).Str("tenant_id", user.TenantID.String()).Msg("Tenant not found")
		return nil, err
	}

	// Update tenant name if provided
	if req.TenantName != "" && req.TenantName != tenant.Name {
		tenant.Name = req.TenantName
		if err := s.repos.Tenant.Update(ctx, tenant); err != nil {
			s.logger.Error().Err(err).Str("tenant_id", tenant.ID.String()).Msg("Failed to update tenant")
			return nil, err
		}
	}

	return tenant, nil
}

func (s *authService) GetCurrentUser(ctx context.Context, clerkUserID string) (*models.User, error) {
	user, err := s.repos.User.GetByClerkID(ctx, clerkUserID)
	if err != nil {
		s.logger.Error().Err(err).Str("clerk_user_id", clerkUserID).Msg("Failed to get user by Clerk ID")
		return nil, err
	}
	return user, nil
}

func (s *authService) GetUserTenant(ctx context.Context, clerkUserID string) (*models.Tenant, error) {
	user, err := s.repos.User.GetByClerkID(ctx, clerkUserID)
	if err != nil {
		s.logger.Error().Err(err).Str("clerk_user_id", clerkUserID).Msg("User not found")
		return nil, err
	}

	tenant, err := s.repos.Tenant.GetByID(ctx, user.TenantID)
	if err != nil {
		s.logger.Error().Err(err).Str("tenant_id", user.TenantID.String()).Msg("Tenant not found")
		return nil, err
	}

	return tenant, nil
}
