#!/usr/bin/env bash
# bench-grpc-slow.sh — drive ghz against a SLOW notifications consumer to expose
# gRPC push collapse / thundering-herd. Sweeps client concurrency past the
# server's service capacity and records RPS, p99 and error rate per step.
#
# This is the collapse scenario the in-proc testing.B bench CANNOT show
# (its publisher semaphore never saturates). The server here is
# cmd/notifications-bench, which injects a fixed synthetic service-time.
#
# Prerequisites:
#   go install github.com/bojand/ghz/cmd/ghz@latest
#   jq (for the summary CSV)
#   make generate                        # proto descriptors in gen/
#   go build -tags bench ./cmd/notifications-bench
#   BENCH_SERVICE_TIME=5ms ./notifications-bench   # start the slow consumer
#
# Usage:
#   ./scripts/bench-grpc-slow.sh
#   CONCURRENCY_SWEEP="10 50 200 500" RPS=0 TOTAL=10000 ./scripts/bench-grpc-slow.sh
#
# RPS=0 means "unthrottled" — ghz pushes as fast as concurrency allows, which is
# what makes the collapse visible. Set RPS>0 to cap the offered rate instead.

set -euo pipefail

GRPC_ADDR="${GRPC_ADDR:-localhost:9091}"
RPS="${RPS:-0}"                 # 0 = unthrottled push (collapse test)
TOTAL="${TOTAL:-10000}"
CONCURRENCY_SWEEP="${CONCURRENCY_SWEEP:-10 50 200 500}"
PROTO_DIR="proto/notification/v1"
RESULTS_DIR="tmp/bench-results/grpc-slow-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$RESULTS_DIR"

SUMMARY="${RESULTS_DIR}/summary.csv"
echo "concurrency,rps,count,errors,p50_ms,p95_ms,p99_ms,avg_ms" > "$SUMMARY"

echo "==> ghz collapse sweep against ${GRPC_ADDR}  rps=${RPS}  total=${TOTAL}"
echo "==> concurrency sweep: ${CONCURRENCY_SWEEP}"
echo "==> ensure the server is cmd/notifications-bench with BENCH_SERVICE_TIME set"

DATA='{
  "saga_id":         "{{newUUID}}",
  "subscription_id": "11111111-1111-1111-1111-111111111111",
  "email":           "bench+{{.RequestNumber}}@example.com",
  "repo":            "owner/repo",
  "confirm_token":   "tok-confirm-{{.RequestNumber}}",
  "unsub_token":     "tok-unsub-{{.RequestNumber}}",
  "event_id":        "{{newUUID}}",
  "idempotency_key": "bench-idem-{{.RequestNumber}}"
}'

for c in $CONCURRENCY_SWEEP; do
  echo ""
  echo "--- concurrency=${c} ---"
  json="${RESULTS_DIR}/c${c}.json"

  ghz \
    --proto="${PROTO_DIR}/notification.proto" \
    --call="notification.v1.NotificationService/SendConfirmation" \
    --insecure \
    --rps="${RPS}" \
    --total="${TOTAL}" \
    --concurrency="${c}" \
    --data="${DATA}" \
    --format=json \
    "${GRPC_ADDR}" \
    > "$json"

  # ghz reports durations in nanoseconds; convert to ms for the summary.
  jq -r --arg c "$c" '
    def ms: . / 1e6;
    def pct(p): (.latencyDistribution[] | select(.percentage==p) | .latency // 0);
    [ $c,
      (.rps // 0 | floor),
      (.count // 0),
      ([.statusCodeDistribution // {} | to_entries[] | select(.key!="OK") | .value] | add // 0),
      (pct(50) | ms), (pct(95) | ms), (pct(99) | ms),
      (.average | ms)
    ] | @csv' "$json" >> "$SUMMARY"

  echo "    saved → ${json}"
done

echo ""
echo "==> sweep done. summary → ${SUMMARY}"
column -s, -t "$SUMMARY" || cat "$SUMMARY"
echo "==> watch RPS plateau and p99 + errors climb as concurrency outruns the consumer."
