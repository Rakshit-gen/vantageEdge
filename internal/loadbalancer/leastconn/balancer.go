package leastconn

import (
	"context"
	"fmt"
	"sync"

	"github.com/vantageedge/backend/internal/models"
)

type LeastConnBalancer struct {
	mu          sync.RWMutex
	connections map[string]int // origin_id -> connection count
}

func NewLeastConnBalancer() *LeastConnBalancer {
	return &LeastConnBalancer{
		connections: make(map[string]int),
	}
}

// SelectOrigin selects an origin with the least active connections
func (b *LeastConnBalancer) SelectOrigin(ctx context.Context, origins []*models.Origin) (*models.Origin, error) {
	if len(origins) == 0 {
		return nil, fmt.Errorf("no origins available")
	}

	b.mu.RLock()

	var selected *models.Origin
	minConns := int(^uint(0) >> 1) // Max int value

	for _, origin := range origins {
		conns := b.connections[origin.ID.String()]
		if conns < minConns {
			minConns = conns
			selected = origin
		}
	}

	b.mu.RUnlock()

	if selected == nil {
		selected = origins[0]
	}

	return selected, nil
}

// IncrementConnections increments the connection count for an origin
func (b *LeastConnBalancer) IncrementConnections(originID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.connections[originID]++
}

// DecrementConnections decrements the connection count for an origin
func (b *LeastConnBalancer) DecrementConnections(originID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if count, exists := b.connections[originID]; exists && count > 0 {
		b.connections[originID]--
	}
}

// GetConnectionCount returns the current connection count for an origin
func (b *LeastConnBalancer) GetConnectionCount(originID string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.connections[originID]
}

// Reset resets all connection counts
func (b *LeastConnBalancer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.connections = make(map[string]int)
}
