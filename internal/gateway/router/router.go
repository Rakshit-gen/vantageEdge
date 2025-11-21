package router

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/repository"
	"github.com/vantageedge/backend/pkg/config"
	"github.com/vantageedge/backend/pkg/logger"
)

type Gateway struct {
	config  *config.Config
	repos   *repository.Repository
	logger  *logger.Logger
}

func New(cfg *config.Config, repos *repository.Repository, log *logger.Logger) http.Handler {
	g := &Gateway{
		config:  cfg,
		repos:   repos,
		logger:  log,
	}

	mux := http.NewServeMux()
	
	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	
	// Main gateway handler
	mux.HandleFunc("/", g.handleRequest)

	return mux
}

func (g *Gateway) handleRequest(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	
	// Extract tenant from subdomain
	tenantID, err := g.extractTenant(r)
	if err != nil {
		g.logger.Error().Err(err).Msg("Failed to extract tenant")
		http.Error(w, "Invalid tenant", http.StatusBadRequest)
		return
	}

	// Find matching route
	route, err := g.repos.Route.FindMatchingRoute(r.Context(), tenantID, r.URL.Path, r.Method)
	if err != nil {
		g.logger.Error().Err(err).Msg("No matching route")
		http.Error(w, "Route not found", http.StatusNotFound)
		return
	}

	// Get origin
	origin, err := g.repos.Origin.GetByID(r.Context(), route.OriginID)
	if err != nil {
		g.logger.Error().Err(err).Msg("Origin not found")
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}

	// TODO: Apply authentication middleware
	// TODO: Apply rate limiting
	// TODO: Check cache

	// Proxy request
	g.proxyRequest(w, r, origin.URL)

	duration := time.Since(start)
	g.logger.Info().
		Str("path", r.URL.Path).
		Str("method", r.Method).
		Dur("duration", duration).
		Msg("Request processed")
}

func (g *Gateway) extractTenant(r *http.Request) (uuid.UUID, error) {
	host := r.Host
	parts := strings.Split(host, ".")
	
	if len(parts) < 2 {
		return uuid.Nil, fmt.Errorf("invalid host format")
	}
	
	subdomain := parts[0]
	
	// Get tenant by subdomain
	tenant, err := g.repos.Tenant.GetBySubdomain(r.Context(), subdomain)
	if err != nil {
		return uuid.Nil, err
	}
	
	return tenant.ID, nil
}

func (g *Gateway) proxyRequest(w http.ResponseWriter, r *http.Request, originURL string) {
	target, err := url.Parse(originURL)
	if err != nil {
		g.logger.Error().Err(err).Msg("Invalid origin URL")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ServeHTTP(w, r)
}
