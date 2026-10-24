ALTER TABLE events
    ADD COLUMN lease_version BIGINT NOT NULL DEFAULT 0 CHECK (lease_version >= 0),
    ADD COLUMN lease_until TIMESTAMPTZ;

CREATE INDEX events_pending_queue_idx ON events (created_at, id)
    WHERE status = 'pending';

CREATE INDEX events_processing_lease_idx ON events (lease_until, created_at, id)
    WHERE status = 'processing';
