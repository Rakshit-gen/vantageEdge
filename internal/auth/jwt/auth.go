package jwt

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

// Claims mirrors the subset of a Clerk session token's claims this backend
// relies on. Field names follow Clerk's default session token shape.
type Claims struct {
	ClerkUserID string `json:"sub"`
	TenantID    string `json:"tenant_id,omitempty"`
	Email       string `json:"email,omitempty"`
	Role        string `json:"role,omitempty"`
	jwt.RegisteredClaims
}

// JWTValidator verifies Clerk-issued JWTs against Clerk's published JWKS.
// It must be constructed once (via NewJWTValidator) and reused: the
// underlying keyfunc.Keyfunc keeps a background refresh goroutine and an
// in-memory key cache, so recreating it per-request would hammer the JWKS
// endpoint and defeat caching.
type JWTValidator struct {
	keyfunc  keyfunc.Keyfunc
	issuer   string
	audience string
}

// NewJWTValidator builds a validator that fetches and caches Clerk's JWKS
// from jwksURL. issuer/audience are optional; when non-empty they are
// enforced against the token's "iss"/"aud" claims.
func NewJWTValidator(ctx context.Context, jwksURL, issuer, audience string) (*JWTValidator, error) {
	if jwksURL == "" {
		return nil, fmt.Errorf("jwks URL is empty")
	}

	kf, err := keyfunc.NewDefaultCtx(ctx, []string{jwksURL})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize JWKS from %s: %w", jwksURL, err)
	}

	return &JWTValidator{
		keyfunc:  kf,
		issuer:   issuer,
		audience: audience,
	}, nil
}

// ValidateToken verifies a JWT's signature against Clerk's JWKS and checks
// standard time-based and (if configured) issuer/audience claims. A token
// that merely parses is not sufficient — this is the only place in the
// request path that proves the token was actually issued by Clerk rather
// than forged by the caller.
func (v *JWTValidator) ValidateToken(tokenString string) (*Claims, error) {
	if tokenString == "" {
		return nil, fmt.Errorf("token is empty")
	}

	tokenString = strings.TrimPrefix(tokenString, "Bearer ")
	if tokenString == "" {
		return nil, fmt.Errorf("token is empty")
	}

	parserOpts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithLeeway(30 * time.Second),
	}
	if v.issuer != "" {
		parserOpts = append(parserOpts, jwt.WithIssuer(v.issuer))
	}
	if v.audience != "" {
		parserOpts = append(parserOpts, jwt.WithAudience(v.audience))
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, v.keyfunc.Keyfunc, parserOpts...)
	if err != nil {
		return nil, fmt.Errorf("token verification failed: %w", err)
	}
	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	if claims.ClerkUserID == "" {
		return nil, fmt.Errorf("clerk_user_id not found in token")
	}

	return claims, nil
}

// ExtractClerkUserID validates the token and extracts the Clerk user ID.
// Kept for callers that only need the subject; it does not skip
// verification.
func (v *JWTValidator) ExtractClerkUserID(tokenString string) (string, error) {
	claims, err := v.ValidateToken(tokenString)
	if err != nil {
		return "", err
	}
	return claims.ClerkUserID, nil
}
