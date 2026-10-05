#!/usr/bin/env bash
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

N="${1:-$N}"

clean_all
build_binary

echo "[tree] launching $N nodes"

for i in $(seq 1 "$N"); do
    port="$(node_port "$i")"
    state="$(node_state_dir "$i")"
    mkdir -p "$state"

    args=(
        -state-dir "$state"
        -listen-host 127.0.0.1
        -listen-port "$port"
        -k "$K"
        -alpha "$ALPHA"
        -export-dir "$METRICS_DIR"
        -export-interval-ms "$EXPORT_INTERVAL_MS"
        -log-level "$LOG_LEVEL"
    )

    if [ "$i" -gt 1 ]; then
        parent=$((i / 2))
        pp=$(node_port "$parent")
        args+=( -bootstrap "127.0.0.1:$pp" )
    fi

    "$ROOT_DIR/bin/node" "${args[@]}" \
        > "$(node_log "$i")" 2>&1 &
    echo $! > "$ROOT_DIR/state/node-$(printf '%02d' "$i").pid"

    wait_port "$port" 10 || exit 1
    echo "[tree] node $i up at 127.0.0.1:$port"

    sleep 0.3
done

echo "[tree] all $N nodes up."
