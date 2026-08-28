package handlers

import (
	"net/http"
	"time"
)

// analyticsWindows is the closed set of look-back windows the endpoint
// accepts. Keeping it a fixed map (rather than parsing an arbitrary
// duration) bounds how much request_logs history a single query can scan.
var analyticsWindows = map[string]time.Duration{
	"1h":  time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

// GetAnalytics returns a rollup of the authenticated tenant's gateway
// traffic (from request_logs) over ?window= (default 24h).
func (h *Handlers) GetAnalytics(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantID(w, r)
	if !ok {
		return
	}

	windowParam := r.URL.Query().Get("window")
	if windowParam == "" {
		windowParam = "24h"
	}
	window, ok := analyticsWindows[windowParam]
	if !ok {
		h.respondError(w, http.StatusBadRequest, "window must be one of: 1h, 24h, 7d, 30d")
		return
	}

	data, err := h.service.Analytics.GetAnalytics(r.Context(), tenantID, window)
	if err != nil {
		h.logger.Error().Err(err).Str("tenant_id", tenantID.String()).Msg("Failed to compute analytics")
		h.respondError(w, http.StatusInternalServerError, "Failed to compute analytics")
		return
	}
	data.Window = windowParam
	h.respondJSON(w, http.StatusOK, data)
}
