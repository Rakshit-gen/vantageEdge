package loadbalancer

import (
	"fmt"
	"math/rand"

	"github.com/vantageedge/backend/internal/models"
)

// SelectWeighted picks one origin from origins at random, weighted by each
// origin's Weight column (already part of the schema but never read by any
// selection logic — every request went straight to route.OriginID via a
// single GetByID call). An origin with Weight <= 0 is treated as weight 1
// rather than excluded, so a misconfigured weight doesn't silently remove
// an otherwise-healthy origin from rotation.
func SelectWeighted(origins []*models.Origin) (*models.Origin, error) {
	if len(origins) == 0 {
		return nil, fmt.Errorf("no origins available")
	}
	if len(origins) == 1 {
		return origins[0], nil
	}

	total := 0
	for _, o := range origins {
		total += normalizedWeight(o)
	}

	pick := rand.Intn(total)
	cumulative := 0
	for _, o := range origins {
		cumulative += normalizedWeight(o)
		if pick < cumulative {
			return o, nil
		}
	}
	// Unreachable given the loop above sums to `total` and pick < total,
	// but return the last origin rather than nil if floating logic ever
	// changes.
	return origins[len(origins)-1], nil
}

func normalizedWeight(o *models.Origin) int {
	if o.Weight <= 0 {
		return 1
	}
	return o.Weight
}
