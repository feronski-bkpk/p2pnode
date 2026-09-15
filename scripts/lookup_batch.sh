#!/usr/bin/env bash

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

N="${N:-15}"
TARGET_LOOKUPS="${TARGET_LOOKUPS:-30}"
LOOKUP_DIR="$METRICS_DIR/lookups"
mkdir -p "$LOOKUP_DIR"

declare -a ids
declare -a states
for i in $(seq 1 "$N"); do
    state="$(node_state_dir "$i")"
    id="$(node_id_from_state "$state")"
    if [ -z "$id" ]; then
        echo "[lookup] no identity in $state" >&2
        exit 1
    fi
    ids[$i]="$id"
    states[$i]="$state"
done

echo "[lookup] collected $N node IDs"

count=0
for i in $(seq 1 "$N"); do
    for j in $(seq 1 "$N"); do
        if [ "$i" = "$j" ]; then
            continue
        fi
        if [ "$j" -eq 1 ]; then
            continue
        fi
        if [ "$count" -ge "$TARGET_LOOKUPS" ]; then
            break 2
        fi

        target_id="${ids[$j]}"
        short="${target_id:0:8}"

        tmp_state="$(mktemp -d)"
        cp "${states[$i]}/identity.key" "$tmp_state/" 2>/dev/null || true
        cp "${states[$i]}/identity.pub" "$tmp_state/" 2>/dev/null || true

        tmp_port=$((BASE_PORT + N + count + 100))

        out_log="$LOOKUP_DIR/lookup-$i-to-$j-$short.json"
        err_log="$LOG_DIR/lookup-$i-to-$j-$short.log"

        "$ROOT_DIR/bin/node" \
            -state-dir "$tmp_state" \
            -listen-host 127.0.0.1 \
            -listen-port "$tmp_port" \
            -bootstrap "127.0.0.1:$BASE_PORT" \
            -skip-self-lookup \
            -k "$K" \
            -alpha "$ALPHA" \
            -export-dir "$LOOKUP_DIR" \
            -lookup-target "$target_id" \
            -dump-routing \
            -log-level WARN \
            > "$err_log" 2>&1 || true

        latest="$(ls -t "$LOOKUP_DIR"/lookup-"$short"-*.json 2>/dev/null | head -n 1 || true)"
        if [ -n "$latest" ] && [ "$latest" != "$out_log" ]; then
            mv "$latest" "$out_log"
        fi

        count=$((count + 1))
        rm -rf "$tmp_state"

        echo "[lookup] $count/$TARGET_LOOKUPS: node $i → node $j ($short)"
    done
done

echo "[lookup] done $count lookups, results in $LOOKUP_DIR"
