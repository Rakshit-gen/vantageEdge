package jwt

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// testJWKS spins up an httptest server serving a JWKS for one RSA key pair
// and returns the private key plus the server, so tests can sign tokens
// that verify against it and confirm forged tokens are rejected.
func testJWKS(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	jwk := map[string]interface{}{
		"kty": "RSA",
		"use": "sig",
		"alg": "RS256",
		"kid": "test-key-1",
		"n":   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
	}
	jwks := map[string]interface{}{"keys": []interface{}{jwk}}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(server.Close)

	return key, server.URL
}

func signToken(t *testing.T, key *rsa.PrivateKey, claims Claims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-key-1"
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return signed
}

func TestValidateToken_AcceptsGenuineToken(t *testing.T) {
	key, jwksURL := testJWKS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	validator, err := NewJWTValidator(ctx, jwksURL, "", "")
	if err != nil {
		t.Fatalf("NewJWTValidator failed: %v", err)
	}

	claims := Claims{
		ClerkUserID: "user_123",
		Email:       "demo@example.com",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	tokenString := signToken(t, key, claims)

	got, err := validator.ValidateToken("Bearer " + tokenString)
	if err != nil {
		t.Fatalf("expected valid token to verify, got error: %v", err)
	}
	if got.ClerkUserID != "user_123" {
		t.Errorf("ClerkUserID = %q, want %q", got.ClerkUserID, "user_123")
	}
}

// TestValidateToken_RejectsForgedSignature is the regression test for the
// core vulnerability this package fixed: the previous implementation used
// jwt.ParseUnverified, which never checks the signature at all, so any
// caller could forge a token claiming to be any user by signing with an
// arbitrary key (or no verification step at all). This confirms a token
// signed by a DIFFERENT key than the one published in the JWKS is rejected.
func TestValidateToken_RejectsForgedSignature(t *testing.T) {
	_, jwksURL := testJWKS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	validator, err := NewJWTValidator(ctx, jwksURL, "", "")
	if err != nil {
		t.Fatalf("NewJWTValidator failed: %v", err)
	}

	forgedKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate forged key: %v", err)
	}
	claims := Claims{
		ClerkUserID: "attacker_pretending_to_be_admin",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	forgedToken := signToken(t, forgedKey, claims)

	_, err = validator.ValidateToken("Bearer " + forgedToken)
	if err == nil {
		t.Fatal("expected forged token to be rejected, but it verified successfully")
	}
}

func TestValidateToken_RejectsExpiredToken(t *testing.T) {
	key, jwksURL := testJWKS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	validator, err := NewJWTValidator(ctx, jwksURL, "", "")
	if err != nil {
		t.Fatalf("NewJWTValidator failed: %v", err)
	}

	claims := Claims{
		ClerkUserID: "user_123",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	tokenString := signToken(t, key, claims)

	_, err = validator.ValidateToken(tokenString)
	if err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestValidateToken_EnforcesIssuerAndAudience(t *testing.T) {
	key, jwksURL := testJWKS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	validator, err := NewJWTValidator(ctx, jwksURL, "https://expected-issuer.example.com", "expected-audience")
	if err != nil {
		t.Fatalf("NewJWTValidator failed: %v", err)
	}

	claims := Claims{
		ClerkUserID: "user_123",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			Issuer:    "https://wrong-issuer.example.com",
		},
	}
	tokenString := signToken(t, key, claims)

	if _, err := validator.ValidateToken(tokenString); err == nil {
		t.Fatal("expected token with wrong issuer to be rejected")
	}
}

func TestValidateToken_RejectsEmptyToken(t *testing.T) {
	_, jwksURL := testJWKS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	validator, err := NewJWTValidator(ctx, jwksURL, "", "")
	if err != nil {
		t.Fatalf("NewJWTValidator failed: %v", err)
	}

	if _, err := validator.ValidateToken(""); err == nil {
		t.Fatal("expected empty token to be rejected")
	}
	if _, err := validator.ValidateToken("Bearer "); err == nil {
		t.Fatal("expected empty bearer token to be rejected")
	}
}

func TestValidateToken_RejectsUnsignedAlgNone(t *testing.T) {
	// A classic JWT bypass: a token with "alg": "none" and no signature at
	// all. jwt.WithValidMethods([]string{"RS256"}) must reject it.
	_, jwksURL := testJWKS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	validator, err := NewJWTValidator(ctx, jwksURL, "", "")
	if err != nil {
		t.Fatalf("NewJWTValidator failed: %v", err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodNone, Claims{
		ClerkUserID: "attacker",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	unsignedToken, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("failed to build alg=none token: %v", err)
	}

	if _, err := validator.ValidateToken(unsignedToken); err == nil {
		t.Fatal("expected alg=none token to be rejected")
	}
}
