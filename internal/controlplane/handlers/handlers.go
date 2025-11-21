package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/controlplane/service"
	"github.com/vantageedge/backend/internal/models"
	"github.com/vantageedge/backend/pkg/logger"
)

type Handlers struct {
	service *service.Service
	logger  *logger.Logger
}

// resolveTenantID converts a tenant ID string (UUID or Clerk ID) to a UUID
// If it's a Clerk ID, it looks up or creates the tenant
func (h *Handlers) resolveTenantID(ctx context.Context, tenantIDStr string) (uuid.UUID, error) {
	// Try to parse as UUID first
	tenantID, err := uuid.Parse(tenantIDStr)
	if err == nil {
		return tenantID, nil
	}

	// If not a UUID, treat it as Clerk org/user ID and look up or create tenant
	tenant, lookupErr := h.service.Repos.Tenant.GetByClerkOrgID(ctx, tenantIDStr)
	if lookupErr != nil {
		// Tenant doesn't exist, create it
		subdomain := tenantIDStr
		if len(subdomain) > 50 {
			subdomain = subdomain[:50]
		}
		tenant = &models.Tenant{
			Name:       "My Workspace",
			Subdomain:  subdomain,
			ClerkOrgID: &tenantIDStr,
			Status:     "active",
			Settings:   models.JSONB{},
		}
		if createErr := h.service.Repos.Tenant.Create(ctx, tenant); createErr != nil {
			h.logger.Error().Err(createErr).Msg("Failed to create tenant")
			return uuid.Nil, createErr
		}
	}
	return tenant.ID, nil
}

func New(svc *service.Service, log *logger.Logger) *Handlers {
	return &Handlers{
		service: svc,
		logger:  log,
	}
}

func (h *Handlers) RegisterRoutes(r chi.Router) {
	// Tenant routes
	r.Route("/tenants", func(r chi.Router) {
		r.Post("/", h.CreateTenant)
		r.Get("/", h.ListTenants)
		r.Get("/{id}", h.GetTenant)
		r.Put("/{id}", h.UpdateTenant)
		r.Delete("/{id}", h.DeleteTenant)
	})

	// Origin routes
	r.Route("/origins", func(r chi.Router) {
		r.Post("/", h.CreateOrigin)
		r.Get("/{id}", h.GetOrigin)
		r.Get("/tenant/{tenant_id}", h.ListOrigins)
		r.Put("/{id}", h.UpdateOrigin)
		r.Patch("/{id}", h.UpdateOrigin)
		r.Delete("/{id}", h.DeleteOrigin)
	})

	// Route rules
	r.Route("/routes", func(r chi.Router) {
		r.Post("/", h.CreateRoute)
		r.Get("/{id}", h.GetRoute)
		r.Get("/tenant/{tenant_id}", h.ListRoutes)
		r.Put("/{id}", h.UpdateRoute)
		r.Patch("/{id}", h.UpdateRoute)
		r.Delete("/{id}", h.DeleteRoute)
	})

	// API keys
	r.Route("/api-keys", func(r chi.Router) {
		r.Post("/", h.CreateAPIKey)
		r.Get("/tenant/{tenant_id}", h.ListAPIKeys)
		r.Delete("/{id}", h.DeleteAPIKey)
	})
}

func (h *Handlers) CreateTenant(w http.ResponseWriter, r *http.Request) {
	var req service.CreateTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	tenant, err := h.service.Tenant.CreateTenant(r.Context(), &req)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "Failed to create tenant")
		return
	}

	h.respondJSON(w, http.StatusCreated, tenant)
}

func (h *Handlers) GetTenant(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid tenant ID")
		return
	}

	tenant, err := h.service.Tenant.GetTenant(r.Context(), id)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "Tenant not found")
		return
	}

	h.respondJSON(w, http.StatusOK, tenant)
}

func (h *Handlers) ListTenants(w http.ResponseWriter, r *http.Request) {
	tenants, err := h.service.Tenant.ListTenants(r.Context())
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "Failed to list tenants")
		return
	}

	h.respondJSON(w, http.StatusOK, tenants)
}

func (h *Handlers) UpdateTenant(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid tenant ID")
		return
	}

	var req service.UpdateTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	tenant, err := h.service.Tenant.UpdateTenant(r.Context(), id, &req)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "Failed to update tenant")
		return
	}

	h.respondJSON(w, http.StatusOK, tenant)
}

func (h *Handlers) DeleteTenant(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid tenant ID")
		return
	}

	if err := h.service.Tenant.DeleteTenant(r.Context(), id); err != nil {
		h.respondError(w, http.StatusInternalServerError, "Failed to delete tenant")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Origin handlers
func (h *Handlers) CreateOrigin(w http.ResponseWriter, r *http.Request) {
	var reqBody map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Get tenant ID from request body or query parameter
	tenantIDStr := ""
	if tid, ok := reqBody["tenant_id"].(string); ok {
		tenantIDStr = tid
	}
	if tenantIDStr == "" {
		tenantIDStr = r.URL.Query().Get("tenant_id")
	}

	if tenantIDStr == "" {
		h.respondError(w, http.StatusBadRequest, "Tenant ID is required")
		return
	}

	// Resolve tenant ID (UUID or Clerk ID)
	tenantID, err := h.resolveTenantID(r.Context(), tenantIDStr)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to resolve tenant ID")
		h.respondError(w, http.StatusInternalServerError, "Failed to resolve tenant ID")
		return
	}

	// Build the request
	req := service.CreateOriginRequest{
		TenantID: tenantID,
	}

	if name, ok := reqBody["name"].(string); ok {
		req.Name = name
	}
	if url, ok := reqBody["url"].(string); ok {
		req.URL = url
	}
	if path, ok := reqBody["health_check_path"].(string); ok {
		req.HealthCheckPath = path
	}
	if interval, ok := reqBody["health_check_interval"].(float64); ok {
		req.HealthCheckInterval = int(interval)
	}
	if timeout, ok := reqBody["timeout_seconds"].(float64); ok {
		req.TimeoutSeconds = int(timeout)
	}
	if retries, ok := reqBody["max_retries"].(float64); ok {
		req.MaxRetries = int(retries)
	}
	if weight, ok := reqBody["weight"].(float64); ok {
		req.Weight = int(weight)
	}

	// Validate request
	if req.Name == "" || req.URL == "" {
		h.respondError(w, http.StatusBadRequest, "Name and URL are required")
		return
	}

	origin, err := h.service.Origin.CreateOrigin(r.Context(), &req)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to create origin")
		h.respondError(w, http.StatusInternalServerError, "Failed to create origin")
		return
	}

	h.respondJSON(w, http.StatusCreated, origin)
}

func (h *Handlers) GetOrigin(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid origin ID")
		return
	}

	origin, err := h.service.Origin.GetOrigin(r.Context(), id)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "Origin not found")
		return
	}

	h.respondJSON(w, http.StatusOK, origin)
}

func (h *Handlers) ListOrigins(w http.ResponseWriter, r *http.Request) {
	tenantIDStr := chi.URLParam(r, "tenant_id")

	// Resolve tenant ID (UUID or Clerk ID)
	tenantID, err := h.resolveTenantID(r.Context(), tenantIDStr)
	if err != nil {
		// If tenant doesn't exist, return empty array
		h.respondJSON(w, http.StatusOK, []interface{}{})
		return
	}

	origins, err := h.service.Origin.ListByTenant(r.Context(), tenantID)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to list origins")
		h.respondError(w, http.StatusInternalServerError, "Failed to list origins")
		return
	}

	h.respondJSON(w, http.StatusOK, origins)
}

func (h *Handlers) UpdateOrigin(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid origin ID")
		return
	}

	var req service.UpdateOriginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	origin, err := h.service.Origin.UpdateOrigin(r.Context(), id, &req)
	if err != nil {
		h.logger.Error().Err(err).Str("id", id.String()).Msg("Failed to update origin")
		h.respondError(w, http.StatusInternalServerError, "Failed to update origin")
		return
	}

	h.respondJSON(w, http.StatusOK, origin)
}

func (h *Handlers) DeleteOrigin(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid origin ID")
		return
	}

	if err := h.service.Origin.DeleteOrigin(r.Context(), id); err != nil {
		h.logger.Error().Err(err).Str("id", id.String()).Msg("Failed to delete origin")
		h.respondError(w, http.StatusInternalServerError, "Failed to delete origin")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Route handlers
func (h *Handlers) CreateRoute(w http.ResponseWriter, r *http.Request) {
	var reqBody map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Get tenant ID from request body or query parameter
	tenantIDStr := ""
	if tid, ok := reqBody["tenant_id"].(string); ok {
		tenantIDStr = tid
	}
	if tenantIDStr == "" {
		tenantIDStr = r.URL.Query().Get("tenant_id")
	}

	if tenantIDStr == "" {
		h.respondError(w, http.StatusBadRequest, "Tenant ID is required")
		return
	}

	// Resolve tenant ID (UUID or Clerk ID)
	tenantID, err := h.resolveTenantID(r.Context(), tenantIDStr)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to resolve tenant ID")
		h.respondError(w, http.StatusInternalServerError, "Failed to resolve tenant ID")
		return
	}

	// Build the request
	req := service.CreateRouteRequest{
		TenantID: tenantID,
	}

	if name, ok := reqBody["name"].(string); ok {
		req.Name = name
	}
	if pattern, ok := reqBody["path_pattern"].(string); ok {
		req.PathPattern = pattern
	}
	if originIDStr, ok := reqBody["origin_id"].(string); ok {
		originID, parseErr := uuid.Parse(originIDStr)
		if parseErr != nil {
			h.respondError(w, http.StatusBadRequest, "Invalid origin ID")
			return
		}
		req.OriginID = originID
	}
	if methods, ok := reqBody["methods"].([]interface{}); ok {
		for _, method := range methods {
			if m, ok := method.(string); ok {
				req.Methods = append(req.Methods, m)
			}
		}
	}
	if priority, ok := reqBody["priority"].(float64); ok {
		req.Priority = int(priority)
	}
	if authMode, ok := reqBody["auth_mode"].(string); ok {
		req.AuthMode = authMode
	}
	if isActive, ok := reqBody["is_active"].(bool); ok {
		req.IsActive = isActive
	}
	if rateLimitEnabled, ok := reqBody["rate_limit_enabled"].(bool); ok {
		req.RateLimitEnabled = rateLimitEnabled
	}
	if rps, ok := reqBody["rate_limit_requests_per_second"].(float64); ok {
		req.RateLimitRequestsPerSecond = int(rps)
	}
	if burst, ok := reqBody["rate_limit_burst"].(float64); ok {
		req.RateLimitBurst = int(burst)
	}
	if strategy, ok := reqBody["rate_limit_key_strategy"].(string); ok {
		req.RateLimitKeyStrategy = strategy
	}
	if cacheEnabled, ok := reqBody["cache_enabled"].(bool); ok {
		req.CacheEnabled = cacheEnabled
	}
	if ttl, ok := reqBody["cache_ttl_seconds"].(float64); ok {
		req.CacheTTLSeconds = int(ttl)
	}
	if pattern, ok := reqBody["cache_key_pattern"].(string); ok {
		req.CacheKeyPattern = pattern
	}
	if timeout, ok := reqBody["timeout_seconds"].(float64); ok {
		req.TimeoutSeconds = int(timeout)
	}
	if retries, ok := reqBody["retry_attempts"].(float64); ok {
		req.RetryAttempts = int(retries)
	}

	// Validate request
	if req.Name == "" || req.PathPattern == "" {
		h.respondError(w, http.StatusBadRequest, "Name and path pattern are required")
		return
	}

	if req.OriginID == uuid.Nil {
		h.respondError(w, http.StatusBadRequest, "Origin ID is required")
		return
	}

	route, err := h.service.Route.CreateRoute(r.Context(), &req)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to create route")
		h.respondError(w, http.StatusInternalServerError, "Failed to create route")
		return
	}

	h.respondJSON(w, http.StatusCreated, route)
}

func (h *Handlers) GetRoute(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid route ID")
		return
	}

	route, err := h.service.Route.GetRoute(r.Context(), id)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "Route not found")
		return
	}

	h.respondJSON(w, http.StatusOK, route)
}

func (h *Handlers) ListRoutes(w http.ResponseWriter, r *http.Request) {
	tenantIDStr := chi.URLParam(r, "tenant_id")

	// Resolve tenant ID (UUID or Clerk ID)
	tenantID, err := h.resolveTenantID(r.Context(), tenantIDStr)
	if err != nil {
		// If tenant doesn't exist, return empty array
		h.respondJSON(w, http.StatusOK, []interface{}{})
		return
	}

	routes, err := h.service.Route.ListByTenant(r.Context(), tenantID)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to list routes")
		h.respondError(w, http.StatusInternalServerError, "Failed to list routes")
		return
	}

	h.respondJSON(w, http.StatusOK, routes)
}

func (h *Handlers) UpdateRoute(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid route ID")
		return
	}

	var req service.UpdateRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	route, err := h.service.Route.UpdateRoute(r.Context(), id, &req)
	if err != nil {
		h.logger.Error().Err(err).Str("id", id.String()).Msg("Failed to update route")
		h.respondError(w, http.StatusInternalServerError, "Failed to update route")
		return
	}

	h.respondJSON(w, http.StatusOK, route)
}

func (h *Handlers) DeleteRoute(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid route ID")
		return
	}

	if err := h.service.Route.DeleteRoute(r.Context(), id); err != nil {
		h.logger.Error().Err(err).Str("id", id.String()).Msg("Failed to delete route")
		h.respondError(w, http.StatusInternalServerError, "Failed to delete route")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var reqBody map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Get tenant ID from request body or query parameter
	tenantIDStr := ""
	if tid, ok := reqBody["tenant_id"].(string); ok {
		tenantIDStr = tid
	}
	if tenantIDStr == "" {
		tenantIDStr = r.URL.Query().Get("tenant_id")
	}

	if tenantIDStr == "" {
		h.respondError(w, http.StatusBadRequest, "Tenant ID is required")
		return
	}

	// Resolve tenant ID (UUID or Clerk ID)
	tenantID, err := h.resolveTenantID(r.Context(), tenantIDStr)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to resolve tenant ID")
		h.respondError(w, http.StatusInternalServerError, "Failed to resolve tenant ID")
		return
	}

	// Build the request with proper types
	req := service.CreateAPIKeyRequest{
		TenantID: tenantID,
		Name:     reqBody["name"].(string),
		Scopes:   []string{},
	}

	if scopes, ok := reqBody["scopes"].([]interface{}); ok {
		for _, scope := range scopes {
			if s, ok := scope.(string); ok {
				req.Scopes = append(req.Scopes, s)
			}
		}
	}

	if expiresAt, ok := reqBody["expires_at"].(string); ok && expiresAt != "" {
		req.ExpiresAt = &expiresAt
	}

	// Validate request
	if req.Name == "" {
		h.respondError(w, http.StatusBadRequest, "API key name is required")
		return
	}

	if len(req.Scopes) == 0 {
		req.Scopes = []string{"read"} // Default scope
	}

	apiKey, keyString, err := h.service.APIKey.CreateAPIKey(r.Context(), &req)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to create API key")
		h.respondError(w, http.StatusInternalServerError, "Failed to create API key")
		return
	}

	// Return the key only once (will be hashed in database)
	response := map[string]interface{}{
		"id":         apiKey.ID,
		"name":       apiKey.Name,
		"key":        keyString,
		"scopes":     apiKey.Scopes,
		"is_active":  apiKey.IsActive,
		"created_at": apiKey.CreatedAt,
	}

	h.respondJSON(w, http.StatusCreated, response)
}

func (h *Handlers) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	tenantIDStr := chi.URLParam(r, "tenant_id")

	// Resolve tenant ID (UUID or Clerk ID)
	tenantID, err := h.resolveTenantID(r.Context(), tenantIDStr)
	if err != nil {
		// If tenant doesn't exist, return empty array
		h.respondJSON(w, http.StatusOK, []interface{}{})
		return
	}

	keys, err := h.service.APIKey.ListByTenant(r.Context(), tenantID)
	if err != nil {
		h.logger.Error().Err(err).Str("tenant_id", tenantID.String()).Msg("Failed to list API keys")
		h.respondError(w, http.StatusInternalServerError, "Failed to list API keys")
		return
	}

	h.respondJSON(w, http.StatusOK, keys)
}

func (h *Handlers) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid API key ID")
		return
	}

	if err := h.service.APIKey.DeleteAPIKey(r.Context(), id); err != nil {
		h.logger.Error().Err(err).Str("id", id.String()).Msg("Failed to delete API key")
		h.respondError(w, http.StatusInternalServerError, "Failed to delete API key")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *Handlers) respondError(w http.ResponseWriter, status int, message string) {
	h.respondJSON(w, status, map[string]string{"error": message})
}
