#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

N="${1:-10}"
WAIT="${2:-15}"

RESULTS_DIR="$ROOT_DIR/visualization/star-vs-ring"
rm -rf "$RESULTS_DIR"
mkdir -p "$RESULTS_DIR"

LOOKUPS_BACKUP=""
if [ -d "$METRICS_DIR/lookups" ]; then
    LOOKUPS_BACKUP="$(mktemp -d)"
    cp -r "$METRICS_DIR/lookups" "$LOOKUPS_BACKUP/" 2>/dev/null || true
fi

restore_lookups() {
    if [ -n "$LOOKUPS_BACKUP" ] && [ -d "$LOOKUPS_BACKUP/lookups" ]; then
        mkdir -p "$METRICS_DIR"
        rm -rf "$METRICS_DIR/lookups"
        cp -r "$LOOKUPS_BACKUP/lookups" "$METRICS_DIR/" 2>/dev/null || true
    fi
    if [ -n "$LOOKUPS_BACKUP" ]; then
        rm -rf "$LOOKUPS_BACKUP" 2>/dev/null || true
    fi
}

trap restore_lookups EXIT INT TERM

# ============================================================
# Star
# ============================================================
echo
echo "=== Star: N=$N, wait=${WAIT}s ==="

clean_all
N="$N" "$ROOT_DIR/scripts/run_star.sh" "$N" > /dev/null

echo "[star] waiting ${WAIT}s..."
sleep "$WAIT"

N="$N" "$ROOT_DIR/scripts/collect_routing.sh" > /dev/null
echo "[star] collected $(ls "$METRICS_DIR/collected"/routing-*.json 2>/dev/null | wc -l) snapshots"

mkdir -p "$RESULTS_DIR/star"
cp "$METRICS_DIR/collected"/*.json "$RESULTS_DIR/star/" 2>/dev/null || true
cp "$LOG_DIR"/node-*.log "$RESULTS_DIR/star/" 2>/dev/null || true

"$ROOT_DIR/scripts/visualize.sh" \
    "$METRICS_DIR/collected" \
    "$RESULTS_DIR/star-overlay" \
    "Star bootstrap (N=$N)"

"$ROOT_DIR/scripts/stop_all.sh" > /dev/null 2>&1 || true

# ============================================================
# Ring
# ============================================================
echo
echo "=== Ring: N=$N, wait=${WAIT}s ==="

clean_all
N="$N" "$ROOT_DIR/scripts/run_ring.sh" "$N" > /dev/null

echo "[ring] waiting ${WAIT}s..."
sleep "$WAIT"

N="$N" "$ROOT_DIR/scripts/collect_routing.sh" > /dev/null
echo "[ring] collected $(ls "$METRICS_DIR/collected"/routing-*.json 2>/dev/null | wc -l) snapshots"

mkdir -p "$RESULTS_DIR/ring"
cp "$METRICS_DIR/collected"/*.json "$RESULTS_DIR/ring/" 2>/dev/null || true
cp "$LOG_DIR"/node-*.log "$RESULTS_DIR/ring/" 2>/dev/null || true

"$ROOT_DIR/scripts/visualize.sh" \
    "$METRICS_DIR/collected" \
    "$RESULTS_DIR/ring-overlay" \
    "Ring bootstrap (N=$N)"

"$ROOT_DIR/scripts/stop_all.sh" > /dev/null 2>&1 || true

# ============================================================
# Сравнительная таблица
# ============================================================
echo
echo "=== Сравнение star vs ring ==="

python3 - "$RESULTS_DIR" "$N" <<'PYEOF'
import json, glob, os, sys

def summarize(dir_path):
    files = sorted(glob.glob(os.path.join(dir_path, "routing-*.json")))
    if not files:
        return None
    sizes, buckets, max_fills = [], [], []
    for f in files:
        s = json.load(open(f))
        sizes.append(s["size"])
        buckets.append(s["bucket_count"])
        max_fills.append(s["max_bucket_fill"])

    edges = set()
    for f in files:
        s = json.load(open(f))
        src = s["node_id"][:8]
        for bucket in s.get("buckets", []):
            for c in bucket.get("contacts", []):
                edges.add((src, c["node_id"][:8]))

    out_deg = {}
    in_deg = {}
    for src, dst in edges:
        out_deg[src] = out_deg.get(src, 0) + 1
        in_deg[dst] = in_deg.get(dst, 0) + 1

    return {
        "n": len(files),
        "size_min": min(sizes),
        "size_max": max(sizes),
        "size_mean": sum(sizes) / len(sizes),
        "buckets_mean": sum(buckets) / len(buckets),
        "max_fill_max": max(max_fills),
        "edges": len(edges),
        "max_out_deg": max(out_deg.values()) if out_deg else 0,
        "max_in_deg": max(in_deg.values()) if in_deg else 0,
    }

root = sys.argv[1]
star = summarize(os.path.join(root, "star"))
ring = summarize(os.path.join(root, "ring"))

if star is None or ring is None:
    print("[compare_visual] missing snapshots")
    sys.exit(1)

print()
print(f"{'metric':25} {'star':>12} {'ring':>12} {'diff':>12}")
print("-" * 65)

def row(name, key, fmt="{}"):
    a, b = star[key], ring[key]
    diff = b - a
    print(f"{name:25} {fmt.format(a):>12} {fmt.format(b):>12} {fmt.format(diff):>12}")

row("nodes", "n", "{:d}")
row("table_size_min", "size_min", "{:d}")
row("table_size_max", "size_max", "{:d}")
row("table_size_mean", "size_mean", "{:.2f}")
row("buckets_per_node_mean", "buckets_mean", "{:.2f}")
row("max_bucket_fill_max", "max_fill_max", "{:d}")
row("edges_total", "edges", "{:d}")
row("max_out_degree", "max_out_deg", "{:d}")
row("max_in_degree", "max_in_deg", "{:d}")

out = {
    "n": star["n"],
    "star": star,
    "ring": ring,
}
with open(os.path.join(root, "summary.json"), "w") as fh:
    json.dump(out, fh, indent=2)

print()
print(f"[compare_visual] summary: {os.path.join(root, 'summary.json')}")
PYEOF

echo
echo "[compare_visual] результаты в $RESULTS_DIR"
echo "  star-overlay.png   — граф star"
echo "  ring-overlay.png   — граф ring"
echo "  summary.json       — сравнительная таблица"
