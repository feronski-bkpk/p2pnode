#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

N="${1:-15}"
SCHEME="${2:-star}"
CLEAN="${3:-}"

IMAGE="p2pnode:latest"
COMPOSE_FILE="$ROOT_DIR/docker-compose.yml"

if docker compose version >/dev/null 2>&1; then
    COMPOSE=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
    COMPOSE=(docker-compose)
else
    echo "[up] docker compose (v2) или docker-compose (v1) не найдены" >&2
    exit 1
fi

echo "=== docker_up: N=$N scheme=$SCHEME ==="

if ! command -v docker >/dev/null 2>&1; then
    echo "[up] docker не найден" >&2
    exit 1
fi
if ! docker info >/dev/null 2>&1; then
    echo "[up] docker daemon недоступен" >&2
    exit 1
fi

"$ROOT_DIR/scripts/docker_down.sh" >/dev/null 2>&1 || true

if [ "$CLEAN" = "--clean" ] || [ ! -d "$ROOT_DIR/state-docker/node-01" ]; then
    echo "[up] cleaning state-docker/, logs-docker/, metrics-docker/..."
    rm -rf "$ROOT_DIR/state-docker" \
           "$ROOT_DIR/logs-docker" \
           "$ROOT_DIR/metrics-docker" 2>/dev/null || true
fi

echo "[up] building image $IMAGE..."
docker build -t "$IMAGE" "$ROOT_DIR"

"$ROOT_DIR/scripts/docker_gen_compose.sh" "$N" "$SCHEME" "$COMPOSE_FILE"

if ! grep -q 'user:' "$COMPOSE_FILE"; then
    echo "[up] WARNING: user: не найден в $COMPOSE_FILE" >&2
fi

mkdir -p "$ROOT_DIR/state-docker" "$ROOT_DIR/logs-docker" "$ROOT_DIR/metrics-docker"
for i in $(seq 1 "$N"); do
    idx=$(printf '%02d' "$i")
    mkdir -p "$ROOT_DIR/state-docker/node-$idx" \
             "$ROOT_DIR/logs-docker/node-$idx" \
             "$ROOT_DIR/metrics-docker/node-$idx"
done

echo "[up] starting $N containers..."
"${COMPOSE[@]}" -f "$COMPOSE_FILE" up -d

echo "[up] waiting for $N containers to be running..."
ready=0
for attempt in $(seq 1 60); do
    running="$("${COMPOSE[@]}" -f "$COMPOSE_FILE" ps --status running --quiet 2>/dev/null | wc -l)"
    if [ "$running" -ge "$N" ]; then
        ready=1
        break
    fi
    sleep 1
done

if [ "$ready" -ne 1 ]; then
    echo "[up] WARNING: not all containers running after 60s" >&2
    "${COMPOSE[@]}" -f "$COMPOSE_FILE" ps
    echo "[up] последние логи node-01:"
    "${COMPOSE[@]}" -f "$COMPOSE_FILE" logs node-01 2>&1 | tail -20
fi

echo "[up] containers up."
echo "[up]   N=$N scheme=$SCHEME"
echo "[up]   state:    $ROOT_DIR/state-docker/"
echo "[up]   logs:     $ROOT_DIR/logs-docker/"
echo "[up]   metrics:  $ROOT_DIR/metrics-docker/"
echo "[up] stop:     ./scripts/docker_down.sh"
