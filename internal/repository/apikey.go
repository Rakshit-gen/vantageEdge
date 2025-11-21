package repository

import (
	"context"
	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/models"
	"github.com/vantageedge/backend/pkg/database"
)

type APIKeyRepository interface {
	Create(ctx context.Context, key *models.APIKey) error
	GetByHash(ctx context.Context, hash string) (*models.APIKey, error)
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*models.APIKey, error)
	Update(ctx context.Context, key *models.APIKey) error
	Delete(ctx context.Context, id uuid.UUID) error
	UpdateUsage(ctx context.Context, id uuid.UUID) error
}

type apiKeyRepository struct {
	db *database.DB
}

func NewAPIKeyRepository(db *database.DB) APIKeyRepository {
	return &apiKeyRepository{db: db}
}

func (r *apiKeyRepository) Create(ctx context.Context, key *models.APIKey) error {
	query := `INSERT INTO api_keys (tenant_id, user_id, name, key_prefix, key_hash, scopes, expires_at)
	          VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, created_at, updated_at`
	return r.db.QueryRowContext(ctx, query,
		key.TenantID, key.UserID, key.Name, key.KeyPrefix, key.KeyHash, key.Scopes, key.ExpiresAt).
		Scan(&key.ID, &key.CreatedAt, &key.UpdatedAt)
}

func (r *apiKeyRepository) GetByHash(ctx context.Context, hash string) (*models.APIKey, error) {
	var key models.APIKey
	query := `SELECT * FROM api_keys WHERE key_hash = $1 AND is_active = true`
	err := r.db.GetContext(ctx, &key, query, hash)
	return &key, err
}

func (r *apiKeyRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*models.APIKey, error) {
	var keys []*models.APIKey
	query := `SELECT * FROM api_keys WHERE tenant_id = $1 ORDER BY created_at DESC`
	err := r.db.SelectContext(ctx, &keys, query, tenantID)
	return keys, err
}

func (r *apiKeyRepository) Update(ctx context.Context, key *models.APIKey) error {
	query := `UPDATE api_keys SET name = $1, is_active = $2 WHERE id = $3`
	_, err := r.db.ExecContext(ctx, query, key.Name, key.IsActive, key.ID)
	return err
}

func (r *apiKeyRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM api_keys WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

func (r *apiKeyRepository) UpdateUsage(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE api_keys SET usage_count = usage_count + 1, last_used_at = NOW() WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}
