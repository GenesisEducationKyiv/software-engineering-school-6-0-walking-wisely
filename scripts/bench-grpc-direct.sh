#!/usr/bin/env bash
# bench-grpc-direct.sh — drive gRPC SendConfirmation directly via ghz
# to isolate Notifications service throughput from the HTTP gateway.
#
# Prerequisites:
#   go install github.com/bojand/ghz/cmd/ghz@latest
#   make generate   (proto descriptors must exist in gen/)
#   docker compose up -d notifications
#
# Usage:
#   ./scripts/bench-grpc-direct.sh
#
# Adjust GRPC_ADDR to point at notifications service (default: localhost:9091).

set -euo pipefail

GRPC_ADDR="${GRPC_ADDR:-localhost:9091}"
RPS="${RPS:-200}"
TOTAL="${TOTAL:-5000}"
CONCURRENCY="${CONCURRENCY:-20}"
PROTO_DIR="proto/notification/v1"
RESULTS_DIR="tmp/bench-results/grpc-direct-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$RESULTS_DIR"

echo "==> ghz against ${GRPC_ADDR}  rps=${RPS}  total=${TOTAL}  concurrency=${CONCURRENCY}"

ghz \
  --proto="${PROTO_DIR}/notification.proto" \
  --call="notification.v1.NotificationService/SendConfirmation" \
  --insecure \
  --rps="${RPS}" \
  --total="${TOTAL}" \
  --concurrency="${CONCURRENCY}" \
  --data='{
    "saga_id":         "{{newUUID}}",
    "subscription_id": "11111111-1111-1111-1111-111111111111",
    "email":           "bench+{{.RequestNumber}}@example.com",
    "repo":            "owner/repo",
    "confirm_token":   "tok-confirm-{{.RequestNumber}}",
    "unsub_token":     "tok-unsub-{{.RequestNumber}}",
    "event_id":        "{{newUUID}}",
    "idempotency_key": "bench-idem-{{.RequestNumber}}"
  }' \
  --format=pretty \
  "${GRPC_ADDR}" \
  | tee "${RESULTS_DIR}/ghz.txt"

echo ""
echo "==> results in ${RESULTS_DIR}/ghz.txt"
echo "==> check Grafana http://localhost:3000 dashboard 'gRPC vs Broker'"
