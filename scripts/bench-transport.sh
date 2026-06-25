#!/usr/bin/env bash
# bench-transport.sh — drive POST /api/subscribe load against the running stack
# to compare gRPC vs NATS transport metrics in Grafana.
#
# Prerequisites:
#   brew install vegeta   (or go install github.com/tsenart/vegeta@latest)
#   docker compose up -d  (stack must be running)
#   GITHUB_SKIP_REPO_VALIDATION=true on the api service — otherwise every unique
#     repo is validated against GitHub and the load run hits rate limits.
#
# Usage:
#   SAGA_TRANSPORT=nats  docker compose up -d api && ./scripts/bench-transport.sh
#   SAGA_TRANSPORT=grpc  docker compose up -d api && ./scripts/bench-transport.sh
#
# The script runs a constant-rate attack, then a spike, then polls the outbox
# backlog drain (outbox_pending_count on /metrics) and reports drain time —
# the apples-to-apples recovery number for gRPC vs NATS. No Grafana required.

set -euo pipefail

API_URL="${API_URL:-http://localhost:8080}"
RATE="${RATE:-50}"          # requests/sec for steady-state run
DURATION="${DURATION:-60s}" # steady-state duration
SPIKE_RATE="${SPIKE_RATE:-500}"
SPIKE_DURATION="${SPIKE_DURATION:-10s}"
DRAIN_WAIT="${DRAIN_WAIT:-30}"  # seconds to wait after load for outbox drain

TRANSPORT="${SAGA_TRANSPORT:-unknown}"
RESULTS_DIR="tmp/bench-results/${TRANSPORT}-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$RESULTS_DIR"

echo "==> transport=${TRANSPORT}  api=${API_URL}"
echo "==> results → ${RESULTS_DIR}"

# Pre-generate a vegeta JSON-format targets file with unique emails.
# Each subscription is new — no deduplication shortcut in the saga path.
# Pool size: 2× worst-case request count so vegeta cycles cleanly if needed.
MAX_UNIQUE=$(( (RATE * 60 + SPIKE_RATE * 10) * 2 ))
TARGETS_FILE="${RESULTS_DIR}/targets.jsonl"
echo "==> generating ${MAX_UNIQUE} unique targets → ${TARGETS_FILE}"
for i in $(seq 1 "$MAX_UNIQUE"); do
  body=$(printf '{"email":"bench+%d@example.com","repo":"owner/repo"}' "$i")
  b64=$(printf '%s' "$body" | base64 | tr -d '\n')
  printf '{"method":"POST","url":"%s/api/subscribe","header":{"Content-Type":["application/json"]},"body":"%s"}\n' \
    "$API_URL" "$b64"
done > "$TARGETS_FILE"

run_attack() {
  local label=$1 rate=$2 dur=$3
  echo ""
  echo "--- ${label}: rate=${rate}/s  duration=${dur} ---"

  vegeta attack \
    -format=json \
    -targets="${TARGETS_FILE}" \
    -rate="${rate}" \
    -duration="${dur}" \
    | tee "${RESULTS_DIR}/${label}.bin" \
    | vegeta report -type=text \
    | tee "${RESULTS_DIR}/${label}.txt"

  vegeta report -type=hdrplot \
    -output="${RESULTS_DIR}/${label}.hdr" \
    < "${RESULTS_DIR}/${label}.bin" || true

  echo "    saved → ${RESULTS_DIR}/${label}.txt"
}

# outbox_backlog reads outbox_pending_count from the API /metrics endpoint.
# Returns the integer pending count, or empty string if the metric is absent.
outbox_backlog() {
  curl -s "${API_URL}/metrics" \
    | awk '/^outbox_pending_count(\{|[[:space:]])/ { print $2; exit }'
}

# poll_drain samples the backlog every second until it reaches 0 or DRAIN_WAIT
# elapses, recording the curve and the drain time. This is the recovery metric.
poll_drain() {
  local drain_log="${RESULTS_DIR}/drain.csv"
  echo "elapsed_s,pending" > "$drain_log"
  local start now pending
  start=$(date +%s)
  while :; do
    now=$(( $(date +%s) - start ))
    pending=$(outbox_backlog)
    echo "${now},${pending:-NA}" >> "$drain_log"
    echo "    t=${now}s  pending=${pending:-NA}"
    if [ "${pending:-1}" = "0" ]; then
      echo "==> drained in ${now}s"
      return
    fi
    if [ "$now" -ge "$DRAIN_WAIT" ]; then
      echo "==> NOT drained within ${DRAIN_WAIT}s (pending=${pending:-NA}) — backlog/collapse"
      return
    fi
    sleep 1
  done
}

run_attack "steady" "$RATE" "$DURATION"
run_attack "spike"  "$SPIKE_RATE" "$SPIKE_DURATION"

echo ""
echo "==> load done. polling outbox drain (max ${DRAIN_WAIT}s)…"
poll_drain
echo "==> done. results in ${RESULTS_DIR}/ (steady.txt, spike.txt, drain.csv)"
