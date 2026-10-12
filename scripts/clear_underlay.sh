#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

N="${1:-7}"

echo "[underlay] clearing on $N containers"

for i in $(seq 1 "$N"); do
    svc=$(printf 'node-%02d' "$i")
    container="p2pnode-$svc"
    pid=$(docker inspect -f '{{.State.Pid}}' "$container" 2>/dev/null || echo "")
    if [ -z "$pid" ] || [ "$pid" = "0" ]; then
        continue
    fi
    echo "delete table inet filter" | sudo nsenter -t "$pid" -n nft -f - 2>/dev/null || true
done

echo "[underlay] cleared"
