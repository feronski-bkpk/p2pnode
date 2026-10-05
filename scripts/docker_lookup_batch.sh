#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

N="${1:-15}"
COUNT="${2:-30}"

STATE_BASE="$ROOT_DIR/state-docker"
LOOKUP_DIR="$ROOT_DIR/metrics/lookups"
LOG_DIR="$ROOT_DIR/logs-docker"
TMP_BASE="$ROOT_DIR/tmp-lookup-state"
COMPOSE_FILE="$ROOT_DIR/docker-compose.yml"

rm -rf "$TMP_BASE"
mkdir -p "$LOOKUP_DIR" "$LOG_DIR" "$TMP_BASE"

if docker compose version >/dev/null 2>&1; then
    COMPOSE=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
    COMPOSE=(docker-compose)
else
    echo "[lookup] docker compose не найден" >&2
    exit 1
fi

if [ ! -f "$COMPOSE_FILE" ]; then
    echo "[lookup] $COMPOSE_FILE not found; run docker_up.sh first" >&2
    exit 1
fi

if [ ! -d "$STATE_BASE/node-01" ]; then
    echo "[lookup] $STATE_BASE/node-01 not found; run docker_up.sh first" >&2
    exit 1
fi

declare -a ids
for i in $(seq 1 "$N"); do
    state="$STATE_BASE/node-$(printf '%02d' "$i")"
    if [ ! -f "$state/identity.pub" ]; then
        echo "[lookup] no identity.pub in $state" >&2
        exit 1
    fi
    id="$(sha256sum "$state/identity.pub" | awk '{print $1}')"
    ids[$i]="$id"
done

echo "[lookup] collected $N node IDs"

rm -f "$LOOKUP_DIR"/*.json

python3 - "$N" <<'PYEOF' > /tmp/lookup_pairs.txt
import sys, hashlib

n = int(sys.argv[1])
ids = {}
for i in range(1, n + 1):
    path = f"state-docker/node-{i:02d}/identity.pub"
    with open(path, "rb") as f:
        ids[i] = hashlib.sha256(f.read()).hexdigest()

pairs = []
for i in range(2, n + 1):
    for j in range(2, n + 1):
        if i == j:
            continue
        xi = int(ids[i], 16)
        xj = int(ids[j], 16)
        d = xi ^ xj
        pairs.append((d, i, j))

pairs.sort(key=lambda p: -p[0])
for d, i, j in pairs:
    print(f"{i} {j}")
PYEOF

mapfile -t PAIRS < /tmp/lookup_pairs.txt
TOTAL_PAIRS="${#PAIRS[@]}"
echo "[lookup] всего пар: $TOTAL_PAIRS, берём первые $COUNT"

count=0
for pair in "${PAIRS[@]}"; do
    [ "$count" -ge "$COUNT" ] && break

    read -r i j <<< "$pair"

    target_id="${ids[$j]}"
    short="${target_id:0:8}"

    out_json="$LOOKUP_DIR/lookup-$i-to-$j-$short.json"
    err_log="$LOG_DIR/lookup-$i-to-$j-$short.log"

    svc="$(printf 'node-%02d' "$i")"
    lookup_port=$((19000 + count))

    lookup_state_dir="$TMP_BASE/lookup-$count"
    mkdir -p "$lookup_state_dir"

    echo "[lookup] $((count+1))/$COUNT: initiator=$svc target=node-$j ($short, port=$lookup_port)"

    "${COMPOSE[@]}" -f "$COMPOSE_FILE" run --rm \
        --no-deps \
        -T \
        --entrypoint /usr/local/bin/node \
        -v "$lookup_state_dir:/state" \
        -v "$LOOKUP_DIR:/metrics" \
        "$svc" \
        -state-dir /state \
        -listen-host 0.0.0.0 \
        -listen-port "$lookup_port" \
        -bootstrap "${BOOTSTRAP:-node-01:9001}" \
        -skip-self-lookup \
        -k 4 \
        -alpha 3 \
        -export-dir /metrics \
        -lookup-target "$target_id" \
        -dump-routing \
        -log-level WARN \
        < /dev/null \
        > "$err_log" 2>&1 || true

    latest="$(ls -t "$LOOKUP_DIR"/lookup-"$short"-*.json 2>/dev/null | head -n 1 || true)"
    if [ -n "$latest" ] && [ "$latest" != "$out_json" ]; then
        mv "$latest" "$out_json"
    fi

    rm -rf "$lookup_state_dir"

    count=$((count + 1))
done

rm -f /tmp/lookup_pairs.txt

echo "[lookup] done $count lookups, results in $LOOKUP_DIR"
