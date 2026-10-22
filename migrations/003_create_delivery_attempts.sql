CREATE TABLE delivery_attempts (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id TEXT NOT NULL REFERENCES events(id) ON DELETE RESTRICT,
    attempt_number INTEGER NOT NULL CHECK (attempt_number > 0),
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    http_status INTEGER CHECK (http_status BETWEEN 100 AND 599),
    error_message TEXT,
    UNIQUE (event_id, attempt_number),
    CHECK (finished_at IS NULL OR finished_at >= started_at)
);
