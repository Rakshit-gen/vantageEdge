package middleware

import (
	"context"
	"testing"
	"time"
)

func TestResponseCache_MemoryStore_RoundTrip(t *testing.T) {
	cache := NewResponseCache(NewMemoryStore())
	ctx := context.Background()

	resp := &CachedResponse{
		StatusCode: 200,
		Header:     map[string][]string{"Content-Type": {"application/json"}},
		Body:       []byte(`{"ok":true}`),
	}
	cache.Set(ctx, "key1", resp, time.Minute)

	got, ok := cache.Get(ctx, "key1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.StatusCode != 200 || string(got.Body) != `{"ok":true}` {
		t.Errorf("got unexpected cached response: %+v", got)
	}
}

func TestResponseCache_MemoryStore_Miss(t *testing.T) {
	cache := NewResponseCache(NewMemoryStore())
	if _, ok := cache.Get(context.Background(), "nonexistent"); ok {
		t.Fatal("expected cache miss for unset key")
	}
}

func TestResponseCache_MemoryStore_Expiry(t *testing.T) {
	cache := NewResponseCache(NewMemoryStore())
	ctx := context.Background()

	cache.Set(ctx, "key1", &CachedResponse{StatusCode: 200}, 10*time.Millisecond)
	time.Sleep(30 * time.Millisecond)

	if _, ok := cache.Get(ctx, "key1"); ok {
		t.Fatal("expected expired entry to be a cache miss")
	}
}

// TestBuildCacheKey_DifferentIdentitiesGetDifferentKeys is the regression
// test for the cross-user cache-leak risk: caching a jwt_required route's
// response keyed only on path+query would let one authenticated user's
// response be served to a completely different authenticated user hitting
// the same path. The identity component of the key must make that
// impossible.
func TestBuildCacheKey_DifferentIdentitiesGetDifferentKeys(t *testing.T) {
	keyUserA := BuildCacheKey("tenant1", "route1", "user:alice", "path+query", "/api/me", "")
	keyUserB := BuildCacheKey("tenant1", "route1", "user:bob", "path+query", "/api/me", "")

	if keyUserA == keyUserB {
		t.Fatalf("expected different identities to produce different cache keys, both got %q", keyUserA)
	}
}

func TestBuildCacheKey_PublicRoutesShareKey(t *testing.T) {
	// Public routes intentionally share one cache entry across callers —
	// this is correct: unauthenticated responses aren't caller-specific.
	keyA := BuildCacheKey("tenant1", "route1", "public", "path+query", "/api/status", "")
	keyB := BuildCacheKey("tenant1", "route1", "public", "path+query", "/api/status", "")

	if keyA != keyB {
		t.Fatalf("expected identical public requests to share a cache key")
	}
}

func TestBuildCacheKey_DifferentTenantsIsolated(t *testing.T) {
	keyTenantA := BuildCacheKey("tenant-a", "route1", "public", "path+query", "/api/status", "")
	keyTenantB := BuildCacheKey("tenant-b", "route1", "public", "path+query", "/api/status", "")

	if keyTenantA == keyTenantB {
		t.Fatal("expected different tenants to never share a cache key")
	}
}
