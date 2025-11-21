-- Create request logs table for analytics
CREATE TABLE IF NOT EXISTS request_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    route_id UUID REFERENCES routes(id) ON DELETE SET NULL,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    
    -- Request details
    method VARCHAR(10) NOT NULL,
    path VARCHAR(500) NOT NULL,
    query_string TEXT,
    user_agent TEXT,
    ip_address INET,
    
    -- Response details
    status_code INTEGER NOT NULL,
    response_time_ms INTEGER NOT NULL,
    response_size_bytes INTEGER,
    
    -- Gateway processing
    cache_hit BOOLEAN DEFAULT false,
    cache_key VARCHAR(500),
    origin_url VARCHAR(500),
    rate_limited BOOLEAN DEFAULT false,
    
    -- Authentication
    auth_method VARCHAR(50),
    api_key_id UUID REFERENCES api_keys(id) ON DELETE SET NULL,
    
    -- Error tracking
    error_message TEXT,
    error_code VARCHAR(50),
    
    -- Metadata
    trace_id VARCHAR(100),
    span_id VARCHAR(100),
    metadata JSONB DEFAULT '{}',
    
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes optimized for analytics queries
CREATE INDEX idx_request_logs_tenant_id ON request_logs(tenant_id);
CREATE INDEX idx_request_logs_route_id ON request_logs(route_id);
CREATE INDEX idx_request_logs_user_id ON request_logs(user_id);
CREATE INDEX idx_request_logs_created_at ON request_logs(created_at DESC);
CREATE INDEX idx_request_logs_tenant_created ON request_logs(tenant_id, created_at DESC);
CREATE INDEX idx_request_logs_status_code ON request_logs(status_code);
CREATE INDEX idx_request_logs_cache_hit ON request_logs(cache_hit);
CREATE INDEX idx_request_logs_trace_id ON request_logs(trace_id);

-- Create a hypertable if TimescaleDB is available (optional)
-- SELECT create_hypertable('request_logs', 'created_at', if_not_exists => TRUE);

-- Create partitioning by month for better performance
-- This is a basic approach; TimescaleDB is recommended for production
