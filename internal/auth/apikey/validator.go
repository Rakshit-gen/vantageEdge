package apikey

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/repository"
)

type Validator struct {
	repos *repository.Repository
}

func NewValidator(repos *repository.Repository) *Validator {
	return &Validator{repos: repos}
}

// ValidateKey validates an API key and returns the associated tenant and user IDs
func (v *Validator) ValidateKey(ctx context.Context, keyString string) (*KeyInfo, error) {
	if keyString == "" {
		return nil, fmt.Errorf("api key is empty")
	}

	// Remove "Bearer " or "ApiKey " prefix if present
	keyString = strings.TrimPrefix(keyString, "Bearer ")
	keyString = strings.TrimPrefix(keyString, "ApiKey ")

	// Hash the key to look up in database
	hash := sha256.Sum256([]byte(keyString))
	keyHash := hex.EncodeToString(hash[:])

	// Look up the key in database
	apiKey, err := v.repos.APIKey.GetByHash(ctx, keyHash)
	if err != nil {
		return nil, fmt.Errorf("invalid api key")
	}

	if !apiKey.IsActive {
		return nil, fmt.Errorf("api key is inactive")
	}

	// Check expiration
	if apiKey.ExpiresAt != nil && apiKey.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("api key is expired")
	}

	// Track usage. Best-effort: a logging failure here must not fail auth,
	// and this must not block the request on the write.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = v.repos.APIKey.UpdateUsage(ctx, apiKey.ID)
	}()

	return &KeyInfo{
		ID:       apiKey.ID,
		TenantID: apiKey.TenantID,
		UserID:   apiKey.UserID,
		Scopes:   apiKey.Scopes,
	}, nil
}

// KeyInfo contains the validated key information
type KeyInfo struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	UserID   *uuid.UUID
	Scopes   []string
}

// HasScope checks if the key has a specific scope
func (k *KeyInfo) HasScope(scope string) bool {
	for _, s := range k.Scopes {
		if s == scope || s == "*" {
			return true
		}
	}
	return false
}
