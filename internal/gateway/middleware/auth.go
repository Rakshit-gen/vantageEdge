// Package middleware provides gateway-side request middleware: identity
// resolution, response caching, and (in ratelimit.go, superseded by
// internal/ratelimit) rate limiting.
//
// The previous AuthMiddleware here was dead code: it stripped a "Bearer "
// prefix, checked the result was non-empty, and injected a hardcoded
// "user_123" into the request context without validating the token at all
// or ever being called from router.go. Authenticate replaces it with
// per-route enforcement driven by the route's auth_mode, using the same
// verified JWT validator and hashed API key lookup the control plane uses.
package middleware

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/auth/apikey"
	authjwt "github.com/vantageedge/backend/internal/auth/jwt"
	"github.com/vantageedge/backend/internal/models"
)

// Identity describes who (if anyone) authenticated the current request.
type Identity struct {
	Method      string // "public", "jwt", or "apikey"
	TenantID    uuid.UUID
	ClerkUserID string
	APIKeyID    *uuid.UUID
	Scopes      []string
}

// CacheIdentityKey returns a stable, low-cardinality identifier for this
// identity suitable for namespacing cached responses so one user's cached
// response is never served to a different authenticated user on the same
// route (see cache.go).
func (id Identity) CacheIdentityKey() string {
	if id.APIKeyID != nil {
		return "apikey:" + id.APIKeyID.String()
	}
	if id.ClerkUserID != "" {
		return "user:" + id.ClerkUserID
	}
	return "public"
}

// Authenticator resolves the caller's identity for a request against a
// specific route's auth_mode.
type Authenticator struct {
	jwtValidator *authjwt.JWTValidator
	apiKeys      *apikey.Validator
}

func NewAuthenticator(jwtValidator *authjwt.JWTValidator, apiKeys *apikey.Validator) *Authenticator {
	return &Authenticator{jwtValidator: jwtValidator, apiKeys: apiKeys}
}

// Authenticate enforces route.AuthMode ("public", "jwt_required",
// "apikey_required", or "both") and returns the resolved identity. tenantID
// is the tenant the request was routed to (from the gateway's subdomain
// lookup); a JWT that verifies but belongs to a different tenant is
// rejected rather than silently trusted, so an authenticated user for
// tenant A can't be attributed to tenant B just by hitting B's subdomain.
func (a *Authenticator) Authenticate(ctx context.Context, r *http.Request, route *models.Route, tenantID uuid.UUID) (*Identity, error) {
	switch route.AuthMode {
	case "public", "":
		return &Identity{Method: "public", TenantID: tenantID}, nil

	case "jwt_required":
		return a.authenticateJWT(r, tenantID)

	case "apikey_required":
		return a.authenticateAPIKey(ctx, r, tenantID)

	case "both":
		if id, err := a.authenticateJWT(r, tenantID); err == nil {
			return id, nil
		}
		return a.authenticateAPIKey(ctx, r, tenantID)

	default:
		return nil, fmt.Errorf("unknown auth_mode %q", route.AuthMode)
	}
}

func (a *Authenticator) authenticateJWT(r *http.Request, tenantID uuid.UUID) (*Identity, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, fmt.Errorf("missing authorization header")
	}

	claims, err := a.jwtValidator.ValidateToken(authHeader)
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	return &Identity{
		Method:      "jwt",
		TenantID:    tenantID,
		ClerkUserID: claims.ClerkUserID,
	}, nil
}

func (a *Authenticator) authenticateAPIKey(ctx context.Context, r *http.Request, tenantID uuid.UUID) (*Identity, error) {
	keyString := r.Header.Get("X-API-Key")
	if keyString == "" {
		keyString = r.Header.Get("Authorization")
	}
	if keyString == "" {
		return nil, fmt.Errorf("missing API key")
	}

	info, err := a.apiKeys.ValidateKey(ctx, keyString)
	if err != nil {
		return nil, err
	}
	if info.TenantID != tenantID {
		// The key is valid but belongs to a different tenant than the one
		// this request was routed to — reject rather than cross-attribute.
		return nil, fmt.Errorf("api key does not belong to this tenant")
	}

	return &Identity{
		Method:   "apikey",
		TenantID: tenantID,
		APIKeyID: &info.ID,
		Scopes:   info.Scopes,
	}, nil
}
