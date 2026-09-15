#!/usr/bin/env bash
# Собирает последние routing-снапшоты всех узлов в одну папку.
# Предполагает, что узлы запущены с -export-dir и -export-interval-ms.

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

OUT_DIR="${1:-$ROOT_DIR/metrics/collected}"
mkdir -p "$OUT_DIR"
rm -f "$OUT_DIR"/*.json

for state in "$STATE_DIR"/node-*; do
    [ -d "$state" ] || continue
    node_id="$(node_id_from_state "$state")"
    [ -n "$node_id" ] || continue
    short="${node_id:0:8}"

    latest="$(ls -t "$METRICS_DIR"/routing-"$short"-*.json 2>/dev/null | head -n 1 || true)"
    if [ -z "$latest" ]; then
        echo "[collect] no snapshot for $short" >&2
        continue
    fi

    cp "$latest" "$OUT_DIR/routing-$short.json"
    echo "[collect] $short ← $(basename "$latest")"
done

echo "[collect] collected snapshots in $OUT_DIR"
