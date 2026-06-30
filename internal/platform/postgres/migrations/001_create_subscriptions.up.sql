CREATE TABLE
    subscriptions (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        email TEXT NOT NULL,
        repo TEXT NOT NULL,
        confirmed BOOLEAN NOT NULL DEFAULT FALSE,
        confirm_token TEXT NOT NULL,
        unsubscribe_token TEXT NOT NULL,
        last_seen_tag TEXT,
        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW (),
        updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW ()
    );

-- Prevents duplicate (email, repo) pairs - the lock target for concurrent subscribes
CREATE UNIQUE INDEX idx_subscriptions_email_repo ON subscriptions (email, repo);

-- O(1) token lookups used by confirm / unsubscribe endpoints
CREATE UNIQUE INDEX idx_subscriptions_confirm_token ON subscriptions (confirm_token);

CREATE UNIQUE INDEX idx_subscriptions_unsubscribe_tok ON subscriptions (unsubscribe_token);

-- Scanner queries only confirmed rows, filtered by repo
CREATE INDEX idx_subscriptions_repo_confirmed ON subscriptions (repo)
WHERE
    confirmed = TRUE;

-- GET /subscriptions filters by email
CREATE INDEX idx_subscriptions_email ON subscriptions (email);
