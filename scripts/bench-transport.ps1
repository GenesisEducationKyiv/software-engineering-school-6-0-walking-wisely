# bench-transport.ps1 — drive POST /api/subscribe load against the running stack
# to compare gRPC vs NATS transport metrics in Grafana.
#
# Prerequisites:
#   go install github.com/tsenart/vegeta@latest
#   docker compose up -d  (stack must be running)
#   GITHUB_SKIP_REPO_VALIDATION=true on the api service — otherwise every unique
#     repo is validated against GitHub and the load run hits rate limits.
#
# Usage:
#   $env:SAGA_TRANSPORT = "nats"; docker compose up -d api; .\scripts\bench-transport.ps1
#   $env:SAGA_TRANSPORT = "grpc"; docker compose up -d api; .\scripts\bench-transport.ps1
#
# Polls the outbox backlog drain (outbox_pending_count on /metrics) and reports
# drain time — the apples-to-apples recovery number for gRPC vs NATS. No Grafana.

param(
    [string]$ApiUrl       = ($env:API_URL       ?? "http://localhost:8080"),
    [int]   $Rate         = ($env:RATE          ?? 50),
    [string]$Duration     = ($env:DURATION      ?? "60s"),
    [int]   $SpikeRate    = ($env:SPIKE_RATE    ?? 500),
    [string]$SpikeDuration= ($env:SPIKE_DURATION ?? "10s"),
    [int]   $DrainWait    = ($env:DRAIN_WAIT    ?? 30)
)

$ErrorActionPreference = "Stop"

$transport  = $env:SAGA_TRANSPORT ?? "unknown"
$timestamp  = Get-Date -Format "yyyyMMdd-HHmmss"
$resultsDir = "tmp\bench-results\$transport-$timestamp"
New-Item -ItemType Directory -Force -Path $resultsDir | Out-Null

Write-Host "==> transport=$transport  api=$ApiUrl"
Write-Host "==> results -> $resultsDir"

# Pre-generate a vegeta JSON-format targets file with unique emails.
# Each subscription is new — no deduplication shortcut in the saga path.
# Pool size: 2× worst-case request count so vegeta cycles cleanly if needed.
$maxUnique   = ($Rate * 60 + $SpikeRate * 10) * 2
$targetsFile = "$resultsDir\targets.jsonl"
Write-Host "==> generating $maxUnique unique targets -> $targetsFile"

$lines = 1..$maxUnique | ForEach-Object {
    $bodyJson = "{`"email`":`"bench+$_@example.com`",`"repo`":`"owner/repo`"}"
    $b64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($bodyJson))
    "{`"method`":`"POST`",`"url`":`"$ApiUrl/api/subscribe`",`"header`":{`"Content-Type`":[`"application/json`"]},`"body`":`"$b64`"}"
}
$lines | Set-Content -Path $targetsFile -Encoding utf8

function Run-Attack {
    param([string]$Label, [int]$AttackRate, [string]$AttackDuration)

    $binFile = "$resultsDir\$Label.bin"
    $txtFile = "$resultsDir\$Label.txt"

    Write-Host ""
    Write-Host "--- ${Label}: rate=${AttackRate}/s  duration=${AttackDuration} ---"

    vegeta attack `
        -format=json `
        -targets="$targetsFile" `
        -rate="$AttackRate" `
        -duration="$AttackDuration" `
        -output="$binFile"

    vegeta report -type=text "$binFile" | Tee-Object -FilePath $txtFile
    Write-Host "    saved -> $txtFile"
}

# Get-Backlog reads outbox_pending_count from the API /metrics endpoint.
# Returns the count as [int], or $null if the metric is absent / scrape fails.
function Get-Backlog {
    try {
        $body = (Invoke-WebRequest -Uri "$ApiUrl/metrics" -UseBasicParsing).Content
    } catch {
        return $null
    }
    foreach ($line in $body -split "`n") {
        if ($line -match '^outbox_pending_count(\{|[\s])\S*\s+([0-9.]+)') {
            return [int][double]$Matches[2]
        }
    }
    return $null
}

# Poll-Drain samples the backlog every second until it reaches 0 or DrainWait
# elapses, recording the curve and the drain time. This is the recovery metric.
function Poll-Drain {
    $drainLog = "$resultsDir\drain.csv"
    "elapsed_s,pending" | Set-Content -Path $drainLog -Encoding utf8
    $start = Get-Date
    while ($true) {
        $elapsed = [int]((Get-Date) - $start).TotalSeconds
        $pending = Get-Backlog
        $pendingStr = if ($null -eq $pending) { "NA" } else { "$pending" }
        "$elapsed,$pendingStr" | Add-Content -Path $drainLog -Encoding utf8
        Write-Host "    t=${elapsed}s  pending=$pendingStr"
        if ($pending -eq 0) {
            Write-Host "==> drained in ${elapsed}s"
            return
        }
        if ($elapsed -ge $DrainWait) {
            Write-Host "==> NOT drained within ${DrainWait}s (pending=$pendingStr) — backlog/collapse"
            return
        }
        Start-Sleep -Seconds 1
    }
}

Run-Attack -Label "steady" -AttackRate $Rate         -AttackDuration $Duration
Run-Attack -Label "spike"  -AttackRate $SpikeRate    -AttackDuration $SpikeDuration

Write-Host ""
Write-Host "==> load done. polling outbox drain (max ${DrainWait}s)..."
Poll-Drain
Write-Host "==> done. results in $resultsDir\ (steady.txt, spike.txt, drain.csv)"
