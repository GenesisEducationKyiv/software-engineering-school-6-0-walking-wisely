CREATE TABLE outbox_events (
    id UUID PRIMARY KEY,
    event_type TEXT NOT NULL,
    aggregate_type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    payload_json JSONB NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    status TEXT NOT NULL DEFAULT 'pending',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    idempotency_key TEXT NOT NULL,
    locked_at TIMESTAMPTZ,
    locked_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_outbox_status CHECK (status IN ('pending', 'processing', 'delivered', 'failed'))
);

CREATE INDEX idx_outbox_events_pending
    ON outbox_events (available_at, occurred_at)
    WHERE status IN ('pending', 'processing');

CREATE INDEX idx_outbox_events_failed
    ON outbox_events (status, available_at)
    WHERE status = 'failed';

CREATE UNIQUE INDEX idx_outbox_events_idempotency_key
    ON outbox_events (idempotency_key);

CREATE TABLE event_deliveries (
    handler_name TEXT NOT NULL,
    event_id UUID NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (handler_name, event_id)
);

CREATE TABLE notification_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_type TEXT NOT NULL,
    subscription_id UUID NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
    event_id UUID NOT NULL,
    to_email TEXT NOT NULL,
    subject TEXT NOT NULL,
    html_body TEXT NOT NULL,
    confirm_token TEXT,
    release_tag TEXT,
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    status TEXT NOT NULL DEFAULT 'pending',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    locked_at TIMESTAMPTZ,
    locked_by TEXT,
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_notification_job_status CHECK (status IN ('pending', 'processing', 'sent', 'failed')),
    CONSTRAINT chk_notification_job_type CHECK (job_type IN ('confirmation', 'release_notification'))
);

CREATE INDEX idx_notification_jobs_pending
    ON notification_jobs (available_at, created_at)
    WHERE status IN ('pending', 'processing');

CREATE INDEX idx_notification_jobs_failed
    ON notification_jobs (status, available_at)
    WHERE status = 'failed';

CREATE UNIQUE INDEX idx_notification_jobs_confirmation_unique
    ON notification_jobs (subscription_id, confirm_token)
    WHERE job_type = 'confirmation';

CREATE UNIQUE INDEX idx_notification_jobs_release_unique
    ON notification_jobs (subscription_id, release_tag)
    WHERE job_type = 'release_notification';
