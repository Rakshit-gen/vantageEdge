package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	cpmiddleware "github.com/vantageedge/backend/internal/controlplane/middleware"
	"github.com/vantageedge/backend/internal/controlplane/service"
	"github.com/vantageedge/backend/pkg/logger"
)

type Handlers struct {
	service *service.Service
	logger  *logger.Logger
}

func New(svc *service.Service, log *logger.Logger) *Handlers {
	return &Handlers{
		service: svc,
		logger:  log,
	}
}

// RegisterRoutes mounts every control-plane resource under r. All routes
// require a verified Clerk JWT (applied by the caller via
// cpmiddleware.RequireAuth) and are scoped to the authenticated caller's
// own tenant — see package cpmiddleware's doc comment for why: previously
// every one of these endpoints trusted a client-supplied tenant_id, so any
// caller could read or mutate any other tenant's data.
func (h *Handlers) RegisterRoutes(r chi.Router) {
	r.Get("/tenants/me", h.GetMyTenant)
	r.Put("/tenants/me", h.UpdateMyTenant)
	r.Delete("/tenants/me", h.DeleteMyTenant)

	r.Get("/analytics", h.GetAnalytics)

	r.Route("/origins", func(r chi.Router) {
		r.Post("/", h.CreateOrigin)
		r.Get("/", h.ListOrigins)
		r.Get("/{id}", h.GetOrigin)
		r.Put("/{id}", h.UpdateOrigin)
		r.Patch("/{id}", h.UpdateOrigin)
		r.Delete("/{id}", h.DeleteOrigin)
	})

	r.Route("/routes", func(r chi.Router) {
		r.Post("/", h.CreateRoute)
		r.Get("/", h.ListRoutes)
		r.Get("/{id}", h.GetRoute)
		r.Put("/{id}", h.UpdateRoute)
		r.Patch("/{id}", h.UpdateRoute)
		r.Delete("/{id}", h.DeleteRoute)

		r.Get("/{id}/origins", h.ListRouteOrigins)
		r.Post("/{id}/origins/{origin_id}", h.AddRouteOrigin)
		r.Delete("/{id}/origins/{origin_id}", h.RemoveRouteOrigin)
	})

	r.Route("/api-keys", func(r chi.Router) {
		r.Post("/", h.CreateAPIKey)
		r.Get("/", h.ListAPIKeys)
		r.Delete("/{id}", h.DeleteAPIKey)
	})
}

// tenantID reads the authenticated caller's tenant from context. It should
// never miss when RequireAuth is applied ahead of these handlers, but a
// clear 401 beats a nil-UUID query if that wiring is ever wrong.
func (h *Handlers) tenantID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, ok := cpmiddleware.TenantIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "authentication required")
		return uuid.Nil, false
	}
	return id, true
}

// --- Tenant ---

func (h *Handlers) GetMyTenant(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantID(w, r)
	if !ok {
		return
	}
	tenant, err := h.service.Tenant.GetTenant(r.Context(), tenantID)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "Tenant not found")
		return
	}
	h.respondJSON(w, http.StatusOK, tenant)
}

func (h *Handlers) UpdateMyTenant(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantID(w, r)
	if !ok {
		return
	}
	var req service.UpdateTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	tenant, err := h.service.Tenant.UpdateTenant(r.Context(), tenantID, &req)
	if err != nil {
		h.logger.Error().Err(err).Str("tenant_id", tenantID.String()).Msg("Failed to update tenant")
		h.respondError(w, http.StatusInternalServerError, "Failed to update tenant")
		return
	}
	h.respondJSON(w, http.StatusOK, tenant)
}

func (h *Handlers) DeleteMyTenant(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantID(w, r)
	if !ok {
		return
	}
	if err := h.service.Tenant.DeleteTenant(r.Context(), tenantID); err != nil {
		h.logger.Error().Err(err).Str("tenant_id", tenantID.String()).Msg("Failed to delete tenant")
		h.respondError(w, http.StatusInternalServerError, "Failed to delete tenant")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Origins ---

func (h *Handlers) CreateOrigin(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantID(w, r)
	if !ok {
		return
	}

	var req service.CreateOriginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	req.TenantID = tenantID // never trust a client-supplied tenant_id

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
	tenantID, ok := h.tenantID(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid origin ID")
		return
	}

	origin, err := h.service.Origin.GetOrigin(r.Context(), id)
	if err != nil || origin.TenantID != tenantID {
		// 404, not 403: don't confirm to the caller that an origin with
		// this ID exists under some other tenant.
		h.respondError(w, http.StatusNotFound, "Origin not found")
		return
	}
	h.respondJSON(w, http.StatusOK, origin)
}

func (h *Handlers) ListOrigins(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantID(w, r)
	if !ok {
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
	tenantID, ok := h.tenantID(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid origin ID")
		return
	}

	existing, err := h.service.Origin.GetOrigin(r.Context(), id)
	if err != nil || existing.TenantID != tenantID {
		h.respondError(w, http.StatusNotFound, "Origin not found")
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
	tenantID, ok := h.tenantID(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid origin ID")
		return
	}

	existing, err := h.service.Origin.GetOrigin(r.Context(), id)
	if err != nil || existing.TenantID != tenantID {
		h.respondError(w, http.StatusNotFound, "Origin not found")
		return
	}

	if err := h.service.Origin.DeleteOrigin(r.Context(), id); err != nil {
		h.logger.Error().Err(err).Str("id", id.String()).Msg("Failed to delete origin")
		h.respondError(w, http.StatusInternalServerError, "Failed to delete origin")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Routes ---

func (h *Handlers) CreateRoute(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantID(w, r)
	if !ok {
		return
	}

	var req service.CreateRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	req.TenantID = tenantID

	if req.Name == "" || req.PathPattern == "" {
		h.respondError(w, http.StatusBadRequest, "Name and path pattern are required")
		return
	}
	if req.OriginID == uuid.Nil {
		h.respondError(w, http.StatusBadRequest, "Origin ID is required")
		return
	}

	// The origin must belong to the same tenant, or a caller could point
	// their route at another tenant's origin and proxy traffic through it.
	origin, err := h.service.Origin.GetOrigin(r.Context(), req.OriginID)
	if err != nil || origin.TenantID != tenantID {
		h.respondError(w, http.StatusBadRequest, "Origin not found")
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
	tenantID, ok := h.tenantID(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid route ID")
		return
	}

	route, err := h.service.Route.GetRoute(r.Context(), id)
	if err != nil || route.TenantID != tenantID {
		h.respondError(w, http.StatusNotFound, "Route not found")
		return
	}
	h.respondJSON(w, http.StatusOK, route)
}

func (h *Handlers) ListRoutes(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantID(w, r)
	if !ok {
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
	tenantID, ok := h.tenantID(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid route ID")
		return
	}

	existing, err := h.service.Route.GetRoute(r.Context(), id)
	if err != nil || existing.TenantID != tenantID {
		h.respondError(w, http.StatusNotFound, "Route not found")
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
	tenantID, ok := h.tenantID(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid route ID")
		return
	}

	existing, err := h.service.Route.GetRoute(r.Context(), id)
	if err != nil || existing.TenantID != tenantID {
		h.respondError(w, http.StatusNotFound, "Route not found")
		return
	}

	if err := h.service.Route.DeleteRoute(r.Context(), id); err != nil {
		h.logger.Error().Err(err).Str("id", id.String()).Msg("Failed to delete route")
		h.respondError(w, http.StatusInternalServerError, "Failed to delete route")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Route origin pools ---

func (h *Handlers) ListRouteOrigins(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantID(w, r)
	if !ok {
		return
	}
	routeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid route ID")
		return
	}

	route, err := h.service.Route.GetRoute(r.Context(), routeID)
	if err != nil || route.TenantID != tenantID {
		h.respondError(w, http.StatusNotFound, "Route not found")
		return
	}

	origins, err := h.service.Route.ListRouteOrigins(r.Context(), routeID)
	if err != nil {
		h.logger.Error().Err(err).Str("route_id", routeID.String()).Msg("Failed to list route origins")
		h.respondError(w, http.StatusInternalServerError, "Failed to list route origins")
		return
	}
	h.respondJSON(w, http.StatusOK, origins)
}

func (h *Handlers) AddRouteOrigin(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantID(w, r)
	if !ok {
		return
	}
	routeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid route ID")
		return
	}
	originID, err := uuid.Parse(chi.URLParam(r, "origin_id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid origin ID")
		return
	}

	route, err := h.service.Route.GetRoute(r.Context(), routeID)
	if err != nil || route.TenantID != tenantID {
		h.respondError(w, http.StatusNotFound, "Route not found")
		return
	}
	// The origin must belong to the same tenant as the route, or a caller
	// could point their route's pool at another tenant's origin.
	origin, err := h.service.Origin.GetOrigin(r.Context(), originID)
	if err != nil || origin.TenantID != tenantID {
		h.respondError(w, http.StatusBadRequest, "Origin not found")
		return
	}

	if err := h.service.Route.AddRouteOrigin(r.Context(), routeID, originID); err != nil {
		h.logger.Error().Err(err).Str("route_id", routeID.String()).Str("origin_id", originID.String()).Msg("Failed to add origin to route pool")
		h.respondError(w, http.StatusInternalServerError, "Failed to add origin to route pool")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) RemoveRouteOrigin(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantID(w, r)
	if !ok {
		return
	}
	routeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid route ID")
		return
	}
	originID, err := uuid.Parse(chi.URLParam(r, "origin_id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid origin ID")
		return
	}

	route, err := h.service.Route.GetRoute(r.Context(), routeID)
	if err != nil || route.TenantID != tenantID {
		h.respondError(w, http.StatusNotFound, "Route not found")
		return
	}

	if err := h.service.Route.RemoveRouteOrigin(r.Context(), routeID, originID); err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- API Keys ---

func (h *Handlers) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantID(w, r)
	if !ok {
		return
	}

	var req service.CreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	req.TenantID = tenantID

	if req.Name == "" {
		h.respondError(w, http.StatusBadRequest, "API key name is required")
		return
	}
	if len(req.Scopes) == 0 {
		req.Scopes = []string{"read"}
	}

	apiKey, keyString, err := h.service.APIKey.CreateAPIKey(r.Context(), &req)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to create API key")
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// The plaintext key is returned exactly once; only its hash is
	// persisted, so the caller must capture it now.
	response := map[string]interface{}{
		"id":         apiKey.ID,
		"name":       apiKey.Name,
		"key":        keyString,
		"scopes":     apiKey.Scopes,
		"expires_at": apiKey.ExpiresAt,
		"is_active":  apiKey.IsActive,
		"created_at": apiKey.CreatedAt,
	}
	h.respondJSON(w, http.StatusCreated, response)
}

func (h *Handlers) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantID(w, r)
	if !ok {
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
	tenantID, ok := h.tenantID(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid API key ID")
		return
	}

	existing, err := h.service.APIKey.GetAPIKey(r.Context(), id)
	if err != nil || existing.TenantID != tenantID {
		h.respondError(w, http.StatusNotFound, "API key not found")
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
