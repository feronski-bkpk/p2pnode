#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

RUN_DIR="${RUN_DIR:-/tmp/p2pnode-demo-tunnel}"
NODES=5
BASE_PORT=9001
SENDER_PORT=9010
CONV_WAIT="${CONV_WAIT:-20}"
PUB_WAIT_MS=15000
REPL_WAIT="${REPL_WAIT:-10}"
SENDER_TIMEOUT="${SENDER_TIMEOUT:-60}"

log()  { printf '\033[1;34m[demo]\033[0m %s\n' "$*"; }
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

log "waiting for convergence + publish + replication (${CONV_WAIT}s + $((PUB_WAIT_MS/1000))s + ${REPL_WAIT}s)"
sleep "$CONV_WAIT"
sleep $((PUB_WAIT_MS / 1000))
sleep "$REPL_WAIT"

log "extracting NodeIDs"
DEST_ID="$(node_id 2)"
[[ -n "$DEST_ID" ]] || fail "cannot determine node-2 id"
log "  node-2 (dest) = $DEST_ID"

# === Sender ===
log "starting sender on :$SENDER_PORT"
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
    -send-text "hello-via-tunnel-$(date +%s)" \
    -max-hops 3 \
    -tunnel-pool-size 1 \
    >"$RUN_DIR/logs/sender.log" 2>&1 &
SENDER_PID=$!

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
done
wait "$SENDER_PID" 2>/dev/null || true
SENDER_PID=""

if grep -q 'no-serve: exiting' "$RUN_DIR/logs/sender.log"; then
    log "sender completed"
else
    warn "sender did not complete; see logs/sender.log"
fi

# === Проверки ===
log "checking relay build entries (E5-3)"
RELAYS=0
for i in $(seq 1 $NODES); do
    if grep -q 'tunnel: relay build' "$RUN_DIR/logs/node-$i.log"; then
        RELAYS=$((RELAYS + 1))
        log "  node-$i acted as relay"
    fi
done
(( RELAYS >= 2 )) && log "OK: $RELAYS relays participated" \
    || warn "only $RELAYS relays (expected ≥2)"

log "checking build ACK from each relay (E5-2)"
if grep -q 'build complete' "$RUN_DIR/logs/sender.log"; then
    grep 'build complete' "$RUN_DIR/logs/sender.log" | tail -1
fi

log "checking route selection logs (E5-7)"
ROUTE_COUNT=$(grep -c 'route:' "$RUN_DIR/logs/sender.log" 2>/dev/null || echo 0)
log "  route: entries = $ROUTE_COUNT"

log "checking E5-4: no plaintext in relay logs"
if grep -R 'hello-via-tunnel' "$RUN_DIR/logs/node-"*.log >/dev/null 2>&1; then
    fail "plaintext found in relay logs — E5-4 broken"
else
    log "OK: plaintext not found in relay logs (E5-4)"
fi

log "checking node-2 received message (E5-5)"
if grep -q 'tunnel: message received' "$RUN_DIR/logs/node-2.log"; then
    log "OK: node-2 received tunnel message"
else
    warn "node-2: no 'message received' in log"
fi

log "demo complete. Logs in $RUN_DIR/logs/"
