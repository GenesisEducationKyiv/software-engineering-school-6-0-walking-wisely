# ADR 005: Extract Notifications as a Separate Microservice via Redis Streams

**Date:** 2026-06-09  
**Status:** Superseded by ADR 006

---

## Context

The application started as a single-binary monolith with four internal domains:
`subscriptions`, `release_monitoring`, `notifications`, and `integrations`.

The `notifications` domain has three characteristics that make it a natural extraction candidate:

1. **Already event-driven at the seam.** The domain consumes `SubscriptionRequested`
   and `ReleaseDetected` events and has no synchronous callers — it is already
   logically decoupled. The coupling was only physical (same process).

2. **Different external dependency.** Notifications owns the Resend API client and
   email delivery retry logic. Isolating it means transient email-provider failures
   (rate limits, timeouts) cannot degrade the subscription or release-scanning paths.

3. **Independent scaling profile.** Release scanning is a cron-driven burst;
   notification delivery is sustained and may need independent horizontal scaling
   as subscriber counts grow.

The internal event bus (`platform/events.Bus`) previously connected the outbox
dispatcher to the notification handlers inside the same process. Extracting
`notifications` into its own binary requires that bus crossing to become network I/O.

---

## Decision

Extract `cmd/notifications` as a standalone binary. Use **Redis Streams** as the
async message bus between the API service and the notifications service.

### Message flow

```
[API service]
  write domain event → outbox (Postgres)
  outbox dispatcher polls → XADD to Redis Stream "events"

[Notifications service]
  XREADGROUP from Stream "events" (consumer group: "notifications")
  → in-process event bus → notification handlers
  → write notification_jobs (Postgres)
  → sender worker → Resend API
```

### Why Redis Streams over alternatives

| Option | Reason rejected / accepted |
|--------|---------------------------|
| **In-process bus** (status quo) | Requires both domains in the same process — defeats extraction. |
| **Kafka** | Operationally heavy; no existing cluster; adds a new stateful service and ZooKeeper/KRaft for a two-service system. |
| **RabbitMQ** | Same operational overhead as Kafka; adds a new broker service. |
| **Redis Pub/Sub** | Fire-and-forget — no consumer groups, no persistence, no at-least-once guarantee. A restarting notifications service would miss messages. |
| **Redis Streams** | Already in the stack (used for GitHub release caching). Consumer groups provide at-least-once delivery. Idle message reclaim (XAUTOCLAIM) handles crashes without an external dead-letter queue. Trim via MAXLEN keeps memory bounded. |

### Delivery guarantees

- **At-least-once** end-to-end: the outbox ensures the event reaches the stream
  even if the API process crashes before XADD; the stream retains messages until
  ACKed; the notification handlers are idempotent via the `event_deliveries` table.
- Messages are ACKed only after the handler returns without error; failed messages
  remain in the PEL and are reclaimed by XAUTOCLAIM after 5 minutes.
- Unknown event types (future domains) are ACKed immediately to prevent blocking.

### One codebase, two binaries

Both services live in the same Go module (`go.mod`). `cmd/server` and
`cmd/notifications` share `internal/platform` (events codec, Postgres pool,
Redis client, logger, metrics) and the `internal/notifications` domain package.
This avoids cross-module versioning overhead while the system is small and
owned by a single team.

---

## Consequences

**Positive**
- Email-provider failures are isolated to the notifications service and cannot
  degrade subscription creation or release scanning.
- Both services can be scaled and deployed independently.
- No new infrastructure: Redis was already a runtime dependency.
- The outbox-to-stream pattern preserves the existing at-least-once guarantee
  established in ADR 002.

**Negative / trade-offs**
- End-to-end latency increases by one network hop (XADD → XREADGROUP vs.
  in-process function call). At the throughput of this application this is
  negligible (sub-millisecond on the same host).
- Two services must be deployed and health-checked instead of one.
- The shared Postgres database remains a deployment coupling point. A future
  ADR may address schema ownership boundaries if the services diverge further.
- Redis is now a critical-path dependency for event delivery. Its existing
  persistence (`--save 60 1`) and the outbox safety net (events stay in Postgres
  until XADD succeeds) mitigate data loss risk.
