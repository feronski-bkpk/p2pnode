#!/usr/bin/env bash

set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

N="${1:-15}"
WAIT_SECONDS="${2:-15}"

echo "=== Experiment: N=$N, wait=${WAIT_SECONDS}s ==="

"$ROOT_DIR/scripts/stop_all.sh" || true
rm -rf "$STATE_DIR" "$LOG_DIR" "$METRICS_DIR"
mkdir -p "$STATE_DIR" "$LOG_DIR" "$METRICS_DIR"

N="$N" "$ROOT_DIR/scripts/run_star.sh" "$N"

echo "[experiment] waiting ${WAIT_SECONDS}s for convergence..."
sleep "$WAIT_SECONDS"

N="$N" "$ROOT_DIR/scripts/collect_routing.sh"

N="$N" "$ROOT_DIR/scripts/lookup_batch.sh"

"$ROOT_DIR/scripts/check_ne_degenerate.sh"

echo
echo "=== Experiment done. Results in $METRICS_DIR ==="
echo "  routing snapshots: $METRICS_DIR/collected/"
echo "  lookup results:    $METRICS_DIR/lookups/"
