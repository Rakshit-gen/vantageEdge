package router

import (
	"context"
	"fmt"

	"github.com/vantageedge/backend/internal/gateway/configclient"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ConfigSource fetches one tenant's full serving config by subdomain,
// returning ErrTenantNotFound when the subdomain matches no tenant.
// ConfigCache layers caching (and, for sources that support it,
// invalidation) on top. The gateway picks a source at startup: the gRPC
// source (control plane's ConfigService, with push invalidation) when a
// control-plane gRPC address is configured, otherwise the direct-DB source
// (see dbsource.go).
type ConfigSource interface {
	Fetch(ctx context.Context, subdomain string) (*tenantConfig, error)
}

// grpcConfigSource reads config from the control plane's gRPC
// ConfigService. Pair it with configclient.WatchInvalidations to get push
// invalidation of the ConfigCache.
type grpcConfigSource struct {
	client *configclient.Client
}

// NewGRPCSource builds a ConfigSource backed by the control plane's gRPC
// ConfigService.
func NewGRPCSource(client *configclient.Client) ConfigSource {
	return &grpcConfigSource{client: client}
}

func (s *grpcConfigSource) Fetch(ctx context.Context, subdomain string) (*tenantConfig, error) {
	pb, err := s.client.GetTenantConfig(ctx, subdomain)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, ErrTenantNotFound
		}
		return nil, fmt.Errorf("failed to fetch tenant config: %w", err)
	}
	return toTenantConfig(pb)
}
