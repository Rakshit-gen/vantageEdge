-- Seed script for VantageEdge demo data
-- Run this after migrations to create demo tenant and configuration

-- Insert demo tenant
INSERT INTO tenants (id, name, subdomain, clerk_org_id, status, settings)
VALUES 
    ('550e8400-e29b-41d4-a716-446655440000', 
     'Demo Company', 
     'demo', 
     'org_demo_clerk_id',
     'active',
     '{"features": ["caching", "rate_limiting", "analytics"], "plan": "enterprise"}'::jsonb)
ON CONFLICT (subdomain) DO NOTHING;

-- Insert demo user
INSERT INTO users (id, tenant_id, clerk_user_id, email, first_name, last_name, role, status)
VALUES 
    ('650e8400-e29b-41d4-a716-446655440000',
     '550e8400-e29b-41d4-a716-446655440000',
     'user_demo_clerk_id',
     'demo@vantageedge.dev',
     'Demo',
     'User',
     'owner',
     'active')
ON CONFLICT (clerk_user_id) DO NOTHING;

-- Insert demo origin (JSONPlaceholder API for testing)
INSERT INTO origins (id, tenant_id, name, url, health_check_path, timeout_seconds, is_healthy)
VALUES 
    ('750e8400-e29b-41d4-a716-446655440000',
     '550e8400-e29b-41d4-a716-446655440000',
     'jsonplaceholder',
     'https://jsonplaceholder.typicode.com',
     '/posts/1',
     30,
     true)
ON CONFLICT (id) DO NOTHING;

-- Insert demo routes with different configurations

-- 1. Public route (no authentication)
INSERT INTO routes (
    id, tenant_id, origin_id, name, path_pattern, methods, priority, auth_mode,
    rate_limit_enabled, rate_limit_requests_per_second, rate_limit_burst,
    cache_enabled, cache_ttl_seconds, cache_key_pattern
)
VALUES 
    ('850e8400-e29b-41d4-a716-446655440001',
     '550e8400-e29b-41d4-a716-446655440000',
     '750e8400-e29b-41d4-a716-446655440000',
     'Public Posts',
     '/api/public/posts*',
     ARRAY['GET'],
     100,
     'public',
     true,
     1000,
     2000,
     true,
     300,
     'path+query')
ON CONFLICT (id) DO NOTHING;

-- 2. JWT protected route with caching
INSERT INTO routes (
    id, tenant_id, origin_id, name, path_pattern, methods, priority, auth_mode,
    rate_limit_enabled, rate_limit_requests_per_second, rate_limit_burst,
    cache_enabled, cache_ttl_seconds, cache_key_pattern,
    path_rewrite_pattern, path_rewrite_target
)
VALUES 
    ('850e8400-e29b-41d4-a716-446655440002',
     '550e8400-e29b-41d4-a716-446655440000',
     '750e8400-e29b-41d4-a716-446655440000',
     'User Data',
     '/api/users/*',
     ARRAY['GET'],
     200,
     'jwt_required',
     true,
     100,
     200,
     true,
     600,
     'path+user',
     '^/api/users/(.*)$',
     '/users/$1')
ON CONFLICT (id) DO NOTHING;

-- 3. API key protected admin route
INSERT INTO routes (
    id, tenant_id, origin_id, name, path_pattern, methods, priority, auth_mode,
    rate_limit_enabled, rate_limit_requests_per_second, rate_limit_burst,
    cache_enabled
)
VALUES 
    ('850e8400-e29b-41d4-a716-446655440003',
     '550e8400-e29b-41d4-a716-446655440000',
     '750e8400-e29b-41d4-a716-446655440000',
     'Admin API',
     '/api/admin/*',
     ARRAY['GET', 'POST', 'PUT', 'DELETE'],
     300,
     'apikey_required',
     true,
     50,
     100,
     false)
ON CONFLICT (id) DO NOTHING;

-- 4. High-traffic route with aggressive rate limiting
INSERT INTO routes (
    id, tenant_id, origin_id, name, path_pattern, methods, priority, auth_mode,
    rate_limit_enabled, rate_limit_requests_per_second, rate_limit_burst,
    cache_enabled, cache_ttl_seconds
)
VALUES 
    ('850e8400-e29b-41d4-a716-446655440004',
     '550e8400-e29b-41d4-a716-446655440000',
     '750e8400-e29b-41d4-a716-446655440000',
     'Comments Feed',
     '/api/comments*',
     ARRAY['GET'],
     150,
     'public',
     true,
     10,
     20,
     true,
     60)
ON CONFLICT (id) DO NOTHING;

-- Insert demo API keys
INSERT INTO api_keys (
    id, tenant_id, user_id, name, key_prefix, key_hash, scopes,
    expires_at, is_active
)
VALUES 
    ('950e8400-e29b-41d4-a716-446655440000',
     '550e8400-e29b-41d4-a716-446655440000',
     '650e8400-e29b-41d4-a716-446655440000',
     'Demo Production Key',
     'vte_prod',
     -- Hash of 'demo_key_abc123' (should be properly hashed in production)
     'f7c3bc1d808e04732adf679965ccc34ca7ae3441',
     ARRAY['read', 'write'],
     NOW() + INTERVAL '1 year',
     true),
    ('950e8400-e29b-41d4-a716-446655440001',
     '550e8400-e29b-41d4-a716-446655440000',
     '650e8400-e29b-41d4-a716-446655440000',
     'Demo Development Key',
     'vte_dev',
     -- Hash of 'demo_key_dev456' (should be properly hashed in production)
     'a94a8fe5ccb19ba61c4c0873d391e987982fbbd3',
     ARRAY['read'],
     NOW() + INTERVAL '6 months',
     true)
ON CONFLICT (id) DO NOTHING;

-- Insert some sample request logs for analytics demo
INSERT INTO request_logs (
    tenant_id, route_id, method, path, status_code, response_time_ms,
    cache_hit, auth_method, created_at
)
SELECT 
    '550e8400-e29b-41d4-a716-446655440000',
    '850e8400-e29b-41d4-a716-446655440001',
    'GET',
    '/api/public/posts',
    200,
    (random() * 200 + 50)::INTEGER,
    random() > 0.3,
    'public',
    NOW() - (random() * INTERVAL '7 days')
FROM generate_series(1, 100);

INSERT INTO request_logs (
    tenant_id, route_id, method, path, status_code, response_time_ms,
    cache_hit, auth_method, created_at
)
SELECT 
    '550e8400-e29b-41d4-a716-446655440000',
    '850e8400-e29b-41d4-a716-446655440002',
    'GET',
    '/api/users/' || (random() * 10)::INTEGER,
    CASE WHEN random() > 0.9 THEN 404 ELSE 200 END,
    (random() * 300 + 100)::INTEGER,
    random() > 0.4,
    'jwt',
    NOW() - (random() * INTERVAL '7 days')
FROM generate_series(1, 150);

-- Print success message
DO $$
BEGIN
    RAISE NOTICE 'Demo data seeded successfully!';
    RAISE NOTICE 'Demo Tenant: demo-company (subdomain: demo)';
    RAISE NOTICE 'Demo User: demo@vantageedge.dev';
    RAISE NOTICE 'Demo Origin: jsonplaceholder';
    RAISE NOTICE 'API Keys: demo_key_abc123 (full access), demo_key_dev456 (read-only)';
    RAISE NOTICE '';
    RAISE NOTICE 'Test URLs:';
    RAISE NOTICE '  - Public: https://demo.vantageedge.dev/api/public/posts';
    RAISE NOTICE '  - Protected: https://demo.vantageedge.dev/api/users/1';
    RAISE NOTICE '  - Admin: https://demo.vantageedge.dev/api/admin/users';
END $$;
