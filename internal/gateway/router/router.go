// Package router implements the gateway's request pipeline.
//
// The previous implementation only extracted the tenant from the
// subdomain, looked up a route via a broken SQL query (see match.go's doc
// comment), and handed the request to a bare
// httputil.NewSingleHostReverseProxy — auth, rate limiting, and caching
// were all implemented elsewhere in the codebase but never called from
// here, and there was no panic recovery, so a single bad request (e.g. a
// nil origin, a malformed header) would crash the entire process for every
// tenant. This version wires the full pipeline together: tenant resolution
// -> route match -> auth -> rate limit -> cache -> proxy -> log/metrics,
// with panic recovery around every request.
package router

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/gateway/middleware"
	"github.com/vantageedge/backend/internal/gateway/proxy"
	"github.com/vantageedge/backend/internal/loadbalancer"
	"github.com/vantageedge/backend/internal/models"
	"github.com/vantageedge/backend/internal/observability"
	"github.com/vantageedge/backend/internal/ratelimit"
	"github.com/vantageedge/backend/internal/repository"
	"github.com/vantageedge/backend/pkg/config"
	"github.com/vantageedge/backend/pkg/logger"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Gateway struct {
	config *config.Config
	// repos is retained only for the request-log write path (an
	// analytics write, not part of serving a request) — everything the
	// gateway needs to *serve* a request (tenant/route/origin config)
	// comes from configCache/the control plane's gRPC ConfigService now,
	// not direct Postgres reads.
	repos  *repository.Repository
	logger *logger.Logger

	configCache   *ConfigCache
	authenticator *middleware.Authenticator
	limiter       ratelimit.Limiter
	cache         *middleware.ResponseCache
	proxy         *proxy.ReverseProxy
	metrics       *observability.Metrics
	healthChecker *loadbalancer.HealthChecker

	// logSem bounds how many request-log writes can be in flight at once.
	// Without it a traffic spike spawns one goroutine per request against
	// a fixed-size DB pool: tens of thousands of goroutines pile up waiting
	// for a connection, and the contention starves the pool for everything
	// else that shares it. Past the cap, log writes are dropped (counted in
	// logsDropped) rather than queued — an analytics gap is survivable,
	// unbounded goroutine growth on the serving path is not.
	logSem      chan struct{}
	logsDropped atomic.Int64
}

// Deps bundles everything the gateway needs beyond config/repos/logger, so
// New's signature stays manageable as the pipeline grows.
type Deps struct {
	ConfigCache   *ConfigCache
	Authenticator *middleware.Authenticator
	Limiter       ratelimit.Limiter
	Cache         *middleware.ResponseCache
	Proxy         *proxy.ReverseProxy
	Metrics       *observability.Metrics
	HealthChecker *loadbalancer.HealthChecker
}

func New(cfg *config.Config, repos *repository.Repository, deps Deps, log *logger.Logger) http.Handler {
	g := &Gateway{
		config:        cfg,
		repos:         repos,
		logger:        log,
		configCache:   deps.ConfigCache,
		authenticator: deps.Authenticator,
		limiter:       deps.Limiter,
		cache:         deps.Cache,
		proxy:         deps.Proxy,
		metrics:       deps.Metrics,
		healthChecker: deps.HealthChecker,
		logSem:        make(chan struct{}, 256),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("/", g.handleRequest)

	return recoverMiddleware(log)(mux)
}

// recoverMiddleware ensures a panic in any single request (nil pointer,
// bad type assertion, etc.) returns a 500 to that one caller instead of
// crashing the process and dropping every in-flight request across every
// tenant.
func recoverMiddleware(log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					log.Error().
						Interface("panic", err).
						Str("path", r.URL.Path).
						Str("method", r.Method).
						Msg("Recovered from panic in gateway request handler")
					http.Error(w, "Internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func (g *Gateway) handleRequest(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// The span's context replaces the request's context for the rest of
	// this handler (including the call into ProxyRequest), so the trace
	// context propagates into the headers sent to the origin — Jaeger was
	// previously receiving nothing at all from this process.
	ctx, span := observability.Tracer("vantageedge/gateway").Start(r.Context(), "gateway.request",
		trace.WithAttributes(attribute.String("http.method", r.Method), attribute.String("http.path", r.URL.Path)))
	defer span.End()
	r = r.WithContext(ctx)

	subdomain, err := resolveSubdomain(r)
	if err != nil {
		g.logger.Warn().Err(err).Str("host", r.Host).Msg("Failed to resolve tenant")
		http.Error(w, "Unknown tenant", http.StatusNotFound)
		span.SetStatus(codes.Error, "unknown tenant")
		return
	}

	route, pool, tenantID, err := g.configCache.Match(ctx, subdomain, r.URL.Path, r.Method)
	if err != nil {
		span.SetAttributes(attribute.String("tenant.id", tenantID.String()))
		switch {
		case errors.Is(err, ErrTenantNotFound):
			http.Error(w, "Unknown tenant", http.StatusNotFound)
		case errors.Is(err, ErrTenantSuspended):
			http.Error(w, "Tenant suspended", http.StatusForbidden)
		case errors.Is(err, ErrNoMatchingRoute):
			http.Error(w, "Route not found", http.StatusNotFound)
		default:
			g.logger.Error().Err(err).Msg("Failed to fetch tenant config from control plane")
			http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		}
		g.logRequest(tenantID, nil, r, http.StatusNotFound, time.Since(start), false, false, "")
		span.SetStatus(codes.Error, err.Error())
		return
	}
	span.SetAttributes(attribute.String("tenant.id", tenantID.String()),
		attribute.String("route.id", route.ID.String()), attribute.String("route.name", route.Name))

	identity, err := g.authenticator.Authenticate(ctx, r, route, tenantID)
	if err != nil {
		g.logger.Debug().Err(err).Str("route_id", route.ID.String()).Msg("Authentication failed")
		w.Header().Set("WWW-Authenticate", `Bearer`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		g.logRequest(tenantID, &route.ID, r, http.StatusUnauthorized, time.Since(start), false, false, "")
		return
	}

	if route.RateLimitEnabled {
		limited, retryAfter := g.checkRateLimit(ctx, r, route, tenantID, *identity)
		if limited {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())+1))
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			g.metrics.RateLimited.WithLabelValues(tenantID.String(), route.ID.String()).Inc()
			g.logRequest(tenantID, &route.ID, r, http.StatusTooManyRequests, time.Since(start), false, true, identity.Method)
			return
		}
	}

	cacheable := route.CacheEnabled && (r.Method == http.MethodGet || r.Method == http.MethodHead)
	var cacheKey string
	if cacheable {
		cacheKey = middleware.BuildCacheKey(tenantID.String(), route.ID.String(), identity.CacheIdentityKey(),
			route.CacheKeyPattern, r.URL.Path, r.URL.RawQuery)
		if cached, ok := g.cache.Get(ctx, cacheKey); ok {
			g.writeCachedResponse(w, cached)
			g.metrics.CacheResults.WithLabelValues(tenantID.String(), route.ID.String(), "hit").Inc()
			g.logRequest(tenantID, &route.ID, r, cached.StatusCode, time.Since(start), true, false, identity.Method)
			return
		}
		g.metrics.CacheResults.WithLabelValues(tenantID.String(), route.ID.String(), "miss").Inc()
	}

	if len(pool) == 0 {
		g.logger.Error().Str("route_id", route.ID.String()).Msg("No origins configured for route")
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		g.logRequest(tenantID, &route.ID, r, http.StatusServiceUnavailable, time.Since(start), false, false, identity.Method)
		return
	}
	// Start (idempotently) monitoring every origin in this pool, and
	// restrict selection to currently-healthy ones — GetHealthyOrigins
	// falls back to the full pool if none are known-healthy yet (e.g.
	// right after the first request touches a new origin), so this never
	// blocks traffic on the very first health check completing.
	g.healthChecker.EnsureMonitored(pool)
	healthyPool := g.healthChecker.GetHealthyOrigins(pool)

	statusCode := g.proxyToOrigin(w, r, route, healthyPool, cacheable, cacheKey, tenantID)

	duration := time.Since(start)
	g.metrics.RequestsTotal.WithLabelValues(tenantID.String(), route.ID.String(), r.Method, strconv.Itoa(statusCode)).Inc()
	g.metrics.RequestDuration.WithLabelValues(tenantID.String(), route.ID.String()).Observe(duration.Seconds())
	g.logRequest(tenantID, &route.ID, r, statusCode, duration, false, false, identity.Method)

	span.SetAttributes(attribute.Int("http.status_code", statusCode))
	if statusCode >= 500 {
		span.SetStatus(codes.Error, fmt.Sprintf("origin returned %d", statusCode))
	}
}

// proxyToOrigin load-balances across pool according to route.LoadBalancing
// ("weighted" default, "round_robin", "least_conn", "ip_hash"). Each retry
// attempt re-selects from the pool rather than hammering the same origin
// again, so a transient failure on one origin fails over to another
// healthy pool member when one exists.
func (g *Gateway) proxyToOrigin(w http.ResponseWriter, r *http.Request, route *models.Route, pool []*models.Origin, cacheable bool, cacheKey string, tenantID uuid.UUID) int {
	attempts := route.RetryAttempts + 1
	if attempts < 1 {
		attempts = 1
	}
	// Only GET/HEAD are safe to retry: retrying a non-idempotent request
	// (POST/PUT/DELETE) after a network error risks double-applying it at
	// the origin.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		attempts = 1
	}

	var lastErr error
	var lastOrigin *models.Origin
	tried := make(map[uuid.UUID]bool, len(pool))
	for attempt := 0; attempt < attempts; attempt++ {
		// Retry on a different origin than the ones already tried, so a
		// failover actually moves off the bad origin. Fall back to the
		// full pool once every member has failed (better to retry than
		// give up while attempts remain).
		candidates := pool
		if len(tried) > 0 {
			candidates = candidates[:0:0]
			for _, o := range pool {
				if !tried[o.ID] {
					candidates = append(candidates, o)
				}
			}
			if len(candidates) == 0 {
				candidates = pool
			}
		}

		origin, err := loadbalancer.Select(route.LoadBalancing, route.ID, clientIP(r), candidates)
		if err != nil {
			lastErr = err
			break
		}
		lastOrigin = origin
		tried[origin.ID] = true

		timeout := time.Duration(route.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = time.Duration(origin.TimeoutSeconds) * time.Second
		}

		loadbalancer.AddConn(origin.ID)
		resp, cancel, err := g.proxy.ProxyRequest(r.Context(), r, origin, nil, timeout)
		if err != nil {
			loadbalancer.DoneConn(origin.ID)
			lastErr = err
			continue
		}

		if cacheable && resp.StatusCode == http.StatusOK && cacheableResponse(resp) {
			g.writeAndCacheResponse(w, resp, route, cacheKey)
		} else {
			g.proxy.WriteResponse(w, resp)
		}
		cancel()
		loadbalancer.DoneConn(origin.ID)
		return resp.StatusCode
	}

	logEvent := g.logger.Error().Err(lastErr)
	if lastOrigin != nil {
		logEvent = logEvent.Str("origin_id", lastOrigin.ID.String())
		g.metrics.OriginErrors.WithLabelValues(tenantID.String(), lastOrigin.ID.String()).Inc()
	}
	logEvent.Msg("Failed to reach origin")
	http.Error(w, "Bad gateway", http.StatusBadGateway)
	return http.StatusBadGateway
}

func (g *Gateway) writeAndCacheResponse(w http.ResponseWriter, resp *http.Response, route *models.Route, cacheKey string) {
	// Read at most one byte past the cache cap. A body larger than that (or
	// an unbounded one that slipped past cacheableResponse) is not cached
	// and is streamed through in full — buffered prefix plus the rest of
	// the wire — so it can never be pulled entirely into memory.
	prefix, err := io.ReadAll(io.LimitReader(resp.Body, maxCacheableBodyBytes+1))
	if err != nil || len(prefix) > maxCacheableBodyBytes {
		if err != nil {
			g.logger.Warn().Err(err).Msg("Failed to buffer response body for caching")
		}
		resp.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(prefix), resp.Body), resp.Body}
		g.proxy.WriteResponse(w, resp)
		return
	}
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(prefix))

	header := make(map[string][]string, len(resp.Header))
	for k, v := range resp.Header {
		header[k] = v
	}
	g.cache.Set(context.Background(), cacheKey, &middleware.CachedResponse{
		StatusCode: resp.StatusCode,
		Header:     header,
		Body:       prefix,
	}, time.Duration(route.CacheTTLSeconds)*time.Second)

	g.proxy.WriteResponse(w, resp)
}

// cacheableResponse rejects responses that must not be stored or replayed
// to another caller: anything the origin marked no-store/no-cache/private,
// anything carrying a Set-Cookie, and event streams (which never end).
func cacheableResponse(resp *http.Response) bool {
	if resp.Header.Get("Set-Cookie") != "" {
		return false
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return false
	}
	cc := strings.ToLower(resp.Header.Get("Cache-Control"))
	return !strings.Contains(cc, "no-store") &&
		!strings.Contains(cc, "no-cache") &&
		!strings.Contains(cc, "private")
}

func (g *Gateway) writeCachedResponse(w http.ResponseWriter, cached *middleware.CachedResponse) {
	for k, values := range cached.Header {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("X-Cache", "HIT")
	w.WriteHeader(cached.StatusCode)
	w.Write(cached.Body)
}

func (g *Gateway) checkRateLimit(ctx context.Context, r *http.Request, route *models.Route, tenantID uuid.UUID, identity middleware.Identity) (limited bool, retryAfter time.Duration) {
	key := g.rateLimitKey(r, route, tenantID, identity)
	rps := float64(route.RateLimitRequestsPerSecond)
	if rps <= 0 {
		rps = float64(g.config.RateLimit.DefaultRPS)
	}
	burst := route.RateLimitBurst
	if burst <= 0 {
		burst = g.config.RateLimit.DefaultBurst
	}

	result, err := g.limiter.Allow(ctx, key, rps, burst)
	if err != nil {
		// Fail open: a rate limiter outage must not take down the whole
		// gateway. It's logged so it's visible in metrics/alerts.
		g.logger.Error().Err(err).Msg("Rate limiter check failed; allowing request")
		return false, 0
	}
	return !result.Allowed, result.RetryAfter
}

func (g *Gateway) rateLimitKey(r *http.Request, route *models.Route, tenantID uuid.UUID, identity middleware.Identity) string {
	switch route.RateLimitKeyStrategy {
	case "tenant":
		return fmt.Sprintf("%s:%s", tenantID, route.ID)
	case "ip":
		return fmt.Sprintf("%s:%s:%s", tenantID, route.ID, clientIP(r))
	default: // "tenant_user" and any unset/unrecognized strategy
		who := identity.CacheIdentityKey()
		if who == "public" {
			who = clientIP(r)
		}
		return fmt.Sprintf("%s:%s:%s", tenantID, route.ID, who)
	}
}

// resolveSubdomain picks the tenant subdomain for a request. An explicit
// X-Tenant-Subdomain header wins — for callers that can't put the tenant
// in the Host, such as a managed platform that terminates every route on
// one hostname, or curl against the raw gateway URL. Otherwise it's the
// first label of the Host. The resolved tenant is still subject to
// per-route auth (middleware.Authenticate rejects a JWT or API key
// belonging to another tenant), so the header can't reach a tenant's
// protected routes.
func resolveSubdomain(r *http.Request) (string, error) {
	if s := r.Header.Get("X-Tenant-Subdomain"); s != "" {
		return s, nil
	}
	return subdomainFromHost(r.Host)
}

func subdomainFromHost(host string) (string, error) {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid host format: %s", host)
	}
	return parts[0], nil
}

func (g *Gateway) logRequest(tenantID uuid.UUID, routeID *uuid.UUID, r *http.Request, statusCode int, duration time.Duration, cacheHit, rateLimited bool, authMethod string) {
	// Fire-and-forget on a bounded timeout: request logging must never add
	// latency to (or fail) the request it's describing. logSem caps the
	// number of these in flight — past the cap the write is dropped, not
	// queued (see the field comment).
	select {
	case g.logSem <- struct{}{}:
	default:
		g.logsDropped.Add(1)
		return
	}
	go func() {
		defer func() { <-g.logSem }()

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		log := &models.RequestLog{
			TenantID:       tenantID,
			RouteID:        routeID,
			Method:         r.Method,
			Path:           r.URL.Path,
			StatusCode:     statusCode,
			ResponseTimeMs: int(duration.Milliseconds()),
			CacheHit:       cacheHit,
			RateLimited:    rateLimited,
		}
		if authMethod != "" {
			log.AuthMethod = &authMethod
		}
		if err := g.repos.Request.Create(ctx, log); err != nil {
			g.logger.Debug().Err(err).Msg("Failed to write request log")
		}
	}()
}

// maxCacheableBodyBytes caps how large a response body this gateway will
// buffer into memory/Redis for caching. Larger responses are still read
// and forwarded to the client in full; they're just not stored in cache.
const maxCacheableBodyBytes = 2 << 20 // 2MB

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
