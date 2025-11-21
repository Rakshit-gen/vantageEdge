package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/models"
	"github.com/vantageedge/backend/pkg/database"
)

type TenantRepository interface {
	Create(ctx context.Context, tenant *models.Tenant) error
	GetByID(ctx context.Context, id uuid.UUID) (*models.Tenant, error)
	GetBySubdomain(ctx context.Context, subdomain string) (*models.Tenant, error)
	GetByClerkOrgID(ctx context.Context, clerkOrgID string) (*models.Tenant, error)
	List(ctx context.Context) ([]*models.Tenant, error)
	Update(ctx context.Context, tenant *models.Tenant) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type tenantRepository struct {
	db *database.DB
}

func NewTenantRepository(db *database.DB) TenantRepository {
	return &tenantRepository{db: db}
}

func (r *tenantRepository) Create(ctx context.Context, tenant *models.Tenant) error {
	query := `INSERT INTO tenants (name, subdomain, clerk_org_id, status, settings) 
	          VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at, updated_at`
	return r.db.QueryRowContext(ctx, query,
		tenant.Name, tenant.Subdomain, tenant.ClerkOrgID,
		tenant.Status, tenant.Settings).
		Scan(&tenant.ID, &tenant.CreatedAt, &tenant.UpdatedAt)
}

func (r *tenantRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Tenant, error) {
	var tenant models.Tenant
	query := `SELECT * FROM tenants WHERE id = $1`
	err := r.db.GetContext(ctx, &tenant, query, id)
	return &tenant, err
}

func (r *tenantRepository) GetBySubdomain(ctx context.Context, subdomain string) (*models.Tenant, error) {
	var tenant models.Tenant
	query := `SELECT * FROM tenants WHERE subdomain = $1`
	err := r.db.GetContext(ctx, &tenant, query, subdomain)
	return &tenant, err
}

func (r *tenantRepository) GetByClerkOrgID(ctx context.Context, clerkOrgID string) (*models.Tenant, error) {
	var tenant models.Tenant
	query := `SELECT * FROM tenants WHERE clerk_org_id = $1`
	err := r.db.GetContext(ctx, &tenant, query, clerkOrgID)
	return &tenant, err
}

func (r *tenantRepository) List(ctx context.Context) ([]*models.Tenant, error) {
	var tenants []*models.Tenant
	query := `SELECT * FROM tenants ORDER BY created_at DESC`
	err := r.db.SelectContext(ctx, &tenants, query)
	return tenants, err
}

func (r *tenantRepository) Update(ctx context.Context, tenant *models.Tenant) error {
	query := `UPDATE tenants SET name = $1, status = $2, settings = $3 WHERE id = $4`
	_, err := r.db.ExecContext(ctx, query, tenant.Name, tenant.Status, tenant.Settings, tenant.ID)
	return err
}

func (r *tenantRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM tenants WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}
