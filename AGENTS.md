# AGENTS.md

Project-level guidance for Claude Code and other agents working in this repo.

See @testing.md for the full testing reference.

## Module

`github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely`

This is a Go microservices project (subscriptions, notifications, release_monitoring)
with strict architectural boundaries between bounded contexts under `internal/`.

## Pre-commit checklist

Run these **in order** before every commit. Do not commit if any step fails.

### 1. Run all tests — they must all pass

```bash
make test-unit          # fast local feedback (go test ./...)
```

Before a commit that touches integration- or e2e-relevant code, also run the
heavier suites that CI runs:

```bash
make test-unit-strict   # unit only, integration tests filtered out
make test-integration   # Testcontainers (Docker required)
make test-e2e           # Playwright/Chromium (needs `make playwright-install` first)
```

**How to decide if a commit touches integration/e2e-relevant code.** Inspect the
staged changes — not just test files, but the production packages they cover.

```bash
git diff --cached --name-only
```

Treat the commit as integration/e2e-relevant if any staged path matches:

- `*_integration_test.go` or `*_e2e_test.go` — the tests themselves changed.
- files carrying a `//go:build integration` or `//go:build e2e` tag.
- `cmd/server/e2e/**` or `cmd/server/web/**` — Playwright page objects / served HTML.
- any production `.go` in a package that *has* an integration/e2e test — changing
  the code under test means its heavy suite must run. Find such packages:

```bash
# packages that contain integration/e2e tests
git ls-files '*_integration_test.go' '*_e2e_test.go' | xargs -n1 dirname | sort -u
```

  Compare that set against the dirs of your staged `.go` files
  (`git diff --cached --name-only -- '*.go' | xargs -n1 dirname | sort -u`); an
  overlap means run the heavy suite for that package.

- infra touched by Testcontainers: migrations (`internal/platform/postgres/migrations/**`),
  `docker-compose*.yml`, `Dockerfile`, or `proto/**` / `buf.*` (regenerated gRPC
  feeds the gateway integration tests — run `make generate` too).

When in doubt, run `make test-all`.

Or everything at once:

```bash
make test-all
```

Notes:
- Generated protobuf/gRPC lives in `gen/` (not committed). From a clean clone run
  `make generate` before testing.
- Integration tests use the `TestIntegration_` prefix + `*_integration_test.go`
  filename, gated by the `integration` build tag. E2E uses the `e2e` build tag.
- `-race` / `-count=1` are CI concerns; skip them in the local loop.

### 2. Lint the entire project before `git add`

Run golangci-lint across **all** packages first so you can see every file with
issues, then fix and stage (amend/add) those files in the same commit:

```bash
golangci-lint run ./...
```

(Version is pinned to `v2.11.4` — see `make setup`.) Fix the reported files,
re-run until clean, then add them to the commit.

### 3. Verify boundaries — healthy dependency graph

Bounded contexts must not leak into each other. After changes that touch
imports, confirm the dependency graph is still healthy with `go list`. Example —
list what a context depends on and check no foreign `internal/<context>` shows up:

```bash
# what does subscriptions import (own + platform/contracts only)?
go list -deps ./internal/subscriptions/... | grep walking-wisely

# reverse: who depends on a package (catch unexpected cross-context coupling)
go list -f '{{.ImportPath}}: {{.Imports}}' ./internal/...
```

A context (`subscriptions`, `notifications`, `release_monitoring`) may depend on
`internal/platform/*` and `internal/contracts/*`, but **not** on another
context's `internal/<other-context>/*`. If `go list` shows such an edge, the
boundary is broken — fix before committing.

## Repository layout

```
cmd/
  notifications/            notifications service entrypoint
  server/                   main server entrypoint
    e2e/                    Playwright e2e helpers / page objects
    web/                    index.html served to the browser
internal/
  contracts/                cross-context contracts (events, mail)
    events/
    mail/
  integrations/             external service adapters
    github/  github/redis/
    resend/
  subscriptions/            bounded context
    app/  domain/  grpc/  postgres/
  notifications/            bounded context
    app/  domain/  postgres/  worker/
  release_monitoring/       bounded context
    app/  domain/  postgres/  worker/
  platform/                 shared infra (no business logic)
    config/  events/  logger/  metrics/  outbox/  redis/  streams/
    http/middleware/
    postgres/  postgres/migrations/
proto/subscription/v1/      protobuf sources (generated -> gen/, not committed)
deploy/observability/       grafana dashboards, datasources
docs/  docs/adr/            documentation, architecture decision records
```

## Common commands

```bash
make setup                 # install toolchain (lint, buf, lefthook, playwright)
make generate              # regenerate protobuf/gRPC into gen/
make playwright-install    # install Chromium for e2e (first run)
make test-all              # unit-strict + integration + e2e
```
