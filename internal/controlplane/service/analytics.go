package service

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/repository"
	"github.com/vantageedge/backend/pkg/logger"
)

type AnalyticsService interface {
	GetAnalytics(ctx context.Context, tenantID uuid.UUID, window time.Duration) (*AnalyticsResponse, error)
}

// AnalyticsResponse is the payload of GET /api/v1/analytics. Rates are
// fractions in [0,1] so the dashboard's formatPercent renders them directly.
type AnalyticsResponse struct {
	Window          string                  `json:"window"`
	GeneratedAt     time.Time               `json:"generated_at"`
	Totals          AnalyticsTotals         `json:"totals"`
	Series          []repository.TimeBucket `json:"series"`
	StatusBreakdown map[string]int64        `json:"status_breakdown"`
	TopRoutes       []repository.RouteStat  `json:"top_routes"`
}

type AnalyticsTotals struct {
	TotalRequests    int64   `json:"total_requests"`
	ErrorRate        float64 `json:"error_rate"`
	CacheHitRate     float64 `json:"cache_hit_rate"`
	RateLimitedCount int64   `json:"rate_limited_count"`
	AvgLatencyMs     float64 `json:"avg_latency_ms"`
	P95LatencyMs     float64 `json:"p95_latency_ms"`
}

type analyticsService struct {
	repos  *repository.Repository
	logger *logger.Logger
}

func NewAnalyticsService(repos *repository.Repository, log *logger.Logger) AnalyticsService {
	return &analyticsService{repos: repos, logger: log}
}

func (s *analyticsService) GetAnalytics(ctx context.Context, tenantID uuid.UUID, window time.Duration) (*AnalyticsResponse, error) {
	since := time.Now().Add(-window)

	// Hourly buckets read well for a day or less; past that the timeline
	// gets too dense, so switch to daily.
	bucket := "hour"
	if window > 24*time.Hour {
		bucket = "day"
	}

	summary, err := s.repos.Request.Summary(ctx, tenantID, since)
	if err != nil {
		return nil, err
	}
	series, err := s.repos.Request.TimeSeries(ctx, tenantID, since, bucket)
	if err != nil {
		return nil, err
	}
	status, err := s.repos.Request.StatusBreakdown(ctx, tenantID, since)
	if err != nil {
		return nil, err
	}
	top, err := s.repos.Request.TopRoutes(ctx, tenantID, since, 10)
	if err != nil {
		return nil, err
	}

	var errorRate, cacheHitRate float64
	if summary.TotalRequests > 0 {
		errorRate = float64(summary.ErrorRequests) / float64(summary.TotalRequests)
		cacheHitRate = float64(summary.CacheHits) / float64(summary.TotalRequests)
	}

	statusByCode := make(map[string]int64, len(status))
	for code, n := range status {
		statusByCode[strconv.Itoa(code)] = n
	}

	return &AnalyticsResponse{
		Window:      window.String(),
		GeneratedAt: time.Now().UTC(),
		Totals: AnalyticsTotals{
			TotalRequests:    summary.TotalRequests,
			ErrorRate:        errorRate,
			CacheHitRate:     cacheHitRate,
			RateLimitedCount: summary.RateLimited,
			AvgLatencyMs:     summary.AvgLatencyMs,
			P95LatencyMs:     summary.P95LatencyMs,
		},
		Series:          series,
		StatusBreakdown: statusByCode,
		TopRoutes:       top,
	}, nil
}
