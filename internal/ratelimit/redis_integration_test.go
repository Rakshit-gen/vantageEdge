//go:build integration

// Run locally with:
//
//	docker compose up -d redis
//	REDIS_HOST=localhost go test -tags=integration ./internal/ratelimit/...
package ratelimit

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	redispkg "github.com/vantageedge/backend/internal/cache/redis"
)

func testRedisClient(t *testing.T) *redispkg.Client {
	t.Helper()

	host := os.Getenv("REDIS_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("REDIS_PORT")
	if port == "" {
		port = "6379"
	}

	client, err := redispkg.NewClient(fmt.Sprintf("redis://%s:%s/0", host, port))
	if err != nil {
		t.Fatalf("failed to connect to test Redis: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// TestIntegration_RedisLimiter_AtomicAcrossConcurrentCallers is the
// regression test for exactly what MemoryLimiter can't provide: multiple
// concurrent callers sharing one counter, which is the entire reason
// RedisLimiter exists (a per-gateway-replica in-memory bucket lets a
// tenant limited to N rps actually get N * replica-count rps). This drives
// far more concurrent requests than the burst capacity and asserts the
// number of *allowed* requests never exceeds it, proving the Lua script's
// GET-then-SET is atomic under real concurrency, not just correct in
// isolation.
func TestIntegration_RedisLimiter_AtomicAcrossConcurrentCallers(t *testing.T) {
	client := testRedisClient(t)
	limiter := NewRedisLimiter(client)
	ctx := context.Background()

	key := "integration-test:" + uuid.NewString()
	const burst = 10
	const rps = 0.001 // effectively no refill within this test's runtime
	const concurrency = 50

	results := make(chan bool, concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			res, err := limiter.Allow(ctx, key, rps, burst)
			if err != nil {
				results <- false
				return
			}
			results <- res.Allowed
		}()
	}

	allowed := 0
	for i := 0; i < concurrency; i++ {
		if <-results {
			allowed++
		}
	}

	if allowed != burst {
		t.Errorf("expected exactly %d of %d concurrent requests to be allowed (burst capacity), got %d", burst, concurrency, allowed)
	}
}

func TestIntegration_RedisLimiter_DifferentKeysIndependent(t *testing.T) {
	client := testRedisClient(t)
	limiter := NewRedisLimiter(client)
	ctx := context.Background()

	keyA := "integration-test:" + uuid.NewString()
	keyB := "integration-test:" + uuid.NewString()

	if _, err := limiter.Allow(ctx, keyA, 0.001, 1); err != nil {
		t.Fatalf("Allow keyA: %v", err)
	}
	resultA, err := limiter.Allow(ctx, keyA, 0.001, 1)
	if err != nil {
		t.Fatalf("Allow keyA (2nd): %v", err)
	}
	if resultA.Allowed {
		t.Fatal("expected keyA's single-token bucket to be exhausted")
	}

	resultB, err := limiter.Allow(ctx, keyB, 0.001, 1)
	if err != nil {
		t.Fatalf("Allow keyB: %v", err)
	}
	if !resultB.Allowed {
		t.Fatal("expected keyB's independent bucket to allow its first request")
	}
}
