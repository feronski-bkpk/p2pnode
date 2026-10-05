#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

N="${1:-15}"
SCHEME="${2:-star}"
WAIT="${3:-20}"

echo "=== Docker experiment: N=$N scheme=$SCHEME wait=${WAIT}s ==="

"$ROOT_DIR/scripts/docker_up.sh" "$N" "$SCHEME"

echo "[exp] waiting ${WAIT}s for convergence..."
sleep "$WAIT"

"$ROOT_DIR/scripts/docker_collect.sh"

if [ -x "$ROOT_DIR/scripts/lookup_batch.sh" ]; then
    echo
    echo "[exp] running lookup_batch.sh..."
    N="$N" "$ROOT_DIR/scripts/lookup_batch.sh" || \
        echo "[exp] lookup_batch failed (skipped)"
fi

echo
echo "[exp] checking non-degeneracy..."
"$ROOT_DIR/scripts/check_ne_degenerate.sh" || true

echo
echo "[exp] generating report.html..."
"$ROOT_DIR/scripts/generate_report.sh" "$ROOT_DIR/report.html" || \
    echo "[exp] report generation failed (skipped)"

echo
echo "=== Done. ==="
echo "  report:    $ROOT_DIR/report.html"
echo "  routing:   $ROOT_DIR/metrics/collected/"
echo "  lookups:   $ROOT_DIR/metrics/lookups/"
echo "  logs:      $ROOT_DIR/logs-docker/"
echo "  stop:      $ROOT_DIR/scripts/docker_down.sh"
