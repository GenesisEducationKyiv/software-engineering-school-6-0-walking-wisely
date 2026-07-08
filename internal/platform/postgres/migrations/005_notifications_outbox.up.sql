CREATE TABLE notifications_outbox (
    id              UUID PRIMARY KEY,
    event_type      TEXT NOT NULL,
    aggregate_type  TEXT NOT NULL,
    aggregate_id    TEXT NOT NULL,
    payload_json    JSONB NOT NULL,
    occurred_at     TIMESTAMPTZ NOT NULL,
    available_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    status          TEXT NOT NULL DEFAULT 'pending',
    attempt_count   INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT,
    idempotency_key TEXT NOT NULL,
    locked_at       TIMESTAMPTZ,
    locked_by       TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_notifications_outbox_status CHECK (status IN ('pending', 'processing', 'delivered', 'failed'))
);

CREATE INDEX idx_notifications_outbox_pending
    ON notifications_outbox (available_at, occurred_at)
    WHERE status IN ('pending', 'processing');

CREATE UNIQUE INDEX idx_notifications_outbox_idempotency_key
    ON notifications_outbox (idempotency_key);
