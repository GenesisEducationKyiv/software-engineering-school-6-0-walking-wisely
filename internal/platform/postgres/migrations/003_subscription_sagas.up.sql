CREATE TABLE subscription_sagas (
    saga_id         UUID PRIMARY KEY,
    subscription_id UUID NOT NULL,
    step            TEXT NOT NULL,
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_saga_step CHECK (step IN ('AWAITING_EMAIL', 'COMPLETED', 'COMPENSATING', 'COMPENSATED'))
);

CREATE INDEX idx_subscription_sagas_step ON subscription_sagas (step);
