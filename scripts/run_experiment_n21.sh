#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

N="${1:-21}"
WAIT="${2:-40}"
RESULTS="$ROOT_DIR/visualization/n21"
mkdir -p "$RESULTS"

echo "=== E6-8: N=$N ==="

"$ROOT_DIR/scripts/docker_down.sh" --clean >/dev/null 2>&1 || true
rm -f "$ROOT_DIR/metrics/lookups"/*.json
rm -f "$ROOT_DIR/metrics/collected"/*.json

"$ROOT_DIR/scripts/docker_up.sh" "$N" star --clean
echo "[n21] ждём ${WAIT}с сходимости..."
sleep "$WAIT"

echo "[n21] собираем routing..."
"$ROOT_DIR/scripts/docker_collect.sh"
cp -r "$ROOT_DIR/metrics/collected" "$RESULTS/routing"

echo "[n21] batch lookup'ов 30..."
"$ROOT_DIR/scripts/docker_lookup_batch.sh" "$N" 30 2>&1 | tail -5
cp -r "$ROOT_DIR/metrics/lookups" "$RESULTS/lookups"

echo "[n21] проверка невырожденности..."
"$ROOT_DIR/scripts/check_ne_degenerate.sh" 2>&1 | tail -25 | tee "$RESULTS/check.log"

echo
echo "[n21] результаты в $RESULTS"
echo "[n21] остановить: ./scripts/docker_down.sh --clean"
