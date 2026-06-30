# bench-grpc-slow.ps1 — drive ghz against a SLOW notifications consumer to expose
# gRPC push collapse / thundering-herd. Sweeps client concurrency past the
# server's service capacity and records RPS, p99 and error rate per step.
#
# This is the collapse scenario the in-proc testing.B bench CANNOT show
# (its publisher semaphore never saturates). The server here is
# cmd/notifications-bench, which injects a fixed synthetic service-time.
#
# Prerequisites:
#   go install github.com/bojand/ghz/cmd/ghz@latest
#   make generate                        # proto descriptors in gen/
#   go build -tags bench ./cmd/notifications-bench
#   $env:BENCH_SERVICE_TIME = "5ms"; .\notifications-bench   # start slow consumer
#
# Usage:
#   .\scripts\bench-grpc-slow.ps1
#   $env:CONCURRENCY_SWEEP = "10 50 200 500"; $env:RPS = "0"; .\scripts\bench-grpc-slow.ps1
#
# Rps=0 means "unthrottled" — ghz pushes as fast as concurrency allows, which is
# what makes the collapse visible. Set Rps>0 to cap the offered rate instead.

param(
    [string]$GrpcAddr        = ($env:GRPC_ADDR         ?? "localhost:9091"),
    [int]   $Rps             = ($env:RPS               ?? 0),
    [int]   $Total           = ($env:TOTAL             ?? 10000),
    [string]$ConcurrencySweep= ($env:CONCURRENCY_SWEEP ?? "10 50 200 500")
)

$ErrorActionPreference = "Stop"

$timestamp  = Get-Date -Format "yyyyMMdd-HHmmss"
$resultsDir = "tmp\bench-results\grpc-slow-$timestamp"
New-Item -ItemType Directory -Force -Path $resultsDir | Out-Null

$protoDir = "proto\notification\v1"
$summary  = "$resultsDir\summary.csv"
"concurrency,rps,count,errors,p50_ms,p95_ms,p99_ms,avg_ms" | Set-Content -Path $summary -Encoding utf8

Write-Host "==> ghz collapse sweep against $GrpcAddr  rps=$Rps  total=$Total"
Write-Host "==> concurrency sweep: $ConcurrencySweep"
Write-Host "==> ensure the server is cmd/notifications-bench with BENCH_SERVICE_TIME set"

$data = '{"saga_id":"{{newUUID}}","subscription_id":"11111111-1111-1111-1111-111111111111","email":"bench+{{.RequestNumber}}@example.com","repo":"owner/repo","confirm_token":"tok-confirm-{{.RequestNumber}}","unsub_token":"tok-unsub-{{.RequestNumber}}","event_id":"{{newUUID}}","idempotency_key":"bench-idem-{{.RequestNumber}}"}'

function Get-Pct {
    param($dist, [int]$p)
    $entry = $dist | Where-Object { $_.percentage -eq $p } | Select-Object -First 1
    if ($null -eq $entry) { return 0 }
    return [math]::Round($entry.latency / 1e6, 3)   # ns -> ms
}

foreach ($c in ($ConcurrencySweep -split '\s+' | Where-Object { $_ })) {
    Write-Host ""
    Write-Host "--- concurrency=$c ---"
    $jsonFile = "$resultsDir\c$c.json"

    ghz `
        --proto="$protoDir\notification.proto" `
        --call="notification.v1.NotificationService/SendConfirmation" `
        --insecure `
        --rps="$Rps" `
        --total="$Total" `
        --concurrency="$c" `
        --data="$data" `
        --format=json `
        "$GrpcAddr" `
        | Set-Content -Path $jsonFile -Encoding utf8

    $r = Get-Content $jsonFile -Raw | ConvertFrom-Json
    $errors = 0
    if ($r.statusCodeDistribution) {
        foreach ($prop in $r.statusCodeDistribution.PSObject.Properties) {
            if ($prop.Name -ne "OK") { $errors += [int]$prop.Value }
        }
    }
    $row = @(
        $c, [math]::Floor($r.rps), $r.count, $errors,
        (Get-Pct $r.latencyDistribution 50),
        (Get-Pct $r.latencyDistribution 95),
        (Get-Pct $r.latencyDistribution 99),
        [math]::Round($r.average / 1e6, 3)
    ) -join ","
    $row | Add-Content -Path $summary -Encoding utf8
    Write-Host "    saved -> $jsonFile"
}

Write-Host ""
Write-Host "==> sweep done. summary -> $summary"
Import-Csv $summary | Format-Table -AutoSize
Write-Host "==> watch RPS plateau and p99 + errors climb as concurrency outruns the consumer."
