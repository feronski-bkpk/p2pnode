#!/usr/bin/env bash
set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

N="${1:-15}"
DEMO_DIR="$ROOT_DIR/visualization/demo-all"
mkdir -p "$DEMO_DIR"

echo "╔══════════════════════════════════════════════════════════════╗"
echo "║  Демонстрация этапов 3–6                                     ║"
echo "║  N=$N, результаты в $DEMO_DIR"                               ║"
echo "╚══════════════════════════════════════════════════════════════╝"
echo

# ---------- ЭТАП 3 ----------
echo "================================================================"
echo "  ЭТАП 3: DHT-хранилище (STORE / FIND_VALUE / R=3 / TTL / alias)"
echo "================================================================"
"$ROOT_DIR/scripts/demo_store.sh" 5 15 2>&1 | tee "$DEMO_DIR/e3-demo-store.log"
echo
echo "[demo] сохранённые артефакты этапа 3:"
ls -la "$DEMO_DIR/e3-demo-store.log"
echo

# ---------- ЭТАП 4 ----------
echo "================================================================"
echo "  ЭТАП 4: Защищённая сессия (AKE, AEAD, anti-replay)"
echo "================================================================"
"$ROOT_DIR/scripts/demo_secure_channel.sh" 2>&1 | tee "$DEMO_DIR/e4-secure-channel.log"
echo

# ---------- ЭТАП 5 ----------
echo "================================================================"
echo "  ЭТАП 5: Туннели через ретрансляторы (E2E, recovery, pool)"
echo "================================================================"
"$ROOT_DIR/scripts/demo_tunnel.sh" 2>&1 | tee "$DEMO_DIR/e5-tunnel.log"
echo
"$ROOT_DIR/scripts/demo_tunnel_recovery.sh" 2>&1 | tee "$DEMO_DIR/e5-recovery.log"
echo
"$ROOT_DIR/scripts/demo_tunnel_pool.sh" 2>&1 | tee "$DEMO_DIR/e5-pool.log"
echo
echo "[demo] артефакты этапа 5:"
ls -la "$DEMO_DIR"/e5-*.log
echo

# ---------- ЭТАП 6 ----------
echo "================================================================"
echo "  ЭТАП 6: Масштабирование (Docker, 4 схемы, N=15–21)"
echo "================================================================"

echo "[demo] 6.1 — N=15 star одной командой"
"$ROOT_DIR/scripts/docker_down.sh" --clean >/dev/null 2>&1 || true
"$ROOT_DIR/scripts/docker_up.sh" "$N" star >/dev/null 2>&1
sleep 30
docker compose ps | tee "$DEMO_DIR/e6-1-ps.log"

echo
echo "[demo] 6.2 — 4 bootstrap-схемы генерируются"
for scheme in star ring tree multiseed; do
    "$ROOT_DIR/scripts/docker_gen_compose.sh" "$N" "$scheme" "/tmp/demo-$scheme.yml" >/dev/null
    echo "  $scheme: $(grep -c BOOTSTRAP_PEERS /tmp/demo-$scheme.yml) bootstrap entries"
done | tee "$DEMO_DIR/e6-2-schemes.log"

echo
echo "[demo] 6.3 — невырожденность + 30 lookup'ов"
"$ROOT_DIR/scripts/docker_collect.sh" >/dev/null 2>&1
"$ROOT_DIR/scripts/docker_lookup_batch.sh" "$N" 30 >/dev/null 2>&1
"$ROOT_DIR/scripts/check_ne_degenerate.sh" 2>&1 | tee "$DEMO_DIR/e6-3-check.log"

echo
echo "[demo] 6.4 — работа без seed'а"
"$ROOT_DIR/scripts/demo_seed_down.sh" "$N" 2>&1 | tee "$DEMO_DIR/e6-4-seed-down.log" | tail -20

echo
echo "[demo] 6.5 — 3 сценария отказа"
"$ROOT_DIR/scripts/docker_down.sh" --clean >/dev/null 2>&1 || true
"$ROOT_DIR/scripts/demo_failures.sh" 7 2>&1 | tee "$DEMO_DIR/e6-5-failures.log" | tail -10

echo
echo "[demo] 6.6 — N=21"
"$ROOT_DIR/scripts/docker_down.sh" --clean >/dev/null 2>&1 || true
"$ROOT_DIR/scripts/run_experiment_n21.sh" 21 40 2>&1 | tee "$DEMO_DIR/e6-6-n21.log" | tail -15

echo
echo "[demo] 6.7 — HTML-отчёт"
"$ROOT_DIR/scripts/generate_report.sh" 2>&1 | tail -10 | tee "$DEMO_DIR/e6-7-report.log"
echo "  report.html: $ROOT_DIR/report.html"

# ---------- ИТОГ ----------
echo
echo "╔══════════════════════════════════════════════════════════════╗"
echo "║  ДЕМОНСТРАЦИЯ ЗАВЕРШЕНА                                      ║"
echo "╚══════════════════════════════════════════════════════════════╝"
echo
echo "Артефакты:"
echo "  $DEMO_DIR/"
echo
echo "Сводка:"
echo "  ЭТАП 3: $(grep -c '\[OK\]' "$DEMO_DIR/e3-demo-store.log" 2>/dev/null || echo 0) OK-проверок"
echo "  ЭТАП 4: $(grep -c 'handshake_done' "$DEMO_DIR/e4-secure-channel.log" 2>/dev/null || echo 0) handshake_done"
echo "  ЭТАП 5: $(grep -c '\[OK\]' "$DEMO_DIR"/e5-*.log 2>/dev/null | awk -F: '{s+=$2} END {print s}') OK-проверок"
echo "  ЭТАП 6: $(grep -c '\[OK\]\|RESULT: NON-DEGENERATE' "$DEMO_DIR"/e6-*.log 2>/dev/null | awk -F: '{s+=$2} END {print s}') OK-проверок"
echo
echo "HTML-отчёт: $ROOT_DIR/report.html"
echo

# Останавливаем стенд
"$ROOT_DIR/scripts/docker_down.sh" --clean >/dev/null 2>&1 || true
