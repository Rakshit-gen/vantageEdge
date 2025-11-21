package tokenbucket

import (
	"sync"
	"time"
)

type TokenBucket struct {
	mu              sync.Mutex
	capacity        float64
	tokensAvailable float64
	refillRate      float64 // tokens per second
	lastRefillTime  time.Time
}

func NewTokenBucket(capacity, refillRate float64) *TokenBucket {
	return &TokenBucket{
		capacity:       capacity,
		tokensAvailable: capacity,
		refillRate:     refillRate,
		lastRefillTime: time.Now(),
	}
}

// Allow checks if a request is allowed based on token availability
func (tb *TokenBucket) Allow(tokens float64) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()

	if tb.tokensAvailable >= tokens {
		tb.tokensAvailable -= tokens
		return true
	}

	return false
}

// AllowN allows n requests at once
func (tb *TokenBucket) AllowN(n int) bool {
	return tb.Allow(float64(n))
}

// refill adds tokens based on elapsed time
func (tb *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefillTime).Seconds()
	tb.lastRefillTime = now

	tokensToAdd := elapsed * tb.refillRate
	tb.tokensAvailable = min(tb.capacity, tb.tokensAvailable+tokensToAdd)
}

// GetAvailableTokens returns the current number of available tokens
func (tb *TokenBucket) GetAvailableTokens() float64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()
	return tb.tokensAvailable
}

// Reset resets the bucket to full capacity
func (tb *TokenBucket) Reset() {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.tokensAvailable = tb.capacity
	tb.lastRefillTime = time.Now()
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
