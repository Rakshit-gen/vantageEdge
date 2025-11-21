package observability

import (
	"sync"
	"time"
)

type Metrics struct {
	mu sync.RWMutex

	// Request metrics
	totalRequests   int64
	totalErrors     int64
	totalCacheHits  int64
	totalCacheMisses int64

	// Latency metrics
	minLatencyMs float64
	maxLatencyMs float64
	avgLatencyMs float64
	latencySum   float64
	latencyCount int64

	// Status code metrics
	statusCodes map[int]int64

	// Origin metrics
	originRequests map[string]int64
	originErrors   map[string]int64
}

func NewMetrics() *Metrics {
	return &Metrics{
		statusCodes:      make(map[int]int64),
		originRequests:   make(map[string]int64),
		originErrors:     make(map[string]int64),
		minLatencyMs:     -1,
	}
}

// RecordRequest records a new request
func (m *Metrics) RecordRequest(statusCode int, latencyMs float64, cacheHit bool, originID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalRequests++
	m.statusCodes[statusCode]++
	m.originRequests[originID]++

	// Record latency
	m.latencySum += latencyMs
	m.latencyCount++
	if m.minLatencyMs < 0 || latencyMs < m.minLatencyMs {
		m.minLatencyMs = latencyMs
	}
	if latencyMs > m.maxLatencyMs {
		m.maxLatencyMs = latencyMs
	}
	m.avgLatencyMs = m.latencySum / float64(m.latencyCount)

	// Record cache hit/miss
	if cacheHit {
		m.totalCacheHits++
	} else {
		m.totalCacheMisses++
	}

	// Track errors
	if statusCode >= 400 {
		m.totalErrors++
		m.originErrors[originID]++
	}
}

// GetMetrics returns a snapshot of current metrics
func (m *Metrics) GetMetrics() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	totalRequests := m.totalRequests
	if totalRequests == 0 {
		totalRequests = 1
	}

	cacheHitRate := float64(m.totalCacheHits) / float64(m.totalRequests+1) * 100

	return map[string]interface{}{
		"total_requests":    m.totalRequests,
		"total_errors":      m.totalErrors,
		"error_rate":        float64(m.totalErrors) / float64(totalRequests) * 100,
		"cache_hits":        m.totalCacheHits,
		"cache_misses":      m.totalCacheMisses,
		"cache_hit_rate":    cacheHitRate,
		"avg_latency_ms":    m.avgLatencyMs,
		"min_latency_ms":    m.minLatencyMs,
		"max_latency_ms":    m.maxLatencyMs,
		"status_codes":      m.statusCodes,
		"origin_requests":   m.originRequests,
		"origin_errors":     m.originErrors,
	}
}

// Reset clears all metrics
func (m *Metrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalRequests = 0
	m.totalErrors = 0
	m.totalCacheHits = 0
	m.totalCacheMisses = 0
	m.latencySum = 0
	m.latencyCount = 0
	m.minLatencyMs = -1
	m.maxLatencyMs = 0
	m.statusCodes = make(map[int]int64)
	m.originRequests = make(map[string]int64)
	m.originErrors = make(map[string]int64)
}

// TimingSample records timing information
type TimingSample struct {
	StartTime time.Time
	EndTime   time.Time
}

func NewTimingSample() *TimingSample {
	return &TimingSample{StartTime: time.Now()}
}

func (ts *TimingSample) End() {
	ts.EndTime = time.Now()
}

func (ts *TimingSample) GetLatencyMs() float64 {
	if ts.EndTime.IsZero() {
		ts.EndTime = time.Now()
	}
	return ts.EndTime.Sub(ts.StartTime).Seconds() * 1000
}
