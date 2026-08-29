package loadbalancer

import (
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/models"
)

// Select picks an origin from candidates according to strategy. Unknown or
// empty strategy falls back to "weighted" (the gateway's original
// behaviour). routeKey scopes the round-robin cursor to one route;
// clientKey (a client IP) is the sticky key for ip_hash.
//
// For "least_conn" the caller must bracket the actual proxy call with
// AddConn(origin.ID) / DoneConn(origin.ID) so the in-flight counts this
// reads stay accurate.
func Select(strategy string, routeKey uuid.UUID, clientKey string, candidates []*models.Origin) (*models.Origin, error) {
	switch strategy {
	case "round_robin":
		return selectRoundRobin(routeKey, candidates)
	case "least_conn":
		return selectLeastConn(candidates)
	case "ip_hash":
		return selectIPHash(clientKey, candidates)
	default:
		return SelectWeighted(candidates)
	}
}

var rrCursors sync.Map // routeID -> *atomic.Uint64

func selectRoundRobin(routeKey uuid.UUID, origins []*models.Origin) (*models.Origin, error) {
	if len(origins) == 0 {
		return nil, fmt.Errorf("no origins available")
	}
	c, _ := rrCursors.LoadOrStore(routeKey, new(atomic.Uint64))
	n := c.(*atomic.Uint64).Add(1) - 1
	return origins[n%uint64(len(origins))], nil
}

var connCounts sync.Map // originID -> *atomic.Int64

// AddConn / DoneConn track the in-flight request count per origin that
// least_conn selection reads. Safe to call for every strategy; only
// selectLeastConn consults the numbers.
func AddConn(originID uuid.UUID)  { counterFor(originID).Add(1) }
func DoneConn(originID uuid.UUID) { counterFor(originID).Add(-1) }

func counterFor(originID uuid.UUID) *atomic.Int64 {
	c, _ := connCounts.LoadOrStore(originID, new(atomic.Int64))
	return c.(*atomic.Int64)
}

func selectLeastConn(origins []*models.Origin) (*models.Origin, error) {
	if len(origins) == 0 {
		return nil, fmt.Errorf("no origins available")
	}
	var best *models.Origin
	var bestN int64
	for _, o := range origins {
		n := counterFor(o.ID).Load()
		if best == nil || n < bestN {
			best, bestN = o, n
		}
	}
	return best, nil
}

// selectIPHash sends every request from the same client key to the same
// origin, as long as the pool membership is unchanged. Origins are sorted
// by ID first so the mapping does not depend on candidate ordering.
//
// ponytail: plain hash-modulo, so growing/shrinking the pool reshuffles
// most keys. Swap in consistenthash if that churn matters.
func selectIPHash(key string, origins []*models.Origin) (*models.Origin, error) {
	if len(origins) == 0 {
		return nil, fmt.Errorf("no origins available")
	}
	sorted := append([]*models.Origin(nil), origins...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID.String() < sorted[j].ID.String() })
	h := fnv.New32a()
	h.Write([]byte(key))
	return sorted[h.Sum32()%uint32(len(sorted))], nil
}
