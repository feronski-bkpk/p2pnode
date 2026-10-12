#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

N="${1:-7}"
SCHEME="${2:-star}"
RESULTS="$ROOT_DIR/visualization/underlay"
mkdir -p "$RESULTS"

if docker compose version >/dev/null 2>&1; then
    COMPOSE=(docker compose)
else
    COMPOSE=(docker-compose)
fi

COMPOSE_FILE="$ROOT_DIR/docker-compose.yml"

echo "=== E7-5: underlay experiments (N=$N scheme=$SCHEME) ==="

"$ROOT_DIR/scripts/docker_down.sh" --clean >/dev/null 2>&1 || true
"$ROOT_DIR/scripts/docker_up.sh" "$N" "$SCHEME" --clean --no-build >/dev/null
echo "[underlay] стенд поднят, 35с сходимости..."
sleep 35

"$ROOT_DIR/scripts/gen_underlay_rules.sh" "$N" "$SCHEME" "$RESULTS/rules.json"

# check_pair: возвращает OK или BLOCKED.
check_pair() {
    local src="$1"
    local dst="$2"
    local src_svc
    src_svc=$(printf 'node-%02d' "$src")
    local dst_svc
    dst_svc=$(printf 'node-%02d' "$dst")
    local dst_port=$((9000 + dst))

    # nc -z -w 2: attempt TCP connect with 2s timeout.
    if "${COMPOSE[@]}" -f "$COMPOSE_FILE" exec -T "$src_svc" \
        nc -z -w 2 "$dst_svc" "$dst_port" >/dev/null 2>&1; then
        echo "OK"
    else
        echo "BLOCKED"
    fi
}

echo
echo "[underlay] === Connectivity ДО ==="
before_1=$(check_pair 2 1)
before_2=$(check_pair 2 3)
before_3=$(check_pair 3 4)
echo "[underlay] node-02 → node-01:9001 = $before_1"
echo "[underlay] node-02 → node-03:9003 = $before_2"
echo "[underlay] node-03 → node-04:9004 = $before_3"

echo
echo "[underlay] === Применение nftables ==="
"$ROOT_DIR/scripts/apply_underlay.sh" "$N" "$SCHEME" "$RESULTS/rules.json" 2>&1 | tail -10

sleep 2

echo
echo "[underlay] === Connectivity ПОСЛЕ ==="
after_1=$(check_pair 2 1)
after_2=$(check_pair 2 3)
after_3=$(check_pair 3 4)
echo "[underlay] node-02 → node-01:9001 = $after_1  (bootstrap, ожидается OK)"
echo "[underlay] node-02 → node-03:9003 = $after_2  (не в bootstrap, ожидается BLOCKED)"
echo "[underlay] node-03 → node-04:9004 = $after_3  (не в bootstrap, ожидается BLOCKED)"

echo
echo "[underlay] === Lookup через разрешённые связи ==="
rm -f "$ROOT_DIR/metrics/lookups"/*.json
"$ROOT_DIR/scripts/docker_lookup_batch.sh" "$N" 15 >/dev/null 2>&1 || true
mkdir -p "$RESULTS/lookups"
cp "$ROOT_DIR/metrics/lookups"/*.json "$RESULTS/lookups/" 2>/dev/null || true

python3 - "$RESULTS" "$before_1" "$before_2" "$before_3" "$after_1" "$after_2" "$after_3" <<'PYEOF'
import json, glob, os, sys

root = sys.argv[1]
before = {"2->1": sys.argv[2], "2->3": sys.argv[3], "3->4": sys.argv[4]}
after = {"2->1": sys.argv[5], "2->3": sys.argv[6], "3->4": sys.argv[7]}

lookups = []
for f in glob.glob(os.path.join(root, "lookups", "lookup-*.json")):
    try:
        lookups.append(json.load(open(f)))
    except Exception:
        pass

rpc = [l.get("rpc", 0) for l in lookups]
found = sum(1 for l in lookups if any(
    c.get("node_id") == l.get("target")
    for c in (l.get("final_contacts") or [])
))

summary = {
    "before": before,
    "after": after,
    "lookups_total": len(lookups),
    "lookups_found": found,
    "lookup_success_rate": found / len(lookups) if lookups else 0,
    "lookup_rpc_mean": sum(rpc)/len(rpc) if rpc else 0,
}

with open(os.path.join(root, "summary.json"), "w") as f:
    json.dump(summary, f, indent=2)

print()
print(f"lookups: {found}/{len(lookups)} (rate={summary['lookup_success_rate']:.2f})")
print(f"rpc_mean: {summary['lookup_rpc_mean']:.2f}")
print(f"[underlay] summary: {os.path.join(root, 'summary.json')}")

ok = True
# Ожидания:
#   before: всё OK (нет правил).
#   after:  2->1 OK; 2->3, 3->4 BLOCKED.
if before["2->1"] != "OK" or before["2->3"] != "OK" or before["3->4"] != "OK":
    print(f"FAIL: до применения все должны быть OK, got {before}")
    ok = False
if after["2->1"] != "OK":
    print(f"FAIL: 2->1 должен быть OK, got {after['2->1']}")
    ok = False
if after["2->3"] != "BLOCKED":
    print(f"FAIL: 2->3 должен быть BLOCKED, got {after['2->3']}")
    ok = False
if after["3->4"] != "BLOCKED":
    print(f"FAIL: 3->4 должен быть BLOCKED, got {after['3->4']}")
    ok = False
if summary["lookup_success_rate"] < 0.5:
    print(f"FAIL: lookup success rate too low: {summary['lookup_success_rate']:.2f}")
    ok = False

if ok:
    print("[underlay] OK: E7-5 satisfied")
    sys.exit(0)
else:
    print("[underlay] FAIL")
    sys.exit(1)
PYEOF

echo
echo "[underlay] результаты в $RESULTS"
