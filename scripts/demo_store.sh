#!/usr/bin/env bash

set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

N="${1:-5}"
WAIT="${2:-15}"

DEMO_DIR="$ROOT_DIR/demo-store"
rm -rf "$DEMO_DIR"
mkdir -p "$DEMO_DIR"

if [ -t 1 ]; then
    BOLD=$(tput bold 2>/dev/null || echo "")
    GREEN=$(tput setaf 2 2>/dev/null || echo "")
    YELLOW=$(tput setaf 3 2>/dev/null || echo "")
    RESET=$(tput sgr0 2>/dev/null || echo "")
else
    BOLD=""; GREEN=""; YELLOW=""; RESET=""
fi

header() {
    echo
    echo "${BOLD}================================================================${RESET}"
    echo "${BOLD} $*${RESET}"
    echo "${BOLD}================================================================${RESET}"
}

ok()   { echo "${GREEN}[OK]${RESET}   $*"; }
warn() { echo "${YELLOW}[WARN]${RESET} $*"; }

# ============================================================
header "STORE/FIND_VALUE demo (N=$N, wait=${WAIT}s)"
# ============================================================

# 0. Очистка.
header "Шаг 0. Очистка"
"$ROOT_DIR/scripts/stop_all.sh" 2>/dev/null || true
sleep 1
rm -rf "$STATE_DIR" "$LOG_DIR" "$METRICS_DIR"
mkdir -p "$STATE_DIR" "$LOG_DIR" "$METRICS_DIR"
ok "стенд очищен"

# 1. Сборка.
header "Шаг 1. Сборка"
build_binary

# 2. Запуск 5 узлов.
header "Шаг 2. Запуск $N узлов (star)"
N="$N" "$ROOT_DIR/scripts/run_star.sh" "$N" > /dev/null
echo "Ожидание сходимости (${WAIT}s)..."
sleep "$WAIT"

# 3. Проверка bootstrap.
header "Шаг 3. Проверка bootstrap"
for i in 1 2 3; do
    log="$(node_log "$i")"
    echo "  --- узел $i ---"
    grep -E "(listening|bootstrap)" "$log" | head -n 3 | sed 's/^/    /'
done

# Получаем NodeID узла 1.
STATE_1="$(node_state_dir 1)"
NODE1_ID="$(node_id_from_state "$STATE_1")"
echo
echo "  NodeID узла 1: $NODE1_ID"

# 4. Публикация записи узла 1.
header "Шаг 4. Публикация записи узла 1"
# Останавливаем узел 1 и запускаем его снова с -publish-self.
# Проще: запускаем **новый** процесс, который подключается к сети,
# публикует свою запись и выходит.

# Узел 1 уже работает. Мы не можем его перезапустить без потери
# соединений. Поэтому запускаем **отдельный процесс** с тем же
# state-dir, который публикует и выходит.

# Но state-dir занят работающим узлом. Используем копию.
TMP_STATE="$(mktemp -d)"
cp "$STATE_1/identity.key" "$TMP_STATE/" 2>/dev/null || true
cp "$STATE_1/identity.pub" "$TMP_STATE/" 2>/dev/null || true

TMP_PORT=$((BASE_PORT + N + 100))

"$ROOT_DIR/bin/node" \
    -state-dir "$TMP_STATE" \
    -listen-host 127.0.0.1 \
    -listen-port "$TMP_PORT" \
    -bootstrap "127.0.0.1:$BASE_PORT" \
    -skip-self-lookup \
    -publish-self \
    -dump-routing \
    -log-level INFO \
    2>&1 | tee "$DEMO_DIR/publish.log"

echo
ok "запись узла 1 опубликована"

rm -rf "$TMP_STATE"

# 5. Поиск записи с узла 3.
header "Шаг 5. Поиск записи с узла 3"
STATE_3="$(node_state_dir 3)"
TMP_STATE3="$(mktemp -d)"
cp "$STATE_3/identity.key" "$TMP_STATE3/" 2>/dev/null || true
cp "$STATE_3/identity.pub" "$TMP_STATE3/" 2>/dev/null || true

TMP_PORT3=$((BASE_PORT + N + 200))

"$ROOT_DIR/bin/node" \
    -state-dir "$TMP_STATE3" \
    -listen-host 127.0.0.1 \
    -listen-port "$TMP_PORT3" \
    -bootstrap "127.0.0.1:$BASE_PORT" \
    -skip-self-lookup \
    -find-node-id "$NODE1_ID" \
    -dump-routing \
    -log-level INFO \
    2>&1 | tee "$DEMO_DIR/find-1.log"

rm -rf "$TMP_STATE3"

if grep -q "findvalue: found" "$DEMO_DIR/find-1.log"; then
    ok "запись найдена с узла 3"
else
    warn "запись не найдена"
fi

# 6. Остановка одного хранителя.
header "Шаг 6. Остановка хранителя (узел 2)"
if [ -f "$STATE_DIR/node-02.pid" ]; then
    pid="$(cat "$STATE_DIR/node-02.pid")"
    kill "$pid" 2>/dev/null || true
    rm -f "$STATE_DIR/node-02.pid"
    ok "узел 2 остановлен (pid=$pid)"
else
    warn "PID узла 2 не найден"
fi

sleep 2

# 7. Повторный поиск с узла 4.
header "Шаг 7. Поиск записи с узла 4 (после отказа хранителя)"
STATE_4="$(node_state_dir 4)"
TMP_STATE4="$(mktemp -d)"
cp "$STATE_4/identity.key" "$TMP_STATE4/" 2>/dev/null || true
cp "$STATE_4/identity.pub" "$TMP_STATE4/" 2>/dev/null || true

TMP_PORT4=$((BASE_PORT + N + 300))

"$ROOT_DIR/bin/node" \
    -state-dir "$TMP_STATE4" \
    -listen-host 127.0.0.1 \
    -listen-port "$TMP_PORT4" \
    -bootstrap "127.0.0.1:$BASE_PORT" \
    -skip-self-lookup \
    -find-node-id "$NODE1_ID" \
    -dump-routing \
    -log-level INFO \
    2>&1 | tee "$DEMO_DIR/find-2.log"

rm -rf "$TMP_STATE4"

if grep -q "findvalue: found" "$DEMO_DIR/find-2.log"; then
    ok "запись найдена после отказа хранителя"
else
    warn "запись НЕ найдена после отказа хранителя"
fi

# 8. Публикация alias.
header "Шаг 8. Публикация alias 'alice'"
TMP_STATE_ALIAS="$(mktemp -d)"
cp "$STATE_1/identity.key" "$TMP_STATE_ALIAS/" 2>/dev/null || true
cp "$STATE_1/identity.pub" "$TMP_STATE_ALIAS/" 2>/dev/null || true

TMP_PORT_ALIAS=$((BASE_PORT + N + 400))

"$ROOT_DIR/bin/node" \
    -state-dir "$TMP_STATE_ALIAS" \
    -listen-host 127.0.0.1 \
    -listen-port "$TMP_PORT_ALIAS" \
    -bootstrap "127.0.0.1:$BASE_PORT" \
    -skip-self-lookup \
    -publish-alias "alice" \
    -dump-routing \
    -log-level INFO \
    2>&1 | tee "$DEMO_DIR/publish-alias.log"

rm -rf "$TMP_STATE_ALIAS"

# 9. Поиск по alias.
header "Шаг 9. Поиск по alias 'alice'"
TMP_STATE_FA="$(mktemp -d)"
cp "$STATE_3/identity.key" "$TMP_STATE_FA/" 2>/dev/null || true
cp "$STATE_3/identity.pub" "$TMP_STATE_FA/" 2>/dev/null || true

TMP_PORT_FA=$((BASE_PORT + N + 500))

"$ROOT_DIR/bin/node" \
    -state-dir "$TMP_STATE_FA" \
    -listen-host 127.0.0.1 \
    -listen-port "$TMP_PORT_FA" \
    -bootstrap "127.0.0.1:$BASE_PORT" \
    -skip-self-lookup \
    -find-alias "alice" \
    -dump-routing \
    -log-level INFO \
    2>&1 | tee "$DEMO_DIR/find-alias.log"

rm -rf "$TMP_STATE_FA"

if grep -q "findvalue: found" "$DEMO_DIR/find-alias.log"; then
    ok "alias 'alice' найден"
else
    warn "alias 'alice' не найден"
fi

# 10. Итог.
header "Итог"
echo
echo "  Артефакты: $DEMO_DIR/"
echo "    publish.log         — публикация записи узла 1"
echo "    find-1.log          — поиск с узла 3"
echo "    find-2.log          — поиск после отказа хранителя"
echo "    publish-alias.log   — публикация alias"
echo "    find-alias.log      — поиск по alias"
echo

"$ROOT_DIR/scripts/stop_all.sh" > /dev/null 2>&1 || true

echo "${BOLD}Демонстрация STORE/FIND_VALUE завершена.${RESET}"
