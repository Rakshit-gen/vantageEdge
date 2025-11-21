-- Create origins table
CREATE TABLE IF NOT EXISTS origins (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    url VARCHAR(500) NOT NULL,
    health_check_path VARCHAR(255) DEFAULT '/health',
    health_check_interval INTEGER DEFAULT 30,
    timeout_seconds INTEGER DEFAULT 30,
    max_retries INTEGER DEFAULT 3,
    weight INTEGER DEFAULT 100,
    is_healthy BOOLEAN DEFAULT true,
    last_health_check TIMESTAMP WITH TIME ZONE,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT unique_tenant_origin_name UNIQUE(tenant_id, name)
);

-- Create indexes
CREATE INDEX idx_origins_tenant_id ON origins(tenant_id);
CREATE INDEX idx_origins_is_healthy ON origins(is_healthy);
CREATE INDEX idx_origins_tenant_healthy ON origins(tenant_id, is_healthy);

-- Create trigger for updated_at
CREATE TRIGGER update_origins_updated_at BEFORE UPDATE ON origins
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
