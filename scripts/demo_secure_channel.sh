#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

RESULTS="$ROOT_DIR/visualization/secure-channel"
mkdir -p "$RESULTS"

SEED_STATE="/tmp/demo-sec-seed"
CLIENT_STATE="/tmp/demo-sec-client"
SEED_PORT=9500
CLIENT_PORT=9501

echo "================================================================"
echo " Демонстрация защищённого канала (этап 4, AKE + AEAD)"
echo "================================================================"

# Очистка
rm -rf "$SEED_STATE" "$CLIENT_STATE"
mkdir -p "$SEED_STATE" "$CLIENT_STATE"

# Сборка
echo
echo "[1] Сборка..."
go build -o "$ROOT_DIR/bin/node" ./cmd/node

# Запуск seed в фоне
echo
echo "[2] Запуск seed (порт $SEED_PORT)..."
"$ROOT_DIR/bin/node" \
    -state-dir "$SEED_STATE" \
    -listen-host 127.0.0.1 \
    -listen-port "$SEED_PORT" \
    -log-level DEBUG \
    > "$RESULTS/seed.log" 2>&1 &
SEED_PID=$!

# Ждём, пока seed поднимется
for i in $(seq 1 20); do
    if grep -q "listening" "$RESULTS/seed.log" 2>/dev/null; then
        break
    fi
    sleep 0.2
done

echo "    seed запущен (pid=$SEED_PID)"

# Запуск клиента
echo
echo "[3] Запуск клиента (порт $CLIENT_PORT) с bootstrap на seed..."
"$ROOT_DIR/bin/node" \
    -state-dir "$CLIENT_STATE" \
    -listen-host 127.0.0.1 \
    -listen-port "$CLIENT_PORT" \
    -bootstrap "127.0.0.1:$SEED_PORT" \
    -no-serve \
    -exit-after-ms 3000 \
    -log-level DEBUG \
    > "$RESULTS/client.log" 2>&1 || true

# Даём немного времени, чтобы seed увидел закрытие соединения
sleep 1

# Останавливаем seed
echo
echo "[4] Останавливаем seed..."
kill "$SEED_PID" 2>/dev/null || true
wait "$SEED_PID" 2>/dev/null || true

# Анализ
echo
echo "================================================================"
echo " Результаты"
echo "================================================================"

echo
echo "[handshake] Из лога seed:"
grep -E "handshake_start|handshake_done|session_id" "$RESULTS/seed.log" | head -5 | sed 's/^/    /'

echo
echo "[handshake] Из лога клиента:"
grep -E "handshake_start|handshake_done|session_id" "$RESULTS/client.log" | head -5 | sed 's/^/    /'

# Проверки
OK_COUNT=0
FAIL_COUNT=0

check() {
    local desc="$1"
    local pattern="$2"
    local file="$3"
    if grep -q "$pattern" "$file"; then
        echo "[OK]   $desc"
        OK_COUNT=$((OK_COUNT + 1))
    else
        echo "[FAIL] $desc"
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
}

echo
echo "[check] Проверки:"
check "seed: handshake_start" "handshake_start" "$RESULTS/seed.log"
check "seed: handshake_done"  "handshake_done"  "$RESULTS/seed.log"
check "client: handshake_start" "handshake_start" "$RESULTS/client.log"
check "client: handshake_done"  "handshake_done"  "$RESULTS/client.log"

echo
echo "[session_id] Уникальные session_id:"
SESSION_IDS=$(grep -oE "session_id=[0-9a-f]+" "$RESULTS/seed.log" "$RESULTS/client.log" 2>/dev/null | sort -u | head -3 || true)
if [ -n "$SESSION_IDS" ]; then
    echo "$SESSION_IDS" | sed 's/^/    /'
    OK_COUNT=$((OK_COUNT + 1))
else
    echo "    (не найдены)"
fi

echo
echo "[AEAD] Проверка, что payload зашифрован — через Go-тесты:"
echo "    go test -v ./internal/crypto/ -run 'TestAEAD|TestReplay|TestHandshake'"
go test -v ./internal/crypto/ -run 'TestAEAD|TestReplay|TestHandshake' 2>&1 | tail -10 | sed 's/^/    /' || true

echo
echo "================================================================"
echo " Итог: OK=$OK_COUNT FAIL=$FAIL_COUNT"
echo " Артефакты: $RESULTS/"
echo "   seed.log    — лог seed'а (handshake, session_id)"
echo "   client.log  — лог клиента (bootstrap, handshake)"
echo "================================================================"

exit $([ "$FAIL_COUNT" -eq 0 ] && echo 0 || echo 1)
