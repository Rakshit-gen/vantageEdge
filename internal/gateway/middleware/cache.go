package middleware

import (
	"net/http"
	"sync"
	"time"
)

type CacheEntry struct {
	data      []byte
	expiresAt time.Time
}

type Cache struct {
	entries map[string]*CacheEntry
	mu      sync.RWMutex
}

func NewCache() *Cache {
	c := &Cache{
		entries: make(map[string]*CacheEntry),
	}
	
	// Cleanup expired entries
	go c.cleanup()
	
	return c
}

func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	entry, exists := c.entries[key]
	if !exists || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	
	return entry.data, true
}

func (c *Cache) Set(key string, data []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.entries[key] = &CacheEntry{
		data:      data,
		expiresAt: time.Now().Add(ttl),
	}
}

func (c *Cache) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	
	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for key, entry := range c.entries {
			if now.After(entry.expiresAt) {
				delete(c.entries, key)
			}
		}
		c.mu.Unlock()
	}
}
