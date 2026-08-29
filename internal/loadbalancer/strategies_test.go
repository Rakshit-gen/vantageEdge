package loadbalancer

import (
	"testing"

	"github.com/google/uuid"
	"github.com/vantageedge/backend/internal/models"
)

func pool(n int) []*models.Origin {
	out := make([]*models.Origin, n)
	for i := range out {
		out[i] = &models.Origin{ID: uuid.New(), Weight: 1}
	}
	return out
}

func TestRoundRobinCyclesEvenly(t *testing.T) {
	origins := pool(3)
	route := uuid.New()
	seen := map[uuid.UUID]int{}
	for i := 0; i < 9; i++ {
		o, err := Select("round_robin", route, "", origins)
		if err != nil {
			t.Fatal(err)
		}
		seen[o.ID]++
	}
	for _, o := range origins {
		if seen[o.ID] != 3 {
			t.Fatalf("origin %s hit %d times, want 3 (%v)", o.ID, seen[o.ID], seen)
		}
	}
}

func TestIPHashIsStickyPerKey(t *testing.T) {
	origins := pool(4)
	first, _ := Select("ip_hash", uuid.Nil, "203.0.113.7", origins)
	for i := 0; i < 20; i++ {
		got, _ := Select("ip_hash", uuid.Nil, "203.0.113.7", origins)
		if got.ID != first.ID {
			t.Fatalf("same client key landed on a different origin")
		}
	}
	// A different key is allowed (not required) to land elsewhere; just
	// make sure it still resolves.
	if o, err := Select("ip_hash", uuid.Nil, "198.51.100.2", origins); err != nil || o == nil {
		t.Fatalf("second key did not resolve: %v", err)
	}
}

func TestLeastConnAvoidsTheBusyOrigin(t *testing.T) {
	origins := pool(2)
	AddConn(origins[0].ID) // origins[0] has one request in flight
	defer DoneConn(origins[0].ID)

	got, err := Select("least_conn", uuid.Nil, "", origins)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != origins[1].ID {
		t.Fatalf("least_conn picked the busy origin")
	}
}

func TestUnknownStrategyFallsBackToWeighted(t *testing.T) {
	origins := pool(1)
	got, err := Select("bogus", uuid.Nil, "", origins)
	if err != nil || got.ID != origins[0].ID {
		t.Fatalf("fallback did not select the only origin: %v", err)
	}
}
