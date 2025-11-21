package loadbalancer

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/vantageedge/backend/internal/models"
	"github.com/vantageedge/backend/pkg/logger"
)

type HealthChecker struct {
	mu             sync.RWMutex
	logger         *logger.Logger
	client         *http.Client
	checkInterval  time.Duration
	checkTimeout   time.Duration
	healthStatuses map[string]bool // origin_id -> is_healthy
	stopChan       chan struct{}
}

func NewHealthChecker(log *logger.Logger, interval time.Duration) *HealthChecker {
	return &HealthChecker{
		logger:         log,
		checkInterval:  interval,
		checkTimeout:   5 * time.Second,
		healthStatuses: make(map[string]bool),
		stopChan:       make(chan struct{}),
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Start begins periodic health checks for origins
func (hc *HealthChecker) Start(origins []*models.Origin) {
	go hc.runHealthChecks(origins)
}

// Stop stops the health checker
func (hc *HealthChecker) Stop() {
	close(hc.stopChan)
}

// IsHealthy checks if an origin is currently healthy
func (hc *HealthChecker) IsHealthy(originID string) bool {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	status, exists := hc.healthStatuses[originID]
	return exists && status
}

// CheckHealth performs a single health check on an origin
func (hc *HealthChecker) CheckHealth(ctx context.Context, origin *models.Origin) bool {
	if origin.HealthCheckPath == "" {
		return true // No health check configured
	}

	url := origin.URL + origin.HealthCheckPath
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		hc.logger.Error().Err(err).Str("origin_id", origin.ID.String()).Msg("Failed to create health check request")
		return false
	}

	resp, err := hc.client.Do(req)
	if err != nil {
		hc.logger.Warn().Err(err).Str("origin_id", origin.ID.String()).Msg("Health check failed")
		return false
	}
	defer resp.Body.Close()

	isHealthy := resp.StatusCode >= 200 && resp.StatusCode < 300
	return isHealthy
}

// runHealthChecks periodically checks the health of origins
func (hc *HealthChecker) runHealthChecks(origins []*models.Origin) {
	ticker := time.NewTicker(hc.checkInterval)
	defer ticker.Stop()

	// Run initial checks
	for _, origin := range origins {
		hc.checkOriginHealth(origin)
	}

	for {
		select {
		case <-hc.stopChan:
			return
		case <-ticker.C:
			for _, origin := range origins {
				hc.checkOriginHealth(origin)
			}
		}
	}
}

// checkOriginHealth checks health of a single origin and updates status
func (hc *HealthChecker) checkOriginHealth(origin *models.Origin) {
	ctx, cancel := context.WithTimeout(context.Background(), hc.checkTimeout)
	defer cancel()

	isHealthy := hc.CheckHealth(ctx, origin)

	hc.mu.Lock()
	defer hc.mu.Unlock()

	oldStatus := hc.healthStatuses[origin.ID.String()]
	hc.healthStatuses[origin.ID.String()] = isHealthy

	if oldStatus != isHealthy {
		if isHealthy {
			hc.logger.Info().Str("origin_id", origin.ID.String()).Msg("Origin became healthy")
		} else {
			hc.logger.Warn().Str("origin_id", origin.ID.String()).Msg("Origin became unhealthy")
		}
	}
}

// GetHealthyOrigins returns only healthy origins from the list
func (hc *HealthChecker) GetHealthyOrigins(origins []*models.Origin) []*models.Origin {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	var healthy []*models.Origin
	for _, origin := range origins {
		if hc.healthStatuses[origin.ID.String()] || origin.HealthCheckPath == "" {
			healthy = append(healthy, origin)
		}
	}

	if len(healthy) == 0 {
		return origins // Return all if none are healthy
	}

	return healthy
}
