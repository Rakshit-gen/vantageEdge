# VantageEdge Backend - Quick Start Guide

This guide will help you get the VantageEdge API Gateway backend up and running in minutes.

## Prerequisites

- Docker Desktop installed and running
- Docker Compose installed
- 8GB+ RAM available
- Ports 5432, 6379, 8000, 8080 available

## Step 1: Set Up Environment

1. Copy the environment template:
```bash
cp .env.example .env
```

2. Update `.env` with your Clerk credentials:
```env
CLERK_SECRET_KEY=sk_test_your_key_here
CLERK_PUBLISHABLE_KEY=pk_test_your_key_here
```

Get your Clerk keys from: https://dashboard.clerk.com

## Step 2: Start the Backend

```bash
# Build and start all services
docker-compose up --build

# Or run in detached mode
docker-compose up -d
```

This will start:
- PostgreSQL database (port 5432)
- Redis cache (port 6379)
- Control Plane API (port 8080)
- API Gateway (port 8000)
- Jaeger tracing (port 16686)
- Prometheus metrics (port 9093)
- Grafana dashboards (port 3001)

## Step 3: Verify Services

Check that all services are running:
```bash
docker-compose ps
```

You should see all services with status "Up".

Test the health endpoints:
```bash
# Control Plane
curl http://localhost:8080/health

# Gateway
curl http://localhost:8000/health
```

## Step 4: Seed Demo Data

The database is automatically seeded with demo data including:
- Demo tenant (subdomain: "demo")
- Demo origin (JSONPlaceholder API)
- Sample routes with different auth modes
- Demo API keys

## Step 5: Test the API

Run the test script:
```bash
bash scripts/test-requests.sh
```

Or test manually:

### Create a tenant
```bash
curl -X POST http://localhost:8080/api/v1/tenants \
  -H "Content-Type: application/json" \
  -d '{
    "name": "My Company",
    "subdomain": "mycompany",
    "clerk_org_id": "org_your_clerk_org_id"
  }'
```

### List all tenants
```bash
curl http://localhost:8080/api/v1/tenants
```

### Test gateway with demo tenant
```bash
# This will proxy to jsonplaceholder.typicode.com
curl http://demo.localhost:8000/api/public/posts
```

## Step 6: Access Observability Tools

### Jaeger (Distributed Tracing)
- URL: http://localhost:16686
- Search for traces by service name: "vantageedge"

### Prometheus (Metrics)
- URL: http://localhost:9093
- View metrics from control-plane and gateway

### Grafana (Dashboards)
- URL: http://localhost:3001
- Login: admin / admin
- Preconfigured with Prometheus datasource

## Step 7: Develop Locally

If you want to run services locally (without Docker):

```bash
# Install dependencies
make install

# Run database migrations
make migrate-up

# Seed the database
make seed

# Start Control Plane (terminal 1)
make run-control-plane

# Start Gateway (terminal 2)
make run-gateway
```

## Common Issues

### Port Already in Use
If ports are already in use, stop conflicting services or modify ports in `docker-compose.yml`.

### Database Connection Failed
- Ensure PostgreSQL container is running: `docker-compose ps postgres`
- Check logs: `docker-compose logs postgres`

### Clerk Authentication Errors
- Verify your Clerk keys are correct in `.env`
- Ensure keys start with `sk_test_` and `pk_test_`

## Next Steps

1. **Configure Your Own Tenant**
   - Create a tenant with your organization
   - Add your backend origins
   - Configure routing rules

2. **Set Up Authentication**
   - Integrate Clerk in your frontend
   - Pass JWT tokens in Authorization header
   - Or generate API keys for service-to-service auth

3. **Configure Rate Limiting**
   - Adjust rate limits per route
   - Set different limits for different tiers

4. **Enable Caching**
   - Configure cache policies per route
   - Set appropriate TTLs based on data freshness

5. **Monitor Performance**
   - Check metrics in Prometheus
   - View traces in Jaeger
   - Create custom Grafana dashboards

## API Documentation

### Control Plane API

Base URL: `http://localhost:8080/api/v1`

**Tenants**
- `POST /tenants` - Create tenant
- `GET /tenants` - List tenants
- `GET /tenants/:id` - Get tenant
- `PUT /tenants/:id` - Update tenant
- `DELETE /tenants/:id` - Delete tenant

**Origins**
- `POST /origins` - Add origin
- `GET /origins/:id` - Get origin
- `GET /origins/tenant/:tenant_id` - List origins
- `PUT /origins/:id` - Update origin
- `DELETE /origins/:id` - Delete origin

**Routes**
- `POST /routes` - Create route
- `GET /routes/:id` - Get route
- `GET /routes/tenant/:tenant_id` - List routes
- `PUT /routes/:id` - Update route
- `DELETE /routes/:id` - Delete route

**API Keys**
- `POST /api-keys` - Generate key
- `GET /api-keys/tenant/:tenant_id` - List keys
- `DELETE /api-keys/:id` - Delete key

### Gateway API

Base URL: `http://<tenant-subdomain>.localhost:8000`

All requests are proxied to configured origins based on routing rules.

**Headers**
- `Authorization: Bearer <token>` - JWT authentication
- `X-API-Key: <key>` - API key authentication

## Production Deployment

For production deployment:

1. Use managed PostgreSQL (AWS RDS, Google Cloud SQL)
2. Use managed Redis (AWS ElastiCache, Redis Cloud)
3. Store secrets in secure vault (AWS Secrets Manager, HashiCorp Vault)
4. Enable HTTPS with valid certificates
5. Set up proper DNS for subdomains
6. Configure horizontal pod autoscaling
7. Set up log aggregation and alerting
8. Implement backup and disaster recovery

## Support

For issues or questions:
- Check logs: `docker-compose logs -f`
- Review documentation in README.md
- Create an issue on GitHub

## License

MIT License
