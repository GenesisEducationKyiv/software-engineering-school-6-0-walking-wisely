ALTER TABLE subscription_sagas DROP CONSTRAINT chk_saga_step;
ALTER TABLE subscription_sagas ADD CONSTRAINT chk_saga_step
    CHECK (step IN ('AWAITING_EMAIL', 'COMPLETED', 'COMPENSATING', 'COMPENSATED'));

ALTER TABLE subscription_sagas DROP COLUMN compensate_attempts;
