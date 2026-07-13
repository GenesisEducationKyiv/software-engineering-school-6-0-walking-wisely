# bench-grpc-direct.ps1 — drive gRPC SendConfirmation directly via ghz
# to isolate Notifications service throughput from the HTTP gateway.
#
# Prerequisites:
#   go install github.com/bojand/ghz/cmd/ghz@latest
#   make generate   (proto descriptors must exist in gen/)
#   docker compose up -d notifications
#
# Usage:
#   .\scripts\bench-grpc-direct.ps1
#
# Adjust -GrpcAddr to point at the notifications service (default: localhost:9091).

param(
    [string]$GrpcAddr   = ($env:GRPC_ADDR   ?? "localhost:9091"),
    [int]   $Rps        = ($env:RPS         ?? 200),
    [int]   $Total      = ($env:TOTAL       ?? 5000),
    [int]   $Concurrency= ($env:CONCURRENCY ?? 20)
)

$ErrorActionPreference = "Stop"

$timestamp  = Get-Date -Format "yyyyMMdd-HHmmss"
$resultsDir = "tmp\bench-results\grpc-direct-$timestamp"
New-Item -ItemType Directory -Force -Path $resultsDir | Out-Null

$protoDir  = "proto\notification\v1"
$outputFile = "$resultsDir\ghz.txt"

Write-Host "==> ghz against $GrpcAddr  rps=$Rps  total=$Total  concurrency=$Concurrency"

# {{.RequestNumber}} is a ghz Go-template variable — unique per request.
# This ensures each call has a distinct idempotency_key and event_id so
# RecordConfirmation performs a real insert rather than an idempotent no-op.
$data = '{"saga_id":"{{newUUID}}","subscription_id":"11111111-1111-1111-1111-111111111111","email":"bench+{{.RequestNumber}}@example.com","repo":"owner/repo","confirm_token":"tok-confirm-{{.RequestNumber}}","unsub_token":"tok-unsub-{{.RequestNumber}}","event_id":"{{newUUID}}","idempotency_key":"bench-idem-{{.RequestNumber}}"}'

ghz `
    --proto="$protoDir\notification.proto" `
    --call="notification.v1.NotificationService/SendConfirmation" `
    --insecure `
    --rps="$Rps" `
    --total="$Total" `
    --concurrency="$Concurrency" `
    --data="$data" `
    --format=pretty `
    "$GrpcAddr" `
    | Tee-Object -FilePath $outputFile

Write-Host ""
Write-Host "==> results in $outputFile"
Write-Host "==> check Grafana http://localhost:3000 dashboard 'gRPC vs Broker'"
