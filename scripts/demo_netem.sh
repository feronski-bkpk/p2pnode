#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

N="${2:-7}"
MODE_ARG="${1:-all}"
RESULTS="$ROOT_DIR/visualization/netem"
mkdir -p "$RESULTS"

if docker compose version >/dev/null 2>&1; then
    COMPOSE=(docker compose)
else
    COMPOSE=(docker-compose)
fi

COMPOSE_FILE="$ROOT_DIR/docker-compose.yml"

run_mode() {
    local mode="$1"
    local delay="$2"
    local loss="$3"
    local jitter="$4"

    echo
    echo "=== Mode: $mode (delay=${delay}ms loss=${loss}% jitter=${jitter}ms) ==="

    local mode_dir="$RESULTS/$mode"
    rm -rf "$mode_dir"
    mkdir -p "$mode_dir"

    "$ROOT_DIR/scripts/docker_down.sh" --clean >/dev/null 2>&1 || true
    # --no-build: image не меняется между режимами.
    "$ROOT_DIR/scripts/docker_up.sh" "$N" star --clean --no-build >/dev/null
    echo "[netem] стенд поднят, 35с сходимости..."
    sleep 35

    if [ "$mode" != "baseline" ]; then
        "$ROOT_DIR/scripts/apply_netem.sh" "$N" "$delay" "$loss" "$jitter" 2>&1 | tail -2
        sleep 3
    fi

    # 15 lookup'ов (быстрее для demo).
    rm -f "$ROOT_DIR/metrics/lookups"/*.json
    "$ROOT_DIR/scripts/docker_lookup_batch.sh" "$N" 15 >/dev/null 2>&1 || true
    mkdir -p "$mode_dir/lookups"
    cp "$ROOT_DIR/metrics/lookups"/*.json "$mode_dir/lookups/" 2>/dev/null || true

    # Файл 256 КиБ (быстрее).
    local src="$mode_dir/src.bin"
    dd if=/dev/urandom of="$src" bs=256K count=1 2>/dev/null
    local src_sha
    src_sha=$(sha256sum "$src" | awk '{print $1}')
    mkdir -p "$ROOT_DIR/state-docker/node-02-send"
    rm -f "$ROOT_DIR/state-docker/node-03-recv/payload.bin"
    cp "$src" "$ROOT_DIR/state-docker/node-02-send/payload.bin"

    local node_03_id
    node_03_id=$("${COMPOSE[@]}" -f "$COMPOSE_FILE" exec -T node-03 \
        sha256sum /state/identity.pub 2>/dev/null | awk '{print $1}')
    echo "[netem] node-03 id: ${node_03_id:0:16}..."

    local send_start
    send_start=$(date +%s%3N)
    "${COMPOSE[@]}" -f "$COMPOSE_FILE" exec -T node-02 \
        /usr/local/bin/node \
        -state-dir /state \
        -no-serve \
        -listen-host 127.0.0.1 \
        -listen-port 29002 \
        -bootstrap node-01:9001 \
        -skip-self-lookup \
        -max-hops 2 \
        -send-to "$node_03_id" \
        -send-file /send/payload.bin \
        -file-chunk-size 32768 \
        -publish-wait-ms 0 \
        -exit-after-ms 60000 \
        -log-level INFO \
        > "$mode_dir/send.log" 2>&1 || true
    local send_end
    send_end=$(date +%s%3N)
    local send_duration=$((send_end - send_start))

    local recv="$ROOT_DIR/state-docker/node-03-recv/payload.bin"
    local recv_sha=""
    if [ -f "$recv" ]; then
        recv_sha=$(sha256sum "$recv" | awk '{print $1}')
    fi

    local match="false"
    if [ "$src_sha" = "$recv_sha" ]; then
        match="true"
    fi

    python3 - "$mode_dir" "$mode" "$src_sha" "$recv_sha" "$send_duration" "$match" <<'PYEOF'
import json, glob, os, sys

mode_dir = sys.argv[1]
mode = sys.argv[2]
src_sha = sys.argv[3]
recv_sha = sys.argv[4]
send_duration_ms = int(sys.argv[5])
match = sys.argv[6] == "true"

lookups = []
for f in glob.glob(os.path.join(mode_dir, "lookups", "lookup-*.json")):
    try:
        lookups.append(json.load(open(f)))
    except Exception:
        pass

rpc = [l.get("rpc", 0) for l in lookups]
dur = [l.get("duration_ms", 0) for l in lookups]
found = sum(1 for l in lookups if any(
    c.get("node_id") == l.get("target")
    for c in (l.get("final_contacts") or [])
))

def q(arr, p):
    if not arr: return 0
    s = sorted(arr)
    return s[min(len(s)-1, int(p*(len(s)-1)))]

summary = {
    "mode": mode,
    "lookups_total": len(lookups),
    "lookups_found": found,
    "lookup_success_rate": found / len(lookups) if lookups else 0,
    "lookup_rpc_mean": sum(rpc)/len(rpc) if rpc else 0,
    "lookup_rpc_median": q(rpc, 0.5),
    "lookup_duration_mean_ms": sum(dur)/len(dur) if dur else 0,
    "lookup_duration_p95_ms": q(dur, 0.95),
    "file_src_sha256": src_sha,
    "file_recv_sha256": recv_sha,
    "file_match": match,
    "file_send_duration_ms": send_duration_ms,
}

with open(os.path.join(mode_dir, "summary.json"), "w") as fh:
    json.dump(summary, fh, indent=2)
print(f"[netem] {mode}: found={found}/{len(lookups)} rpc_mean={summary['lookup_rpc_mean']:.2f} dur_mean={summary['lookup_duration_mean_ms']:.1f}ms file_match={match}")
PYEOF

    "$ROOT_DIR/scripts/clear_netem.sh" "$N" >/dev/null 2>&1 || true
    "$ROOT_DIR/scripts/docker_down.sh" --clean >/dev/null 2>&1 || true
}

print_summary() {
    echo
    echo "[netem] === Сводная таблица ==="
    python3 - "$RESULTS" <<'PYEOF'
import json, os, sys

root = sys.argv[1]
modes = ["baseline", "delay-50", "loss-5", "stress"]
rows = {}
for m in modes:
    path = os.path.join(root, m, "summary.json")
    if os.path.exists(path):
        rows[m] = json.load(open(path))

print()
print(f"{'mode':12} {'found':>8} {'rpc_mean':>10} {'dur_mean':>10} {'dur_p95':>10} {'file_ok':>8} {'dur_ms':>9}")
print("-" * 75)
for m in modes:
    r = rows.get(m, {})
    print(f"{m:12} "
          f"{r.get('lookups_found',0):>3}/{r.get('lookups_total',0):<3} "
          f"{r.get('lookup_rpc_mean',0):>10.2f} "
          f"{r.get('lookup_duration_mean_ms',0):>10.1f} "
          f"{r.get('lookup_duration_p95_ms',0):>10.1f} "
          f"{str(r.get('file_match','?')):>8} "
          f"{r.get('file_send_duration_ms',0):>9}")

with open(os.path.join(root, "summary.json"), "w") as fh:
    json.dump(rows, fh, indent=2)
print()
print(f"[netem] сводка: {os.path.join(root, 'summary.json')}")
PYEOF
}

case "$MODE_ARG" in
    all)
        run_mode "baseline"  0   0  0
        run_mode "delay-50"  50  0  0
        run_mode "loss-5"    0   5  0
        run_mode "stress"    100 10 20
        print_summary
        ;;
    summary)
        print_summary
        ;;
    baseline)
        run_mode "baseline" 0 0 0
        print_summary
        ;;
    delay-50)
        run_mode "delay-50" 50 0 0
        print_summary
        ;;
    loss-5)
        run_mode "loss-5" 0 5 0
        print_summary
        ;;
    stress)
        run_mode "stress" 100 10 20
        print_summary
        ;;
    *)
        echo "usage: $0 {all|baseline|delay-50|loss-5|stress|summary} [N]"
        exit 1
        ;;
esac

echo
echo "[netem] результаты в $RESULTS"
