// Package configclient is the gateway's gRPC client for the control
// plane's ConfigService (see api/proto/config.proto). It replaces the
// gateway reading tenant/route/origin config directly out of Postgres.
package configclient

import (
	"context"
	"fmt"
	"time"

	"github.com/vantageedge/backend/api/proto/configpb"
	"github.com/vantageedge/backend/pkg/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	conn   *grpc.ClientConn
	api    configpb.ConfigServiceClient
	logger *logger.Logger
}

// NewClient dials the control plane's gRPC address (e.g.
// "control-plane:9090"). The connection is internal-network-only, matching
// the rest of this project's assumption that TLS termination happens at
// the edge, not between these two services — see the production-readiness
// report for that as a called-out gap if this ever crosses an untrusted
// network.
func NewClient(addr string, log *logger.Logger) (*Client, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC client for control plane at %s: %w", addr, err)
	}
	return NewClientFromConn(conn, log), nil
}

// NewClientFromConn builds a Client around an already-established
// connection — used by tests that dial an in-memory (bufconn) server
// rather than a real address.
func NewClientFromConn(conn *grpc.ClientConn, log *logger.Logger) *Client {
	return &Client{conn: conn, api: configpb.NewConfigServiceClient(conn), logger: log}
}

func (c *Client) Close() error {
	return c.conn.Close()
}

// GetTenantConfig fetches one tenant's full serving config by subdomain.
func (c *Client) GetTenantConfig(ctx context.Context, subdomain string) (*configpb.TenantConfig, error) {
	return c.api.GetTenantConfig(ctx, &configpb.GetTenantConfigRequest{Subdomain: subdomain})
}

// WatchInvalidations subscribes to the control plane's config-change
// stream and calls onInvalidate with each tenant ID that changed, until
// ctx is cancelled. It reconnects with backoff on any stream error — a
// control-plane restart or network blip must not permanently stop
// invalidations from flowing, since the alternative is stale config served
// forever (bounded only by the ConfigCache's TTL fallback, but that
// fallback existing doesn't excuse this from trying to reconnect).
func (c *Client) WatchInvalidations(ctx context.Context, onInvalidate func(tenantID string)) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}

		stream, err := c.api.StreamConfigUpdates(ctx, &configpb.StreamConfigUpdatesRequest{})
		if err != nil {
			c.logger.Warn().Err(err).Dur("retry_in", backoff).Msg("Failed to open config update stream; retrying")
			if !sleepOrDone(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff, maxBackoff)
			continue
		}

		backoff = time.Second // reset after a successful connection
		for {
			event, err := stream.Recv()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				c.logger.Warn().Err(err).Msg("Config update stream closed; reconnecting")
				break
			}
			onInvalidate(event.TenantId)
		}
	}
}

func sleepOrDone(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextBackoff(current, max time.Duration) time.Duration {
	next := current * 2
	if next > max {
		return max
	}
	return next
}
