package ratelimit

import (
	"context"
	"testing"
)

func TestMemoryLimiter_AllowsUpToBurstThenBlocks(t *testing.T) {
	l := NewMemoryLimiter()
	ctx := context.Background()

	// A very low refill rate means, within this test's timeframe, only the
	// initial burst capacity is available.
	const rps = 0.001
	const burst = 3

	for i := 0; i < burst; i++ {
		result, err := l.Allow(ctx, "tenant:route:user", rps, burst)
		if err != nil {
			t.Fatalf("Allow() error: %v", err)
		}
		if !result.Allowed {
			t.Fatalf("request %d: expected allowed, got denied", i)
		}
	}

	result, err := l.Allow(ctx, "tenant:route:user", rps, burst)
	if err != nil {
		t.Fatalf("Allow() error: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected request beyond burst capacity to be denied")
	}
	if result.RetryAfter <= 0 {
		t.Error("expected a positive RetryAfter when denied")
	}
}

func TestMemoryLimiter_KeysAreIndependent(t *testing.T) {
	l := NewMemoryLimiter()
	ctx := context.Background()

	// Exhaust one key's bucket.
	if _, err := l.Allow(ctx, "tenant-a:route", 0.001, 1); err != nil {
		t.Fatalf("Allow() error: %v", err)
	}
	deniedForA, err := l.Allow(ctx, "tenant-a:route", 0.001, 1)
	if err != nil {
		t.Fatalf("Allow() error: %v", err)
	}
	if deniedForA.Allowed {
		t.Fatal("expected tenant-a's bucket to be exhausted")
	}

	// A different key must not be affected.
	allowedForB, err := l.Allow(ctx, "tenant-b:route", 0.001, 1)
	if err != nil {
		t.Fatalf("Allow() error: %v", err)
	}
	if !allowedForB.Allowed {
		t.Fatal("expected tenant-b's independent bucket to allow its first request")
	}
}
