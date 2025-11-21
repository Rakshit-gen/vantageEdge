package repository

import (
	"context"
	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/models"
	"github.com/vantageedge/backend/pkg/database"
)

type UserRepository interface {
	Create(ctx context.Context, user *models.User) error
	GetByID(ctx context.Context, id uuid.UUID) (*models.User, error)
	GetByClerkID(ctx context.Context, clerkUserID string) (*models.User, error)
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*models.User, error)
	Update(ctx context.Context, user *models.User) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type userRepository struct {
	db *database.DB
}

func NewUserRepository(db *database.DB) UserRepository {
	return &userRepository{db: db}
}

func (r *userRepository) Create(ctx context.Context, user *models.User) error {
	query := `INSERT INTO users (tenant_id, clerk_user_id, email, first_name, last_name, role, status)
	          VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, created_at, updated_at`
	return r.db.QueryRowContext(ctx, query,
		user.TenantID, user.ClerkUserID, user.Email, user.FirstName, user.LastName, user.Role, user.Status).
		Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)
}

func (r *userRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	var user models.User
	query := `SELECT * FROM users WHERE id = $1`
	err := r.db.GetContext(ctx, &user, query, id)
	return &user, err
}

func (r *userRepository) GetByClerkID(ctx context.Context, clerkUserID string) (*models.User, error) {
	var user models.User
	query := `SELECT * FROM users WHERE clerk_user_id = $1`
	err := r.db.GetContext(ctx, &user, query, clerkUserID)
	return &user, err
}

func (r *userRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*models.User, error) {
	var users []*models.User
	query := `SELECT * FROM users WHERE tenant_id = $1 ORDER BY created_at DESC`
	err := r.db.SelectContext(ctx, &users, query, tenantID)
	return users, err
}

func (r *userRepository) Update(ctx context.Context, user *models.User) error {
	query := `UPDATE users SET email = $1, first_name = $2, last_name = $3, role = $4 WHERE id = $5`
	_, err := r.db.ExecContext(ctx, query, user.Email, user.FirstName, user.LastName, user.Role, user.ID)
	return err
}

func (r *userRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM users WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}
