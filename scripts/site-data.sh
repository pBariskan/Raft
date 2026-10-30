#!/usr/bin/env bash
# Regenerates the numbers the site shows, from real runs (web/data/). Takes about 10 minutes.
#   chaos/none.json            10,000 seeds with no mistake switched on
#   chaos/<mistake>.json       1,000 seeds with each classic mistake (first 3 failures kept)
#   bench.json                 real-mode benchmark on this machine (skip with NO_BENCH=1)
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p web/data/chaos
go build -o /tmp/raft-chaos ./cmd/chaos
/tmp/raft-chaos -seeds 10000 -keep 3 -json web/data/chaos/none.json
for m in commit-old-terms vote-ignores-log accept-stale-leader forget-vote local-reads no-dedupe; do
  /tmp/raft-chaos -seeds 1000 -mistake "$m" -keep 3 -json "web/data/chaos/$m.json"
done
if [ -z "${NO_BENCH:-}" ]; then
  go run ./cmd/bench -json web/data/bench.json -note "${BENCH_NOTE:-}"
fi
