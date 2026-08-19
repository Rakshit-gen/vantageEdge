package loadbalancer

import (
	"testing"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/models"
)

func TestSelectWeighted_NoOrigins(t *testing.T) {
	if _, err := SelectWeighted(nil); err == nil {
		t.Fatal("expected error for empty origin list")
	}
}

func TestSelectWeighted_SingleOrigin(t *testing.T) {
	origin := &models.Origin{ID: uuid.New(), Weight: 100}
	got, err := SelectWeighted([]*models.Origin{origin})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != origin {
		t.Fatal("expected the only origin to be returned")
	}
}

func TestSelectWeighted_DistributionRoughlyMatchesWeight(t *testing.T) {
	heavy := &models.Origin{ID: uuid.New(), Weight: 90}
	light := &models.Origin{ID: uuid.New(), Weight: 10}
	origins := []*models.Origin{heavy, light}

	counts := map[uuid.UUID]int{}
	const trials = 20000
	for i := 0; i < trials; i++ {
		got, err := SelectWeighted(origins)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		counts[got.ID]++
	}

	heavyShare := float64(counts[heavy.ID]) / float64(trials)
	// Expect roughly 90%, allow a wide tolerance to avoid test flakiness.
	if heavyShare < 0.80 || heavyShare > 0.98 {
		t.Errorf("heavy origin (weight 90) got %.1f%% of selections, want roughly 90%%", heavyShare*100)
	}
}

func TestSelectWeighted_ZeroOrNegativeWeightTreatedAsOne(t *testing.T) {
	zero := &models.Origin{ID: uuid.New(), Weight: 0}
	negative := &models.Origin{ID: uuid.New(), Weight: -5}
	origins := []*models.Origin{zero, negative}

	seen := map[uuid.UUID]bool{}
	for i := 0; i < 200; i++ {
		got, err := SelectWeighted(origins)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		seen[got.ID] = true
	}

	if len(seen) != 2 {
		t.Errorf("expected both zero/negative-weight origins to be selectable, got %d distinct picks", len(seen))
	}
}
