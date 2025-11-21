package roundrobin

import (
	"context"
	"fmt"
	"sync"

	"github.com/vantageedge/backend/internal/models"
)

type RoundRobinBalancer struct {
	mu      sync.Mutex
	counter int
}

func NewRoundRobinBalancer() *RoundRobinBalancer {
	return &RoundRobinBalancer{counter: 0}
}

// SelectOrigin selects an origin using round-robin algorithm
func (b *RoundRobinBalancer) SelectOrigin(ctx context.Context, origins []*models.Origin) (*models.Origin, error) {
	if len(origins) == 0 {
		return nil, fmt.Errorf("no origins available")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Select origin by rotating through the list
	selected := origins[b.counter%len(origins)]
	b.counter++

	return selected, nil
}

// Reset resets the counter
func (b *RoundRobinBalancer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.counter = 0
}
