#!/usr/bin/env sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"
GOCACHE=${GOCACHE:-"$repo_root/.cache/go-build"}
export GOCACHE

for command_name in npm go; do
    if ! command -v "$command_name" >/dev/null 2>&1; then
        echo "Missing command: $command_name. Install it and make sure it is available from PATH." >&2
        exit 1
    fi
done

echo "[1/3] Installing web dependencies"
npm install

echo "[2/3] Building web client"
npm run build

use_local_data=true
for argument in "$@"; do
    case "$argument" in --config|--config=*|-config|-config=*|--data-dir|--data-dir=*|-data-dir|-data-dir=*) use_local_data=false ;; esac
done
if [ -n "${MUSIC_DATA_DIR:-}" ] || [ -n "${MUSIC_CONFIG:-}" ]; then
    use_local_data=false
fi
if [ "$use_local_data" = true ]; then
    set -- --data-dir "$repo_root/.data" "$@"
fi
echo "[3/3] Starting music player (resolved configuration follows)"
exec go run ./apps/server/cmd/music-player "$@"
