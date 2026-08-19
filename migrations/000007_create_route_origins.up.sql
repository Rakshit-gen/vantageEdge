-- Origin pools: a route can now load-balance across multiple origins
-- instead of pointing at exactly one. routes.origin_id is kept as the
-- route's default/primary origin (existing single-origin routes keep
-- working unchanged); route_origins is the full pool the gateway selects
-- from, weighted by each origin's existing `weight` column.
CREATE TABLE IF NOT EXISTS route_origins (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    route_id UUID NOT NULL REFERENCES routes(id) ON DELETE CASCADE,
    origin_id UUID NOT NULL REFERENCES origins(id) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT unique_route_origin UNIQUE(route_id, origin_id)
);

CREATE INDEX idx_route_origins_route_id ON route_origins(route_id);
CREATE INDEX idx_route_origins_origin_id ON route_origins(origin_id);

-- Backfill: every existing route's current origin_id becomes the first
-- (and, until more are added, only) member of its pool.
INSERT INTO route_origins (route_id, origin_id)
SELECT id, origin_id FROM routes
ON CONFLICT (route_id, origin_id) DO NOTHING;
