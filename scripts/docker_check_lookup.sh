#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

INIT="${1:-7}"
TGT="${2:-13}"

STATE_BASE="$ROOT_DIR/state-docker"
COMPOSE_FILE="$ROOT_DIR/docker-compose.yml"

svc="$(printf 'node-%02d' "$INIT")"
target_state="$STATE_BASE/$(printf 'node-%02d' "$TGT")"

if [ ! -f "$target_state/identity.pub" ]; then
    echo "[check] $target_state/identity.pub not found" >&2
    exit 1
fi

target_id="$(sha256sum "$target_state/identity.pub" | awk '{print $1}')"
echo "[check] initiator=$svc target=$target_id"

mkdir -p /tmp/dbg-lookup
rm -f /tmp/dbg-lookup/*.json

if docker compose version >/dev/null 2>&1; then
    COMPOSE=(docker compose)
else
    COMPOSE=(docker-compose)
fi

"${COMPOSE[@]}" -f "$COMPOSE_FILE" run --rm \
    --no-deps \
    -T \
    --entrypoint /usr/local/bin/node \
    -v "$STATE_BASE/$svc:/state:ro" \
    -v "/tmp/dbg-lookup:/metrics" \
    "$svc" \
    -state-dir /state \
    -listen-host 0.0.0.0 \
    -listen-port 29000 \
    -bootstrap node-01:9001 \
    -skip-self-lookup \
    -k 4 \
    -alpha 3 \
    -export-dir /metrics \
    -lookup-target "$target_id" \
    -dump-routing \
    -log-level DEBUG \
    2>&1 | head -40

echo
echo "[check] result:"
ls -la /tmp/dbg-lookup/
for f in /tmp/dbg-lookup/lookup-*.json; do
    [ -f "$f" ] || continue
    echo "--- $f ---"
    cat "$f" | python3 -m json.tool
done
