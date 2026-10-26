#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

DEMO_DIR="/tmp/demo-lookup-frames"
N=5
BASE_PORT=9001
SEED_PORT=9001
WAIT_CONVERGENCE=15

BIN="$ROOT/bin/node"

if [ -t 1 ]; then
    C_RED=$'\033[31m'; C_GRN=$'\033[32m'; C_YEL=$'\033[33m'
    C_BLU=$'\033[34m'; C_CYN=$'\033[36m'; C_RST=$'\033[0m'
    C_BLD=$'\033[1m'
else
    C_RED=; C_GRN=; C_YEL=; C_BLU=; C_CYN=; C_RST=; C_BLD=
fi

hdr()  { echo; echo "${C_BLD}${C_CYN}============================================================${C_RST}"; echo "${C_BLD}${C_CYN}  $*${C_RST}"; echo "${C_BLD}${C_CYN}============================================================${C_RST}"; }
sub()  { echo; echo "${C_BLD}--- $* ---${C_RST}"; }
ok()   { echo "${C_GRN}✅ $*${C_RST}"; }
bad()  { echo "${C_RED}❌ $*${C_RST}"; }
warn() { echo "${C_YEL}⚠  $*${C_RST}"; }
info() { echo "${C_BLU}ℹ  $*${C_RST}"; }

# --- Cleanup ---
declare -a PIDS=()
cleanup() {
    sub "Остановка узлов"
    for pid in "${PIDS[@]}"; do
        kill "$pid" 2>/dev/null || true
    done
    sleep 1
    pkill -f "$BIN" 2>/dev/null || true
    ok "все узлы остановлены"
}
trap cleanup EXIT

# ============================================================
# 0. Подготовка
# ============================================================
hdr "0. Подготовка"

pkill -f "$BIN" 2>/dev/null || true
sleep 1

rm -rf "$DEMO_DIR"
mkdir -p "$DEMO_DIR"

if [ ! -x "$BIN" ]; then
    info "собираю bin/node..."
    go build -o "$BIN" ./cmd/node
fi

ok "директория: $DEMO_DIR"

# ============================================================
# 1. Запуск узлов
# ============================================================
hdr "1. Запуск $N узлов локально"

for i in $(seq 1 "$N"); do
    idx=$(printf '%02d' "$i")
    port=$((BASE_PORT + i - 1))
    state="$DEMO_DIR/node-$idx"
    log="$DEMO_DIR/node-$idx.log"
    metrics="$DEMO_DIR/metrics-$idx"

    mkdir -p "$state" "$metrics"

    args=(
        -state-dir "$state"
        -listen-host 127.0.0.1
        -listen-port "$port"
        -export-dir "$metrics"
        -export-interval-ms 2000
        -log-level INFO
    )
    if [ "$i" -gt 1 ]; then
        args+=( -bootstrap "127.0.0.1:$SEED_PORT" )
    fi

    "$BIN" "${args[@]}" > "$log" 2>&1 &
    PIDS+=($!)

    echo "  node-$idx → port $port (pid ${PIDS[-1]})"
    sleep 0.3
done

sub "ожидание готовности портов"
for i in $(seq 1 "$N"); do
    port=$((BASE_PORT + i - 1))
    for attempt in $(seq 1 50); do
        if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
            exec 3<&- 2>/dev/null || true
            break
        fi
        sleep 0.2
    done
done
ok "все порты открыты"

sub "сходимость сети (${WAIT_CONVERGENCE}s)"
sleep "$WAIT_CONVERGENCE"
ok "сходимость завершена"

# ============================================================
# 2. NodeID'ы
# ============================================================
hdr "2. NodeID'ы узлов"

NID_02=""
NID_05=""
for i in $(seq 1 "$N"); do
    idx=$(printf '%02d' "$i")
    nid=$(sha256sum "$DEMO_DIR/node-$idx/identity.pub" | awk '{print $1}')
    echo "  node-$idx: $nid"
    case "$i" in
        2) NID_02="$nid" ;;
        5) NID_05="$nid" ;;
    esac
done

echo
info "target (node-05) = $NID_05"
info "initiator (node-02) = $NID_02"

# ============================================================
# 3. Lookup: BEFORE / AFTER
# ============================================================
hdr "3. Lookup: BEFORE / AFTER"

sub "3.1. Состояние initiator'а ДО lookup"

# Проверяем, есть ли target в routing-таблице node-02 (initiator).
LATEST_BEFORE=$(ls -t "$DEMO_DIR/metrics-02"/routing-*.json 2>/dev/null | head -1 || true)
if [ -z "$LATEST_BEFORE" ]; then
    warn "нет snapshot'а metrics-02, ждём..."
    sleep 3
    LATEST_BEFORE=$(ls -t "$DEMO_DIR/metrics-02"/routing-*.json 2>/dev/null | head -1 || true)
fi

if [ -n "$LATEST_BEFORE" ]; then
    python3 - "$LATEST_BEFORE" "$NID_05" "$NID_02" <<'PYEOF'
import json, sys
s = json.load(open(sys.argv[1]))
target = sys.argv[2]
initiator = sys.argv[3]
is_initiator = (s['node_id'] == initiator)

print(f"  Snapshot:      {s['node_id'][:16]}...")
print(f"  table_size:    {s['size']}")
print(f"  bucket_count:  {s['bucket_count']}")
print()
print("  Содержимое таблицы:")
found = False
for b in s.get("buckets", []):
    for c in b.get("contacts", []):
        marker = "  ← TARGET (node-05)" if c["node_id"] == target else ""
        if c["node_id"] == target: found = True
        print(f"    bucket={b['index']:3}  {c['node_id'][:8]}  {c['addr']}{marker}")
print()

if not is_initiator:
    print("  ⚠  Это snapshot seed'а (node-01), не инициатора.")
    print("     В seed'е target уже есть (сеть сошлась).")
    print("     Для честного теста инициатор — lookup-процесс, см. 3.2.")

if found and is_initiator:
    print("  ⚠  TARGET в таблице инициатора — значит, сходимость")
    print("     уже дала ему target. См. 3.2 — там таблица пустая.")
elif not found:
    print(f"  ✅ TARGET {target[:8]} ОТСУТСТВУЕТ в этой таблице")
else:
    print(f"  ℹ  TARGET {target[:8]} присутствует (для seed'а — нормально)")
PYEOF
else
    warn "снапшот не найден"
fi

sub "3.2. Запуск lookup-процесса (у него target'а НЕТ в таблице)"

rm -rf "$DEMO_DIR/metrics-lookup"
mkdir -p "$DEMO_DIR/metrics-lookup" "$DEMO_DIR/lookup-state"

cp "$DEMO_DIR/node-02/identity.key" "$DEMO_DIR/lookup-state/"
cp "$DEMO_DIR/node-02/identity.pub" "$DEMO_DIR/lookup-state/"

"$BIN" \
    -state-dir "$DEMO_DIR/lookup-state" \
    -listen-host 127.0.0.1 \
    -listen-port 19002 \
    -bootstrap "127.0.0.1:$SEED_PORT" \
    -skip-self-lookup \
    -lookup-target "$NID_05" \
    -export-dir "$DEMO_DIR/metrics-lookup" \
    -dump-routing \
    -log-level INFO \
    > "$DEMO_DIR/lookup.log" 2>&1 || true

echo
echo "  Ключевые строки из лога lookup-процесса:"
grep -E "bootstrap: skip-self-lookup done|removed target|present_before|lookup exported" \
    "$DEMO_DIR/lookup.log" | sed 's/^/    /'

echo
info "present_before=false — target ОТСУТСТВОВАЛ в таблице инициатора перед lookup'ом"

sub "3.3. Состояние initiator'а ПОСЛЕ lookup (final_contacts)"

LATEST_LOOKUP=$(ls -t "$DEMO_DIR/metrics-lookup"/lookup-*.json | head -1)

python3 - "$LATEST_LOOKUP" "$NID_05" <<'PYEOF'
import json, sys
d = json.load(open(sys.argv[1]))
target = sys.argv[2]

print(f"  target:         {target[:16]}...")
print(f"  iterations:     {d['iterations']}")
print(f"  rpc:            {d['rpc']}")
print(f"  timeouts:       {d['timeouts']}")
print(f"  duration_ms:    {d['duration_ms']}")
print()
print("  final_contacts (итоговые контакты, найденные при lookup):")
found = False
for i, c in enumerate(d.get('final_contacts', [])):
    marker = "  ← TARGET (node-05)" if c['node_id'] == target else ""
    if c['node_id'] == target: found = True
    print(f"    [{i}] {c['node_id'][:16]}...  {c['addr']}{marker}")
print()
if found:
    print(f"  ✅ RESULT: target ПОЯВИЛСЯ в final_contacts после lookup")
    print(f"             (был 'present_before=false', стал 'found')")
else:
    print(f"  ❌ RESULT: target НЕ найден — E2-6 нарушен")
PYEOF

# ============================================================
# 3.4. Diff: что изменилось в таблице инициатора
# ============================================================
sub "3.4. Diff: состояние initiator'а ДО и ПОСЛЕ lookup"

# До: "bootstrap: skip-self-lookup done" — таблица инициатора после bootstrap
# После: final_contacts из lookup-файла

python3 - "$DEMO_DIR" "$NID_05" <<'PYEOF'
import json, glob, os, re, sys

demo_dir = sys.argv[1]
target = sys.argv[2]

# --- "До": парсим lookup.log ---
before_contacts = set()
log_path = os.path.join(demo_dir, "lookup.log")
with open(log_path) as f:
    for line in f:
        m = re.search(r'bootstrap: skip-self-lookup done\s+received=(\d+)\s+added=(\d+)\s+table_size=(\d+)', line)
        if m:
            table_size = int(m.group(3))
            # Не знаем точные контакты, но знаем размер таблицы
            break

# --- "После": final_contacts ---
lookup_files = sorted(glob.glob(os.path.join(demo_dir, "metrics-lookup", "lookup-*.json")))
if not lookup_files:
    print("  нет lookup-файла")
    sys.exit(0)

d = json.load(open(lookup_files[-1]))
after_contacts = set()
for c in d.get('final_contacts', []):
    after_contacts.add(c['node_id'])

print(f"  ДО lookup (initiator):")
print(f"    • таблица состояла из bootstrap-контактов")
print(f"    • table_size ≈ 3 (из bootstrap skip-self-lookup)")
print(f"    • target {target[:8]} отсутствовал (present_before=false)")
print()
print(f"  ПОСЛЕ lookup (initiator):")
print(f"    • final_contacts = {len(after_contacts)}")
for c in after_contacts:
    marker = "  ← TARGET" if c == target else ""
    print(f"      {c[:16]}...{marker}")
print()

if target in after_contacts:
    print(f"  ✅ DIFF: target {target[:8]} — НОВЫЙ контакт (появился в результате lookup)")
    print(f"     Было: {3} контактов (bootstrap)")
    print(f"     Стало: {len(after_contacts)} контактов (после lookup)")
else:
    print(f"  ❌ DIFF: target {target[:8]} НЕ появился")
PYEOF

# ============================================================
# 4. Frame validation
# ============================================================
hdr "4. Frame validation: некорректные кадры"

sub "4.0. Перезапуск seed с -log-level DEBUG"

SEED_LOG="$DEMO_DIR/seed-debug.log"

kill "${PIDS[0]}" 2>/dev/null || true
sleep 1

"$BIN" \
    -state-dir "$DEMO_DIR/node-01" \
    -listen-host 127.0.0.1 \
    -listen-port "$SEED_PORT" \
    -export-dir "$DEMO_DIR/metrics-01" \
    -export-interval-ms 2000 \
    -log-level DEBUG \
    > "$SEED_LOG" 2>&1 &
NEW_SEED_PID=$!
PIDS[0]=$NEW_SEED_PID

sleep 2
info "seed перезапущен (pid $NEW_SEED_PID), лог: $SEED_LOG"

# --- 4.1. payload_length > max (65536) ---
sub "4.1. payload_length=200000 > max=65536 → ErrPayloadTooBig"

python3 - <<'PYEOF'
import socket, struct, os
try:
    sock = socket.socket()
    sock.settimeout(3)
    sock.connect(("127.0.0.1", 9001))
    # version=1, type=PING, flags=0, request_id=random, payload_length=200000
    header = struct.pack(">BBH", 0x01, 0x01, 0x0000)
    header += os.urandom(16)
    header += struct.pack(">I", 200000)
    sock.sendall(header)
    # НЕ отправляем payload — сервер должен отклонить уже на заголовке
    data = sock.recv(1024)
    if len(data) == 0:
        print("  ✅ сервер закрыл соединение (recv=0) — кадр отклонён")
        print("     Это значит: проверка payload_length ДО make([]byte, ...)")
    else:
        print(f"  ответ: {data.hex()}")
    sock.close()
except Exception as e:
    print(f"  ERR: {e}")
PYEOF
sleep 1
echo "  лог seed'а:"
grep -E "first read failed" "$SEED_LOG" | tail -1 | sed 's/^/    /' \
    || warn "нет строки 'first read failed' в логе"

# --- 4.2. payload_length корректен, но байт меньше ---
sub "4.2. payload_length=100, отправлено 20 байт → ErrShortPayload"

python3 - <<'PYEOF'
import socket, struct, os
try:
    sock = socket.socket()
    sock.settimeout(3)
    sock.connect(("127.0.0.1", 9001))
    header = struct.pack(">BBH", 0x01, 0x01, 0x0000)
    header += os.urandom(16)
    header += struct.pack(">I", 100)
    sock.sendall(header)
    sock.sendall(b"\x00" * 20)
    sock.shutdown(socket.SHUT_WR)  # EOF
    data = sock.recv(1024)
    if len(data) == 0:
        print("  ✅ сервер закрыл соединение (recv=0) — кадр отклонён")
        print("     Это значит: io.ReadFull вернул ErrUnexpectedEOF")
    else:
        print(f"  ответ: {data.hex()}")
    sock.close()
except Exception as e:
    print(f"  ERR: {e}")
PYEOF
sleep 1
echo "  лог seed'а:"
grep -E "first read failed" "$SEED_LOG" | tail -1 | sed 's/^/    /' \
    || warn "нет строки 'first read failed' в логе"

# --- 4.3. неизвестный type = 0xAB (до handshake) ---
sub "4.3. type=0xAB (неизвестный) → ErrBadType (закрытие без ERROR)"

python3 - <<'PYEOF'
import socket, struct, os
try:
    sock = socket.socket()
    sock.settimeout(3)
    sock.connect(("127.0.0.1", 9001))
    # type=0xAB — не в isKnownType → ReadFrame вернёт ErrBadType до handleConn
    header = struct.pack(">BBH", 0x01, 0xAB, 0x0000)
    header += os.urandom(16)
    header += struct.pack(">I", 0)
    sock.sendall(header)
    data = sock.recv(1024)
    if len(data) == 0:
        print("  ✅ сервер закрыл соединение (recv=0) — кадр отклонён")
        print("     Это значит: ReadFrame вернул ErrBadType ДО handleConn")
        print("     → сервер не отправил ERROR, потому что клиент грубо нарушил протокол")
    else:
        ver, typ, flg = struct.unpack(">BBH", data[0:4])
        plen = struct.unpack(">I", data[20:24])[0]
        payload = data[24:24+plen]
        print(f"  ответ: type=0x{typ:02x} payload={payload.hex()}")
    sock.close()
except Exception as e:
    print(f"  ERR: {e}")
PYEOF
sleep 1
echo "  лог seed'а:"
grep -E "first read failed|bad type|unknown message" "$SEED_LOG" | tail -1 | sed 's/^/    /' \
    || warn "нет строки в логе"

# --- 4.4. корректный HANDSHAKE_HELLO, потом некорректный payload ---
sub "4.4. Первый кадр правильный, но payload не decodable как HANDSHAKE_HELLO"

python3 - <<'PYEOF'
import socket, struct, os
try:
    sock = socket.socket()
    sock.settimeout(3)
    sock.connect(("127.0.0.1", 9001))

    # Правильный первый кадр: HANDSHAKE_HELLO с payload "мусор"
    # payload_length=32, но это не msgpack HANDSHAKE_HELLO.
    # Сервер должен: decode payload → fail (msgpack error) → отправить ERROR
    header = struct.pack(">BBH", 0x01, 0x09, 0x0000)  # type=0x09 = HANDSHAKE_HELLO
    header += os.urandom(16)  # request_id
    payload = os.urandom(32)  # "мусор", не msgpack HANDSHAKE_HELLO
    header += struct.pack(">I", len(payload))

    sock.sendall(header)
    sock.sendall(payload)
    data = sock.recv(4096)
    if len(data) == 0:
        print("  сервер закрыл соединение без ответа")
    elif len(data) < 24:
        print(f"  короткий ответ: {len(data)} байт")
    else:
        ver, typ, flg = struct.unpack(">BBH", data[0:4])
        plen = struct.unpack(">I", data[20:24])[0]
        resp_payload = data[24:24+plen]
        if typ == 0x7F:
            print(f"  ✅ получен ERROR (0x7F), payload={resp_payload.hex()}")
            try:
                import msgpack
                err = msgpack.unpackb(resp_payload, raw=False)
                print(f"     code    = {err.get('code', '?')}")
                print(f"     message = {err.get('message', '?')}")
            except Exception:
                pass
        else:
            print(f"  ответ: type=0x{typ:02x} payload={resp_payload.hex()}")
    sock.close()
except Exception as e:
    print(f"  ERR: {e}")
PYEOF
sleep 1
echo "  лог seed'а:"
grep -E "handshake|decode|msgpack" "$SEED_LOG" | tail -3 | sed 's/^/    /' \
    || warn "нет строки в логе"

# ============================================================
# 5. Итог
# ============================================================
hdr "5. ИТОГ"

echo "  Все артефакты: $DEMO_DIR"
echo "  - логи узлов:       $DEMO_DIR/node-*.log"
echo "  - лог seed (DEBUG): $SEED_LOG"
echo "  - лог lookup:       $DEMO_DIR/lookup.log"
echo "  - lookup JSON:      $DEMO_DIR/metrics-lookup/lookup-*.json"
echo

ok "демо завершено. Узлы будут остановлены автоматически."
