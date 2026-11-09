$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot
if (-not $env:GOCACHE) {
    $env:GOCACHE = Join-Path $repoRoot ".cache\go-build"
}

foreach ($command in @("npm", "go")) {
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

$serverArguments = @()
if (-not $env:MUSIC_DATA_DIR -and -not $env:MUSIC_CONFIG -and -not ($args | Where-Object { $_ -match '^--?(config|data-dir)(=|$)' })) {
    $serverArguments += @("--data-dir", (Join-Path $repoRoot ".data"))
}
Write-Host "[3/3] Starting music player (resolved configuration follows)"
& go run ./apps/server/cmd/music-player @serverArguments @args
exit $LASTEXITCODE
