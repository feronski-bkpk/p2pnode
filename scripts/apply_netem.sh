#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

N="${1:-5}"
DELAY_MS="${2:-50}"
LOSS_PCT="${3:-0}"
JITTER_MS="${4:-0}"

if docker compose version >/dev/null 2>&1; then
    COMPOSE=(docker compose)
else
    COMPOSE=(docker-compose)
fi

COMPOSE_FILE="$ROOT_DIR/docker-compose.yml"

echo "[netem] applying: N=$N delay=${DELAY_MS}ms loss=${LOSS_PCT}% jitter=${JITTER_MS}ms"

applied=0
failed=0

for i in $(seq 1 "$N"); do
    svc=$(printf 'node-%02d' "$i")
    container="p2pnode-$svc"

    pid=$(docker inspect -f '{{.State.Pid}}' "$container" 2>/dev/null || echo "")
    if [ -z "$pid" ] || [ "$pid" = "0" ]; then
        echo "[netem] $svc: container not running, skip"
        continue
    fi

    NETEM="delay ${DELAY_MS}ms"
    if [ "$JITTER_MS" -gt 0 ]; then
        NETEM="$NETEM ${JITTER_MS}ms"
    fi
    if [ "$LOSS_PCT" -gt 0 ]; then
        NETEM="$NETEM loss ${LOSS_PCT}%"
    fi

    echo "[netem] $svc (pid=$pid): $NETEM"

    if sudo nsenter -t "$pid" -n sh -c "
        tc qdisc del dev eth0 root 2>/dev/null || true
        tc qdisc add dev eth0 root netem $NETEM
    " 2>&1; then
        applied=$((applied + 1))
    else
        failed=$((failed + 1))
        echo "[netem] WARN: failed for $svc"
    fi
done

echo "[netem] applied=$applied failed=$failed"
