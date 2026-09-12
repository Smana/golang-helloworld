-- 004: asynchronous image processing (worker) and the demo controls row.
--
-- Existing rows become 'ready': they are served as originals, and a NULL
-- thumbnail_path falls back to the original. New uploads insert 'pending'.
ALTER TABLE images ADD COLUMN IF NOT EXISTS status VARCHAR(20) NOT NULL DEFAULT 'ready';
ALTER TABLE images ADD CONSTRAINT images_status_check CHECK (status IN ('pending', 'processing', 'ready', 'failed'));
ALTER TABLE images ADD COLUMN IF NOT EXISTS processing_error TEXT;
ALTER TABLE images ADD COLUMN IF NOT EXISTS processed_at TIMESTAMP WITH TIME ZONE;
CREATE INDEX IF NOT EXISTS idx_images_status_not_ready ON images(status) WHERE status <> 'ready';

-- Fault injection for the observability demo. One row; every control off.
CREATE TABLE IF NOT EXISTS demo_controls (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    latency_ms INTEGER NOT NULL DEFAULT 0 CHECK (latency_ms BETWEEN 0 AND 30000),
    latency_probability NUMERIC(4,3) NOT NULL DEFAULT 0 CHECK (latency_probability BETWEEN 0 AND 1),
    latency_routes TEXT[] NOT NULL DEFAULT '{}',
    error_probability NUMERIC(4,3) NOT NULL DEFAULT 0 CHECK (error_probability BETWEEN 0 AND 1),
    slow_db_ms INTEGER NOT NULL DEFAULT 0 CHECK (slow_db_ms BETWEEN 0 AND 30000),
    worker_failure_probability NUMERIC(4,3) NOT NULL DEFAULT 0 CHECK (worker_failure_probability BETWEEN 0 AND 1),
    worker_delay_ms INTEGER NOT NULL DEFAULT 0 CHECK (worker_delay_ms BETWEEN 0 AND 60000),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
INSERT INTO demo_controls (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
