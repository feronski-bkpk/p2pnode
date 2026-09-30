#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

STATE_DIR="${STATE_DIR:-$ROOT_DIR/state}"
LOG_DIR="${LOG_DIR:-$ROOT_DIR/logs}"
METRICS_DIR="${METRICS_DIR:-$ROOT_DIR/metrics}"

N="${N:-5}"
BASE_PORT="${BASE_PORT:-9001}"
K="${K:-4}"
ALPHA="${ALPHA:-3}"
EXPORT_INTERVAL_MS="${EXPORT_INTERVAL_MS:-2000}"
LOG_LEVEL="${LOG_LEVEL:-INFO}"

build_binary() {
    echo "[build] building cmd/node..."
    go build -o "$ROOT_DIR/bin/node" ./cmd/node
}

node_state_dir() {
    local idx="$1"
    echo "$STATE_DIR/node-$(printf '%02d' "$idx")"
}

node_port() {
    local idx="$1"
    echo $((BASE_PORT + idx - 1))
}

node_log() {
    local idx="$1"
    echo "$LOG_DIR/node-$(printf '%02d' "$idx").log"
}

clean_all() {
    pkill -f "$ROOT_DIR/bin/node" 2>/dev/null || true
    sleep 0.5

    rm -rf "$STATE_DIR" "$LOG_DIR" "$METRICS_DIR"

    if [ -d "$METRICS_DIR" ]; then
        echo "[clean_all] WARNING: $METRICS_DIR still exists, retrying" >&2
        sleep 1
        rm -rf "$METRICS_DIR"
    fi

    mkdir -p "$STATE_DIR" "$LOG_DIR" "$METRICS_DIR"
}

wait_port() {
    local port="$1"
    local timeout="${2:-10}"
    local elapsed=0
    while ! (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; do
        sleep 0.1
        elapsed=$((elapsed + 1))
        if [ "$elapsed" -ge $((timeout * 10)) ]; then
            echo "[wait_port] timeout waiting for 127.0.0.1:$port" >&2
            return 1
        fi
    done
    exec 3<&- 2>/dev/null || true
    return 0
}

node_id_from_state() {
    local state_dir="$1"
    if [ ! -f "$state_dir/identity.pub" ]; then
        echo ""
        return 0
    fi
    sha256sum "$state_dir/identity.pub" | awk '{print $1}'
}
