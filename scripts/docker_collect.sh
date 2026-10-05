#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

SRC_DIR="$ROOT_DIR/metrics-docker"
OUT_DIR="$ROOT_DIR/metrics/collected"

mkdir -p "$OUT_DIR" "$ROOT_DIR/metrics/lookups"

if [ ! -d "$SRC_DIR" ]; then
    echo "[collect] $SRC_DIR not found; run docker_up.sh first" >&2
    exit 0
fi

rm -f "$OUT_DIR"/*.json

collected=0
for node_dir in "$SRC_DIR"/node-*; do
    [ -d "$node_dir" ] || continue

    latest="$(ls -t "$node_dir"/routing-*.json 2>/dev/null | head -n 1 || true)"
    if [ -z "$latest" ]; then
        echo "[collect] no routing snapshot in $node_dir" >&2
        continue
    fi

    base="$(basename "$latest")"
    nodeid_short="$(echo "$base" | sed -E 's/^routing-([0-9a-f]{8})-.*/\1/')"
    if [ -z "$nodeid_short" ] || [ "$nodeid_short" = "$base" ]; then
        nodeid_short="$(basename "$node_dir")"
    fi

    cp "$latest" "$OUT_DIR/routing-$nodeid_short.json"
    echo "[collect] $nodeid_short ← $base"
    collected=$((collected + 1))
done

echo "[collect] collected $collected routing snapshots in $OUT_DIR"
echo "[collect] lookups: $(ls "$ROOT_DIR/metrics/lookups"/*.json 2>/dev/null | wc -l)"
