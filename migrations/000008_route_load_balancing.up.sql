-- Per-route load-balancing strategy across the origin pool.
-- 'weighted' matches the prior hard-coded behaviour (random, biased by
-- each origin's weight); 'round_robin', 'least_conn', 'ip_hash' are new.
ALTER TABLE routes
    ADD COLUMN IF NOT EXISTS load_balancing VARCHAR(50) NOT NULL DEFAULT 'weighted';
