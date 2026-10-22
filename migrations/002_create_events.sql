CREATE TABLE events (
    id TEXT PRIMARY KEY,
    endpoint_id TEXT NOT NULL REFERENCES endpoints(id) ON DELETE RESTRICT,
    type TEXT NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'processing', 'delivered', 'failed')),
    created_at TIMESTAMPTZ NOT NULL
);
