$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot
if (-not $env:GOCACHE) {
    $env:GOCACHE = Join-Path $repoRoot ".cache\go-build"
}

foreach ($command in @("npm", "go", "ffmpeg", "ffprobe")) {
    if (-not (Get-Command $command -ErrorAction SilentlyContinue)) {
        throw "Missing command: $command. Install it and make sure it is available from PATH."
    }
}

Write-Host "[1/3] Installing web dependencies"
& npm install
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host "[2/3] Building web client"
& npm run build
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$listenAddress = if ($env:MUSIC_ADDR) { $env:MUSIC_ADDR } else { "127.0.0.1:8080" }
Write-Host "[3/3] Starting music player at http://$listenAddress"
& go run ./apps/server/cmd/music-player
exit $LASTEXITCODE
