package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/auth/clerk"
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
	repos       *repository.Repository
	clerkClient *clerk.ClerkClient
	logger      *logger.Logger
}

func NewAuthService(repos *repository.Repository, clerkClient *clerk.ClerkClient, log *logger.Logger) AuthService {
	return &authService{repos: repos, clerkClient: clerkClient, logger: log}
}

func strPtrEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (s *authService) SyncUser(ctx context.Context, req *SyncUserRequest) (*models.User, error) {
	if req.ClerkUserID == "" {
		return nil, fmt.Errorf("clerk_user_id is required")
	}

	// Try to get existing user. SyncUser runs on every authenticated
	// request (RequireAuth middleware), so the common path here must not
	// write: only persist when a field actually changed.
	existingUser, err := s.repos.User.GetByClerkID(ctx, req.ClerkUserID)
	if err == nil {
		changed := false
		if req.Email != "" && req.Email != existingUser.Email {
			existingUser.Email = req.Email
			changed = true
		}
		if req.FirstName != nil && !strPtrEqual(req.FirstName, existingUser.FirstName) {
			existingUser.FirstName = req.FirstName
			changed = true
		}
		if req.LastName != nil && !strPtrEqual(req.LastName, existingUser.LastName) {
			existingUser.LastName = req.LastName
			changed = true
		}
		if changed {
			if err := s.repos.User.Update(ctx, existingUser); err != nil {
				s.logger.Error().Err(err).Str("clerk_user_id", req.ClerkUserID).Msg("Failed to update user")
				return nil, err
			}
		}
		return existingUser, nil
	}

	// User doesn't exist yet. The JWT session token may not carry an email
	// claim (Clerk only includes it if the session token's JWT template
	// adds it), so fall back to the Backend API rather than persist a
	// blank/garbage email into a NOT NULL column.
	if req.Email == "" && s.clerkClient != nil {
		if info, lookupErr := s.clerkClient.GetUser(ctx, req.ClerkUserID); lookupErr == nil && info.Email != "" {
			req.Email = info.Email
			if req.FirstName == nil {
				req.FirstName = info.FirstName
			}
			if req.LastName == nil {
				req.LastName = info.LastName
			}
		}
	}
	if req.Email == "" {
		return nil, fmt.Errorf("unable to determine email for clerk user %s", req.ClerkUserID)
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
		Role:        "owner", // First user in a newly-provisioned tenant owns it
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
