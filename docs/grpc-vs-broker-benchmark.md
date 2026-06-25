# gRPC vs Message Broker -- Transport Benchmark

Benchmarks a **synchronous gRPC** transport against the current **broker** (NATS
JetStream behind `events.Publisher`) for the API->Notifications saga. This file
holds both the methodology and the running log of measurements.

---

## Goal & scope

Make the API->Notifications saga transport pluggable, then benchmark gRPC vs the
broker for the one message where the comparison is meaningful. Results are
captured as reproducible tool output (`benchstat` / ghz / vegeta); Grafana is out
of scope.

- **In scope -- one message.** The `SendConfirmationEmail` command (API ->
  Notifications). It is request/response-shaped -- the API asks Notifications to
  record a confirmation job and the ack is fast (DB insert, no email send) -- so
  it maps cleanly to a gRPC unary call.
- **Out of scope -- the replies.** `ConfirmationEmailSent` / `ConfirmationEmailFailed`
  (Notifications -> API) stay on the broker. They are pure async signals fired
  after slow, retried provider work, with no synchronous caller, so a unary gRPC
  mapping would be semantically wrong and the comparison not fair.

Both transports share the same outbox dispatcher; the gRPC path is just a second
`events.Publisher` impl, so the outbox's atomicity and at-least-once retry stay
unchanged. Delivery semantics are kept equal: a non-OK gRPC status leaves the
outbox row undelivered and retries next poll, same at-least-once as the broker
ack, with idempotency via `eventID` in `RecordConfirmation`.

## The thesis -- what the benchmark demonstrates

The axis is **not durability** (the outbox covers both) but **latency vs
decoupling / backpressure / fan-out**. The two transports differ on what the
producer depends on:

|                              | NATS (broker)       | gRPC (direct)                             |
| ---------------------------- | ------------------- | ----------------------------------------- |
| Producer depends on          | event + broker      | callee's typed contract                   |
| Ack semantics                | broker persisted    | callee handler ran + returned (app-level) |
| Contract checking            | runtime JSON decode | compile-time (proto)                      |
| Latency                      | broker hop          | direct, lower                             |
| Fan-out / replay / buffering | native              | none -- build it yourself                 |
| Availability coupling        | decoupled           | callee must be up                         |

So **NATS = decouple / buffer / fan-out / replay, paid for in latency + eventual
consistency; gRPC = direct / fast / typed / immediate ack, paid for in coupling +
self-built backpressure.** NATS is consumer-paced pull; gRPC is producer-paced
push. The benchmark puts numbers on that tradeoff.

## Methodology -- two layers

### Layer A -- transport micro-bench (the headline)

Drive each `events.Publisher` impl directly in-process (`testing.B`), identical
payload, comparing latency (p50/p95/p99) and allocations via `benchstat`. This is
the clean **transport-overhead** number, **handler excluded**: the in-proc gRPC
server is a stub that returns `Ack` without the `RecordConfirmation` insert, and
the NATS side returns after JetStream persists.

The two acks are intentionally asymmetric, and that is the point:

- **gRPC** -- `Publish` returns after the in-proc server replied: a transport-only
  loopback round-trip.
- **NATS** -- `Publish` returns after JetStream persisted the message to disk:
  network + durable broker write. The handler runs later on the consumer side, off
  the hot path.

Label them accordingly when reading: in-proc gRPC = "publish-to-ack (transport
only)", NATS = "publish-to-ack (broker persisted)".

`testing.B` cannot model unbounded push against a bounded consumer (`RunParallel`
stays under the publisher semaphore, so backpressure never triggers). The collapse
/ recovery scenario therefore runs **over the wire**:

- **gRPC push:** ghz against a slow-consumer build, sweeping concurrency past the
  server's service capacity -- capturing RPS, p99, error-rate as the push path
  amplifies the incident.
- **NATS pull:** the same slow consumer draining an outbox backlog -- pull-pacing
  is expected to degrade gracefully where naive gRPC push collapses.

### Layer B -- end-to-end (sanity only)

`POST /subscribe` -> confirmation job recorded over the whole path. The transport
delta is diluted by HTTP + DB, so this is a sanity check, not the headline.

### Synthetic consumer service-time -- a controlled knob

The collapse needs the consumer to have **bounded capacity**, but the real handler
is a fast DB insert that rarely saturates. We do **not** model this with real email
work (the email send is on the out-of-scope async reply path and would conflate
transport latency with handler cost). Instead a fixed, tunable synthetic
service-time is injected into the `SendConfirmation` handler, identical for both
transports:

- knob = 0 -> pure transport micro-bench (Layer A headline stays clean);
- knob > 0 -> models a slow/recovering consumer, swept to expose where push
  amplifies the incident vs pull pacing it.

The synthetic cost has no production meaning, so it lives behind a compile-time
boundary (test wiring / a bench-only build), never in the shipped binary. The
`SAGA_TRANSPORT={nats|grpc}` flag and the noop sinks, by contrast, are legitimate
runtime adapters that do ship.

---

## Environment

Everything below ran on a single laptop:

- `goos: windows  goarch: amd64`
- CPU: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz (8 logical)
- Go 1.25.5
- NATS: JetStream in Docker (`docker compose up -d`), over loopback.

Commands:

```powershell
go test -bench '^BenchmarkPublish_GRPC' -benchmem -run '^$' ./internal/subscriptions/notify/
$env:NATS_URL = "nats://localhost:4222"
go test -bench '^BenchmarkPublish_NATS' -benchmem -run '^$' ./internal/subscriptions/notify/
```

## Layer A -- transport micro-bench

A single run on an idle machine, best-case and noisy -- superseded by the
`-count=10` numbers below, kept here only to show why a single sample misleads:

| Benchmark                        | ns/op   | µs/op  | B/op  | allocs/op | iters |
| -------------------------------- | ------- | ------ | ----- | --------- | ----- |
| `BenchmarkPublish_GRPC`          | 192758  | 192.8  | 10146 | 157       | 5805  |
| `BenchmarkPublish_GRPC_Parallel` | 67078   | 67.1   | 10116 | 142       | 16353 |
| `BenchmarkPublish_NATS`          | 3218869 | 3218.9 | 2849  | 31        | 435   |
| `BenchmarkPublish_NATS_Parallel` | 410327  | 410.3  | 2856  | 31        | 2457  |

Putting the two side by side:

|                  | gRPC    | NATS     | NATS / gRPC |
| ---------------- | ------- | -------- | ----------- |
| Serial latency   | 192.8µs | 3218.9µs | **16.7x**   |
| Parallel latency | 67.1µs  | 410.3µs  | **6.1x**    |
| B/op             | 10146   | 2849     | 0.28x       |
| allocs/op        | 157     | 31       | 0.20x       |

---

## Reading the first run

On these numbers gRPC wins latency by a wide margin: serial publish-to-ack is
~16.7x faster (193µs vs 3.22ms), and ~6.1x faster under concurrency (67µs vs
410µs).

The caveat is what's being timed, because the two acks are not the same thing:

- **gRPC's** ack is an in-proc loopback round-trip into a stub handler that does
  **no insert** -- so it's "publish-to-ack, transport only".
- **NATS's** ack only comes back after JetStream has **persisted the message to
  disk**. That fsync-class durable write is where most of the 3.22ms goes; the
  handler runs later, off the hot path.

So the gRPC number excludes work NATS pays up front (the durable buffer), and the
NATS number excludes work gRPC pays later over the wire (the real handler insert,
which Layer B picks up). Both serial latencies also drop sharply under concurrency
(gRPC 2.9x, NATS 7.8x), so both transports pipeline well.

On allocations the broker wins. NATS marshals into a compact subject payload --
2849 B / 31 allocs, flat across serial and parallel. gRPC carries ~3.5x the bytes
and ~5x the allocs (10KB / ~150): the generated proto message plus the grpc-go
call machinery (metadata, streams, status) on every call. For GC pressure per
message the broker path is cheaper.

The tradeoff: gRPC buys low latency and an immediate, typed, app-level ack, at the
cost of per-call allocation and availability coupling (the callee must be up).
NATS buys durability, temporal decoupling, and buffering/fan-out, at the cost of a
~3ms persist-to-ack and eventual consistency. Layer A confirms the thesis above:
the axis is **latency vs decoupling/durability**, not "one is faster". The real
divergence -- push collapse vs pull pacing -- is Layer B.

## Layer A, `-count=10` on a quiesced host (the headline)

This is the result. Re-run `-count=10` with the noisy stack down (only NATS,
Postgres, Redis left up) and passed through `benchstat`, so each figure carries a
confidence interval. Means ± CI:

| Benchmark         | gRPC          | NATS          | NATS / gRPC |
| ----------------- | ------------- | ------------- | ----------- |
| serial (sec/op)   | 393.8µs ± 12% | 1.087ms ± 30% | **~2.8x**   |
| parallel (sec/op) | 75.63µs ± 8%  | 203.2µs ± 8%  | **~2.7x**   |
| B/op              | 9.97 KiB ± 1% | 2.78 KiB ± 0% | 0.28x       |
| allocs/op         | 157 ± 0%      | 31 ± 0%       | 0.20x       |

**gRPC is ~2.7x lower latency than NATS broker-persist.** The serial NATS number
still carries a wide ±30% CI -- the durable fsync-class write is inherently variable
-- so treat the serial ratio as 2.5–3x, not a precise 2.8x. Parallel is tighter
(both ±8%). The allocation gap is the rock-solid number: NATS ~5x fewer allocs and
~3.6x fewer bytes, flat at 31 allocs across every sample (±0%). Same ack-asymmetry
caveat applies (gRPC = transport-only stub, NATS = durable persist).

Raw `benchstat` columns:

```
                         grpc           nats          (sec/op)
Publish_GRPC-8           393.8µ ± 12%
Publish_GRPC_Parallel-8  75.63µ ±  8%
Publish_NATS-8                          1.087m ± 30%
Publish_NATS_Parallel-8                 203.2µ ±  8%
```

## Layer B -- over-the-wire gRPC (ghz, real handler + insert)

No stub here. `scripts/bench-grpc-direct.ps1` runs ghz against
`cmd/notifications-bench` (built `-tags bench`), where `SendConfirmation` calls
`RecordConfirmation` and inserts into Postgres. Seeded one subscription
`11111111-…-1`, each request gets a unique `event_id`.

Run parameters:

| Param        | Value          |
| ------------ | -------------- |
| host         | localhost:9091 |
| rps (target) | 200            |
| total        | 5000           |
| concurrency  | 20             |

| Metric    | Value                         |
| --------- | ----------------------------- |
| status    | **OK 5000 / 5000** (0 errors) |
| rps (eff) | 199.8                         |
| average   | 30.66ms                       |
| fastest   | 12.66ms                       |
| slowest   | 205.96ms                      |
| p50       | 19.23ms                       |
| p75       | 33.14ms                       |
| p90       | 63.55ms                       |
| p95       | 83.69ms                       |
| p99       | 139.14ms                      |

Clean run, zero errors after the schema/FK wiring fix. This is the
**handler-inclusive** publish-to-ack (transport + DB insert), so latency sits an
order of magnitude above the Layer A transport-only gRPC numbers (193µs serial /
67µs parallel). That gap is the `RecordConfirmation` Postgres write -- the cost
Layer A excludes by design. The long right tail (p50 19ms => p99 139ms, slowest
206ms) at only 20 concurrency / 200rps is not transport; it's DB plus lock
contention on the unique confirmation index of the single seeded subscription.
The collapse sweep follows.

## Layer B -- collapse sweep (ghz, slow consumer, unthrottled push)

`scripts/bench-grpc-slow.ps1` runs ghz wide open (`rps=0`) against
`cmd/notifications-bench` with `BENCH_SERVICE_TIME=5ms` to model a bounded
consumer, sweeping client concurrency. This is the push-collapse scenario the
in-proc Layer A cannot show -- the publisher semaphore never saturates under
`RunParallel`.

`total=10000`, `rps=0`:

| concurrency | rps | count | errors | p50_ms  | p95_ms   | p99_ms   | avg_ms  |
| ----------- | --- | ----- | ------ | ------- | -------- | -------- | ------- |
| 10          | 474 | 10000 | 0      | 17.517  | 37.907   | 64.496   | 20.532  |
| 50          | 544 | 10000 | 0      | 85.281  | 160.886  | 243.921  | 91.092  |
| 200         | 520 | 10000 | 0      | 348.348 | 595.266  | 724.585  | 380.748 |
| 500         | 550 | 10000 | 0      | 853.986 | 1116.026 | 1133.689 | 887.587 |

This is the collapse, and it shows up as latency, not errors.

- **Throughput plateaus.** RPS stays flat at ~474–550/s across a 50x concurrency
  increase (10=>500). The slow consumer's capacity is the ceiling; extra client
  concurrency buys zero extra goodput.
- **Latency grows linearly with concurrency.** p50 17.5ms => 854ms (~49x), p99
  64ms => 1134ms (~18x). Classic Little's Law: once throughput is saturated, every
  extra in-flight request deepens the queue, so wait time tracks offered
  concurrency.
- **Zero errors -- the dangerous part.** Naive gRPC push has no backpressure. The
  server accepts and queues everything, so nothing is rejected (ghz's 20s timeout
  never fires). The incident is invisible as an error rate and surfaces only as
  unbounded latency. A real caller with a sub-second deadline would start timing
  out around c=200–500 -- a latency cliff masquerading as "healthy, 0 errors".

Contrast with the broker, which is the thesis. NATS pull paces to the same
consumer capacity, but the backlog absorbs into the stream instead of inflating
caller-visible latency: publish-to-ack stays flat (broker persist), queue depth
grows, and it drains when load subsides. gRPC pushes the queue onto the caller's
latency budget; NATS holds it in a durable buffer. Same bounded consumer, opposite
failure mode -- latency cliff vs graceful backlog. (NATS drain-curve counterpart:
the outbox-drain poll in `scripts/bench-transport.ps1`.)

## Layer B -- end-to-end HTTP (vegeta over POST /api/subscribe, both transports)

`scripts/bench-transport.ps1`: steady 50/s for 60s, then a 500/s spike for 10s,
against the full stack (`api` + the real `notifications` service), then it polls
`outbox_pending_count` while the backlog drains. This is a sanity layer -- the
transport delta is diluted by HTTP, the DB, and the shared outbox dispatcher, so
it is deliberately **not** the headline (Layer B is sanity-only by design).

One run per transport:

### Request latency (vegeta)

| Phase  | Metric     | gRPC     | NATS     |
| ------ | ---------- | -------- | -------- |
| steady | success    | 100%     | 100%     |
| steady | p50        | 12.3ms   | 13.6ms   |
| steady | p95        | 49.5ms   | 98.0ms   |
| steady | p99        | 82.9ms   | 282.2ms  |
| steady | max        | 207.9ms  | 488.4ms  |
| spike  | success    | 100%     | 100%     |
| spike  | throughput | 448/s    | 358/s    |
| spike  | p50        | 343.4ms  | 3445.0ms |
| spike  | p95        | 1121.9ms | 4159.6ms |
| spike  | p99        | 1230.9ms | 4242.5ms |
| spike  | max        | 1259.1ms | 4356.1ms |

### Outbox drain after the spike (pending count, 30s poll)

| t   | gRPC pending       | NATS pending       |
| --- | ------------------ | ------------------ |
| 0s  | 4904               | 4968               |
| 10s | 4296               | 4104               |
| 20s | 3656               | 3432               |
| 30s | 3048 (not drained) | 2600 (not drained) |

Drain rate is roughly **62/s (gRPC)** vs **~79/s (NATS)**; neither cleared the ~5k
spike backlog within 30s.

Why this is sanity-only, and what it still shows:

- **Drain rate is nearly transport-independent here, by construction.** Both paths
  share the same outbox dispatcher (batch 32 every 200ms) feeding a fast consumer
  (real `RecordConfirmation` insert, no synthetic knob). The backlog bleeds off at
  roughly the dispatcher's pace either way (~60–80/s) -- the curves are close
  because the bottleneck is the shared dispatcher + DB, not the transport hop. A
  clean transport-recovery contrast needs a bounded consumer, i.e. the over-wire
  collapse sweep above, not this run.
- **Spike request latency does diverge -- gRPC ~3.4x lower (p99 1.23s vs 4.24s),
  +25% throughput (448 vs 358/s).** The `POST /subscribe` handler returns after the
  DB write, but under the 500/s spike the two builds contend differently: the gRPC
  dispatcher does a direct unary call, the NATS dispatcher round-trips JetStream
  persistence, and that extra broker write amplifies under load into higher tail
  latency on the request path sharing the same DB/CPU. Directionally consistent
  with Layer A (gRPC lower latency), but single-run and noisy -- corroborating, not
  authoritative.

**Across layers.** The headline is Layer A (transport-only: gRPC ~2.7x lower
latency quiesced, NATS ~5x fewer allocs) plus the collapse sweep (gRPC
push degrades into a silent latency cliff at 0 errors; broker pull would pace).
The end-to-end HTTP layer agrees on direction (gRPC lower latency) but, with a fast
shared consumer, shows near-equal drain -- diluted sanity, as the methodology predicted.

## Caveats

Keep in mind before trusting any of this:

- Single host, loopback -- no real network RTT, which would shrink gRPC's relative
  edge.
- The gRPC server stub skips the `RecordConfirmation` insert by design; the
  over-wire Layer B run includes it.
- Layer A is `-count=10` + `benchstat` on a quiesced host. The
  Layer B runs are still single-run with no variance bars -- treat them as
  directional, not final.
- The Layer B HTTP drain used a _fast_ consumer (no synthetic knob), so its drain
  curve does not isolate the transport -- use the over-wire collapse sweep for that.
