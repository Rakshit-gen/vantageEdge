package repository

import (
	"context"
	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/models"
	"github.com/vantageedge/backend/pkg/database"
)

type OriginRepository interface {
	Create(ctx context.Context, origin *models.Origin) error
	GetByID(ctx context.Context, id uuid.UUID) (*models.Origin, error)
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*models.Origin, error)
	Update(ctx context.Context, origin *models.Origin) error
	Delete(ctx context.Context, id uuid.UUID) error
	UpdateHealth(ctx context.Context, id uuid.UUID, isHealthy bool) error
}

type originRepository struct {
	db *database.DB
}

func NewOriginRepository(db *database.DB) OriginRepository {
	return &originRepository{db: db}
}

func (r *originRepository) Create(ctx context.Context, origin *models.Origin) error {
	query := `INSERT INTO origins (tenant_id, name, url, health_check_path, timeout_seconds)
	          VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at, updated_at`
	return r.db.QueryRowContext(ctx, query,
		origin.TenantID, origin.Name, origin.URL, origin.HealthCheckPath, origin.TimeoutSeconds).
		Scan(&origin.ID, &origin.CreatedAt, &origin.UpdatedAt)
}

func (r *originRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Origin, error) {
	var origin models.Origin
	query := `SELECT * FROM origins WHERE id = $1`
	err := r.db.GetContext(ctx, &origin, query, id)
	return &origin, err
}

func (r *originRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*models.Origin, error) {
	var origins []*models.Origin
	query := `SELECT * FROM origins WHERE tenant_id = $1 ORDER BY created_at DESC`
	err := r.db.SelectContext(ctx, &origins, query, tenantID)
	return origins, err
}

func (r *originRepository) Update(ctx context.Context, origin *models.Origin) error {
	query := `UPDATE origins SET name = $1, url = $2, timeout_seconds = $3 WHERE id = $4`
	_, err := r.db.ExecContext(ctx, query, origin.Name, origin.URL, origin.TimeoutSeconds, origin.ID)
	return err
}

func (r *originRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM origins WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

func (r *originRepository) UpdateHealth(ctx context.Context, id uuid.UUID, isHealthy bool) error {
	query := `UPDATE origins SET is_healthy = $1, last_health_check = NOW() WHERE id = $2`
	_, err := r.db.ExecContext(ctx, query, isHealthy, id)
	return err
}
