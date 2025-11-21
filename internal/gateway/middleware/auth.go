package middleware

import (
	"context"
	"net/http"
	"strings"
)

type contextKey string

const (
	TenantIDKey contextKey = "tenant_id"
	UserIDKey   contextKey = "user_id"
)

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract auth header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Validate token (placeholder)
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == "" {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		// TODO: Validate JWT or API key
		// TODO: Extract user and tenant ID
		
		ctx := context.WithValue(r.Context(), UserIDKey, "user_123")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
