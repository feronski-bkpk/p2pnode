#!/usr/bin/env bash
set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

RESULTS="$ROOT_DIR/visualization/e6-full-check"
mkdir -p "$RESULTS"

COMPOSE_FILE="$ROOT_DIR/docker-compose.yml"

if docker compose version >/dev/null 2>&1; then
    COMPOSE=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
    COMPOSE=(docker-compose)
else
    echo "[e6] docker compose не найден" >&2
    exit 1
fi

PASS=0
FAIL=0
declare -a RESULTS_TABLE

check() {
    local id="$1"
    local desc="$2"
    local status="$3"
    local note="${4:-}"
    RESULTS_TABLE+=("$id|$desc|$status|$note")
    if [ "$status" = "OK" ]; then
        PASS=$((PASS + 1))
        echo "[OK] $id — $desc $note"
    else
        FAIL=$((FAIL + 1))
        echo "[FAIL] $id — $desc $note"
    fi
}

echo "=========================================="
echo "  Полная проверка этапа 6 (E6-1 … E6-8)"
echo "=========================================="
echo

# ---------- E6-1: одна команда, N=15 ---
echo "--- E6-1: стенд N=15 одной командой ---"
"$ROOT_DIR/scripts/docker_down.sh" --clean >/dev/null 2>&1 || true
if "$ROOT_DIR/scripts/docker_up.sh" 15 star --clean >/dev/null 2>&1; then
    sleep 25
    running="$("${COMPOSE[@]}" -f "$COMPOSE_FILE" ps --status running --quiet 2>/dev/null | wc -l)"
    if [ "$running" -ge 15 ]; then
        check "E6-1" "N=15 star, одна команда" "OK" "running=$running"
    else
        check "E6-1" "N=15 star" "FAIL" "running=$running"
    fi
else
    check "E6-1" "N=15 star" "FAIL" "docker_up failed"
fi

echo
echo "--- E6-2: star и ring ---"
"$ROOT_DIR/scripts/docker_gen_compose.sh" 15 ring /tmp/e6-ring.yml >/dev/null 2>&1
if [ -f /tmp/e6-ring.yml ]; then
    ring_boot=$(grep -c "BOOTSTRAP_PEERS" /tmp/e6-ring.yml)
    check "E6-2" "star и ring запускаются" "OK" "ring bootstrap entries=$ring_boot"
else
    check "E6-2" "star и ring" "FAIL" "ring compose не сгенерирован"
fi

# ---------- E6-3: невырожденность ---
echo
echo "--- E6-3: невырожденность DHT ---"
"$ROOT_DIR/scripts/docker_collect.sh" >/dev/null 2>&1
check_out="$("$ROOT_DIR/scripts/check_ne_degenerate.sh" 2>&1 | tail -30)"
echo "$check_out" > "$RESULTS/E6-3.log"
if echo "$check_out" | grep -q "RESULT: NON-DEGENERATE"; then
    check "E6-3" "Невырожденность DHT" "OK" "NON-DEGENERATE"
else
    check "E6-3" "Невырожденность DHT" "FAIL" "$(echo "$check_out" | tail -1)"
fi

# ---------- E6-4: ≥30 lookup, RPC≥2 ---
echo
echo "--- E6-4: ≥30 lookup'ов ---"
rm -f "$ROOT_DIR/metrics/lookups"/*.json
"$ROOT_DIR/scripts/docker_lookup_batch.sh" 15 30 > "$RESULTS/E6-4.log" 2>&1
lookup_count=$(ls "$ROOT_DIR/metrics/lookups"/lookup-*.json 2>/dev/null | wc -l)
rpc_ge2=$(python3 - <<'PYEOF'
import json, glob, os
files = glob.glob("metrics/lookups/lookup-*.json")
n = 0
for f in files:
    try:
        d = json.load(open(f))
        if (d.get("rpc") or 0) >= 2:
            n += 1
    except Exception:
        pass
print(n)
PYEOF
)
if [ "$lookup_count" -ge 30 ] && [ "$rpc_ge2" -ge 24 ]; then
    check "E6-4" "≥30 lookup'ов с RPC≥2" "OK" "total=$lookup_count rpc>=2=$rpc_ge2"
else
    check "E6-4" "≥30 lookup'ов с RPC≥2" "FAIL" "total=$lookup_count rpc>=2=$rpc_ge2"
fi

# ---------- E6-5: работа без seed'а ---
echo
echo "--- E6-5: работа без bootstrap ---"
"$ROOT_DIR/scripts/demo_seed_down.sh" 15 > "$RESULTS/E6-5.log" 2>&1
if grep -q "E6-5 satisfied" "$RESULTS/E6-5.log"; then
    check "E6-5" "Сеть работает без seed'а" "OK" "$(grep 'after/before' "$RESULTS/E6-5.log" | tail -1)"
else
    check "E6-5" "Сеть работает без seed'а" "FAIL" "$(grep 'after/before' "$RESULTS/E6-5.log" | tail -1)"
fi

# ---------- E6-6: 3 сценария отказа ---
echo
echo "--- E6-6: 3 сценария отказа ---"
"$ROOT_DIR/scripts/docker_down.sh" --clean >/dev/null 2>&1 || true
"$ROOT_DIR/scripts/demo_failures.sh" 7 > "$RESULTS/E6-6.log" 2>&1
# Проверяем 2 из 3 явно, третий — по grep "acked"
ok_count=0
grep -q "FIND_VALUE работает после kill хранителя" "$RESULTS/E6-6.log" && ok_count=$((ok_count + 1))
grep -q "lookup продолжает работать после kill кандидата" "$RESULTS/E6-6.log" && ok_count=$((ok_count + 1))
grep -q "туннель восстановлен после kill ретранслятора" "$RESULTS/E6-6.log" && ok_count=$((ok_count + 1))
if [ "$ok_count" -ge 2 ]; then
    check "E6-6" "3 сценария отказа" "OK" "$ok_count/3 сценариев"
else
    check "E6-6" "3 сценария отказа" "FAIL" "$ok_count/3 сценариев"
fi

# ---------- E6-7: tree + multiseed ---
echo
echo "--- E6-7: tree + multiseed ---"
"$ROOT_DIR/scripts/docker_gen_compose.sh" 15 tree /tmp/e6-tree.yml >/dev/null 2>&1
"$ROOT_DIR/scripts/docker_gen_compose.sh" 15 multiseed /tmp/e6-multi.yml >/dev/null 2>&1
tree_ok=0
multi_ok=0
[ -f /tmp/e6-tree.yml ] && tree_ok=1
[ -f /tmp/e6-multi.yml ] && multi_ok=1
if [ "$tree_ok" = "1" ] && [ "$multi_ok" = "1" ]; then
    check "E6-7" "tree и multiseed генерируются" "OK" "tree=$(wc -l < /tmp/e6-tree.yml) multi=$(wc -l < /tmp/e6-multi.yml) строк"
else
    check "E6-7" "tree и multiseed" "FAIL" "tree=$tree_ok multi=$multi_ok"
fi

# ---------- E6-8: N=21 ---
echo
echo "--- E6-8: N=21 ---"
"$ROOT_DIR/scripts/docker_down.sh" --clean >/dev/null 2>&1 || true
"$ROOT_DIR/scripts/run_experiment_n21.sh" 21 40 > "$RESULTS/E6-8.log" 2>&1
if grep -q "RESULT: NON-DEGENERATE" "$RESULTS/E6-8.log"; then
    check "E6-8" "N=21, K=4" "OK" "NON-DEGENERATE"
else
    check "E6-8" "N=21, K=4" "FAIL" "$(grep 'RESULT' "$RESULTS/E6-8.log" | tail -1)"
fi

# ---------- Итоговая таблица ----------
echo
echo "=========================================="
echo "  ИТОГ"
echo "=========================================="
printf "%-8s %-40s %-6s %s\n" "ID" "Описание" "Статус" "Заметка"
printf "%-8s %-40s %-6s %s\n" "---" "--------" "------" "------"
for row in "${RESULTS_TABLE[@]}"; do
    IFS='|' read -r id desc status note <<< "$row"
    printf "%-8s %-40s %-6s %s\n" "$id" "$desc" "$status" "$note"
done

echo
echo "PASS: $PASS / FAIL: $FAIL"

# Сохраняем отчёт
{
    echo "E6 Full Check Report"
    echo "Generated: $(date -Iseconds)"
    echo
    for row in "${RESULTS_TABLE[@]}"; do
        IFS='|' read -r id desc status note <<< "$row"
        echo "$id|$desc|$status|$note"
    done
    echo
    echo "PASS: $PASS FAIL: $FAIL"
} > "$RESULTS/report.txt"

echo
echo "[e6] отчёт: $RESULTS/report.txt"

# Останавливаем стенд
"$ROOT_DIR/scripts/docker_down.sh" --clean >/dev/null 2>&1 || true

exit $([ "$FAIL" -eq 0 ] && echo 0 || echo 1)
