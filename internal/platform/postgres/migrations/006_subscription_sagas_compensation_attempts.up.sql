ALTER TABLE subscription_sagas ADD COLUMN compensate_attempts INT NOT NULL DEFAULT 0;

ALTER TABLE subscription_sagas DROP CONSTRAINT chk_saga_step;
ALTER TABLE subscription_sagas ADD CONSTRAINT chk_saga_step
    CHECK (step IN ('AWAITING_EMAIL', 'COMPLETED', 'COMPENSATING', 'COMPENSATED', 'COMPENSATION_FAILED'));
