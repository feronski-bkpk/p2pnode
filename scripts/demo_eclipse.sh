#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

N="${1:-11}"
PATHS="${2:-3}"
ITER="${3:-30}"
SYBIL_COUNT="${4:-5}"

RESULTS="$ROOT_DIR/visualization/eclipse"
mkdir -p "$RESULTS"

if docker compose version >/dev/null 2>&1; then
    COMPOSE=(docker compose)
else
    COMPOSE=(docker-compose)
fi

COMPOSE_FILE="$ROOT_DIR/docker-compose.yml"

echo "=== E7-8: Eclipse/Sybil (N=$N, paths=$PATHS, iter=$ITER, sybils=$SYBIL_COUNT) ==="

# ---- Шаг 1: честный стенд ----
echo "[eclipse] запуск $N честных узлов..."
"$ROOT_DIR/scripts/docker_down.sh" --clean >/dev/null 2>&1 || true
"$ROOT_DIR/scripts/docker_up.sh" "$N" star --clean --no-build >/dev/null
sleep 40

# ---- Шаг 2: victim_id ----
VICTIM_ID=$("${COMPOSE[@]}" -f "$COMPOSE_FILE" exec -T node-11 \
    sha256sum /state/identity.pub 2>/dev/null | awk '{print $1}')
echo "[eclipse] victim_id: $VICTIM_ID"

if [ -z "$VICTIM_ID" ]; then
    echo "[eclipse] FAIL: cannot get victim_id" >&2
    exit 1
fi

# ---- Шаг 3: Sybil-идентичности ----
echo "[eclipse] генерация $SYBIL_COUNT Sybil-идентичностей..."
for j in $(seq 1 "$SYBIL_COUNT"); do
    sybil_dir="$ROOT_DIR/state-docker/sybil-$(printf '%02d' "$j")"
    mkdir -p "$sybil_dir"
    "$ROOT_DIR/scripts/gen_sybil_identity.sh" "$VICTIM_ID" "$sybil_dir" 2>&1 | tail -2
done

# ---- Шаг 4: пересоздать compose с Sybil'ами ----
echo "[eclipse] регенерация compose с Sybil'ами..."
WITH_SYBIL=1 SYBIL_COUNT="$SYBIL_COUNT" SYBIL_VICTIM="$VICTIM_ID" \
    "$ROOT_DIR/scripts/docker_gen_compose.sh" "$N" star "$COMPOSE_FILE" >/dev/null

sybil_svcs=""
for j in $(seq 1 "$SYBIL_COUNT"); do
    sybil_svcs="$sybil_svcs sybil-$(printf '%02d' "$j")"
done
echo "[eclipse] запуск Sybil-контейнеров..."
"${COMPOSE[@]}" -f "$COMPOSE_FILE" up -d $sybil_svcs
sleep 20

# ---- Шаг 5: lookup'ы ----
run_mode() {
    local mode="$1"
    local lookup_paths="$2"
    local mode_dir="$RESULTS/$mode"
    rm -rf "$mode_dir"
    mkdir -p "$mode_dir"

    echo
    echo "=== Mode: $mode (lookup_paths=$lookup_paths) ==="

    local correct=0
    local total=$ITER

    for i in $(seq 1 "$total"); do
        local iter_log="$mode_dir/iter-$i.log"

        "${COMPOSE[@]}" -f "$COMPOSE_FILE" exec -T node-02 \
            /usr/local/bin/node \
            -state-dir /state \
            -no-serve \
            -listen-host 127.0.0.1 \
            -listen-port "$((29000 + i))" \
            -bootstrap node-01:9001,node-02:9002,node-03:9003 \
            -skip-self-lookup \
            -lookup-target "$VICTIM_ID" \
            -lookup-paths "$lookup_paths" \
            -export-dir /metrics \
            -dump-routing \
            -log-level INFO \
            > "$iter_log" 2>&1 || true

        local winner=""
        if [ "$lookup_paths" -gt 1 ]; then
            winner=$(grep -o 'winner=[^ ]*' "$iter_log" | head -1 | cut -d= -f2 || echo "")
        else
            winner=$(grep -o '"node_id":"'$VICTIM_ID'"[^}]*"addr":"[^"]*"' "$iter_log" | head -1 | grep -o 'addr":"[^"]*"' | cut -d'"' -f4 || echo "")
        fi

        local is_correct="false"
        if echo "$winner" | grep -q "node-11"; then
            is_correct="true"
        fi
        if [ "$is_correct" = "true" ]; then
            correct=$((correct + 1))
        fi

        if [ $((i % 5)) -eq 0 ]; then
            echo "[eclipse] iter $i/$total: winner=$winner correct=$is_correct"
        fi
    done

    python3 - "$mode_dir" "$mode" "$lookup_paths" "$correct" "$total" "$VICTIM_ID" <<'PYEOF'
import json, os, sys

mode_dir = sys.argv[1]
mode = sys.argv[2]
lookup_paths = int(sys.argv[3])
correct = int(sys.argv[4])
total = int(sys.argv[5])
victim_id = sys.argv[6]

summary = {
    "mode": mode,
    "lookup_paths": lookup_paths,
    "iterations": total,
    "correct_lookups": correct,
    "correct_pct": correct / total if total else 0,
    "victim_id": victim_id,
}
with open(os.path.join(mode_dir, "summary.json"), "w") as f:
    json.dump(summary, f, indent=2)
print(f"[eclipse] {mode}: correct={correct}/{total} ({summary['correct_pct']*100:.1f}%)")
PYEOF
}

run_mode "single" 1
run_mode "multipath" "$PATHS"

echo
echo "[eclipse] === Сводная таблица ==="
python3 - "$RESULTS" <<'PYEOF'
import json, os, sys
root = sys.argv[1]
modes = ["single", "multipath"]
rows = {}
for m in modes:
    p = os.path.join(root, m, "summary.json")
    if os.path.exists(p):
        rows[m] = json.load(open(p))

print()
print(f"{'mode':12} {'paths':>6} {'correct':>10} {'correct_pct':>12}")
print("-" * 45)
for m in modes:
    r = rows.get(m, {})
    print(f"{m:12} {r.get('lookup_paths',0):>6} "
          f"{r.get('correct_lookups',0):>4}/{r.get('iterations',0):<4} "
          f"{r.get('correct_pct',0)*100:>11.1f}%")

with open(os.path.join(root, "summary.json"), "w") as f:
    json.dump(rows, f, indent=2)
PYEOF
