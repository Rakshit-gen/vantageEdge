package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/models"
	"github.com/vantageedge/backend/pkg/database"
)

type RequestLogRepository interface {
	Create(ctx context.Context, log *models.RequestLog) error

	// Read side — powers GET /api/v1/analytics. Every method is scoped to a
	// single tenant and a time window; nothing here returns cross-tenant data.
	Summary(ctx context.Context, tenantID uuid.UUID, since time.Time) (*RequestLogSummary, error)
	TimeSeries(ctx context.Context, tenantID uuid.UUID, since time.Time, bucket string) ([]TimeBucket, error)
	StatusBreakdown(ctx context.Context, tenantID uuid.UUID, since time.Time) (map[int]int64, error)
	TopRoutes(ctx context.Context, tenantID uuid.UUID, since time.Time, limit int) ([]RouteStat, error)
}

// RequestLogSummary is a single-row rollup of a tenant's traffic over a window.
type RequestLogSummary struct {
	TotalRequests int64   `db:"total_requests"`
	ErrorRequests int64   `db:"error_requests"`
	CacheHits     int64   `db:"cache_hits"`
	RateLimited   int64   `db:"rate_limited"`
	AvgLatencyMs  float64 `db:"avg_latency_ms"`
	P95LatencyMs  float64 `db:"p95_latency_ms"`
}

// TimeBucket is one point on the throughput/latency timeline.
type TimeBucket struct {
	TS           time.Time `db:"ts" json:"ts"`
	Count        int64     `db:"count" json:"count"`
	AvgLatencyMs float64   `db:"avg_latency_ms" json:"avg_latency_ms"`
	ErrorCount   int64     `db:"error_count" json:"error_count"`
}

// RouteStat is traffic aggregated by request path.
type RouteStat struct {
	Path         string  `db:"path" json:"path"`
	Count        int64   `db:"count" json:"count"`
	AvgLatencyMs float64 `db:"avg_latency_ms" json:"avg_latency_ms"`
	ErrorCount   int64   `db:"error_count" json:"error_count"`
}

type requestLogRepository struct {
	db *database.DB
}

func NewRequestLogRepository(db *database.DB) RequestLogRepository {
	return &requestLogRepository{db: db}
}

func (r *requestLogRepository) Create(ctx context.Context, log *models.RequestLog) error {
	query := `INSERT INTO request_logs (tenant_id, route_id, user_id, method, path, status_code,
	          response_time_ms, cache_hit, rate_limited, auth_method, trace_id)
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	_, err := r.db.ExecContext(ctx, query,
		log.TenantID, log.RouteID, log.UserID, log.Method, log.Path,
		log.StatusCode, log.ResponseTimeMs, log.CacheHit, log.RateLimited, log.AuthMethod, log.TraceID)
	return err
}

// COALESCE wraps every aggregate: AVG and PERCENTILE_CONT return NULL for an
// empty window, and the dashboard expects numbers it can render, not nulls.
func (r *requestLogRepository) Summary(ctx context.Context, tenantID uuid.UUID, since time.Time) (*RequestLogSummary, error) {
	var s RequestLogSummary
	query := `
		SELECT
			COUNT(*)                                                                    AS total_requests,
			COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), 0)             AS error_requests,
			COALESCE(SUM(CASE WHEN cache_hit THEN 1 ELSE 0 END), 0)                      AS cache_hits,
			COALESCE(SUM(CASE WHEN rate_limited THEN 1 ELSE 0 END), 0)                   AS rate_limited,
			COALESCE(AVG(response_time_ms), 0)                                           AS avg_latency_ms,
			COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY response_time_ms), 0)  AS p95_latency_ms
		FROM request_logs
		WHERE tenant_id = $1 AND created_at >= $2`
	err := r.db.GetContext(ctx, &s, query, tenantID, since)
	return &s, err
}

// bucket is passed straight to date_trunc as a bind parameter (not string
// interpolation), so it is not an injection vector; Postgres rejects an
// unknown unit. Callers pass "hour" or "day".
func (r *requestLogRepository) TimeSeries(ctx context.Context, tenantID uuid.UUID, since time.Time, bucket string) ([]TimeBucket, error) {
	buckets := []TimeBucket{}
	query := `
		SELECT
			date_trunc($3, created_at)                                        AS ts,
			COUNT(*)                                                          AS count,
			COALESCE(AVG(response_time_ms), 0)                                AS avg_latency_ms,
			COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), 0)  AS error_count
		FROM request_logs
		WHERE tenant_id = $1 AND created_at >= $2
		GROUP BY 1
		ORDER BY 1`
	err := r.db.SelectContext(ctx, &buckets, query, tenantID, since, bucket)
	return buckets, err
}

func (r *requestLogRepository) StatusBreakdown(ctx context.Context, tenantID uuid.UUID, since time.Time) (map[int]int64, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT status_code, COUNT(*)
		FROM request_logs
		WHERE tenant_id = $1 AND created_at >= $2
		GROUP BY status_code
		ORDER BY status_code`, tenantID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int]int64)
	for rows.Next() {
		var code int
		var n int64
		if err := rows.Scan(&code, &n); err != nil {
			return nil, err
		}
		out[code] = n
	}
	return out, rows.Err()
}

func (r *requestLogRepository) TopRoutes(ctx context.Context, tenantID uuid.UUID, since time.Time, limit int) ([]RouteStat, error) {
	stats := []RouteStat{}
	query := `
		SELECT
			path,
			COUNT(*)                                                          AS count,
			COALESCE(AVG(response_time_ms), 0)                                AS avg_latency_ms,
			COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), 0)  AS error_count
		FROM request_logs
		WHERE tenant_id = $1 AND created_at >= $2
		GROUP BY path
		ORDER BY count DESC
		LIMIT $3`
	err := r.db.SelectContext(ctx, &stats, query, tenantID, since, limit)
	return stats, err
}
