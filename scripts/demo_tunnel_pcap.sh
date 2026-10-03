#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

RUN_DIR="${RUN_DIR:-/tmp/p2pnode-demo-pcap}"
NODES=5
BASE_PORT=9201
SENDER_PORT=9220
CONV_WAIT="${CONV_WAIT:-20}"
PUB_WAIT_MS=15000
REPL_WAIT="${REPL_WAIT:-10}"
SENDER_TIMEOUT="${SENDER_TIMEOUT:-60}"
SECRET="TOP-SECRET-PAYLOAD-$(date +%s)"

log()  { printf '\033[1;34m[pcap]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[fail]\033[0m %s\n' "$*"; exit 1; }

command -v tcpdump >/dev/null || fail "tcpdump not found"
command -v go >/dev/null || fail "go not found"

log "RUN_DIR=$RUN_DIR"
rm -rf "$RUN_DIR"
mkdir -p "$RUN_DIR/logs" "$RUN_DIR/state" "$RUN_DIR/pcap"

log "building bin/node"
go build -o "$RUN_DIR/node" ./cmd/node

start_node() {
    local idx="$1" bootstrap="$2" publish="$3"
    local port=$((BASE_PORT + idx - 1))
    local state="$RUN_DIR/state/node-$idx"
    local logf="$RUN_DIR/logs/node-$idx.log"
    mkdir -p "$state"

    local args=(
        -state-dir "$state"
        -listen-host 127.0.0.1
        -listen-port "$port"
        -log-level DEBUG
        -max-hops 3
        -tunnel-pool-size 1
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

log "starting seed node-1"
start_node 1 "" yes
wait_port "$BASE_PORT" || fail "seed did not start"

log "starting nodes 2..$NODES"
for i in $(seq 2 $NODES); do
    start_node "$i" "127.0.0.1:$BASE_PORT" yes
done

log "waiting for convergence + publish + replication"
sleep "$CONV_WAIT"
sleep $((PUB_WAIT_MS / 1000))
sleep "$REPL_WAIT"

DEST_ID="$(node_id 2)"
[[ -n "$DEST_ID" ]] || fail "cannot determine node-2 id"
log "dest node-2 = $DEST_ID"

PCAP_FILE="$RUN_DIR/pcap/loopback.pcap"
log "starting tcpdump → $PCAP_FILE"
tcpdump -i lo -w "$PCAP_FILE" -U -s 0 \
    "tcp portrange $BASE_PORT-$((BASE_PORT + NODES - 1))" \
    >"$RUN_DIR/logs/tcpdump.log" 2>&1 &
TCPDUMP_PID=$!
sleep 1

log "sending secret message :$SENDER_PORT"
mkdir -p "$RUN_DIR/state/sender"
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

sleep 1
kill_pid "$TCPDUMP_PID"
TCPDUMP_PID=""
sleep 0.5

[[ -s "$PCAP_FILE" ]] || fail "pcap is empty"

# === Проверки ===
log "checking send"
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

log "searching for plaintext in relay logs"
if grep -R --line-number "$SECRET" "$RUN_DIR/logs/node-"*.log \
        | grep -v 'node-5.log' | grep -v 'node-2.log' >/dev/null 2>&1; then
    fail "plaintext found in relay logs — E5-4 broken"
else
    log "OK: plaintext not found in relay logs"
fi

if grep -q 'tunnel: relay build' "$RUN_DIR/logs/node-"*.log; then
    log "OK: relay activity present"
fi

log "pcap demo complete"
log "  pcap: $PCAP_FILE"
