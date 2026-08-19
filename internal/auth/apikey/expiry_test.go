package apikey

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/models"
	"github.com/vantageedge/backend/internal/repository"
)

// fakeAPIKeyRepo lets ValidateKey be tested without a real database.
type fakeAPIKeyRepo struct {
	repository.APIKeyRepository // embed to satisfy the interface; only GetByHash is used below
	key                         *models.APIKey
}

func (f fakeAPIKeyRepo) GetByHash(ctx context.Context, hash string) (*models.APIKey, error) {
	if f.key == nil {
		return nil, fmt.Errorf("not found")
	}
	return f.key, nil
}

// UpdateUsage is overridden as a no-op: ValidateKey fires it in a
// background goroutine, and the embedded nil APIKeyRepository would panic
// if it were actually dispatched to.
func (f fakeAPIKeyRepo) UpdateUsage(ctx context.Context, id uuid.UUID) error {
	return nil
}

// TestValidateKey_RejectsExpiredKey is the regression test for the
// previous bug where the expiry check compared against a placeholder
// now() function that returned nil interface{} instead of time.Now(),
// which didn't even compile (`ExpiresAt.Before(now())` — Before wants a
// time.Time). Beyond the compile failure, the intent — reject expired
// keys — was never actually exercised.
func TestValidateKey_RejectsExpiredKey(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	repos := &repository.Repository{
		APIKey: fakeAPIKeyRepo{key: &models.APIKey{
			ID:        uuid.New(),
			TenantID:  uuid.New(),
			IsActive:  true,
			ExpiresAt: &past,
		}},
	}
	v := NewValidator(repos)

	_, err := v.ValidateKey(context.Background(), "ve_live_anything")
	if err == nil {
		t.Fatal("expected expired key to be rejected")
	}
}

func TestValidateKey_AcceptsFutureExpiry(t *testing.T) {
	future := time.Now().Add(time.Hour)
	repos := &repository.Repository{
		APIKey: fakeAPIKeyRepo{key: &models.APIKey{
			ID:        uuid.New(),
			TenantID:  uuid.New(),
			IsActive:  true,
			ExpiresAt: &future,
		}},
	}
	v := NewValidator(repos)

	info, err := v.ValidateKey(context.Background(), "ve_live_anything")
	if err != nil {
		t.Fatalf("expected key with future expiry to be accepted, got: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil KeyInfo")
	}
}

func TestValidateKey_AcceptsNoExpiry(t *testing.T) {
	repos := &repository.Repository{
		APIKey: fakeAPIKeyRepo{key: &models.APIKey{
			ID:        uuid.New(),
			TenantID:  uuid.New(),
			IsActive:  true,
			ExpiresAt: nil,
		}},
	}
	v := NewValidator(repos)

	if _, err := v.ValidateKey(context.Background(), "ve_live_anything"); err != nil {
		t.Fatalf("expected key with no expiry to be accepted, got: %v", err)
	}
}

func TestValidateKey_RejectsInactive(t *testing.T) {
	repos := &repository.Repository{
		APIKey: fakeAPIKeyRepo{key: &models.APIKey{
			ID:       uuid.New(),
			TenantID: uuid.New(),
			IsActive: false,
		}},
	}
	v := NewValidator(repos)

	if _, err := v.ValidateKey(context.Background(), "ve_live_anything"); err == nil {
		t.Fatal("expected inactive key to be rejected")
	}
}

func TestValidateKey_RejectsEmptyKey(t *testing.T) {
	v := NewValidator(&repository.Repository{})
	if _, err := v.ValidateKey(context.Background(), ""); err == nil {
		t.Fatal("expected empty key to be rejected")
	}
}
