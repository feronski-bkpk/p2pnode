#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="$ROOT_DIR/docker-compose.yml"
CLEAN="${1:-}"

if ! command -v docker >/dev/null 2>&1; then
    echo "[down] docker not found" >&2
    exit 0
fi

if docker compose version >/dev/null 2>&1; then
    COMPOSE=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
    COMPOSE=(docker-compose)
else
    echo "[down] docker compose не найден" >&2
    exit 0
fi

if [ -f "$COMPOSE_FILE" ]; then
    echo "[down] stopping compose project..."
    "${COMPOSE[@]}" -f "$COMPOSE_FILE" down -v --remove-orphans 2>/dev/null || true
else
    echo "[down] no $COMPOSE_FILE, nothing to stop"
fi

if [ "$CLEAN" = "--clean" ]; then
    echo "[down] cleaning state-docker/, logs-docker/, metrics-docker/..."
    sudo rm -rf "$ROOT_DIR/state-docker" \
                "$ROOT_DIR/logs-docker" \
                "$ROOT_DIR/metrics-docker" 2>/dev/null || \
        rm -rf "$ROOT_DIR/state-docker" \
               "$ROOT_DIR/logs-docker" \
               "$ROOT_DIR/metrics-docker" 2>/dev/null || true
fi

echo "[down] done"
