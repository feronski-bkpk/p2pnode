#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

N="${1:-5}"
FILE_SIZE_MB="${2:-1}"
MAX_HOPS="${3:-2}"   # для N=5 достаточно 2 ретранслятора
RESULTS="$ROOT_DIR/visualization/file-transfer"
mkdir -p "$RESULTS"

if docker compose version >/dev/null 2>&1; then
    COMPOSE=(docker compose)
else
    COMPOSE=(docker-compose)
fi

COMPOSE_FILE="$ROOT_DIR/docker-compose.yml"

echo "=== E7-4: file transfer (N=$N, size=${FILE_SIZE_MB}MB, max_hops=$MAX_HOPS) ==="

# 1. Стенд.
"$ROOT_DIR/scripts/docker_down.sh" --clean >/dev/null 2>&1 || true
"$ROOT_DIR/scripts/docker_up.sh" "$N" star --clean >/dev/null
echo "[ft] стенд поднят, ждём 40с сходимости и публикации..."
sleep 40

# 2. Тестовый файл.
SRC="$RESULTS/src.bin"
dd if=/dev/urandom of="$SRC" bs=1M count="$FILE_SIZE_MB" 2>/dev/null
SRC_SHA=$(sha256sum "$SRC" | awk '{print $1}')
SRC_SIZE=$(stat -c%s "$SRC")
echo "[ft] src: $SRC ($SRC_SIZE байт, sha256=$SRC_SHA)"

# 3. Копируем файл в /send node-02.
mkdir -p "$ROOT_DIR/state-docker/node-02-send"
cp "$SRC" "$ROOT_DIR/state-docker/node-02-send/payload.bin"
echo "[ft] файл скопирован в node-02-send/payload.bin"

# 4. Получаем NodeID node-03.
NODE_03_ID=$("${COMPOSE[@]}" -f "$COMPOSE_FILE" exec -T node-03 \
    sha256sum /state/identity.pub 2>/dev/null | awk '{print $1}')
if [ -z "$NODE_03_ID" ]; then
    echo "[ft] FAIL: не удалось получить NodeID node-03"
    exit 1
fi
echo "[ft] node-03 id: ${NODE_03_ID:0:16}..."

# 5. Проверяем, что node-03 опубликован в DHT.
echo "[ft] проверка: node-03 опубликован в DHT?"
"${COMPOSE[@]}" -f "$COMPOSE_FILE" exec -T node-02 \
    /usr/local/bin/node \
    -state-dir /state \
    -no-serve \
    -listen-host 127.0.0.1 \
    -listen-port 29002 \
    -bootstrap node-01:9001 \
    -skip-self-lookup \
    -find-node-id "$NODE_03_ID" \
    -publish-wait-ms 0 \
    -exit-after-ms 15000 \
    -log-level INFO \
    > "$RESULTS/check-dht.log" 2>&1 || true

if grep -q "findvalue: found" "$RESULTS/check-dht.log"; then
    echo "[ft] OK: node-03 найден в DHT"
else
    echo "[ft] WARN: node-03 не найден в DHT — см. $RESULTS/check-dht.log"
    tail -20 "$RESULTS/check-dht.log" | sed 's/^/    /'
fi

# 6. node-03 уже готов принимать (RECV_FILE_DIR=/recv в compose).
echo "[ft] node-03 готов принимать файлы (RECV_FILE_DIR=/recv)"

# 7. Отправка из node-02.
# Используем -max-hops, чтобы подстроиться под число узлов.
echo "[ft] отправка из node-02 (max_hops=$MAX_HOPS)..."
"${COMPOSE[@]}" -f "$COMPOSE_FILE" exec -T node-02 \
    /usr/local/bin/node \
    -state-dir /state \
    -no-serve \
    -listen-host 127.0.0.1 \
    -listen-port 29002 \
    -bootstrap node-01:9001 \
    -skip-self-lookup \
    -max-hops "$MAX_HOPS" \
    -send-to "$NODE_03_ID" \
    -send-file /send/payload.bin \
    -file-chunk-size 32768 \
    -publish-wait-ms 0 \
    -exit-after-ms 180000 \
    -log-level INFO \
    > "$RESULTS/send.log" 2>&1 || true

echo "[ft] отправка завершена, см. $RESULTS/send.log"

# 8. Проверка.
RECV="$ROOT_DIR/state-docker/node-03-recv/payload.bin"
if [ ! -f "$RECV" ]; then
    echo "[ft] FAIL: получатель не создал $RECV"
    echo "[ft] последние 30 строк send.log:"
    tail -30 "$RESULTS/send.log" | sed 's/^/    /'
    echo "[ft] логи node-03:"
    "${COMPOSE[@]}" -f "$COMPOSE_FILE" logs node-03 2>&1 | tail -30 | sed 's/^/    /'
    exit 1
fi

RECV_SHA=$(sha256sum "$RECV" | awk '{print $1}')
RECV_SIZE=$(stat -c%s "$RECV")

echo
echo "[ft] === Проверка ==="
echo "[ft] src  size: $SRC_SIZE, sha256: $SRC_SHA"
echo "[ft] recv size: $RECV_SIZE, sha256: $RECV_SHA"

if [ "$SRC_SHA" = "$RECV_SHA" ] && [ "$SRC_SIZE" = "$RECV_SIZE" ]; then
    echo "[ft] OK: размер и SHA-256 совпадают — E7-4 satisfied"
    exit 0
else
    echo "[ft] FAIL: несоответствие"
    exit 1
fi
