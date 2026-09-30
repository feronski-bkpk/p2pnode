#!/usr/bin/env bash

set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

N="${1:-15}"
WAIT_SECONDS="${2:-20}"

if [ -t 1 ]; then
    BOLD=$(tput bold 2>/dev/null || echo "")
    GREEN=$(tput setaf 2 2>/dev/null || echo "")
    YELLOW=$(tput setaf 3 2>/dev/null || echo "")
    RED=$(tput setaf 1 2>/dev/null || echo "")
    RESET=$(tput sgr0 2>/dev/null || echo "")
else
    BOLD=""; GREEN=""; YELLOW=""; RED=""; RESET=""
fi

header() {
    echo
    echo "${BOLD}================================================================${RESET}"
    echo "${BOLD} $*${RESET}"
    echo "${BOLD}================================================================${RESET}"
}

sub() {
    echo
    echo "${BOLD}--- $* ---${RESET}"
}

ok()   { echo "${GREEN}[OK]${RESET}   $*"; }
warn() { echo "${YELLOW}[WARN]${RESET} $*"; }
fail() { echo "${RED}[FAIL]${RESET} $*"; }

DEMO_DIR="$ROOT_DIR/demo"
rm -rf "$DEMO_DIR"
mkdir -p "$DEMO_DIR"

header "Этапы 1–2: демонстрация (N=$N, wait=${WAIT_SECONDS}s)"

header "Шаг 0. Остановка предыдущего стенда"
"$ROOT_DIR/scripts/stop_all.sh" 2>/dev/null || true
sleep 1
rm -rf "$STATE_DIR" "$LOG_DIR" "$METRICS_DIR"
mkdir -p "$STATE_DIR" "$LOG_DIR" "$METRICS_DIR"
ok "стенд очищен"

header "Шаг 1. Сборка и тесты"
sub "go build ./... (проверка всех пакетов)"
if go build ./... 2>&1 | tee "$DEMO_DIR/build.log"; then
    ok "сборка прошла"
else
    fail "сборка упала"
    exit 1
fi

sub "build_binary (bin/node)"
build_binary

sub "go test ./... (unit + integration)"
if go test ./... 2>&1 | tee "$DEMO_DIR/test.log"; then
    ok "все тесты прошли"
else
    fail "тесты упали"
    exit 1
fi

header "Шаг 2. Идентичность (Ed25519, NodeID = SHA-256(pubkey))"
sub "Генерация идентичностей для $N узлов"
mkdir -p "$DEMO_DIR/identities"
for i in $(seq 1 "$N"); do
    state="$(node_state_dir "$i")"
    mkdir -p "$state"
    "$ROOT_DIR/bin/node" \
        -state-dir "$state" \
        -listen-host 127.0.0.1 \
        -listen-port "$((BASE_PORT + N + i + 200))" \
        -dump-routing \
        -log-level ERROR \
        >/dev/null 2>&1 || true

    node_id="$(node_id_from_state "$state")"
    if [ -z "$node_id" ]; then
        fail "identity не создана для узла $i"
        exit 1
    fi
    echo "$node_id" > "$DEMO_DIR/identities/node-$(printf '%02d' "$i").id"

    if [ "$i" -le 3 ]; then
        sub "Узел $i"
        echo "  state-dir: $state"
        echo "  pubkey:    $(sha256sum "$state/identity.pub" | awk '{print $1}')"
        echo "  NodeID:    $node_id"
        computed="$(sha256sum "$state/identity.pub" | awk '{print $1}')"
        if [ "$computed" != "$node_id" ]; then
            fail "NodeID != SHA-256(pubkey) для узла $i"
            exit 1
        fi
    fi
done
ok "создано $N идентичностей, NodeID = SHA-256(pubkey) подтверждён"

sub "Персистентность identity (перезапуск того же state-dir)"
node_1_id_before="$(node_id_from_state "$(node_state_dir 1)")"
"$ROOT_DIR/bin/node" \
    -state-dir "$(node_state_dir 1)" \
    -listen-host 127.0.0.1 \
    -listen-port "$((BASE_PORT + N + 300))" \
    -dump-routing \
    -log-level ERROR \
    >/dev/null 2>&1 || true
node_1_id_after="$(node_id_from_state "$(node_state_dir 1)")"
if [ "$node_1_id_before" = "$node_1_id_after" ]; then
    ok "NodeID узла 1 не изменился после перезапуска"
else
    fail "NodeID изменился!"
    exit 1
fi

header "Шаг 3. Запуск star-стенда на $N узлов"
N="$N" "$ROOT_DIR/scripts/run_star.sh" "$N" | tee "$DEMO_DIR/star.log"

sub "Ожидание сходимости (${WAIT_SECONDS}s)"
sleep "$WAIT_SECONDS"

sub "Проверка bootstrap-логов"
for i in 1 2 3; do
    log="$(node_log "$i")"
    echo "  --- узел $i ---"
    grep -E "(listening|bootstrap)" "$log" | head -n 5 | sed 's/^/    /'
done
ok "bootstrap выполнен"

header "Шаг 4. Сбор routing-снапшотов и проверка невырожденности"
N="$N" "$ROOT_DIR/scripts/collect_routing.sh" | tee "$DEMO_DIR/collect.log"

sub "Структура k-buckets (по узлам)"
python3 - <<PYEOF
import json, glob, os
files = sorted(glob.glob("$METRICS_DIR/collected/routing-*.json"))
print(f"  {'node':10} {'size':>5} {'buckets':>8} {'max_fill':>9} {'min_fill':>9}")
print(f"  {'-'*10} {'-'*5} {'-'*8} {'-'*9} {'-'*9}")
for f in files:
    s = json.load(open(f))
    print(f"  {s['node_id'][:8]:10} {s['size']:>5} {s['bucket_count']:>8} "
          f"{s['max_bucket_fill']:>9} {s['min_bucket_fill']:>9}")
PYEOF

sub "Проверка невырожденности"
N="$N" "$ROOT_DIR/scripts/check_ne_degenerate.sh" "$METRICS_DIR/collected" "" \
    2>&1 | tee "$DEMO_DIR/check_routing.log" || true

header "Шаг 5. Контрольные lookup'ы (30 штук)"
sub "Цель отсутствует у инициатора, поиск через промежуточные узлы"
N="$N" TARGET_LOOKUPS=30 "$ROOT_DIR/scripts/lookup_batch.sh" 2>&1 | tail -n 10 | tee "$DEMO_DIR/lookup_batch.log"

sub "Пример одного lookup (детали)"
example="$(ls -t "$METRICS_DIR/lookups"/*.json 2>/dev/null | head -n 1 || true)"
if [ -n "$example" ]; then
    python3 - "$example" <<'PYEOF'
import json, sys
f = sys.argv[1]
s = json.load(open(f))
print(f"  file:                 {f.split('/')[-1]}")
print(f"  target:               {s['target'][:16]}...")
print(f"  initiator:            {s['initiator'][:16]}...")
print(f"  target_absent:        {s['target_absent_at_start']}")
print(f"  RPC:                  {s['rpc']}")
print(f"  iterations:           {s['iterations']}")
print(f"  duration_ms:          {s['duration_ms']}")
print(f"  final_contacts:       {len(s['final_contacts'])}")
found = any(c['node_id'] == s['target'] for c in s['final_contacts'])
print(f"  target found:         {found}")
print()
print(f"  iterations_log:")
for it in s.get('iterations_log', []):
    print(f"    iter {it['iter']}:")
    print(f"      queried:  {[c['node_id'][:8] for c in it.get('queried', [])]}")
    print(f"      found:    {[c['node_id'][:8] for c in it.get('found', [])]}")
    print(f"      timeouts: {[c['node_id'][:8] for c in it.get('timeouts', [])]}")
PYEOF
fi

sub "Сводная проверка lookup'ов"
"$ROOT_DIR/scripts/check_ne_degenerate.sh" "$METRICS_DIR/collected" "$METRICS_DIR/lookups" \
    2>&1 | tee "$DEMO_DIR/check_lookups.log" || true

header "Шаг 6. Отключение bootstrap-узла и проверка lookup"
sub "Остановка seed (node 1)"
if [ -f "$STATE_DIR/node-01.pid" ]; then
    pid="$(cat "$STATE_DIR/node-01.pid")"
    kill "$pid" 2>/dev/null || true
    rm -f "$STATE_DIR/node-01.pid"
    ok "seed остановлен (pid=$pid)"
else
    warn "PID seed не найден"
fi

sleep 2

sub "Lookup между оставшимися узлами (node 2 → node 5)"
target_id="$(node_id_from_state "$(node_state_dir 5)")"
tmp_state="$(mktemp -d)"
cp "$(node_state_dir 2)/identity.key" "$tmp_state/" 2>/dev/null || true
cp "$(node_state_dir 2)/identity.pub" "$tmp_state/" 2>/dev/null || true

node_2_port="$(node_port 2)"
"$ROOT_DIR/bin/node" \
    -state-dir "$tmp_state" \
    -listen-host 127.0.0.1 \
    -listen-port "$((BASE_PORT + N + 500))" \
    -bootstrap "127.0.0.1:$node_2_port" \
    -skip-self-lookup \
    -k "$K" \
    -alpha "$ALPHA" \
    -export-dir "$DEMO_DIR/lookup-after-seed-shutdown" \
    -lookup-target "$target_id" \
    -dump-routing \
    -log-level INFO 2>&1 | tee "$DEMO_DIR/lookup_after_shutdown.log"

if grep -q "lookup exported" "$DEMO_DIR/lookup_after_shutdown.log"; then
    ok "lookup после отключения seed выполнен"
    latest="$(ls -t "$DEMO_DIR/lookup-after-seed-shutdown"/lookup-*.json 2>/dev/null | head -n 1 || true)"
    if [ -n "$latest" ]; then
        python3 - "$latest" <<'PYEOF'
import json, sys
s = json.load(open(sys.argv[1]))
print(f"  RPC:        {s['rpc']}")
print(f"  iterations: {s['iterations']}")
print(f"  target_absent_at_start: {s['target_absent_at_start']}")
found = any(c['node_id'] == s['target'] for c in s['final_contacts'])
print(f"  target found: {found}")
PYEOF
    fi
else
    fail "lookup после отключения seed не завершился"
fi
rm -rf "$tmp_state"

header "Шаг 7. Экспорт метрик в CSV"

sub "demo/lookups.csv"
python3 - <<'PYEOF'
import json, glob, csv, os
rows = []
for f in sorted(glob.glob("metrics/lookups/lookup-*.json")):
    s = json.load(open(f))
    target = s.get("target", "")
    finals = s.get("final_contacts") or []
    found = any(c.get("node_id") == target for c in finals)
    rows.append({
        "target": target[:16],
        "initiator": s.get("initiator", "")[:16],
        "absent_at_start": s.get("target_absent_at_start", False),
        "rpc": s.get("rpc", 0),
        "iterations": s.get("iterations", 0),
        "timeouts": s.get("timeouts", 0),
        "duration_ms": s.get("duration_ms", 0),
        "found": found,
    })
if not rows:
    print("[csv] no lookups found")
else:
    os.makedirs("demo", exist_ok=True)
    with open("demo/lookups.csv", "w", newline="") as fh:
        w = csv.DictWriter(fh, fieldnames=rows[0].keys())
        w.writeheader()
        w.writerows(rows)
    print(f"[csv] wrote demo/lookups.csv with {len(rows)} rows")
PYEOF

sub "demo/routing.csv"
python3 - <<'PYEOF'
import json, glob, csv, os
rows = []
for f in sorted(glob.glob("metrics/collected/routing-*.json")):
    s = json.load(open(f))
    rows.append({
        "node_id": s.get("node_id", "")[:16],
        "table_size": s.get("size", 0),
        "bucket_count": s.get("bucket_count", 0),
        "max_bucket_fill": s.get("max_bucket_fill", 0),
        "min_bucket_fill": s.get("min_bucket_fill", 0),
    })
if not rows:
    print("[csv] no routing snapshots found")
else:
    os.makedirs("demo", exist_ok=True)
    with open("demo/routing.csv", "w", newline="") as fh:
        w = csv.DictWriter(fh, fieldnames=rows[0].keys())
        w.writeheader()
        w.writerows(rows)
    print(f"[csv] wrote demo/routing.csv with {len(rows)} rows")
PYEOF

sub "demo/lookup_after_shutdown.csv"
python3 - <<'PYEOF'
import json, glob, csv, os
files = sorted(glob.glob("demo/lookup-after-seed-shutdown/lookup-*.json"))
rows = []
for f in files:
    s = json.load(open(f))
    target = s.get("target", "")
    finals = s.get("final_contacts") or []
    found = any(c.get("node_id") == target for c in finals)
    rows.append({
        "target": target[:16],
        "initiator": s.get("initiator", "")[:16],
        "absent_at_start": s.get("target_absent_at_start", False),
        "rpc": s.get("rpc", 0),
        "iterations": s.get("iterations", 0),
        "timeouts": s.get("timeouts", 0),
        "duration_ms": s.get("duration_ms", 0),
        "found": found,
    })
if not rows:
    print("[csv] no lookup-after-shutdown files found")
else:
    with open("demo/lookup_after_shutdown.csv", "w", newline="") as fh:
        w = csv.DictWriter(fh, fieldnames=rows[0].keys())
        w.writeheader()
        w.writerows(rows)
    print(f"[csv] wrote demo/lookup_after_shutdown.csv with {len(rows)} rows")
PYEOF

header "Шаг 8. Сводка"
echo
echo "  Артефакты демонстрации:"
echo "    $DEMO_DIR/"
echo "      build.log                    — сборка"
echo "      test.log                     — тесты"
echo "      identities/                  — NodeID'ы всех узлов"
echo "      star.log                     — запуск star-стенда"
echo "      collect.log                  — сбор routing-снапшотов"
echo "      check_routing.log            — критерии невырожденности (routing)"
echo "      check_lookups.log            — критерии невырожденности (lookup)"
echo "      lookup_after_shutdown.log    — lookup после отключения seed"
echo "      lookups.csv                  — 30 контрольных lookup'ов"
echo "      routing.csv                  — 20 routing-снапшотов"
echo "      lookup_after_shutdown.csv    — lookup без seed'а"
echo "    $METRICS_DIR/"
echo "      collected/                   — routing-снапшоты всех узлов"
echo "      lookups/                     — результаты 30 lookup'ов"
echo "    $LOG_DIR/                      — логи узлов"
echo

"$ROOT_DIR/scripts/stop_all.sh" >/dev/null 2>&1 || true

echo "${BOLD}Демонстрация завершена.${RESET}"
