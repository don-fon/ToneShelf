#!/usr/bin/env sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"
GOCACHE=${GOCACHE:-"$repo_root/.cache/go-build"}
export GOCACHE

for command_name in npm go ffmpeg ffprobe; do
    if ! command -v "$command_name" >/dev/null 2>&1; then
        echo "Missing command: $command_name. Install it and make sure it is available from PATH." >&2
        exit 1
    fi
done

echo "[1/3] Installing web dependencies"
npm install

echo "[2/3] Building web client"
npm run build

echo "[3/3] Starting music player at http://${MUSIC_ADDR:-127.0.0.1:8080}"
exec go run ./apps/server/cmd/music-player
