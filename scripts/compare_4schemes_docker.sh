#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

N="${1:-15}"
WAIT="${2:-30}"
RESULTS="$ROOT_DIR/visualization/bootstrap-4-docker"
mkdir -p "$RESULTS"

for scheme in star ring tree multiseed; do
    echo
    echo "=== scheme: $scheme ==="

    "$ROOT_DIR/scripts/docker_down.sh" --clean >/dev/null 2>&1 || true
    "$ROOT_DIR/scripts/docker_up.sh" "$N" "$scheme" --clean >/dev/null
    echo "[$scheme] запущен, ждём ${WAIT}с..."
    sleep "$WAIT"

    "$ROOT_DIR/scripts/docker_collect.sh" >/dev/null
    mkdir -p "$RESULTS/$scheme"
    cp "$ROOT_DIR/metrics/collected"/*.json "$RESULTS/$scheme/" 2>/dev/null || true
    echo "[$scheme] собрано $(ls "$RESULTS/$scheme"/routing-*.json 2>/dev/null | wc -l) snapshots"

    "$ROOT_DIR/scripts/docker_down.sh" --clean >/dev/null 2>&1 || true
done

python3 - "$RESULTS" <<'PYEOF'
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
    n = len(files)
    full_registry = sum(1 for s in sizes if s >= n - 1)
    less = sum(1 for s in sizes if s < n - 1)
    out[s] = {
        "nodes": n,
        "size_min": min(sizes),
        "size_max": max(sizes),
        "size_mean": sum(sizes) / n,
        "buckets_mean": sum(buckets) / n,
        "max_fill_max": max(max_fills),
        "edges": len(edges),
        "max_out_deg": max(out_deg.values()) if out_deg else 0,
        "max_in_deg": max(in_deg.values()) if in_deg else 0,
        "full_registry_count": full_registry,
        "pct_less_n_minus_1": 100.0 * less / n,
    }

print()
print(f"{'metric':25} " + " ".join(f"{s:>12}" for s in schemes))
print("-" * (25 + 13 * len(schemes)))
metrics = ["nodes", "size_min", "size_max", "size_mean",
           "buckets_mean", "max_fill_max", "edges",
           "max_out_deg", "max_in_deg",
           "full_registry_count", "pct_less_n_minus_1"]
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
echo "[compare4] результаты в $RESULTS"
