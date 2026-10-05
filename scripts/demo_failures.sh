#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

N="${1:-7}"
RESULTS="$ROOT_DIR/visualization/failures"
COMPOSE_FILE="$ROOT_DIR/docker-compose.yml"

mkdir -p "$RESULTS"

if docker compose version >/dev/null 2>&1; then
    COMPOSE=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
    COMPOSE=(docker-compose)
else
    echo "[fail] docker compose не найден" >&2
    exit 1
fi

stop_node() {
    local idx="$1"
    local svc
    svc="$(printf 'node-%02d' "$idx")"
    echo "[fail] stopping $svc"
    "${COMPOSE[@]}" -f "$COMPOSE_FILE" stop "$svc" 2>&1 | tail -1
}

start_node() {
    local idx="$1"
    local svc
    svc="$(printf 'node-%02d' "$idx")"
    echo "[fail] starting $svc"
    "${COMPOSE[@]}" -f "$COMPOSE_FILE" start "$svc" 2>&1 | tail -1
}

echo "=== E6-6: failure scenarios (N=$N) ==="

echo "[fail] старт стенда N=$N..."
"$ROOT_DIR/scripts/docker_down.sh" --clean >/dev/null 2>&1 || true
"$ROOT_DIR/scripts/docker_up.sh" "$N" star --clean >/dev/null
sleep 30
echo "[fail] ждём 30с сходимости... ok"

echo
echo "=== [1/3] Отказ хранителя ==="
echo "[1] ждём 20с, чтобы node-01 опубликовал свою запись..."
sleep 20

node_01_id="$("${COMPOSE[@]}" -f "$COMPOSE_FILE" exec -T node-01 sha256sum /state/identity.pub 2>/dev/null | awk '{print $1}' || echo '')"
echo "[1] node-01 id: ${node_01_id:0:16}…"

echo "[1] baseline: FIND_VALUE через node-02..."
"${COMPOSE[@]}" -f "$COMPOSE_FILE" exec -T node-02 /usr/local/bin/node \
    -state-dir /state \
    -no-serve \
    -listen-host 127.0.0.1 \
    -listen-port 29002 \
    -find-node-id "$node_01_id" \
    -log-level INFO \
    > "$RESULTS/1-findvalue-before.log" 2>&1 || true

if grep -q "findvalue: found" "$RESULTS/1-findvalue-before.log"; then
    echo "[1] baseline OK: запись найдена ДО kill"
else
    echo "[1] baseline FAIL: см. $RESULTS/1-findvalue-before.log"
    tail -5 "$RESULTS/1-findvalue-before.log" | sed 's/^/    /'
fi

victim=5
echo "[1] убиваем node-0$victim (потенциальный хранитель)..."
stop_node "$victim"
sleep 3

echo "[1] FIND_VALUE после kill..."
"${COMPOSE[@]}" -f "$COMPOSE_FILE" exec -T node-02 /usr/local/bin/node \
    -state-dir /state \
    -no-serve \
    -listen-host 127.0.0.1 \
    -listen-port 29003 \
    -find-node-id "$node_01_id" \
    -log-level INFO \
    > "$RESULTS/1-findvalue-after-kill.log" 2>&1 || true

if grep -q "findvalue: found" "$RESULTS/1-findvalue-after-kill.log"; then
    echo "[1] OK: FIND_VALUE работает после kill хранителя"
else
    echo "[1] FAIL: FIND_VALUE не нашёл запись"
    tail -5 "$RESULTS/1-findvalue-after-kill.log" | sed 's/^/    /'
fi

start_node "$victim"
sleep 3

echo
echo "=== [2/3] Отказ кандидата lookup ==="

echo "[2] baseline: batch lookup'ов..."
rm -f "$ROOT_DIR/metrics/lookups"/*.json
"$ROOT_DIR/scripts/docker_lookup_batch.sh" "$N" 15 2>&1 | tail -3
cp -r "$ROOT_DIR/metrics/lookups" "$RESULTS/2-before-lookups"

victim=4
echo "[2] убиваем node-0$victim во время серии..."
stop_node "$victim"
sleep 2

echo "[2] after: batch lookup'ов..."
rm -f "$ROOT_DIR/metrics/lookups"/*.json
"$ROOT_DIR/scripts/docker_lookup_batch.sh" "$N" 15 2>&1 | tail -3
cp -r "$ROOT_DIR/metrics/lookups" "$RESULTS/2-after-lookups"

python3 - "$RESULTS" <<'PYEOF'
import json, glob, os, sys
root = sys.argv[1]
def summarize(dirpath):
    files = sorted(glob.glob(os.path.join(dirpath, "lookup-*.json")))
    if not files: return None
    found = 0
    for f in files:
        try:
            d = json.load(open(f))
        except Exception:
            continue
        tgt = d.get("target")
        finals = d.get("final_contacts") or []
        if tgt and any(c.get("node_id") == tgt for c in finals):
            found += 1
    return {"total": len(files), "found": found}
b = summarize(os.path.join(root, "2-before-lookups"))
a = summarize(os.path.join(root, "2-after-lookups"))
print(f"[2] before: {b}")
print(f"[2] after:  {a}")
if b and a and b["total"] > 0:
    ratio = a["found"] / max(b["found"], 1)
    print(f"[2] ratio after/before: {ratio:.2f}")
    if ratio >= 0.6:
        print("[2] OK: lookup продолжает работать после kill кандидата")
PYEOF

start_node "$victim"
sleep 3

echo
echo "=== [3/3] Отказ активного ретранслятора ==="

if [ -x "$ROOT_DIR/scripts/demo_tunnel_recovery.sh" ]; then
    echo "[3] запуск demo_tunnel_recovery.sh..."
    "$ROOT_DIR/scripts/demo_tunnel_recovery.sh" > "$RESULTS/3-tunnel-recovery.log" 2>&1 || true

    if grep -q "acked" "$RESULTS/3-tunnel-recovery.log"; then
        echo "[3] OK: туннель восстановлен после kill ретранслятора"
    else
        echo "[3] WARN: см. $RESULTS/3-tunnel-recovery.log"
    fi
else
    echo "[3] SKIP: demo_tunnel_recovery.sh не найден"
fi

echo
echo "=== E6-6: done ==="
echo "[fail] логи: $RESULTS/"
echo "[fail] стенд оставлен запущенным."
echo "[fail] остановить: ./scripts/docker_down.sh --clean"
