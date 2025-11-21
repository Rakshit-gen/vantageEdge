-- Create enum type for auth modes
CREATE TYPE auth_mode AS ENUM ('public', 'jwt_required', 'apikey_required', 'both');

-- Create routes table
CREATE TABLE IF NOT EXISTS routes (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    origin_id UUID NOT NULL REFERENCES origins(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    path_pattern VARCHAR(500) NOT NULL,
    methods TEXT[] DEFAULT ARRAY['GET', 'POST', 'PUT', 'DELETE', 'PATCH'],
    priority INTEGER DEFAULT 0,
    auth_mode auth_mode DEFAULT 'jwt_required',
    is_active BOOLEAN DEFAULT true,
    
    -- Rate limiting configuration
    rate_limit_enabled BOOLEAN DEFAULT true,
    rate_limit_requests_per_second INTEGER DEFAULT 100,
    rate_limit_burst INTEGER DEFAULT 200,
    rate_limit_key_strategy VARCHAR(50) DEFAULT 'tenant_user',
    
    -- Cache configuration
    cache_enabled BOOLEAN DEFAULT false,
    cache_ttl_seconds INTEGER DEFAULT 300,
    cache_key_pattern VARCHAR(100) DEFAULT 'path+query',
    cache_bypass_rules JSONB DEFAULT '[]',
    
    -- Request transformation
    request_headers JSONB DEFAULT '{}',
    response_headers JSONB DEFAULT '{}',
    path_rewrite_pattern VARCHAR(500),
    path_rewrite_target VARCHAR(500),
    
    -- Advanced settings
    timeout_seconds INTEGER DEFAULT 30,
    retry_attempts INTEGER DEFAULT 0,
    circuit_breaker_enabled BOOLEAN DEFAULT false,
    circuit_breaker_threshold INTEGER DEFAULT 5,
    
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    
    CONSTRAINT unique_tenant_route_path UNIQUE(tenant_id, path_pattern)
);

-- Create indexes
CREATE INDEX idx_routes_tenant_id ON routes(tenant_id);
CREATE INDEX idx_routes_origin_id ON routes(origin_id);
CREATE INDEX idx_routes_path_pattern ON routes(path_pattern);
CREATE INDEX idx_routes_tenant_active ON routes(tenant_id, is_active);
CREATE INDEX idx_routes_priority ON routes(priority DESC);
CREATE INDEX idx_routes_auth_mode ON routes(auth_mode);

-- Create trigger for updated_at
CREATE TRIGGER update_routes_updated_at BEFORE UPDATE ON routes
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
