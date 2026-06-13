# ADR 006: Message Broker Selection for API-to-Notifications Events

**Date:** 2026-06-13  
**Status:** Accepted

---

## Context

The application is split into two runtime services:

- **API service** owns subscriptions, release scanning, and the transactional
  outbox.
- **Notifications service** consumes subscription and release events, creates
  notification jobs, and sends emails through the email provider integration.

The service boundary is asynchronous. The API service must not call the
notifications service synchronously, because email delivery has different
latency, retry, and failure characteristics.

The broker between the services should support:

- durable publication of events after the API database transaction commits;
- at-least-once delivery to the notifications service;
- explicit acknowledgement after the consumer finishes processing;
- redelivery when the consumer crashes or returns an error;
- dead-letter or poison-message handling;
- simple local development and Go integration;
- enough replay capability to debug or add future consumers.

The previous implementation used a transactional outbox in Postgres and Redis
Streams as the transport. This ADR replaces that transport with a dedicated
messaging broker.

---

## Decision

Use **NATS JetStream** as the broker for API-to-notifications events.

The service interaction should remain event-driven:

```
[API service]
  write domain data + outbox event in Postgres
  outbox dispatcher publishes event to broker subject/topic

[Message broker]
  persists event
  tracks consumer delivery and acknowledgement state

[Notifications service]
  consumes event
  handles event idempotently
  ACKs only after successful processing
```

Suggested subject names:

```
events.subscriptions.subscription_requested
events.release_monitoring.release_detected
```

The transactional outbox remains part of the design. The broker does not replace
the outbox, because the outbox is what keeps database state changes and event
publication from drifting apart.

---

## Considered Options

### 1. Kafka

Kafka is a partitioned append-only event log. Producers publish records to
topics, consumers track offsets, and records are retained by time or size.

- **Pros:** very high throughput; strong replay model; independent consumer
  groups; good fit for analytics, stream processing, audit trails, and many
  downstream consumers.
- **Cons:** heavier developer experience for a two-service notification flow;
  topic partitioning, offset commits, rebalances, retention, and schema
  evolution become central concerns; local setup and tests are more complex.

Kafka is a good choice if the system wants a long-lived event backbone. It is
more than this service boundary currently needs.

### 2. RabbitMQ

RabbitMQ is a message broker built around exchanges, queues, routing keys, and
consumer acknowledgements.

- **Pros:** excellent work-queue semantics; explicit ACK/NACK; mature retry and
  DLQ patterns; easy to understand for command-style messages like "send this
  notification".
- **Cons:** replay is not a native first-class model after messages are ACKed;
  adding future consumers requires extra queue/binding design; exchange/routing
  features are mostly unused if the system only needs one event stream between
  two services.

RabbitMQ is a strong safe choice for task delivery. It is less natural if the
messages are domain events that may later be consumed by more services.

### 3. Redis Streams

Redis Streams provide append-only streams, consumer groups, pending-entry lists,
ACK, and idle message claiming.

- **Pros:** already present in the current stack; simple Go client; consumer
  groups provide at-least-once delivery; `XAUTOCLAIM` can recover messages from
  crashed consumers; easy to run locally.
- **Cons:** Redis is primarily an in-memory data store, not a dedicated durable
  event broker; durability depends on Redis persistence configuration
  (`AOF`, `RDB`, replication) and can still lose recent messages under common
  snapshot-only setups; trimming policies can remove history; DLQ and poison
  handling are application-owned.

Redis Streams are acceptable for a small project and are already implemented in
ADR 005. For the target broker decision, weak or configuration-dependent
durability is a nay: notification events should survive broker restarts and
crashes with clearer guarantees than a cache-oriented dependency usually
provides by default.

### 4. NATS JetStream

NATS is a messaging system. JetStream adds persistence, streams, durable
consumers, replay, ACKs, redelivery, and retention policies.

- **Pros:** durable pub/sub model fits domain events; explicit ACK and
  redelivery fit notification processing; streams and consumers are separate,
  so future services can consume the same event stream independently; simpler
  developer experience than Kafka; less routing ceremony than RabbitMQ; strong
  Go ecosystem.
- **Cons:** adds a new infrastructure dependency if Redis is still needed for
  caching; less common than Kafka/RabbitMQ in some teams; stream and consumer
  configuration still needs ownership and tests.

NATS JetStream is the best fit for this boundary because it combines
event-stream semantics with queue-like consumption.

---

## Why NATS JetStream Fits Best

The API-to-notifications boundary is event-shaped, but the notifications service
needs queue-shaped processing guarantees.

Event-shaped means:

- the API service publishes facts that already happened;
- future services may consume the same facts independently;
- events should be replayable while retained.

Queue-shaped means:

- notifications must process each event at least once;
- processing success must be explicitly ACKed;
- failed or abandoned messages must be redelivered;
- poison messages must not block the whole consumer indefinitely.

Kafka strongly satisfies the event-log side, but adds more partition and offset
management than this flow needs. RabbitMQ strongly satisfies the work-queue side,
but replay and independent future consumers are less central to its model.
Redis Streams were close enough for the previous implementation, but the durability
story is weaker unless Redis is configured and operated as a durable data store.

NATS JetStream sits in the middle:

```
subject publish -> persisted stream -> durable consumer -> explicit ACK
```

That model matches this system without introducing Kafka's full event-platform
complexity or RabbitMQ's routing-heavy queue topology.

---

## Consequences

**Positive**

- The broker is a dedicated messaging component rather than a cache dependency
  reused as a queue.
- Notifications get durable consumers, explicit ACK, retry/redelivery, and
  replay within retention.
- Future consumers can be added without changing the notifications consumer.
- The API service remains decoupled from notification delivery latency and email
  provider failures.
- The transactional outbox continues to protect publication after database
  commits.

**Negative / trade-offs**

- NATS JetStream adds a new runtime dependency if Redis remains in use for
  GitHub release caching.
- Redis Streams publisher and consumer implementations remain in the codebase
  temporarily for historical tests, but runtime services use JetStream.
- Consumer behavior must be tested around ACK, retry, max delivery, and
  idempotent event handling.
- The system still provides at-least-once delivery, not exactly-once delivery;
  handlers must remain idempotent.

**Rejected for now**

- Kafka is rejected because the system does not currently need a full
  partitioned event log.
- RabbitMQ is rejected because the system does not need complex routing and may
  benefit from event replay and independent future consumers.
- Redis Streams are rejected as the target broker because broker durability is
  configuration-dependent and weaker than the desired target guarantee.
