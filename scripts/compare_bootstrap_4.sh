#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

N="${1:-15}"
WAIT="${2:-20}"

RESULTS_DIR="$ROOT_DIR/visualization/bootstrap-4"
rm -rf "$RESULTS_DIR"
mkdir -p "$RESULTS_DIR"

run_scheme() {
    local scheme="$1"
    local script="$ROOT_DIR/scripts/run_${scheme}.sh"

    if [ ! -x "$script" ]; then
        echo "[compare4] $script not found, skip" >&2
        return 0
    fi

    echo
    echo "=== $scheme: N=$N, wait=${WAIT}s ==="

    "$ROOT_DIR/scripts/stop_all.sh" >/dev/null 2>&1 || true
    sleep 1
    rm -rf "$STATE_DIR" "$LOG_DIR" "$METRICS_DIR"
    mkdir -p "$STATE_DIR" "$LOG_DIR" "$METRICS_DIR"

    N="$N" "$script" "$N" >/dev/null
    echo "[$scheme] waiting ${WAIT}s..."
    sleep "$WAIT"

    N="$N" "$ROOT_DIR/scripts/collect_routing.sh" >/dev/null

    mkdir -p "$RESULTS_DIR/$scheme"
    cp "$METRICS_DIR/collected"/*.json "$RESULTS_DIR/$scheme/" 2>/dev/null || true
    cp "$LOG_DIR"/node-*.log "$RESULTS_DIR/$scheme/" 2>/dev/null || true

    echo "[$scheme] collected $(ls "$RESULTS_DIR/$scheme"/routing-*.json 2>/dev/null | wc -l) snapshots"

    "$ROOT_DIR/scripts/stop_all.sh" >/dev/null 2>&1 || true
}

for s in star ring tree multiseed; do
    run_scheme "$s"
done

python3 - "$RESULTS_DIR" <<'PYEOF'
import json, glob, os, sys

root = sys.argv[1]
schemes = ["star", "ring", "tree", "multiseed"]
out = {}

for s in schemes:
    d = os.path.join(root, s)
    files = sorted(glob.glob(os.path.join(d, "routing-*.json")))
    if not files:
        continue
    sizes, buckets, max_fills = [], [], []
    edges = set()
    out_deg, in_deg = {}, {}
    for f in files:
        snap = json.load(open(f))
        sizes.append(snap["size"])
        buckets.append(snap["bucket_count"])
        max_fills.append(snap["max_bucket_fill"])
        src = snap["node_id"][:8]
        for b in snap.get("buckets", []):
            for c in b.get("contacts", []):
                dst = c["node_id"][:8]
                edges.add((src, dst))
    for src, dst in edges:
        out_deg[src] = out_deg.get(src, 0) + 1
        in_deg[dst] = in_deg.get(dst, 0) + 1
    out[s] = {
        "nodes": len(files),
        "size_min": min(sizes),
        "size_max": max(sizes),
        "size_mean": sum(sizes) / len(sizes),
        "buckets_mean": sum(buckets) / len(buckets),
        "max_fill_max": max(max_fills),
        "edges": len(edges),
        "max_out_deg": max(out_deg.values()) if out_deg else 0,
        "max_in_deg": max(in_deg.values()) if in_deg else 0,
    }

print()
print(f"{'metric':25} " + " ".join(f"{s:>12}" for s in schemes))
print("-" * (25 + 13 * len(schemes)))
metrics = ["nodes", "size_min", "size_max", "size_mean",
           "buckets_mean", "max_fill_max", "edges",
           "max_out_deg", "max_in_deg"]
for m in metrics:
    row = f"{m:25} "
    for s in schemes:
        v = out.get(s, {}).get(m, "-")
        if isinstance(v, float):
            row += f"{v:>12.2f} "
        else:
            row += f"{v:>12} "
    print(row)

with open(os.path.join(root, "summary.json"), "w") as fh:
    json.dump(out, fh, indent=2)
print()
print("[compare4] summary:", os.path.join(root, "summary.json"))
PYEOF

echo
echo "[compare4] results in $RESULTS_DIR"
