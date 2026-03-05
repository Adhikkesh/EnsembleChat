# ╔══════════════════════════════════════════════════════════════╗
# ║  Launch 3 Coordinator Nodes as Separate Processes          ║
# ║  Each node runs its own RPC server — true multi-process    ║
# ╚══════════════════════════════════════════════════════════════╝
#
# Usage:  .\scripts\launch_cluster.ps1
#
# This script opens 3 separate PowerShell windows simulating 3 machines.
# Communication happens over net/rpc (TCP).
#
# To stop: press Ctrl+C in each window, or close them.

$ErrorActionPreference = "Stop"

Write-Host ""
Write-Host "Building coordinator binary..." -ForegroundColor Cyan
go build -o bin/coordinator.exe ./cmd/coordinator
if ($LASTEXITCODE -ne 0) {
    Write-Host "Build failed!" -ForegroundColor Red
    exit 1
}
Write-Host "Build successful." -ForegroundColor Green
Write-Host ""

$binary = Join-Path $PSScriptRoot "..\bin\coordinator.exe"
$binary = (Resolve-Path $binary).Path

Write-Host "Starting 3 coordinator nodes..." -ForegroundColor Yellow
Write-Host "  Node 0  ->  localhost:7000" -ForegroundColor White
Write-Host "  Node 1  ->  localhost:7001" -ForegroundColor White
Write-Host "  Node 2  ->  localhost:7002" -ForegroundColor White
Write-Host ""

# Node 0
Start-Process powershell -ArgumentList @(
    "-NoExit", "-Command",
    "Write-Host '═══ Coordinator Node 0 ═══' -ForegroundColor Green; & '$binary' -id 0 -addr :7000 -peers '1=127.0.0.1:7001,2=127.0.0.1:7002'"
)

# Small delay so Node 0 starts listening before others try to connect
Start-Sleep -Milliseconds 500

# Node 1
Start-Process powershell -ArgumentList @(
    "-NoExit", "-Command",
    "Write-Host '═══ Coordinator Node 1 ═══' -ForegroundColor Blue; & '$binary' -id 1 -addr :7001 -peers '0=127.0.0.1:7000,2=127.0.0.1:7002'"
)

Start-Sleep -Milliseconds 500

# Node 2
Start-Process powershell -ArgumentList @(
    "-NoExit", "-Command",
    "Write-Host '═══ Coordinator Node 2 ═══' -ForegroundColor Magenta; & '$binary' -id 2 -addr :7002 -peers '0=127.0.0.1:7000,1=127.0.0.1:7001'"
)

Write-Host ""
Write-Host "3 coordinator windows launched." -ForegroundColor Green
Write-Host "Each window has an interactive CLI. Type 'help' for commands." -ForegroundColor Cyan
Write-Host ""
Write-Host "Quick test commands (run in the LEADER's window):" -ForegroundColor Yellow
Write-Host "  status                                   — Check election state" -ForegroundColor White
Write-Host "  token                                    — See token ring status" -ForegroundColor White
Write-Host "  lock myroom AdminService                 — Enter critical section" -ForegroundColor White
Write-Host "  unlock myroom AdminService               — Exit critical section" -ForegroundColor White
Write-Host "  propose tx1 add_room Lobby Server1:9001  — Replicate via Raft" -ForegroundColor White
Write-Host "  state                                    — View routing table" -ForegroundColor White
Write-Host ""
