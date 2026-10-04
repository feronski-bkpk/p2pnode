#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

RUN_DIR="${RUN_DIR:-/tmp/p2pnode-demo-capture}"
NODES=5
BASE_PORT=9401
SENDER_PORT=9420
CONV_WAIT="${CONV_WAIT:-20}"
PUB_WAIT_MS=15000
REPL_WAIT="${REPL_WAIT:-10}"
SENDER_TIMEOUT="${SENDER_TIMEOUT:-60}"
SECRET="TOP-SECRET-PAYLOAD-$(date +%s)"

log()  { printf '\033[1;35m[capture]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[fail]\033[0m %s\n' "$*"; exit 1; }

command -v go >/dev/null || fail "go not found"
HAVE_TCPDUMP=0
HAVE_TSHARK=0
HAVE_PYTHON3=0
command -v tcpdump >/dev/null && HAVE_TCPDUMP=1
command -v tshark >/dev/null && HAVE_TSHARK=1
command -v python3 >/dev/null && HAVE_PYTHON3=1
[[ $HAVE_TCPDUMP -eq 1 ]] || fail "tcpdump not found"

log "RUN_DIR=$RUN_DIR"
log "tcpdump=$HAVE_TCPDUMP tshark=$HAVE_TSHARK python3=$HAVE_PYTHON3"
rm -rf "$RUN_DIR"
mkdir -p "$RUN_DIR/logs" "$RUN_DIR/state" "$RUN_DIR/pcap" "$RUN_DIR/export"

log "building bin/node"
go build -o "$RUN_DIR/node" ./cmd/node

start_node() {
    local idx="$1" bootstrap="$2" publish="$3"
    local port=$((BASE_PORT + idx - 1))
    local state="$RUN_DIR/state/node-$idx"
    local logf="$RUN_DIR/logs/node-$idx.log"
    local exp="$RUN_DIR/export/node-$idx"
    mkdir -p "$state" "$exp"

    local args=(
        -state-dir "$state"
        -listen-host 127.0.0.1
        -listen-port "$port"
        -log-level DEBUG
        -max-hops 3
        -tunnel-pool-size 1
        -export-dir "$exp"
    )
    if [[ -n "$bootstrap" ]]; then
        args+=( -bootstrap "$bootstrap" )
    fi
    if [[ "$publish" == "yes" ]]; then
        args+=( -publish-self -publish-wait-ms "$PUB_WAIT_MS" )
    fi

    "$RUN_DIR/node" "${args[@]}" >"$logf" 2>&1 &
    echo $! > "$RUN_DIR/logs/node-$idx.pid"
}

wait_port() {
    local port="$1" deadline=$((SECONDS + 10))
    while ! (echo >"/dev/tcp/127.0.0.1/$port") 2>/dev/null; do
        if (( SECONDS > deadline )); then return 1; fi
        sleep 0.2
    done
    return 0
}

node_id() {
    grep -o 'node_id=[0-9a-f]\{64\}' "$RUN_DIR/logs/node-$1.log" \
        | head -1 | cut -d= -f2
}

kill_pid() {
    local pid="$1"
    if [[ -z "$pid" ]]; then return; fi
    kill "$pid" 2>/dev/null || true
    sleep 1
    kill -9 "$pid" 2>/dev/null || true
}

cleanup() {
    [[ -n "${TCPDUMP_PID:-}" ]] && kill_pid "$TCPDUMP_PID"
    [[ -n "${SENDER_PID:-}" ]] && kill_pid "$SENDER_PID"
    for i in $(seq 1 $NODES); do
        [[ -f "$RUN_DIR/logs/node-$i.pid" ]] && \
            kill_pid "$(cat "$RUN_DIR/logs/node-$i.pid")"
    done
}
trap cleanup EXIT INT TERM

# === Узлы стенда ===
log "starting seed node-1"
start_node 1 "" yes
wait_port "$BASE_PORT" || fail "seed did not start"

log "starting nodes 2..$NODES"
for i in $(seq 2 $NODES); do
    start_node "$i" "127.0.0.1:$BASE_PORT" yes
done

log "waiting for convergence"
sleep "$CONV_WAIT"
sleep $((PUB_WAIT_MS / 1000))
sleep "$REPL_WAIT"

DEST_ID="$(node_id 2)"
[[ -n "$DEST_ID" ]] || fail "cannot determine node-2 id"
log "dest node-2 = $DEST_ID"

# === tcpdump ===
PCAP_FILE="$RUN_DIR/pcap/loopback.pcap"
log "starting tcpdump → $PCAP_FILE"
tcpdump -i lo -w "$PCAP_FILE" -U -s 0 \
    "tcp portrange $BASE_PORT-$((BASE_PORT + NODES - 1))" \
    >"$RUN_DIR/logs/tcpdump.log" 2>&1 &
TCPDUMP_PID=$!
sleep 1

# === Sender ===
log "sending secret message"
mkdir -p "$RUN_DIR/state/sender" "$RUN_DIR/export/sender"
"$RUN_DIR/node" \
    -state-dir "$RUN_DIR/state/sender" \
    -listen-host 127.0.0.1 \
    -listen-port "$SENDER_PORT" \
    -bootstrap "127.0.0.1:$BASE_PORT" \
    -log-level DEBUG \
    -no-serve \
    -publish-self \
    -publish-wait-ms 3000 \
    -send-to "$DEST_ID" \
    -send-text "$SECRET" \
    -max-hops 3 \
    -tunnel-pool-size 1 \
    -export-dir "$RUN_DIR/export/sender" \
    >"$RUN_DIR/logs/send.log" 2>&1 &
SENDER_PID=$!

log "waiting for sender (timeout ${SENDER_TIMEOUT}s)"
WAIT_START=$SECONDS
while kill -0 "$SENDER_PID" 2>/dev/null; do
    if (( SECONDS - WAIT_START > SENDER_TIMEOUT )); then
        warn "sender timeout; killing"
        kill_pid "$SENDER_PID"
        break
    fi
    sleep 2
    if grep -q 'no-serve: exiting' "$RUN_DIR/logs/send.log" 2>/dev/null; then
        log "  sender completed"
        break
    fi
done
wait "$SENDER_PID" 2>/dev/null || true
SENDER_PID=""

sleep 2
kill_pid "$TCPDUMP_PID"
TCPDUMP_PID=""
sleep 0.5

[[ -s "$PCAP_FILE" ]] || fail "pcap is empty"

# === Проверки ===
log "checking send succeeded"
if grep -q 'send: acked' "$RUN_DIR/logs/send.log"; then
    log "OK: message sent successfully"
else
    warn "send did not ack"
fi

log "searching for plaintext in pcap"
if grep -a -q "$SECRET" "$PCAP_FILE"; then
    fail "plaintext found in pcap — E5-4 broken"
else
    log "OK: plaintext not found in pcap (E5-4 satisfied)"
fi

log "searching for protocol keywords in pcap (FIND_NODE, STORE, TUNNEL_BUILD)"
PROTO_KEYWORDS=0
for kw in 'FIND_NODE' 'STORE_REQUEST' 'TUNNEL_BUILD' 'hello-via-tunnel' 'HANDSHAKE'; do
    if grep -a -q "$kw" "$PCAP_FILE"; then
        PROTO_KEYWORDS=$((PROTO_KEYWORDS + 1))
        warn "found keyword '$kw' in pcap"
    fi
done
if (( PROTO_KEYWORDS == 0 )); then
    log "OK: no protocol keywords found in pcap (all encrypted)"
fi

# === tshark: hex первых сегментов ===
FRAMES_JSON="[]"
if [[ $HAVE_TSHARK -eq 1 ]]; then
    log "tshark: extracting payload hex"
    tshark -r "$PCAP_FILE" -Y "tcp.len > 0" -T fields -e data 2>/dev/null \
        > "$RUN_DIR/logs/payloads.txt" || true
    log "  payloads: $(wc -l < "$RUN_DIR/logs/payloads.txt") segments"

    if [[ $HAVE_PYTHON3 -eq 1 ]] && [[ -s "$RUN_DIR/logs/payloads.txt" ]]; then
        log "parsing first segments with python"
        FRAMES_JSON=$(python3 - "$RUN_DIR/logs/payloads.txt" <<'PYEOF'
import sys, json, struct

path = sys.argv[1]
frames = []

# Тип-имена.
TYPE_NAMES = {
    0x0C: "TUNNEL_BUILD",
    0x0D: "TUNNEL_BUILD_OK",
    0x0E: "TUNNEL_BUILD_FAIL",
    0x0F: "TUNNEL_DATA",
    0x10: "TUNNEL_ACK",
    0x11: "TUNNEL_CLOSE",
    0x12: "TUNNEL_BUILD_ACK",
}

def read_msgpack(data):
    """
    Очень простой msgpack-парсер только для полей верхнего уровня.
    Возвращает dict {str: bytes|int|str} или None при ошибке.
    """
    i = 0
    n = len(data)

    def read_byte():
        nonlocal i
        if i >= n: raise ValueError("eof")
        b = data[i]; i += 1
        return b

    def read_bytes():
        nonlocal i
        first = read_byte()
        if first <= 0xbf:
            # fixstr или fixint
            if first <= 0x7f:
                return first  # small int
            if 0xa0 <= first <= 0xbf:
                ln = first - 0xa0
                if i + ln > n: raise ValueError("eof")
                s = data[i:i+ln]; i += ln
                return s
            raise ValueError(f"unknown fixbyte {first:#x}")
        if first == 0xc4:  # bin8
            ln = read_byte()
            if i + ln > n: raise ValueError("eof")
            b = data[i:i+ln]; i += ln
            return b
        if first == 0xc5:  # bin16
            ln = struct.unpack('>H', data[i:i+2])[0]; i += 2
            if i + ln > n: raise ValueError("eof")
            b = data[i:i+ln]; i += ln
            return b
        if first == 0xc6:  # bin32
            ln = struct.unpack('>I', data[i:i+4])[0]; i += 4
            if i + ln > n: raise ValueError("eof")
            b = data[i:i+ln]; i += ln
            return b
        if first == 0xd9:  # str8
            ln = read_byte()
            if i + ln > n: raise ValueError("eof")
            s = data[i:i+ln]; i += ln
            return s
        if first == 0xda:  # str16
            ln = struct.unpack('>H', data[i:i+2])[0]; i += 2
            if i + ln > n: raise ValueError("eof")
            s = data[i:i+ln]; i += ln
            return s
        if first == 0xcc:  # uint8
            return read_byte()
        if first == 0xcd:  # uint16
            v = struct.unpack('>H', data[i:i+2])[0]; i += 2
            return v
        if first == 0xce:  # uint32
            v = struct.unpack('>I', data[i:i+4])[0]; i += 4
            return v
        if first == 0xcf:  # uint64
            v = struct.unpack('>Q', data[i:i+8])[0]; i += 8
            return v
        raise ValueError(f"unknown byte {first:#x}")

    first = read_byte()
    if first == 0x80:  # fixmap 0
        return {}
    if 0x80 <= first <= 0x8f:  # fixmap
        size = first - 0x80
        out = {}
        for _ in range(size):
            key = read_bytes()
            val = read_bytes()
            out[key] = val
        return out
    if first == 0xde:  # map16
        size = struct.unpack('>H', data[i:i+2])[0]; i += 2
        out = {}
        for _ in range(size):
            key = read_bytes()
            val = read_bytes()
            out[key] = val
        return out
    raise ValueError(f"not a map: {first:#x}")

with open(path) as fh:
    for line in fh:
        line = line.strip()
        if not line:
            continue
        try:
            data = bytes.fromhex(line)
        except ValueError:
            continue
        if len(data) < 24:
            continue
        hdr = data[:24]
        version = hdr[0]
        mtype = hdr[1]
        flags = int.from_bytes(hdr[2:4], 'big')
        req_id = hdr[4:20].hex()
        plen = int.from_bytes(hdr[20:24], 'big')
        if version != 1:
            continue
        if mtype not in TYPE_NAMES:
            continue

        payload = data[24:24 + plen]
        msgpack_fields = {}
        ciphertext_aead = b""
        try:
            msgpack_fields = read_msgpack(payload)
            # Извлекаем поле "ciphertext" или "ciphertext_aead".
            if isinstance(msgpack_fields, dict):
                ct = msgpack_fields.get(b"ciphertext")
                if isinstance(ct, (bytes, bytearray)):
                    ciphertext_aead = bytes(ct)
        except Exception:
            pass

        # Первые 32 байта payload — как msgpack-заголовок.
        payload_preview = payload[:48]

        frames.append({
            "type": mtype,
            "type_name": TYPE_NAMES[mtype],
            "header": {
                "version": version,
                "type": mtype,
                "flags": flags,
                "request_id": req_id,
                "payload_length": plen,
            },
            "msgpack_fields": {k.decode('utf-8', 'replace'): v.hex() if isinstance(v, (bytes, bytearray)) else v
                               for k, v in (msgpack_fields.items() if isinstance(msgpack_fields, dict) else [])},
            "ciphertext_aead_hex": ciphertext_aead.hex() if ciphertext_aead else "",
            "payload_preview_hex": payload_preview.hex(),
            "total_hex_prefix": data[:80].hex(),
        })
        if len(frames) >= 20:
            break
print(json.dumps(frames))
PYEOF
)
    fi
else
    log "tshark not found; using hexdump"
    hexdump -C "$PCAP_FILE" | head -100 > "$RUN_DIR/logs/payloads.txt" || true
fi

# === Экспорт verify.json ===
VERIFY_FILE="$RUN_DIR/verify.json"
PLAINTEXT_IN_PCAP="false"
grep -a -q "$SECRET" "$PCAP_FILE" && PLAINTEXT_IN_PCAP="true"

if [[ $HAVE_PYTHON3 -eq 1 ]]; then
    python3 - "$VERIFY_FILE" "$SECRET" "$PCAP_FILE" "$PLAINTEXT_IN_PCAP" "$PROTO_KEYWORDS" "$RUN_DIR" "$HAVE_TSHARK" "$FRAMES_JSON" <<'PYEOF'
import sys, json, os

verify_path = sys.argv[1]
secret = sys.argv[2]
pcap = sys.argv[3]
plaintext_in_pcap = sys.argv[4] == "true"
proto_kw = int(sys.argv[5])
run_dir = sys.argv[6]
have_tshark = sys.argv[7] == "1"
frames = json.loads(sys.argv[8]) if sys.argv[8].strip() else []

def file_has(path, pat):
    """
    pat — str или bytes. Не вызываем .encode() на bytes.
    """
    if isinstance(pat, str):
        pat = pat.encode()
    try:
        with open(path, 'rb') as fh:
            return pat in fh.read()
    except Exception:
        return False

send_acked = file_has(os.path.join(run_dir, "logs/send.log"), "send: acked")
msg_recv = file_has(os.path.join(run_dir, "logs/node-2.log"), "tunnel: message received")
pcap_size = os.path.getsize(pcap) if os.path.exists(pcap) else 0

data = {
    "secret": secret,
    "pcap": pcap,
    "pcap_size_bytes": pcap_size,
    "plaintext_in_pcap": plaintext_in_pcap,
    "protocol_keywords_in_pcap": proto_kw,
    "send_acked": send_acked,
    "message_received_on_dest": msg_recv,
    "node_logs_dir": os.path.join(run_dir, "logs"),
    "tshark_available": have_tshark,
    "frames": frames,
}
with open(verify_path, "w") as fh:
    json.dump(data, fh, indent=2, ensure_ascii=False)
PYEOF
else
    cat > "$VERIFY_FILE" <<EOF
{
  "secret": "$SECRET",
  "pcap": "$PCAP_FILE",
  "pcap_size_bytes": $(stat -c%s "$PCAP_FILE" 2>/dev/null || echo 0),
  "plaintext_in_pcap": $PLAINTEXT_IN_PCAP,
  "protocol_keywords_in_pcap": $PROTO_KEYWORDS,
  "send_acked": $(grep -q 'send: acked' "$RUN_DIR/logs/send.log" && echo true || echo false),
  "message_received_on_dest": $(grep -q 'tunnel: message received' "$RUN_DIR/logs/node-2.log" && echo true || echo false),
  "node_logs_dir": "$RUN_DIR/logs",
  "tshark_available": $([[ $HAVE_TSHARK -eq 1 ]] && echo true || echo false),
  "frames": []
}
EOF
fi

log "verify.json: $VERIFY_FILE"
cat "$VERIFY_FILE" | head -40

log "capture demo complete. Logs in $RUN_DIR/logs/"
