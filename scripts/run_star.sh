#!/usr/bin/env bash

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

N="${1:-$N}"
SEED_PORT="$(node_port 1)"

clean_all
build_binary

echo "[star] launching $N nodes, seed port=$SEED_PORT"

mkdir -p "$(node_state_dir 1)"
"$ROOT_DIR/bin/node" \
    -state-dir "$(node_state_dir 1)" \
    -listen-host 127.0.0.1 \
    -listen-port "$SEED_PORT" \
    -k "$K" \
    -alpha "$ALPHA" \
    -export-dir "$METRICS_DIR" \
    -export-interval-ms "$EXPORT_INTERVAL_MS" \
    -log-level "$LOG_LEVEL" \
    > "$(node_log 1)" 2>&1 &
echo $! > "$ROOT_DIR/state/node-01.pid"

wait_port "$SEED_PORT" 10 || exit 1
echo "[star] seed up at 127.0.0.1:$SEED_PORT"

for i in $(seq 2 "$N"); do
    port="$(node_port "$i")"
    mkdir -p "$(node_state_dir "$i")"
    "$ROOT_DIR/bin/node" \
        -state-dir "$(node_state_dir "$i")" \
        -listen-host 127.0.0.1 \
        -listen-port "$port" \
        -bootstrap "127.0.0.1:$SEED_PORT" \
        -k "$K" \
        -alpha "$ALPHA" \
        -export-dir "$METRICS_DIR" \
        -export-interval-ms "$EXPORT_INTERVAL_MS" \
        -log-level "$LOG_LEVEL" \
        > "$(node_log "$i")" 2>&1 &
    echo $! > "$ROOT_DIR/state/node-$(printf '%02d' "$i").pid"

    wait_port "$port" 10 || exit 1
    echo "[star] node $i up at 127.0.0.1:$port"

    sleep 0.3
done

echo "[star] all $N nodes up."
echo "[star] logs: $LOG_DIR/node-*.log"
echo "[star] metrics: $METRICS_DIR/routing-*.json"
echo "[star] to stop: $ROOT_DIR/scripts/stop_all.sh"
