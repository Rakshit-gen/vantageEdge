// Package eventbus provides a small in-process fan-out hub used to notify
// connected gRPC config-stream subscribers (gateway instances) when a
// tenant's routes or origins change, so they can invalidate their local
// config cache instead of waiting out a fixed TTL.
//
// This is intentionally in-process, not backed by Redis pub/sub or a
// message queue: the control plane is the only writer of route/origin
// config, and losing an in-flight event on a control-plane restart is
// harmless because every gateway also falls back to a periodic re-fetch
// (see the gateway's config client) — the stream is a latency optimization
// over that fallback, not the source of truth.
package eventbus

import "sync"

// Hub broadcasts tenant IDs to any number of subscribers.
type Hub struct {
	mu   sync.Mutex
	subs map[chan string]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: make(map[chan string]struct{})}
}

// Publish notifies every current subscriber that tenantID's config
// changed. Non-blocking: a subscriber that isn't keeping up has its event
// dropped rather than stalling the publisher (the caller is a control-plane
// HTTP handler completing a write — it must not block on a slow gRPC
// client).
func (h *Hub) Publish(tenantID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- tenantID:
		default:
		}
	}
}

// Subscribe registers a new listener and returns its channel plus an
// unsubscribe func the caller must call when done (typically via defer)
// to avoid leaking the channel and goroutine-side buffer forever.
func (h *Hub) Subscribe() (<-chan string, func()) {
	ch := make(chan string, 16)

	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()

	unsubscribe := func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
	return ch, unsubscribe
}
