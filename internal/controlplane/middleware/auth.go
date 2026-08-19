// Package middleware provides control-plane HTTP middleware.
//
// Previously the control-plane API had no authentication at all: every
// tenant/origin/route/api-key endpoint accepted unauthenticated requests,
// and handlers trusted a client-supplied tenant_id in the request body to
// decide which tenant's data to read or write — so any caller could create,
// list, or delete any other tenant's resources by simply naming that
// tenant's ID. RequireAuth closes both holes: it requires a verified Clerk
// JWT, resolves the caller's own tenant (auto-provisioning it on first
// login, same as the pre-existing resolveTenantID convenience), and
// injects it into the request context so handlers scope every operation to
// the authenticated caller's tenant instead of whatever the client claims.
package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	authjwt "github.com/vantageedge/backend/internal/auth/jwt"
	"github.com/vantageedge/backend/internal/controlplane/service"
	"github.com/vantageedge/backend/pkg/logger"
)

type contextKey string

const (
	TenantIDKey    contextKey = "tenant_id"
	UserIDKey      contextKey = "user_id"
	ClerkUserIDKey contextKey = "clerk_user_id"
	RoleKey        contextKey = "role"
)

// RequireAuth validates the caller's Clerk JWT and attaches their tenant
// and user identity to the request context. It provisions a tenant/user
// record on first sight of a Clerk identity, mirroring the auto-create
// behavior handlers already relied on for client-supplied tenant IDs.
func RequireAuth(validator *authjwt.JWTValidator, authSvc service.AuthService, log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				respondUnauthorized(w, "missing bearer token")
				return
			}

			claims, err := validator.ValidateToken(authHeader)
			if err != nil {
				log.Warn().Err(err).Msg("JWT validation failed")
				respondUnauthorized(w, "invalid or expired token")
				return
			}

			user, err := authSvc.SyncUser(r.Context(), &service.SyncUserRequest{
				ClerkUserID: claims.ClerkUserID,
				Email:       claims.Email,
			})
			if err != nil {
				log.Error().Err(err).Str("clerk_user_id", claims.ClerkUserID).Msg("Failed to resolve user/tenant for authenticated request")
				respondUnauthorized(w, "unable to resolve account")
				return
			}

			ctx := context.WithValue(r.Context(), TenantIDKey, user.TenantID)
			ctx = context.WithValue(ctx, UserIDKey, user.ID)
			ctx = context.WithValue(ctx, ClerkUserIDKey, claims.ClerkUserID)
			ctx = context.WithValue(ctx, RoleKey, user.Role)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func respondUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"` + message + `"}`))
}

// TenantIDFromContext returns the authenticated caller's tenant ID. It
// only returns ok=false if RequireAuth was not applied to this route.
func TenantIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(TenantIDKey).(uuid.UUID)
	return id, ok
}

// UserIDFromContext returns the authenticated caller's user ID.
func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(UserIDKey).(uuid.UUID)
	return id, ok
}
