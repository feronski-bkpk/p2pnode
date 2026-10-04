#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

N="${1:-15}"
WAIT_SECONDS="${2:-15}"
WITH_COMPARE="${WITH_COMPARE:-0}"

echo "=== Experiment: N=$N, wait=${WAIT_SECONDS}s ==="

"$ROOT_DIR/scripts/stop_all.sh" || true
rm -rf "$STATE_DIR" "$LOG_DIR" "$METRICS_DIR"
mkdir -p "$STATE_DIR" "$LOG_DIR" "$METRICS_DIR"

N="$N" "$ROOT_DIR/scripts/run_star.sh" "$N"

echo "[experiment] waiting ${WAIT_SECONDS}s for convergence..."
sleep "$WAIT_SECONDS"

N="$N" "$ROOT_DIR/scripts/collect_routing.sh"
N="$N" "$ROOT_DIR/scripts/lookup_batch.sh"

"$ROOT_DIR/scripts/check_ne_degenerate.sh" || true

if [ "$WITH_COMPARE" = "1" ]; then
    LOOKUPS_BACKUP=""
    if [ -d "$METRICS_DIR/lookups" ]; then
        LOOKUPS_BACKUP="$(mktemp -d)"
        cp -r "$METRICS_DIR/lookups" "$LOOKUPS_BACKUP/" 2>/dev/null || true
        echo "[experiment] backed up $(ls "$METRICS_DIR/lookups" 2>/dev/null | wc -l) lookups"
    fi

    echo
    echo "[experiment] running compare_visual (star vs ring)..."
    "$ROOT_DIR/scripts/compare_visual.sh" "$N" "$WAIT_SECONDS" || \
        echo "[experiment] compare_visual failed (skipped)"

    # Восстанавливаем lookups, если compare_visual их потерял.
    if [ -n "$LOOKUPS_BACKUP" ] && [ -d "$LOOKUPS_BACKUP/lookups" ]; then
        if [ ! -d "$METRICS_DIR/lookups" ]; then
            echo "[experiment] restoring lookups after compare_visual..."
            mkdir -p "$METRICS_DIR"
            cp -r "$LOOKUPS_BACKUP/lookups" "$METRICS_DIR/" 2>/dev/null || true
        fi
        rm -rf "$LOOKUPS_BACKUP"
    fi

    echo
    echo "[experiment] running compare_serialization..."
    "$ROOT_DIR/scripts/compare_serialization.sh" || \
        echo "[experiment] compare_serialization failed (skipped)"
fi

echo
echo "[experiment] generating report.html..."
"$ROOT_DIR/scripts/generate_report.sh" "$ROOT_DIR/report.html" || \
    echo "[experiment] report generation failed (skipped)"

echo
echo "=== Experiment done. Results in $METRICS_DIR ==="
echo "  routing snapshots: $METRICS_DIR/collected/"
echo "  lookup results:    $METRICS_DIR/lookups/"
echo "  report:            $ROOT_DIR/report.html"
