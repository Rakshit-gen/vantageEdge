#!/bin/bash

# VantageEdge API Gateway - Test Requests Script
# This script demonstrates all API endpoints with example cURL commands

BASE_URL="http://localhost:8080/api/v1"
GATEWAY_URL="http://demo.localhost:8000"

echo "=========================================="
echo "VantageEdge API Gateway - Test Suite"
echo "=========================================="
echo ""

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_section() {
    echo ""
    echo -e "${BLUE}=========================================="
    echo -e "$1"
    echo -e "==========================================${NC}"
    echo ""
}

print_test() {
    echo -e "${GREEN}Test: $1${NC}"
    echo "Command: $2"
    echo ""
}

# Wait for services to be ready
echo "Waiting for services to be ready..."
sleep 5

print_section "1. TENANT MANAGEMENT"

print_test "Create a new tenant" \
    'curl -X POST $BASE_URL/tenants \
    -H "Content-Type: application/json" \
    -d '"'"'{
      "name": "Acme Corporation",
      "subdomain": "acme",
      "clerk_org_id": "org_acme_123",
      "settings": {
        "features": ["caching", "rate_limiting"],
        "plan": "professional"
      }
    }'"'"

curl -X POST $BASE_URL/tenants \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Acme Corporation",
    "subdomain": "acme",
    "clerk_org_id": "org_acme_123",
    "settings": {
      "features": ["caching", "rate_limiting"],
      "plan": "professional"
    }
  }'
echo -e "\n"

print_test "List all tenants" "curl $BASE_URL/tenants"
curl $BASE_URL/tenants
echo -e "\n"

print_test "Get specific tenant" "curl $BASE_URL/tenants/{tenant_id}"
# Replace {tenant_id} with actual ID from previous response
echo -e "\n"

print_section "2. ORIGIN MANAGEMENT"

print_test "Add a backend origin" \
    'curl -X POST $BASE_URL/origins \
    -H "Content-Type: application/json" \
    -d '"'"'{
      "tenant_id": "{tenant_id}",
      "name": "API Backend",
      "url": "https://api.example.com",
      "health_check_path": "/health",
      "timeout_seconds": 30
    }'"'"

echo "Note: Replace {tenant_id} with actual tenant ID"
echo -e "\n"

print_section "3. ROUTE CONFIGURATION"

print_test "Create a routing rule with rate limiting and caching" \
    'curl -X POST $BASE_URL/routes \
    -H "Content-Type: application/json" \
    -d '"'"'{
      "tenant_id": "{tenant_id}",
      "origin_id": "{origin_id}",
      "name": "User API",
      "path_pattern": "/api/users/*",
      "methods": ["GET", "POST"],
      "auth_mode": "jwt_required",
      "rate_limit": {
        "enabled": true,
        "requests_per_second": 100,
        "burst": 200
      },
      "cache_policy": {
        "enabled": true,
        "ttl_seconds": 300,
        "cache_key_pattern": "path+query"
      }
    }'"'"

echo "Note: Replace {tenant_id} and {origin_id} with actual IDs"
echo -e "\n"

print_test "List routes for a tenant" "curl $BASE_URL/routes/tenant/{tenant_id}"
echo -e "\n"

print_section "4. API KEY MANAGEMENT"

print_test "Generate an API key" \
    'curl -X POST $BASE_URL/api-keys \
    -H "Content-Type: application/json" \
    -d '"'"'{
      "tenant_id": "{tenant_id}",
      "name": "Production API Key",
      "scopes": ["read", "write"],
      "expires_at": "2025-12-31T23:59:59Z"
    }'"'"

echo "Note: Replace {tenant_id} with actual tenant ID"
echo -e "\n"

print_section "5. GATEWAY REQUESTS (Demo Tenant)"

print_test "Public route (no authentication)" \
    "curl $GATEWAY_URL/api/public/posts"

echo "Making request to demo public route..."
curl -s $GATEWAY_URL/api/public/posts 2>/dev/null || echo "Gateway not configured yet"
echo -e "\n"

print_test "Protected route with JWT" \
    'curl $GATEWAY_URL/api/users/1 \
    -H "Authorization: Bearer {jwt_token}"'

echo "Note: Replace {jwt_token} with a valid Clerk JWT token"
echo -e "\n"

print_test "Protected route with API key" \
    'curl $GATEWAY_URL/api/admin/users \
    -H "X-API-Key: {api_key}"'

echo "Note: Replace {api_key} with a generated API key"
echo -e "\n"

print_section "6. CACHE DEMONSTRATION"

print_test "First request (cache MISS)" \
    "curl -v $GATEWAY_URL/api/posts/1"

echo "Note: Check X-Cache-Status header in response"
echo -e "\n"

print_test "Second request (cache HIT)" \
    "curl -v $GATEWAY_URL/api/posts/1"

echo "Note: This request should be faster with X-Cache-Status: HIT"
echo -e "\n"

print_section "7. RATE LIMIT DEMONSTRATION"

print_test "Trigger rate limit with rapid requests" \
    'for i in {1..150}; do
      curl -s $GATEWAY_URL/api/users/1
    done'

echo "Note: After ~100 requests, you should get 429 Too Many Requests"
echo -e "\n"

print_section "8. HEALTH CHECKS"

print_test "Control Plane health" "curl http://localhost:8080/health"
curl -s http://localhost:8080/health
echo -e "\n"

print_test "Gateway health" "curl http://localhost:8000/health"
curl -s http://localhost:8000/health
echo -e "\n"

print_section "9. METRICS AND OBSERVABILITY"

echo "Prometheus metrics available at:"
echo "  - Control Plane: http://localhost:9091/metrics"
echo "  - Gateway: http://localhost:9092/metrics"
echo ""

echo "Jaeger UI available at:"
echo "  - http://localhost:16686"
echo ""

echo "Grafana dashboard available at:"
echo "  - http://localhost:3001 (admin/admin)"
echo ""

print_section "Test Suite Complete!"

echo "For more examples, see the API documentation in README.md"
echo ""
