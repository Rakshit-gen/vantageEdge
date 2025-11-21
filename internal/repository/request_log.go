package repository

import (
	"context"
	"github.com/vantageedge/backend/internal/models"
	"github.com/vantageedge/backend/pkg/database"
)

type RequestLogRepository interface {
	Create(ctx context.Context, log *models.RequestLog) error
}

type requestLogRepository struct {
	db *database.DB
}

func NewRequestLogRepository(db *database.DB) RequestLogRepository {
	return &requestLogRepository{db: db}
}

func (r *requestLogRepository) Create(ctx context.Context, log *models.RequestLog) error {
	query := `INSERT INTO request_logs (tenant_id, route_id, user_id, method, path, status_code,
	          response_time_ms, cache_hit, auth_method, trace_id)
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err := r.db.ExecContext(ctx, query,
		log.TenantID, log.RouteID, log.UserID, log.Method, log.Path,
		log.StatusCode, log.ResponseTimeMs, log.CacheHit, log.AuthMethod, log.TraceID)
	return err
}
