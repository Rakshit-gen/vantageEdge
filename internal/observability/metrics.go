// Package observability provides real, exported Prometheus metrics for the
// gateway and control plane.
//
// The previous version of this package kept an in-memory struct
// (request counts, latency sums, a hand-rolled "tracer") that was never
// wired into any handler and exposed nothing over the network — despite
// docker-compose provisioning Prometheus and Jaeger to scrape/receive from
// it. That made the observability stack pure decoration. This version
// registers real prometheus.Collectors and serves them over /metrics so the
// existing Prometheus service in docker-compose has something to scrape.
//
// Distributed tracing (Jaeger/OTel export) is not implemented here — see
// the production-readiness report for that gap.
package observability

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds the Prometheus collectors for a service (gateway or
// control-plane). Create one per process with NewMetrics and reuse it.
type Metrics struct {
	registry *prometheus.Registry

	RequestsTotal   *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
	CacheResults    *prometheus.CounterVec
	RateLimited     *prometheus.CounterVec
	OriginErrors    *prometheus.CounterVec
}

// NewMetrics creates and registers the collectors for one service.
// namespace distinguishes gateway vs control-plane metrics
// (e.g. "vantageedge_gateway").
func NewMetrics(namespace string) *Metrics {
	registry := prometheus.NewRegistry()

	m := &Metrics{
		registry: registry,
		RequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "requests_total",
			Help:      "Total number of requests processed, labeled by tenant, route, method, and status code.",
		}, []string{"tenant", "route", "method", "status"}),
		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "request_duration_seconds",
			Help:      "Request latency in seconds, labeled by tenant and route.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"tenant", "route"}),
		CacheResults: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_results_total",
			Help:      "Cache hits/misses, labeled by tenant, route, and result (hit|miss).",
		}, []string{"tenant", "route", "result"}),
		RateLimited: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "rate_limited_total",
			Help:      "Requests rejected by rate limiting, labeled by tenant and route.",
		}, []string{"tenant", "route"}),
		OriginErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "origin_errors_total",
			Help:      "Errors proxying to an origin, labeled by tenant and origin.",
		}, []string{"tenant", "origin"}),
	}

	registry.MustRegister(
		m.RequestsTotal,
		m.RequestDuration,
		m.CacheResults,
		m.RateLimited,
		m.OriginErrors,
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
	)

	return m
}

// Handler returns the /metrics HTTP handler for this service's registry.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// Serve starts a dedicated HTTP server exposing /metrics on addr and blocks
// until ctx is cancelled, then shuts it down gracefully. Run it in its own
// goroutine.
func (m *Metrics) Serve(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", m.Handler())
	server := &http.Server{Addr: addr, Handler: mux}

	errCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
