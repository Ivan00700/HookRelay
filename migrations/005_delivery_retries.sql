ALTER TABLE events
    ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    ADD COLUMN cycle_attempts INTEGER NOT NULL DEFAULT 0 CHECK (cycle_attempts >= 0),
    ADD COLUMN next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;

UPDATE events SET attempt_count = history.last_attempt
FROM (
    SELECT event_id, MAX(attempt_number) AS last_attempt
    FROM delivery_attempts GROUP BY event_id
) AS history
WHERE events.id = history.event_id;

DROP INDEX events_pending_queue_idx;
CREATE INDEX events_pending_queue_idx ON events (next_attempt_at, created_at, id)
    WHERE status = 'pending';
