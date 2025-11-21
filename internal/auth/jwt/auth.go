package jwt

import (
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	ClerkUserID string
	TenantID    string
	Email       string
	Role        string
	jwt.RegisteredClaims
}

type JWTValidator struct {
	// No secret needed for Clerk JWT validation - uses public key
}

func NewJWTValidator() *JWTValidator {
	return &JWTValidator{}
}

// ValidateToken validates a JWT token from Clerk
// For production, this should verify the signature using Clerk's public key
func (v *JWTValidator) ValidateToken(tokenString string) (*Claims, error) {
	if tokenString == "" {
		return nil, fmt.Errorf("token is empty")
	}

	// Remove "Bearer " prefix if present
	tokenString = strings.TrimPrefix(tokenString, "Bearer ")

	// Parse token without verification for now
	// In production, use Clerk SDK to verify signatures
	token, _, err := new(jwt.Parser).ParseUnverified(tokenString, &Claims{})
	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil
}

// ExtractClerkUserID extracts the Clerk user ID from a token string
func ExtractClerkUserID(tokenString string) (string, error) {
	validator := NewJWTValidator()
	claims, err := validator.ValidateToken(tokenString)
	if err != nil {
		return "", err
	}

	if claims.ClerkUserID == "" {
		return "", fmt.Errorf("clerk_user_id not found in token")
	}

	return claims.ClerkUserID, nil
}
