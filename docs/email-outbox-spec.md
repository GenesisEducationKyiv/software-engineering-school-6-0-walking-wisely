# Email Outbox Specification

## 1. Goal

Add a PostgreSQL-backed email outbox that replaces best-effort in-memory email delivery for release notifications.

Current-state confirmation emails are sent directly by the API Server through Email Provider Integration. This specification does not require moving confirmation emails into the outbox in the first implementation, although the data model keeps a generic `type` field so the same mechanism can support them later.

The target behavior is:

- Scanner detects a new GitHub release.
- Scanner creates durable email delivery records before marking the release as processed.
- Sender workers claim pending records from PostgreSQL.
- Sender sends emails through the email provider.
- Sender marks records as sent only after provider success.
- Temporary failures are retried with backoff.
- Exhausted or permanent failures move to a final failed/dead-letter state.

This is the target-state change needed to satisfy at-least-once delivery. Duplicate emails are acceptable; silent loss is not.

## 2. Scope

In scope:

- Release notification emails.
- Durable storage of pending email deliveries.
- Retry metadata and backoff.
- Concurrent Sender workers.
- Safe ordering between release-state updates and durable notification creation.
- Operational visibility through status fields and basic metrics.

Out of scope for the first implementation:

- External message brokers.
- Full admin UI for dead-letter review.
- Exactly-once email delivery.
- Durable delivery for confirmation emails in the first outbox iteration.

## 3. Data Model

Add a table named `email_outbox`.

Suggested columns:

```sql
CREATE TYPE email_outbox_status AS ENUM (
    'pending',
    'processing',
    'sent',
    'retryable_failed',
    'permanent_failed'
);

CREATE TABLE email_outbox (
    id UUID PRIMARY KEY,
    type TEXT NOT NULL,
    status email_outbox_status NOT NULL DEFAULT 'pending',

    recipient_email TEXT NOT NULL,
    subject TEXT NOT NULL,
    html_body TEXT NOT NULL,
    text_body TEXT,

    repo_owner TEXT,
    repo_name TEXT,
    release_id TEXT,
    release_tag TEXT,
    unsubscribe_token TEXT,

    provider_message_id TEXT,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 8,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at TIMESTAMPTZ,
    locked_by TEXT,

    last_error_code TEXT,
    last_error_message TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at TIMESTAMPTZ
);
```

Required indexes:

```sql
CREATE INDEX idx_email_outbox_pending
    ON email_outbox (next_attempt_at, created_at)
    WHERE status IN ('pending', 'retryable_failed');

CREATE INDEX idx_email_outbox_processing_stale
    ON email_outbox (locked_at)
    WHERE status = 'processing';

CREATE UNIQUE INDEX idx_email_outbox_release_recipient_once
    ON email_outbox (type, recipient_email, repo_owner, repo_name, release_id)
    WHERE type = 'release_notification';
```

The unique release-recipient index prevents accidental duplicate outbox rows for the same release and subscriber. It does not guarantee exactly-once provider delivery, because a provider call can succeed while the local status update fails.

## 4. Scanner Transaction

When Scanner detects a new release for a repository:

1. Start a database transaction.
2. Lock the repository state row for update.
3. Re-check that the detected release is still newer than the stored `last_seen_release`.
4. Load confirmed subscribers for the repository.
5. Insert one `email_outbox` row per subscriber.
6. Update repository state to the detected release.
7. Commit.

The repository state must not be advanced before durable outbox rows are created. Otherwise the system can record the release as processed while losing the notification events.

Use `INSERT ... ON CONFLICT DO NOTHING` against `idx_email_outbox_release_recipient_once` so retries of the scanner transaction are idempotent.

## 5. Sender Claiming

Sender workers should claim messages in batches.

Suggested query shape:

```sql
WITH claimed AS (
    SELECT id
    FROM email_outbox
    WHERE status IN ('pending', 'retryable_failed')
      AND next_attempt_at <= now()
    ORDER BY next_attempt_at ASC, created_at ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE email_outbox e
SET status = 'processing',
    locked_at = now(),
    locked_by = $2,
    updated_at = now()
FROM claimed
WHERE e.id = claimed.id
RETURNING e.*;
```

`$1` is batch size. `$2` is a stable worker id, for example hostname plus process id.

## 6. Sending And Status Updates

For each claimed record:

- On provider success:
  - set `status = 'sent'`;
  - set `sent_at = now()`;
  - store `provider_message_id` if available;
  - clear `last_error_code` and `last_error_message`.

- On retryable failure:
  - increment `attempt_count`;
  - if `attempt_count < max_attempts`, set `status = 'retryable_failed'`;
  - set `next_attempt_at` using exponential backoff with jitter;
  - store error code/message;
  - clear `locked_at` and `locked_by`.

- On permanent failure:
  - set `status = 'permanent_failed'`;
  - store error code/message;
  - clear `locked_at` and `locked_by`.

Retryable errors include provider 5xx, network timeout, connection error, and 429/rate limit. Permanent errors include invalid recipient, provider-level rejected content, malformed request, or a missing required template field.

## 7. Backoff

Use exponential backoff with jitter.

Suggested schedule:

```text
attempt 1: 1 minute
attempt 2: 5 minutes
attempt 3: 15 minutes
attempt 4: 1 hour
attempt 5: 3 hours
attempt 6: 6 hours
attempt 7: 12 hours
attempt 8: permanent_failed
```

Add random jitter of +/-20% to avoid retry spikes.

If the provider returns `Retry-After`, use the later value between the computed backoff and provider-provided retry time.

## 8. Stale Processing Recovery

A Sender can crash after claiming a row and before updating status. Add a periodic recovery job:

```sql
UPDATE email_outbox
SET status = 'retryable_failed',
    next_attempt_at = now(),
    locked_at = NULL,
    locked_by = NULL,
    updated_at = now(),
    last_error_code = 'stale_processing_lock',
    last_error_message = 'Processing lock expired before sender completed'
WHERE status = 'processing'
  AND locked_at < now() - interval '15 minutes';
```

This makes claimed-but-unfinished messages eligible for retry.

## 9. API And Service Boundaries

Recommended internal interfaces:

```text
OutboxRepository.EnqueueReleaseNotifications(ctx, release, subscribers) error
OutboxRepository.ClaimPending(ctx, workerID, limit) ([]EmailDelivery, error)
OutboxRepository.MarkSent(ctx, id, providerMessageID) error
OutboxRepository.MarkRetryableFailure(ctx, id, failure) error
OutboxRepository.MarkPermanentFailure(ctx, id, failure) error
OutboxRepository.RequeueStaleProcessing(ctx, olderThan) error
```

Scanner should depend on `EnqueueReleaseNotifications`, not on Sender internals. Sender should depend on claim/update methods, not on Scanner internals.

## 10. Metrics

Expose at least:

- `email_outbox_pending_total`
- `email_outbox_processing_total`
- `email_outbox_sent_total`
- `email_outbox_retryable_failed_total`
- `email_outbox_permanent_failed_total`
- `email_outbox_attempts_total`
- `email_outbox_delivery_latency_seconds`
- `email_outbox_oldest_pending_age_seconds`

Alert when:

- oldest pending age exceeds expected delivery SLA;
- permanent failures appear;
- retryable failures grow continuously;
- stale processing rows are recovered.

## 11. Acceptance Criteria

Implementation is complete when:

- Release notification rows are inserted transactionally before repository state advances.
- Restarting the process after outbox insert does not lose pending notifications.
- Restarting the process after Sender claims a row eventually makes the row retryable again.
- Temporary provider failure schedules retry instead of dropping the message.
- Permanent provider failure ends in `permanent_failed`.
- Multiple Sender workers can run without sending the same pending row concurrently.
- Scanner retries do not create duplicate outbox rows for the same release and recipient.
- Basic metrics expose queue depth, failures, attempts and oldest pending age.
