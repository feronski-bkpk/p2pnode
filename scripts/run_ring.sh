#!/usr/bin/env bash

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

N="${1:-$N}"
FIRST_PORT="$(node_port 1)"

clean_all
build_binary

echo "[ring] launching $N nodes, first port=$FIRST_PORT"

mkdir -p "$(node_state_dir 1)"
"$ROOT_DIR/bin/node" \
    -state-dir "$(node_state_dir 1)" \
    -listen-host 127.0.0.1 \
    -listen-port "$FIRST_PORT" \
    -k "$K" \
    -alpha "$ALPHA" \
    -export-dir "$METRICS_DIR" \
    -export-interval-ms "$EXPORT_INTERVAL_MS" \
    -log-level "$LOG_LEVEL" \
    > "$(node_log 1)" 2>&1 &
echo $! > "$ROOT_DIR/state/node-01.pid"

wait_port "$FIRST_PORT" 10 || exit 1
echo "[ring] node 1 up at 127.0.0.1:$FIRST_PORT"

PREV_PORT="$FIRST_PORT"
for i in $(seq 2 "$N"); do
    port="$(node_port "$i")"
    mkdir -p "$(node_state_dir "$i")"
    "$ROOT_DIR/bin/node" \
        -state-dir "$(node_state_dir "$i")" \
        -listen-host 127.0.0.1 \
        -listen-port "$port" \
        -bootstrap "127.0.0.1:$PREV_PORT" \
        -k "$K" \
        -alpha "$ALPHA" \
        -export-dir "$METRICS_DIR" \
        -export-interval-ms "$EXPORT_INTERVAL_MS" \
        -log-level "$LOG_LEVEL" \
        > "$(node_log "$i")" 2>&1 &
    echo $! > "$ROOT_DIR/state/node-$(printf '%02d' "$i").pid"

    wait_port "$port" 10 || exit 1
    echo "[ring] node $i up at 127.0.0.1:$port (bootstrap from $PREV_PORT)"

    PREV_PORT="$port"
    sleep 0.3
done

echo "[ring] all $N nodes up."
