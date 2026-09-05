// Package ratelimit provides the Limiter interface used by the gateway to
// enforce per-route rate limits, plus a Redis-backed distributed
// implementation and an in-memory fallback.
//
// The route-specific algorithms in ratelimit/tokenbucket and
// ratelimit/slidingwindow are single-process only: each gateway replica
// would keep its own independent counters, so a tenant limited to 100 rps
// could actually get N*100 rps across N replicas. Result implements the
// same token-bucket semantics (rate + burst) but atomically in Redis via a
// Lua script, so all replicas share one counter.
package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"

	redispkg "github.com/vantageedge/backend/internal/cache/redis"
	"github.com/vantageedge/backend/internal/ratelimit/tokenbucket"
)

// Result describes the outcome of a rate limit check.
type Result struct {
	Allowed    bool
	Remaining  int
	RetryAfter time.Duration
}

// Limiter checks whether a request identified by key is allowed under a
// token-bucket policy of rps (refill rate) and burst (capacity).
type Limiter interface {
	Allow(ctx context.Context, key string, rps float64, burst int) (Result, error)
}

// tokenBucketScript atomically refills and (if enough tokens are available)
// decrements a token bucket stored in Redis. Using Redis's own TIME command
// (rather than the caller's clock) keeps the bucket correct even if gateway
// replicas have skewed clocks.
const tokenBucketScript = `
local tokens_key = KEYS[1] .. ":tokens"
local ts_key = KEYS[1] .. ":ts"
local capacity = tonumber(ARGV[1])
local refill_rate = tonumber(ARGV[2])
local requested = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])

local time_arr = redis.call("TIME")
local now = tonumber(time_arr[1]) + (tonumber(time_arr[2]) / 1000000)

local last_tokens = tonumber(redis.call("GET", tokens_key))
if last_tokens == nil then
  last_tokens = capacity
end

local last_refreshed = tonumber(redis.call("GET", ts_key))
if last_refreshed == nil then
  last_refreshed = now
end

local delta = now - last_refreshed
if delta < 0 then
  delta = 0
end

local filled = math.min(capacity, last_tokens + (delta * refill_rate))
local allowed = filled >= requested
local remaining = filled
if allowed then
  remaining = filled - requested
end

redis.call("SET", tokens_key, remaining, "EX", ttl)
redis.call("SET", ts_key, now, "EX", ttl)

-- Returned as a string: a Lua number in a table reply comes back as a RESP
-- integer (fraction truncated), which would make RetryAfter below overshoot
-- by up to a full refill tick.
if allowed then
  return {1, tostring(remaining)}
else
  return {0, tostring(remaining)}
end
`

// RedisLimiter is the distributed, production-grade Limiter.
type RedisLimiter struct {
	client *redispkg.Client
}

func NewRedisLimiter(client *redispkg.Client) *RedisLimiter {
	return &RedisLimiter{client: client}
}

func (l *RedisLimiter) Allow(ctx context.Context, key string, rps float64, burst int) (Result, error) {
	if rps <= 0 {
		rps = 1 // guard: rps is divided by below and in the Lua refill
	}
	if burst <= 0 {
		burst = 1
	}
	// Keep idle buckets around long enough to survive a quiet period without
	// losing their fill level, but not forever.
	ttlSeconds := int((float64(burst) / rps) * 4)
	if ttlSeconds < 60 {
		ttlSeconds = 60
	}

	res, err := l.client.Eval(ctx, tokenBucketScript, []string{"ratelimit:" + key},
		float64(burst), rps, 1.0, ttlSeconds)
	if err != nil {
		return Result{}, fmt.Errorf("rate limit check failed: %w", err)
	}

	arr, ok := res.([]interface{})
	if !ok || len(arr) != 2 {
		return Result{}, fmt.Errorf("unexpected rate limit script result: %v", res)
	}

	allowedVal := toInt64(arr[0])
	// remaining is returned as a Lua string (see the script) specifically to
	// preserve its fractional part across the RESP boundary.
	remaining := toFloat64(arr[1])

	allowed := allowedVal == 1
	result := Result{
		Allowed:   allowed,
		Remaining: int(remaining),
	}
	if !allowed {
		deficit := 1.0 - remaining
		if deficit < 0 {
			deficit = 0
		}
		result.RetryAfter = time.Duration(deficit/rps*1000) * time.Millisecond
	}
	return result, nil
}

func toInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	default:
		var parsed int64
		fmt.Sscanf(fmt.Sprintf("%v", v), "%d", &parsed)
		return parsed
	}
}

func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case string:
		var parsed float64
		fmt.Sscanf(n, "%g", &parsed)
		return parsed
	case float64:
		return n
	case int64:
		return float64(n)
	default:
		var parsed float64
		fmt.Sscanf(fmt.Sprintf("%v", v), "%g", &parsed)
		return parsed
	}
}

// MemoryLimiter is a single-process fallback used when Redis is unavailable
// or rate limiting is running in a dev/single-instance setup. It is NOT
// correct across multiple gateway replicas.
type MemoryLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenbucket.TokenBucket
}

func NewMemoryLimiter() *MemoryLimiter {
	m := &MemoryLimiter{buckets: make(map[string]*tokenbucket.TokenBucket)}
	go m.evictStale()
	return m
}

func (l *MemoryLimiter) Allow(_ context.Context, key string, rps float64, burst int) (Result, error) {
	if rps <= 0 {
		rps = 1
	}
	if burst <= 0 {
		burst = 1
	}

	l.mu.Lock()
	bucket, ok := l.buckets[key]
	if !ok {
		bucket = tokenbucket.NewTokenBucket(float64(burst), rps)
		l.buckets[key] = bucket
	}
	l.mu.Unlock()

	allowed := bucket.Allow(1)
	remaining := int(bucket.GetAvailableTokens())
	result := Result{Allowed: allowed, Remaining: remaining}
	if !allowed {
		result.RetryAfter = time.Duration(1.0/rps*1000) * time.Millisecond
	}
	return result, nil
}

// evictStale periodically drops buckets that haven't been touched, so a
// long-running gateway doesn't accumulate one entry per distinct client
// forever.
func (l *MemoryLimiter) evictStale() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		for key, bucket := range l.buckets {
			if bucket.GetAvailableTokens() == bucket.Capacity() {
				delete(l.buckets, key)
			}
		}
		l.mu.Unlock()
	}
}
