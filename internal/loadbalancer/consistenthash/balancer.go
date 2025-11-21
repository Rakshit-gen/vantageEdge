package consistenthash

import (
	"context"
	"fmt"
	"hash/crc32"
	"sort"
	"sync"

	"github.com/vantageedge/backend/internal/models"
)

type ConsistentHashBalancer struct {
	mu    sync.RWMutex
	ring  map[uint32]string     // hash -> origin_id
	keys  []uint32              // sorted hashes
	nodes map[string]int        // origin_id -> replica count
}

const defaultReplicas = 3

func NewConsistentHashBalancer() *ConsistentHashBalancer {
	return &ConsistentHashBalancer{
		ring:  make(map[uint32]string),
		keys:  make([]uint32, 0),
		nodes: make(map[string]int),
	}
}

// AddOrigin adds an origin to the consistent hash ring
func (b *ConsistentHashBalancer) AddOrigin(origin *models.Origin) {
	b.mu.Lock()
	defer b.mu.Unlock()

	originID := origin.ID.String()
	b.nodes[originID] = defaultReplicas

	for i := 0; i < defaultReplicas; i++ {
		hash := b.hash(fmt.Sprintf("%s-%d", originID, i))
		b.ring[hash] = originID
		b.keys = append(b.keys, hash)
	}

	sort.Slice(b.keys, func(i, j int) bool {
		return b.keys[i] < b.keys[j]
	})
}

// RemoveOrigin removes an origin from the consistent hash ring
func (b *ConsistentHashBalancer) RemoveOrigin(originID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	replicas := b.nodes[originID]
	delete(b.nodes, originID)

	newKeys := make([]uint32, 0)
	for i := 0; i < replicas; i++ {
		hash := b.hash(fmt.Sprintf("%s-%d", originID, i))
		delete(b.ring, hash)
	}

	// Rebuild keys
	for hash := range b.ring {
		newKeys = append(newKeys, hash)
	}
	sort.Slice(newKeys, func(i, j int) bool {
		return newKeys[i] < newKeys[j]
	})
	b.keys = newKeys
}

// SelectOrigin selects an origin based on consistent hashing of the key
func (b *ConsistentHashBalancer) SelectOrigin(ctx context.Context, key string, origins []*models.Origin) (*models.Origin, error) {
	if len(origins) == 0 {
		return nil, fmt.Errorf("no origins available")
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.keys) == 0 {
		return origins[0], nil
	}

	hash := b.hash(key)
	idx := sort.Search(len(b.keys), func(i int) bool {
		return b.keys[i] >= hash
	})

	if idx == len(b.keys) {
		idx = 0
	}

	originID := b.ring[b.keys[idx]]

	// Find the origin by ID
	for _, origin := range origins {
		if origin.ID.String() == originID {
			return origin, nil
		}
	}

	return origins[0], nil
}

// hash generates a hash value for a key
func (b *ConsistentHashBalancer) hash(key string) uint32 {
	return crc32.ChecksumIEEE([]byte(key))
}
