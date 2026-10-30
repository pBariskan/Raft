#!/usr/bin/env bash
# Builds the simulator for the browser: web/engine/raft.wasm plus the matching Go loader.
# Run from anywhere; serve web/ with any static server afterwards (scripts/serve-web.sh).
set -euo pipefail
cd "$(dirname "$0")/.."
GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o web/engine/raft.wasm ./wasm
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" web/engine/wasm_exec.js
echo "built web/engine/raft.wasm ($(du -h web/engine/raft.wasm | cut -f1))"
