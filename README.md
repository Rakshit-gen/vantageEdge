# VantageEdge API Gateway - Backend

A high-performance, multi-tenant API Gateway with distributed caching, rate limiting, and advanced routing capabilities.

## Architecture Overview

The backend consists of 6 core components:

1. **Control Plane Service** - Configuration and tenant management (REST API for operators, gRPC for the gateway)
2. **Authentication Layer** - Clerk integration and identity management
3. **API Gateway** - Request routing and traffic management
4. **Load Balancer** - Health-aware traffic distribution across a route's origin pool, per-route strategy (`weighted`, `round_robin`, `least_conn`, `ip_hash`)
5. **Distributed Cache** - Redis-backed response cache and rate limiter, shared across gateway replicas
6. **Observability** - Prometheus metrics and OpenTelemetry traces (exported to Jaeger)

The gateway does not talk to Postgres to serve requests. It holds a short-TTL,
push-invalidated cache of each tenant's config, fetched from the control
plane's gRPC `ConfigService` (`api/proto/config.proto`) — `GetTenantConfig`
for the initial/refetch path, `StreamConfigUpdates` for near-immediate
invalidation when an operator changes a route or origin. This keeps
database credentials off the gateway (a compromised or misconfigured
gateway instance can't read or write tenant data it doesn't need to) and
decouples gateway replica count from Postgres connection limits. The
gateway does still write directly to Postgres for request-log analytics —
that's a write on the side, not on the read path routing depends on.

## Tech Stack

- **Language**: Go 1.25+
- **Database**: PostgreSQL 15
- **Cache**: Redis 7
- **Authentication**: Clerk (JWT verified against Clerk's JWKS)
- **Inter-service**: gRPC (control plane → gateway config sync)
- **Observability**: Prometheus + OpenTelemetry/Jaeger
- **Containerization**: Docker & Docker Compose

## Quick Start

### Prerequisites

- Docker & Docker Compose
- Go 1.21+ (for local development)
- Clerk account with API keys

### Environment Setup

1. Copy the environment template:
```bash
cp .env.example .env
```

2. Update `.env` with your Clerk credentials:
```env
CLERK_SECRET_KEY=your_clerk_secret_key
CLERK_PUBLISHABLE_KEY=your_clerk_publishable_key
```

### Running with Docker

```bash
# Build and start all services
docker-compose up --build

# Run in detached mode
docker-compose up -d

# View logs
docker-compose logs -f

# Stop all services
docker-compose down
```

### Running Locally (Development)

```bash
# Install dependencies
go mod download

# Run database migrations
make migrate-up

# Seed initial data
make seed

# Run control plane
make run-control-plane

# Run gateway (in another terminal)
make run-gateway
```

## API Documentation

### Control Plane API (Port 8080)

Every `/api/v1/*` route requires `Authorization: Bearer <clerk_jwt>`. The
JWT is verified against Clerk's JWKS (see `CLERK_JWKS_URL` below) — there is
no unauthenticated access to any tenant's data. The authenticated caller's
tenant is resolved automatically from their Clerk identity (auto-provisioned
on first request) and used for every operation; **you cannot pass a
`tenant_id` to act on another tenant** — requests are always scoped to your
own tenant, and any resource ID that belongs to a different tenant returns
`404`.

#### Tenant (your own)

```bash
curl http://localhost:8080/api/v1/tenants/me \
  -H "Authorization: Bearer <clerk_jwt>"

curl -X PUT http://localhost:8080/api/v1/tenants/me \
  -H "Authorization: Bearer <clerk_jwt>" \
  -H "Content-Type: application/json" \
  -d '{"name": "Acme Corp"}'
```

#### Origins

**Add Origin**
```bash
curl -X POST http://localhost:8080/api/v1/origins \
  -H "Authorization: Bearer <clerk_jwt>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "api-backend",
    "url": "https://api.example.com",
    "health_check_path": "/health",
    "timeout_seconds": 30
  }'
```

#### Route Rules

Route patterns use `*` as a wildcard matched by the gateway itself (e.g.
`/api/users/*` matches `/api/users/123/profile`) — not SQL `LIKE` syntax.

**Create Route**
```bash
curl -X POST http://localhost:8080/api/v1/routes \
  -H "Authorization: Bearer <clerk_jwt>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "users-api",
    "path_pattern": "/api/users/*",
    "origin_id": "origin_uuid",
    "auth_mode": "jwt_required",
    "rate_limit_enabled": true,
    "rate_limit_requests_per_second": 100,
    "rate_limit_burst": 200,
    "cache_enabled": true,
    "cache_ttl_seconds": 300,
    "cache_key_pattern": "path+query"
  }'
```

#### API Keys

**Generate API Key**
```bash
curl -X POST http://localhost:8080/api/v1/api-keys \
  -H "Authorization: Bearer <clerk_jwt>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "production-key",
    "scopes": ["read", "write"],
    "expires_at": "2025-12-31T23:59:59Z"
  }'
```

#### Analytics

**Traffic rollup for the authenticated tenant** (aggregated from `request_logs`)
```bash
curl "http://localhost:8080/api/v1/analytics?window=24h" \
  -H "Authorization: Bearer <clerk_jwt>"
# window ∈ {1h, 24h, 7d, 30d} (default 24h). Returns:
#   { window, generated_at,
#     totals: { total_requests, error_rate, cache_hit_rate,
#               rate_limited_count, avg_latency_ms, p95_latency_ms },
#     series: [ { ts, count, avg_latency_ms, error_count } ],   # hourly ≤24h, else daily
#     status_breakdown: { "200": n, ... },
#     top_routes: [ { path, count, avg_latency_ms, error_count } ] }
```

### Gateway API (Port 8000)

**Make Request Through Gateway**
```bash
# JWT Authentication
curl -X GET https://acme.vantageedge.dev/api/users/123 \
  -H "Authorization: Bearer <clerk_jwt_token>"

# API Key Authentication
curl -X GET https://acme.vantageedge.dev/api/users/123 \
  -H "X-API-Key: <generated_api_key>"

# Public Route (no auth)
curl -X GET https://acme.vantageedge.dev/api/public/status
```

## Project Structure

```
vantageedge-backend/
├── cmd/
│   ├── control-plane/       # Control plane service entry point
│   ├── gateway/              # API gateway entry point
│   └── migrator/             # Database migration tool
├── internal/
│   ├── auth/                 # Authentication & authorization
│   │   ├── clerk/           # Clerk integration
│   │   ├── jwt/             # JWT validation
│   │   └── apikey/          # API key management
│   ├── controlplane/        # Control plane business logic
│   │   ├── handlers/        # HTTP handlers
│   │   ├── grpc/           # gRPC service
│   │   └── service/        # Business logic
│   ├── gateway/             # Gateway core
│   │   ├── router/         # Request routing
│   │   ├── middleware/     # Middleware chain
│   │   └── proxy/          # Reverse proxy
│   ├── loadbalancer/        # Load balancing algorithms
│   │   ├── roundrobin/
│   │   ├── leastconn/
│   │   └── consistenthash/
│   ├── cache/               # Distributed cache
│   │   ├── redis/          # Redis implementation
│   │   └── memory/         # In-memory fallback
│   ├── ratelimit/           # Rate limiting
│   │   ├── tokenbucket/
│   │   └── slidingwindow/
│   ├── models/              # Domain models
│   ├── repository/          # Data access layer
│   └── observability/       # Metrics, traces, logs
├── migrations/              # Database migrations
├── pkg/                     # Shared packages
│   ├── config/
│   ├── database/
│   ├── logger/
│   └── telemetry/
├── api/
│   ├── proto/              # gRPC definitions
│   └── openapi/            # OpenAPI specs
├── scripts/
│   ├── seed.sql            # Sample data
│   └── test-requests.sh    # Example requests
├── docker/
│   ├── control-plane.Dockerfile
│   ├── gateway.Dockerfile
│   └── nginx.conf
├── docker-compose.yml
├── Makefile
├── go.mod
├── go.sum
└── README.md
```

## Features

### Multi-Tenancy
- Subdomain-based tenant routing (`https://<tenant>.vantageedge.dev`)
- Isolated configurations per tenant
- Clerk organization mapping

### Authentication
- Clerk JWT validation
- API key authentication
- Service-to-service authentication
- OAuth token support

### Rate Limiting
- Token bucket algorithm
- Sliding window counters
- Per-tenant, per-route, and per-user limits
- Configurable burst capacity

### Caching
- Distributed Redis cache
- Tenant-aware namespacing
- Configurable TTL and eviction policies
- Cache key patterns (path, query, headers)
- Selective cache bypass rules

### Load Balancing
- Round robin distribution
- Least connections
- Consistent hashing
- Health checking
- Circuit breaking

### Observability
- OpenTelemetry traces
- Structured JSON logging
- Prometheus metrics:
  - Request latency histograms
  - Cache hit/miss ratios
  - Rate limit actions
  - Error rates by status code
  - Active connections

## Database Schema

### Tenants
- `id` (UUID, PK)
- `name` (String)
- `subdomain` (String, Unique)
- `clerk_org_id` (String, Unique)
- `created_at`, `updated_at`

### Users
- `id` (UUID, PK)
- `clerk_user_id` (String, Unique)
- `tenant_id` (UUID, FK)
- `email` (String)
- `role` (Enum)
- `created_at`, `updated_at`

### Origins
- `id` (UUID, PK)
- `tenant_id` (UUID, FK)
- `name` (String)
- `url` (String)
- `health_check_path` (String)
- `timeout_seconds` (Integer)
- `created_at`, `updated_at`

### Routes
- `id` (UUID, PK)
- `tenant_id` (UUID, FK)
- `origin_id` (UUID, FK)
- `path_pattern` (String)
- `auth_mode` (Enum: public, jwt_required, apikey_required)
- `priority` (Integer)
- `rate_limit_config` (JSONB)
- `cache_policy` (JSONB)
- `created_at`, `updated_at`

### API Keys
- `id` (UUID, PK)
- `tenant_id` (UUID, FK)
- `key_hash` (String, Unique)
- `name` (String)
- `scopes` (JSONB)
- `expires_at` (Timestamp)
- `created_at`, `updated_at`

## Testing

```bash
# Run all tests
make test

# Run with coverage
make test-coverage

# Run integration tests
make test-integration

# Load testing
make load-test
```

## Deployment

### Production Considerations

1. **Database**
   - Use managed PostgreSQL (AWS RDS, Google Cloud SQL)
   - Enable connection pooling
   - Regular backups

2. **Cache**
   - Use managed Redis (AWS ElastiCache, Redis Cloud)
   - Enable persistence for critical cache data
   - Set up replication for HA

3. **Secrets**
   - Use secret management (AWS Secrets Manager, HashiCorp Vault)
   - Rotate Clerk API keys regularly
   - Secure API key hashing with strong algorithms

4. **Monitoring**
   - Set up alerting for error rates, latency spikes
   - Monitor cache hit ratios
   - Track rate limit violations

5. **Scaling**
   - Gateway and control plane can scale independently
   - Use horizontal pod autoscaling based on CPU/memory
   - Consider multi-region deployment for global users

## Example Demo Scenario

The system comes with preconfigured demo data:

**Tenant**: demo-company
**Subdomain**: demo
**Origin**: https://jsonplaceholder.typicode.com

### Test Requests

```bash
# 1. Public route (no auth)
curl https://demo.vantageedge.dev/api/public/posts

# 2. Protected route with JWT
curl https://demo.vantageedge.dev/api/users/1 \
  -H "Authorization: Bearer <your_clerk_jwt>"

# 3. API key protected route
curl https://demo.vantageedge.dev/api/admin/users \
  -H "X-API-Key: demo_key_abc123"

# 4. Demonstrate caching (first call: MISS, second: HIT)
curl -v https://demo.vantageedge.dev/api/posts/1
curl -v https://demo.vantageedge.dev/api/posts/1

# 5. Trigger rate limit
for i in {1..150}; do
  curl https://demo.vantageedge.dev/api/users/1
done
```

## Troubleshooting

### Common Issues

**Database Connection Failed**
- Check PostgreSQL is running: `docker-compose ps`
- Verify connection string in `.env`
- Check network connectivity

**Clerk Token Validation Failed**
- Ensure `CLERK_SECRET_KEY` is correct
- Verify token is not expired
- Check token format: `Bearer <token>`

**Cache Not Working**
- Verify Redis is running
- Check cache policy configuration
- Ensure route has caching enabled

**Rate Limit Not Applied**
- Check rate limit configuration in route
- Verify tenant identification is working
- Check Redis connectivity for state storage

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run tests
5. Submit a pull request

## License

MIT License - See LICENSE file for details

## Support

For issues and questions:
- GitHub Issues: [Create an issue]
- Documentation: [Wiki]
- Email: support@vantageedge.dev
# vantageEdge
