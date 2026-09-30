#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

N="${1:-$N}"
WAIT="${2:-20}"

RESULTS_DIR="$ROOT_DIR/visualization/bootstrap-compare"
rm -rf "$RESULTS_DIR"
mkdir -p "$RESULTS_DIR/star" "$RESULTS_DIR/ring"

# ============================================================
# Star
# ============================================================
echo
echo "=== Star: N=$N, wait=${WAIT}s ==="

"$ROOT_DIR/scripts/stop_all.sh" 2>/dev/null || true
sleep 1
rm -rf "$STATE_DIR" "$LOG_DIR" "$METRICS_DIR"
mkdir -p "$STATE_DIR" "$LOG_DIR" "$METRICS_DIR"

N="$N" "$ROOT_DIR/scripts/run_star.sh" "$N" > /dev/null

echo "[star] waiting ${WAIT}s for convergence..."
sleep "$WAIT"

N="$N" "$ROOT_DIR/scripts/collect_routing.sh" > /dev/null

cp "$METRICS_DIR/collected"/*.json "$RESULTS_DIR/star/" 2>/dev/null || true
cp "$LOG_DIR"/node-*.log "$RESULTS_DIR/star/" 2>/dev/null || true
echo "[star] collected $(ls "$RESULTS_DIR/star"/routing-*.json 2>/dev/null | wc -l) snapshots"

# ============================================================
# Ring
# ============================================================
echo
echo "=== Ring: N=$N, wait=${WAIT}s ==="

"$ROOT_DIR/scripts/stop_all.sh" 2>/dev/null || true
sleep 1
rm -rf "$STATE_DIR" "$LOG_DIR" "$METRICS_DIR"
mkdir -p "$STATE_DIR" "$LOG_DIR" "$METRICS_DIR"

N="$N" "$ROOT_DIR/scripts/run_ring.sh" "$N" > /dev/null

echo "[ring] waiting ${WAIT}s for convergence..."
sleep "$WAIT"

N="$N" "$ROOT_DIR/scripts/collect_routing.sh" > /dev/null

cp "$METRICS_DIR/collected"/*.json "$RESULTS_DIR/ring/" 2>/dev/null || true
cp "$LOG_DIR"/node-*.log "$RESULTS_DIR/ring/" 2>/dev/null || true
echo "[ring] collected $(ls "$RESULTS_DIR/ring"/routing-*.json 2>/dev/null | wc -l) snapshots"

# ============================================================
# Сводка
# ============================================================
echo
echo "=== Сравнение star vs ring ==="

python3 - "$RESULTS_DIR" <<'PYEOF'
import json, glob, os, sys

def summarize(dir_path):
    files = sorted(glob.glob(os.path.join(dir_path, "routing-*.json")))
    if not files:
        return None
    sizes, buckets, max_fills, min_fills = [], [], [], []
    for f in files:
        s = json.load(open(f))
        sizes.append(s["size"])
        buckets.append(s["bucket_count"])
        max_fills.append(s["max_bucket_fill"])
        min_fills.append(s["min_bucket_fill"])
    return {
        "n": len(files),
        "size_min": min(sizes),
        "size_max": max(sizes),
        "size_mean": sum(sizes) / len(sizes),
        "buckets_mean": sum(buckets) / len(buckets),
        "max_fill_max": max(max_fills),
        "min_fill_min": min(min_fills),
    }

root = sys.argv[1]
star = summarize(os.path.join(root, "star"))
ring = summarize(os.path.join(root, "ring"))

if star is None or ring is None:
    print("[compare] missing snapshots; check earlier output")
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
row("min_bucket_fill_min", "min_fill_min", "{:d}")

# Сохраняем JSON.
out = {"star": star, "ring": ring, "N": star["n"]}
with open(os.path.join(root, "summary.json"), "w") as fh:
    json.dump(out, fh, indent=2)

print()
print("[compare_bootstrap] summary saved to", os.path.join(root, "summary.json"))
PYEOF

# Останавливаем стенд.
"$ROOT_DIR/scripts/stop_all.sh" > /dev/null 2>&1 || true

echo
echo "[compare_bootstrap] results in $RESULTS_DIR"
