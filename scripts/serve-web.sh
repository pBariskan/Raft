#!/usr/bin/env bash
# Serves web/ on http://localhost:8080 (builds the engine first).
set -euo pipefail
cd "$(dirname "$0")/.."
scripts/build-web.sh
echo "open http://localhost:${PORT:-8080}"
exec python3 -m http.server "${PORT:-8080}" --directory web
