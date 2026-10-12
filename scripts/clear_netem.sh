#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

N="${1:-5}"

echo "[netem] clearing on $N containers"

for i in $(seq 1 "$N"); do
    svc=$(printf 'node-%02d' "$i")
    container="p2pnode-$svc"
    pid=$(docker inspect -f '{{.State.Pid}}' "$container" 2>/dev/null || echo "")
    if [ -z "$pid" ] || [ "$pid" = "0" ]; then
        continue
    fi
    sudo nsenter -t "$pid" -n sh -c "tc qdisc del dev eth0 root 2>/dev/null || true" \
        2>/dev/null || true
done

echo "[netem] cleared"
