package middleware

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/auth/apikey"
	authjwt "github.com/vantageedge/backend/internal/auth/jwt"
	"github.com/vantageedge/backend/internal/models"
)

// fakeUsers is a minimal repository.UserRepository stub: only GetByClerkID
// is exercised by the jwt_required auth path under test.
type fakeUsers struct {
	byClerkID map[string]*models.User
}

func (f *fakeUsers) Create(context.Context, *models.User) error { return errors.New("not implemented") }
func (f *fakeUsers) GetByID(context.Context, uuid.UUID) (*models.User, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeUsers) GetByClerkID(_ context.Context, clerkUserID string) (*models.User, error) {
	u, ok := f.byClerkID[clerkUserID]
	if !ok {
		return nil, errors.New("not found")
	}
	return u, nil
}
func (f *fakeUsers) ListByTenant(context.Context, uuid.UUID) ([]*models.User, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeUsers) Update(context.Context, *models.User) error { return errors.New("not implemented") }
func (f *fakeUsers) Delete(context.Context, uuid.UUID) error    { return errors.New("not implemented") }

// testJWKS mirrors internal/auth/jwt's test helper of the same shape, kept
// package-local so this test needs no DB/Redis to run.
func testJWKS(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	jwk := map[string]interface{}{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "test-key-1",
		"n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"keys": []interface{}{jwk}})
	}))
	t.Cleanup(server.Close)
	return key, server.URL
}

func signToken(t *testing.T, key *rsa.PrivateKey, claims authjwt.Claims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-key-1"
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return signed
}

// TestAuthenticateJWT_RejectsTokenForDifferentTenant is the regression test
// for the bug fixed here: authenticateJWT verified a Clerk-issued token's
// signature but never checked that the Clerk user it belongs to is actually
// a member of the tenant the request was routed to, so any valid Clerk JWT
// could authenticate a jwt_required route on ANY tenant's subdomain.
func TestAuthenticateJWT_RejectsTokenForDifferentTenant(t *testing.T) {
	key, jwksURL := testJWKS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	validator, err := authjwt.NewJWTValidator(ctx, jwksURL, "", "")
	if err != nil {
		t.Fatalf("NewJWTValidator failed: %v", err)
	}

	ownTenant := uuid.New()
	otherTenant := uuid.New()
	users := &fakeUsers{byClerkID: map[string]*models.User{
		"user_123": {ClerkUserID: "user_123", TenantID: ownTenant},
	}}
	a := NewAuthenticator(validator, apikey.NewValidator(nil), users)

	claims := authjwt.Claims{
		ClerkUserID: "user_123",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	tokenString := signToken(t, key, claims)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	route := &models.Route{AuthMode: "jwt_required"}

	if _, err := a.Authenticate(context.Background(), req, route, ownTenant); err != nil {
		t.Fatalf("expected a token to authenticate against its own tenant, got: %v", err)
	}

	if _, err := a.Authenticate(context.Background(), req, route, otherTenant); err == nil {
		t.Fatal("expected a token to be rejected against a different tenant, but it authenticated")
	}
}
