#!/usr/bin/env bash

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

shopt -s nullglob
pids=("$ROOT_DIR"/state/node-*.pid)
killed_by_pid=0
for pf in "${pids[@]}"; do
    pid="$(cat "$pf" 2>/dev/null || true)"
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
        echo "[stop] killing pid $pid ($(basename "$pf"))"
        kill "$pid" 2>/dev/null || true
        killed_by_pid=$((killed_by_pid + 1))
    fi
    rm -f "$pf"
done

if [ "$killed_by_pid" -eq 0 ]; then
    echo "[stop] no pid files found (or all dead)"
fi

if pkill -f "$ROOT_DIR/bin/node" 2>/dev/null; then
    echo "[stop] killed lingering bin/node processes"
fi

sleep 0.5

remaining="$(pgrep -f "$ROOT_DIR/bin/node" 2>/dev/null | wc -l)"
if [ "$remaining" -gt 0 ]; then
    echo "[stop] WARNING: $remaining process(es) still running. Trying kill -9."
    pkill -9 -f "$ROOT_DIR/bin/node" 2>/dev/null || true
    sleep 0.3
fi

echo "[stop] done"
