// cache.go implements the gateway's response cache.
//
// The previous Cache type stored raw []byte under a caller-supplied key
// with no notion of *what* was cached or *who* it was cached for. Wiring
// that naively into route-level caching (cache_key_pattern: "path+query")
// would be a cross-user data leak: two different authenticated users
// hitting the same jwt_required route with the same path+query would be
// served each other's cached response, because nothing distinguished them
// in the key. ResponseCache fixes this by requiring the caller to fold the
// resolved Identity into the cache key for any non-public route (see
// router.go's cacheKey helper) and by caching the full response (status +
// headers + body) rather than just a body blob.
package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	redispkg "github.com/vantageedge/backend/internal/cache/redis"
)

// CachedResponse is what gets stored per cache key.
type CachedResponse struct {
	StatusCode int                 `json:"status_code"`
	Header     map[string][]string `json:"header"`
	Body       []byte              `json:"body"`
}

// byteStore is the minimal storage interface both cache backends satisfy.
type byteStore interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, data []byte, ttl time.Duration) error
}

// ResponseCache is the gateway-facing cache API; it JSON-encodes
// CachedResponse and delegates raw storage to a byteStore.
type ResponseCache struct {
	store byteStore
}

func NewResponseCache(store byteStore) *ResponseCache {
	return &ResponseCache{store: store}
}

func (c *ResponseCache) Get(ctx context.Context, key string) (*CachedResponse, bool) {
	raw, ok, err := c.store.Get(ctx, key)
	if err != nil || !ok {
		// A cache backend error must never fail the request — treat it as
		// a miss and fall through to the origin.
		return nil, false
	}
	var resp CachedResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, false
	}
	return &resp, true
}

func (c *ResponseCache) Set(ctx context.Context, key string, resp *CachedResponse, ttl time.Duration) {
	raw, err := json.Marshal(resp)
	if err != nil {
		return
	}
	// Best-effort: a failed cache write must not fail the request that
	// already succeeded against the origin.
	_ = c.store.Set(ctx, key, raw, ttl)
}

// --- In-memory backend (single-instance / no Redis configured) ---

type memoryEntry struct {
	data      []byte
	expiresAt time.Time
}

// MemoryStore is a process-local byteStore. It is NOT shared across gateway
// replicas — each instance builds its own cache, so cache hit rates and
// invalidation are per-instance only. Use RedisStore in any multi-replica
// deployment.
type MemoryStore struct {
	mu      sync.RWMutex
	entries map[string]memoryEntry
}

func NewMemoryStore() *MemoryStore {
	s := &MemoryStore{entries: make(map[string]memoryEntry)}
	go s.cleanup()
	return s
}

func (s *MemoryStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.entries[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false, nil
	}
	return entry.data, true, nil
}

func (s *MemoryStore) Set(_ context.Context, key string, data []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key] = memoryEntry{data: data, expiresAt: time.Now().Add(ttl)}
	return nil
}

func (s *MemoryStore) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for key, entry := range s.entries {
			if now.After(entry.expiresAt) {
				delete(s.entries, key)
			}
		}
		s.mu.Unlock()
	}
}

// --- Redis backend (production / multi-replica) ---

// RedisStore adapts internal/cache/redis.Client (raw byte Get/Set) to
// byteStore.
type RedisStore struct {
	client *redispkg.Client
}

func NewRedisStore(client *redispkg.Client) *RedisStore {
	return &RedisStore{client: client}
}

func (s *RedisStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	return s.client.GetBytes(ctx, key)
}

func (s *RedisStore) Set(ctx context.Context, key string, data []byte, ttl time.Duration) error {
	return s.client.SetBytes(ctx, key, data, ttl)
}

// BuildCacheKey builds a namespaced cache key. identityKey must be folded
// in for any non-public route so responses are never shared across
// distinct callers (see the package doc comment above).
func BuildCacheKey(tenantID, routeID, identityKey, pattern, path, query string) string {
	switch pattern {
	case "path":
		return fmt.Sprintf("resp:%s:%s:%s:%s", tenantID, routeID, identityKey, path)
	default: // "path+query" and any unrecognized pattern default to the safer choice
		return fmt.Sprintf("resp:%s:%s:%s:%s?%s", tenantID, routeID, identityKey, path, query)
	}
}
