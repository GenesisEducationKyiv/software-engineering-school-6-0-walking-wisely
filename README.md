# GitHub Release Notifications API

A monolithic Go service that lets users subscribe to email notifications whenever a watched GitHub repository ships a new release.

Built for **Software Engineering School 6.0** (Genesis Academy).

---

## Features

| Requirement | Status |
|---|---|
| REST API matching the provided Swagger contract | Done |
| Single monolith (API + Scanner + Notifier) | Done |
| PostgreSQL with automatic migration on startup | Done |
| Dockerfile + docker-compose.yml | Done |
| Background scanner with `last_seen_tag` deduplication | Done |
| GitHub repo validation on subscribe (404 / 400) | Done |
| GitHub 429 / 403 rate-limit handling | Done |
| Unit tests on business logic | Done |
| **Extra:** HTML subscription page at `/` | Done |
| **Extra:** gRPC interface (gRPC-Gateway bridges to REST) | Done |
| **Extra:** Redis caching of GitHub API responses (TTL 10 min) | Done |
| **Extra:** Prometheus metrics at `/metrics` | Done |
| **Extra:** GitHub Actions CI (lint + tests on every push/PR) | Done |

---

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                        Monolith                         │
│                                                         │
│  ┌──────────────┐   gRPC-Gateway   ┌────────────────┐  │
│  │  HTTP :8080  │ ◄──────────────► │  gRPC :9090    │  │
│  │  (REST + UI) │                  │ (SubscribeSvc) │  │
│  └──────────────┘                  └────────────────┘  │
│                                                         │
│  ┌──────────────────────────────────────────────────┐  │
│  │  Scanner worker (ticker)                         │  │
│  │  - polls all confirmed repos every SCANNER_INTERVAL│  │
│  │  - pushes EmailMessage → buffered channel        │  │
│  └──────────────────────────────────────────────────┘  │
│                                                         │
│  ┌──────────────────────────────────────────────────┐  │
│  │  Sender worker                                   │  │
│  │  - drains channel, batches up to 100 emails      │  │
│  │  - flushes every RESEND_MAX_WAIT (default 200 ms)│  │
│  └──────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
         │                         │
   PostgreSQL 16              Redis 8
```

### Key design decisions

**gRPC-Gateway as the single source of truth for routing.**
The service definition lives in `proto/subscription/v1/subscription.proto`. `buf generate` produces the gRPC server stubs and the HTTP gateway simultaneously, so the REST and gRPC contracts are always identical and in sync — no separate routing code needed.

**Scanner ↔ Sender decoupled via a buffered channel.**
The scanner never calls the email API directly. It pushes `domain.EmailMessage` values into a buffered channel (`EMAIL_CHANNEL_SIZE`, default 1 000) using a non-blocking `select` with a `default` drop case. A temporary Resend outage therefore cannot block or slow down the scan loop — the worst outcome is a logged warning that a notification was dropped, not a stalled goroutine.

**Email batching and back-pressure.**
The sender worker accumulates messages from the channel and flushes them in chunks of up to 100 (Resend's batch limit) using two triggers: a ticker fires every `RESEND_MAX_WAIT` (default 200 ms) so messages are not held indefinitely, and an immediate flush fires whenever the buffer reaches 100 entries so a burst of notifications is delivered without waiting for the ticker. This keeps the number of HTTP calls to Resend proportional to the number of releases detected, not the number of subscribers. On shutdown the sender drains the channel with a 15-second deadline before exiting, so in-flight notifications are not silently lost on a rolling restart.

**Per-repo `last_seen_tag` instead of per-subscriber.**
A single `UPDATE ... WHERE repo=$1 AND confirmed=TRUE` stamps the current release on every confirmed subscriber for a repo at once. This is O(1) in SQL rather than N individual updates, and it correctly handles the case where a new subscriber joins mid-cycle — they will see the next release, not the current one.

**HMAC-SHA256 tokens for confirmation and unsubscribe.**
Every token is `hex(HMAC-SHA256(secret, 16-byte random nonce))`. The HMAC binds the token to the application secret, so an attacker who learns the algorithm (or the token format) still cannot forge a valid one without the secret. Tokens are stored in unique-indexed columns and looked up with `SELECT FOR UPDATE` to prevent concurrent double-confirms.

**Redis caching with 10-minute TTL.**
The GitHub client checks Redis before every API call. On a cache hit the result is returned immediately without touching GitHub's rate-limit budget. This is critical for repos with many subscribers: without caching, one scan cycle would cost one GitHub API call per subscriber; with caching it costs at most one per repo per 10 minutes.

**Exponential-backoff startup retries for Postgres and Redis.**
Docker Compose health checks prevent the app from starting until the databases are ready, but network timing in cloud environments can still cause transient failures. Both `InitDB` and `InitRedis` retry with exponential back-off (configurable via `DB_RETRY_*` / `REDIS_RETRY_*` env vars).

**Graceful shutdown.**
On `SIGINT` / `SIGTERM`:
1. The context is cancelled, stopping the scanner and sender loops.
2. The gRPC server is given time to finish in-flight RPCs (`GracefulStop`).
3. The HTTP server shuts down with a 20-second deadline.
4. The sender drains the email channel with a 15-second deadline before exiting.

---

## API

The full machine-readable contract is at `/swagger.json` when the server is running.

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/subscribe` | Subscribe an email to a repo's releases |
| `GET` | `/api/confirm/{token}` | Confirm a subscription (link sent by email) |
| `GET` | `/api/unsubscribe/{token}` | Unsubscribe (link in every notification email) |
| `GET` | `/api/subscriptions?email=` | List all subscriptions for an email |
| `GET` | `/health` | Health check — returns `{"status":"ok"}` |
| `GET` | `/metrics` | Prometheus metrics |
| `GET` | `/swagger.json` | OpenAPI 3 spec |
| `GET` | `/` | HTML subscription page |

### Prometheus metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `http_requests_total` | Counter | `method`, `path`, `status` | Total HTTP requests handled |
| `http_request_duration_seconds` | Histogram | `method`, `path` | Request latency (default buckets) |
| `email_channel_depth` | Gauge | — | Number of pending emails currently buffered in the send queue |

`email_channel_depth` is the primary operational signal for the sender pipeline — a value consistently near `EMAIL_CHANNEL_SIZE` means the sender cannot keep up with the scanner and notifications may start being dropped.

### Error responses

| HTTP | gRPC | Cause |
|---|---|---|
| 400 | `INVALID_ARGUMENT` | Malformed email, invalid `owner/repo` format, or bad token |
| 404 | `NOT_FOUND` | Repo not found on GitHub, or token not found |
| 409 | `ALREADY_EXISTS` | Email already has a confirmed subscription to this repo |
| 503 | `UNAVAILABLE` | GitHub rate limit hit — response includes `Retry-After` |

---

## Live instance

The API is publicly available at **https://genesis-api.ivan-dutov.com**.

---

## Quick start with Docker

```bash
# 1. Copy and fill in your secrets
cp .env.example .env
# Required: DATABASE_URL, REDIS_URL, RESEND_API_KEY,
#           EMAIL_SECRET_KEY, BASE_URL, FROM_EMAIL

# 2. Start everything
docker compose up --build
```

The REST API is available at `http://localhost:8080` and the gRPC server at `localhost:9090`.

---

## Local development

**Prerequisites:** Go 1.25+, `buf` CLI, Docker (for Postgres + Redis).

```bash
# Install tooling (buf, lefthook, goimports)
make setup

# Start only the backing services
docker compose -f docker-compose.dev.yml up -d

# Copy and fill in the dev env
cp .env.example .env
# Set DATABASE_URL and REDIS_URL to the local addresses printed by docker compose

# Run with live reload (requires air)
air

# Or run directly
go run ./cmd/server
```

### Running tests

```bash
go test -race -count=1 ./...
```

### Regenerating protobuf code

```bash
buf generate
```

---

## Configuration reference

All configuration is done through environment variables. See `.env.example` for a complete annotated list.

| Variable | Default | Description |
|---|---|---|
| `REST_PORT` | `8080` | HTTP / REST port |
| `GRPC_PORT` | `9090` | gRPC port |
| `DATABASE_URL` | — | PostgreSQL DSN (required) |
| `REDIS_URL` | — | Redis URL (required) |
| `RESEND_API_KEY` | — | [Resend](https://resend.com) API key (required) |
| `FROM_EMAIL` | — | Sender address (required) |
| `EMAIL_SECRET_KEY` | — | HMAC secret for token generation (required) |
| `BASE_URL` | — | Public URL used in email links, e.g. `https://your-domain.com` (required) |
| `GITHUB_TOKEN` | _(empty)_ | Personal access token — raises rate limit from 60 to 5 000 req/h |
| `SCANNER_INTERVAL` | `5m` | How often to check repos for new releases |
| `RESEND_MAX_WAIT` | `200ms` | Max time to buffer emails before flushing a batch |
| `EMAIL_CHANNEL_SIZE` | `1000` | Buffered channel size between scanner and sender |

---

## Project structure

```
cmd/server/
  main.go           — wiring: config, DB, Redis, workers, HTTP + gRPC servers
  web/index.html    — embedded HTML subscription page

internal/
  config/           — env loading, DB + Redis init with retry
  domain/           — Subscription model, EmailMessage, token generation, error types
  clients/
    github.go       — GitHub REST client with Redis cache and rate-limit handling
    resend.go       — Resend batch email client
  http/
    handlers/       — gRPC service implementation (Subscribe, Confirm, Unsubscribe, GetSubscriptions)
    middleware/     — structured logging, panic recovery, Prometheus metrics
  repository/
    subscription.go — all SQL queries (pgx/v5 connection pool)
    migrations/     — embedded SQL migration files
  workers/
    scanner.go      — periodic release poller
    sender.go       — batching email dispatcher

proto/subscription/v1/
  subscription.proto  — single source of truth for the service contract

gen/subscription/v1/  — generated gRPC + HTTP gateway code (do not edit by hand)

.github/workflows/
  ci.yml            — lint + test on every push/PR
```

---

## Tech stack

- **Language:** Go 1.25
- **HTTP / gRPC:** `net/http` stdlib + `google.golang.org/grpc` + `grpc-gateway/v2`
- **Protobuf:** `buf` toolchain
- **Database:** PostgreSQL 16 via `pgx/v5`
- **Migrations:** `golang-migrate/migrate`
- **Cache:** Redis 8 via `go-redis/v9`
- **Email:** [Resend](https://resend.com) batch API
- **Metrics:** Prometheus (`prometheus/client_golang`)
- **Container:** Docker multi-stage build (buf → Go builder → Alpine runtime)
