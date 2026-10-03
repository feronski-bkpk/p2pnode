#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

RUN_DIR="${RUN_DIR:-/tmp/p2pnode-demo-recovery}"
NODES=5
BASE_PORT=9101
SENDER_PORT=9120
CONV_WAIT="${CONV_WAIT:-20}"
PUB_WAIT_MS=15000
REPL_WAIT="${REPL_WAIT:-10}"
SEND_INTERVAL_MS=8000
SENDER_TIMEOUT="${SENDER_TIMEOUT:-60}"

log()  { printf '\033[1;34m[recovery]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[fail]\033[0m %s\n' "$*"; exit 1; }

command -v go >/dev/null || fail "go not found"

log "RUN_DIR=$RUN_DIR"
rm -rf "$RUN_DIR"
mkdir -p "$RUN_DIR/logs" "$RUN_DIR/state"

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
        -max-hops 2
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
    log "stopping nodes"
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

log "waiting for convergence + publish + replication"
sleep "$CONV_WAIT"
sleep $((PUB_WAIT_MS / 1000))
sleep "$REPL_WAIT"

DEST_ID="$(node_id 2)"
[[ -n "$DEST_ID" ]] || fail "cannot determine node-2 id"
log "dest node-2 = $DEST_ID"

# === Sender с двумя отправками ===
log "starting sender on :$SENDER_PORT (send-repeat=2, interval=${SEND_INTERVAL_MS}ms, max-hops=2)"
echo
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
    -send-text "msg" \
    -send-repeat 2 \
    -send-interval-ms "$SEND_INTERVAL_MS" \
    -max-hops 2 \
    -tunnel-pool-size 1 \
    -tunnel-ack-timeout-ms 2000 \
    >"$RUN_DIR/logs/send.log" 2>&1 &
SENDER_PID=$!

# === Ждём первую ACK ===
log "waiting for first ACK"
WAIT_START=$SECONDS
until grep -q 'send: acked.*iteration=1' "$RUN_DIR/logs/send.log" 2>/dev/null; do
    if (( SECONDS - WAIT_START > 30 )); then
        warn "timeout waiting for first ACK after 30s"
        break
    fi
    sleep 0.5
done

# === Определяем активный ретранслятор ===
RELAY_IDX=""
if grep -q 'route: built' "$RUN_DIR/logs/send.log"; then
    RELAY_ADDR=$(grep 'route: built' "$RUN_DIR/logs/send.log" \
        | head -1 \
        | grep -o 'path="\[[^ ]*' \
        | sed 's/.*\[//' \
        | sed 's/.*@//' \
        | sed 's/ .*//')
    log "relay addr = $RELAY_ADDR"
    for i in $(seq 1 $NODES); do
        if grep -q "listening addr=$RELAY_ADDR" "$RUN_DIR/logs/node-$i.log" 2>/dev/null; then
            RELAY_IDX="$i"
            break
        fi
    done
fi

if [[ -z "$RELAY_IDX" ]]; then
    warn "cannot determine relay; defaulting to node-3"
    RELAY_IDX=3
fi

log "stopping relay node-$RELAY_IDX"
if [[ -f "$RUN_DIR/logs/node-$RELAY_IDX.pid" ]]; then
    kill_pid "$(cat "$RUN_DIR/logs/node-$RELAY_IDX.pid")"
    log "killed relay node-$RELAY_IDX"
fi

# === Ждём sender с таймаутом ===
log "waiting for sender (timeout ${SENDER_TIMEOUT}s)"
WAIT_START=$SECONDS
while kill -0 "$SENDER_PID" 2>/dev/null; do
    if (( SECONDS - WAIT_START > SENDER_TIMEOUT )); then
        warn "sender timeout after ${SENDER_TIMEOUT}s; killing"
        kill_pid "$SENDER_PID"
        break
    fi
    sleep 2
    if grep -q "send: acked.*iteration=2" "$RUN_DIR/logs/send.log" 2>/dev/null; then
        log "  second iteration ACK received"
        break
    fi
done
wait "$SENDER_PID" 2>/dev/null || true
SENDER_PID=""

# === Проверки ===
ACKED=$(grep -c 'send: acked' "$RUN_DIR/logs/send.log" || echo 0)
log "send: acked × $ACKED"

log "tunnel stats:"
grep 'tunnel stats' "$RUN_DIR/logs/send.log" | tail -3

MSG_COUNT=$(grep -c 'tunnel: message received' "$RUN_DIR/logs/node-2.log" 2>/dev/null || echo 0)
log "node-2 received $MSG_COUNT message(s)"

log "recovery demo complete. Logs in $RUN_DIR/logs/"
