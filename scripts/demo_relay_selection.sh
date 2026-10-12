#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

N="${1:-9}"
ITER="${2:-10}"
RESULTS="$ROOT_DIR/visualization/relay-selection"
mkdir -p "$RESULTS"

if docker compose version >/dev/null 2>&1; then
    COMPOSE=(docker compose)
else
    COMPOSE=(docker-compose)
fi

COMPOSE_FILE="$ROOT_DIR/docker-compose.yml"

echo "=== E7-7: relay selection (N=$N, iterations=$ITER) ==="

run_mode() {
    local mode="$1"
    local mode_dir="$RESULTS/$mode"
    rm -rf "$mode_dir"
    mkdir -p "$mode_dir"

    echo
    echo "=== Mode: $mode ==="

    "$ROOT_DIR/scripts/docker_down.sh" --clean >/dev/null 2>&1 || true
    RELAY_SELECT_MODE="$mode" "$ROOT_DIR/scripts/docker_up.sh" "$N" star --clean --no-build >/dev/null
    echo "[relay] стенд поднят (mode=$mode), 40с сходимости..."
    sleep 40

    # Замедляем "плохие" узлы (05, 07, 09).
    for idx in 5 7 9; do
        [ "$idx" -gt "$N" ] && continue
        svc=$(printf 'node-%02d' "$idx")
        container="p2pnode-$svc"
        pid=$(docker inspect -f '{{.State.Pid}}' "$container" 2>/dev/null || echo "")
        if [ -n "$pid" ] && [ "$pid" != "0" ]; then
            sudo nsenter -t "$pid" -n sh -c "
                tc qdisc del dev eth0 root 2>/dev/null || true
                tc qdisc add dev eth0 root netem delay 500ms
            " 2>/dev/null || true
            echo "[relay] $svc: delay 500ms"
        fi
    done

    local node_03_id
    node_03_id=$("${COMPOSE[@]}" -f "$COMPOSE_FILE" exec -T node-03 \
        sha256sum /state/identity.pub 2>/dev/null | awk '{print $1}')
    echo "[relay] node-03 id: ${node_03_id:0:16}..."

    # 10 отдельных процессов, каждый — fresh tunnel.
    local total_start
    total_start=$(date +%s%3N)
    for i in $(seq 1 "$ITER"); do
        "${COMPOSE[@]}" -f "$COMPOSE_FILE" exec -T node-02 \
            /usr/local/bin/node \
            -state-dir /state \
            -no-serve \
            -listen-host 127.0.0.1 \
            -listen-port "$((29000 + i))" \
            -bootstrap node-01:9001 \
            -skip-self-lookup \
            -max-hops 2 \
            -relay-select-mode "$mode" \
            -send-to "$node_03_id" \
            -send-text "hello-$mode-$i" \
            -publish-wait-ms 0 \
            -exit-after-ms 8000 \
            -log-level INFO \
            >> "$mode_dir/send.log" 2>&1 || true
        echo "[relay] $mode: iter $i/$ITER done"
    done
    local total_end
    total_end=$(date +%s%3N)
    local total_duration=$((total_end - total_start))

    python3 - "$mode_dir" "$mode" "$total_duration" "$ITER" <<'PYEOF'
import json, re, os, sys

mode_dir = sys.argv[1]
mode = sys.argv[2]
total_duration_ms = int(sys.argv[3])
iterations = int(sys.argv[4])

log_path = os.path.join(mode_dir, "send.log")
routes_built = 0
accepted_count = 0
subnets = set()
subnet_rejects = 0
rejected_reasons = {}
relay_addrs_used = []
acked = 0

with open(log_path) as f:
    for line in f:
        if "route: built" in line:
            routes_built += 1
        if "route: candidate accepted" in line:
            accepted_count += 1
            m = re.search(r'subnet=(\S+)', line)
            if m:
                subnets.add(m.group(1))
            m2 = re.search(r'addr=(\S+)', line)
            if m2:
                relay_addrs_used.append(m2.group(1))
        if "route: subnet-diversity reject" in line:
            subnet_rejects += 1
        if "route: candidate rejected" in line:
            m = re.search(r'reason=(\S+)', line)
            if m:
                r = m.group(1)
                rejected_reasons[r] = rejected_reasons.get(r, 0) + 1
        if "send: acked" in line:
            acked += 1

summary = {
    "mode": mode,
    "iterations": iterations,
    "total_duration_ms": total_duration_ms,
    "routes_built": routes_built,
    "accepted_candidates": accepted_count,
    "messages_acked": acked,
    "unique_subnets": len(subnets),
    "subnets": sorted(subnets),
    "subnet_diversity_rejects": subnet_rejects,
    "rejected_reasons": rejected_reasons,
    "relay_addrs": sorted(set(relay_addrs_used)),
}

with open(os.path.join(mode_dir, "summary.json"), "w") as fh:
    json.dump(summary, fh, indent=2)

print(f"[relay] {mode}: routes={routes_built} acked={acked}/{iterations} "
      f"accepted={accepted_count} subnets={len(subnets)} "
      f"sub_rej={subnet_rejects} duration_ms={total_duration_ms}")
PYEOF

    "$ROOT_DIR/scripts/docker_down.sh" --clean >/dev/null 2>&1 || true
}

run_mode "ranked"
run_mode "random"
run_mode "subnet"

echo
echo "[relay] === Сводная таблица ==="
python3 - "$RESULTS" <<'PYEOF'
import json, os, sys

root = sys.argv[1]
modes = ["ranked", "random", "subnet"]
rows = {}
for m in modes:
    path = os.path.join(root, m, "summary.json")
    if os.path.exists(path):
        rows[m] = json.load(open(path))

print()
print(f"{'mode':10} {'routes':>7} {'acked':>7} {'accepted':>9} "
      f"{'subnets':>8} {'sub_rej':>8} {'dur_ms':>9}")
print("-" * 70)
for m in modes:
    r = rows.get(m, {})
    print(f"{m:10} "
          f"{r.get('routes_built',0):>7} "
          f"{r.get('messages_acked',0):>7} "
          f"{r.get('accepted_candidates',0):>9} "
          f"{r.get('unique_subnets',0):>8} "
          f"{r.get('subnet_diversity_rejects',0):>8} "
          f"{r.get('total_duration_ms',0):>9}")

with open(os.path.join(root, "summary.json"), "w") as fh:
    json.dump(rows, fh, indent=2)
print()
print(f"[relay] сводка: {os.path.join(root, 'summary.json')}")
PYEOF

echo
echo "[relay] результаты в $RESULTS"
