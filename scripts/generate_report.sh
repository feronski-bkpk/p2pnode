#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

OUTPUT="${1:-$ROOT_DIR/report.html}"
TEMPLATE="$ROOT_DIR/scripts/report_template.html"
D3_FILE="$ROOT_DIR/scripts/d3.v7.min.js"

if [ ! -f "$TEMPLATE" ]; then
    echo "[report] template not found: $TEMPLATE" >&2
    exit 1
fi

if [ ! -f "$D3_FILE" ]; then
    echo "[report] D3 not found: $D3_FILE" >&2
    echo "[report] Скачайте: curl -o scripts/d3.v7.min.js https://d3js.org/d3.v7.min.js" >&2
    exit 1
fi

if ! command -v python3 >/dev/null 2>&1; then
    echo "[report] python3 not found" >&2
    exit 1
fi

python3 - "$ROOT_DIR" "$TEMPLATE" "$OUTPUT" "$D3_FILE" <<'PYEOF'
import json
import glob
import os
import sys
from datetime import datetime

root = sys.argv[1]
template_path = sys.argv[2]
output_path = sys.argv[3]
d3_path = sys.argv[4]

def load_routing(root):
    files = sorted(glob.glob(os.path.join(root, "metrics/collected/routing-*.json")))
    out = []
    for f in files:
        try:
            with open(f) as fh:
                s = json.load(fh)
            out.append({
                "node_id": s.get("node_id", ""),
                "size": s.get("size", 0),
                "bucket_count": s.get("bucket_count", 0),
                "max_bucket_fill": s.get("max_bucket_fill", 0),
                "min_bucket_fill": s.get("min_bucket_fill", 0),
            })
        except Exception as e:
            print(f"[report] skip {f}: {e}", file=sys.stderr)
    return out

def load_edges(root):
    """Собирает рёбра из routing-снапшотов (buckets[].contacts[])."""
    files = glob.glob(os.path.join(root, "metrics/collected/routing-*.json"))
    edges = []
    seen = set()
    for f in files:
        try:
            with open(f) as fh:
                s = json.load(fh)
        except Exception:
            continue
        src = s.get("node_id", "")
        for bucket in s.get("buckets", []):
            for c in bucket.get("contacts", []):
                dst = c.get("node_id", "")
                if not dst or dst == src:
                    continue
                key = (src, dst)
                if key in seen:
                    continue
                seen.add(key)
                edges.append({
                    "source": src,
                    "target": dst,
                    "bucket": bucket.get("index", 0),
                })
    return edges

def load_lookups(root):
    files = sorted(glob.glob(os.path.join(root, "metrics/lookups/lookup-*.json")))
    out = []
    for f in files:
        try:
            with open(f) as fh:
                s = json.load(fh)
            target = s.get("target", "")
            finals = s.get("final_contacts") or []
            found = any(c.get("node_id") == target for c in finals)
            out.append({
                "target": target,
                "initiator": s.get("initiator", ""),
                "target_absent_at_start": s.get("target_absent_at_start", False),
                "rpc": s.get("rpc", 0),
                "iterations": s.get("iterations", 0),
                "timeouts": s.get("timeouts", 0),
                "duration_ms": s.get("duration_ms", 0),
                "found": found,
            })
        except Exception as e:
            print(f"[report] skip {f}: {e}", file=sys.stderr)
    return out

def load_star_ring(root):
    path = os.path.join(root, "visualization/star-vs-ring/summary.json")
    if not os.path.exists(path):
        return None
    try:
        with open(path) as fh:
            return json.load(fh)
    except Exception:
        return None

def load_serialization(root):
    path = os.path.join(root, "visualization/serialization.json")
    if not os.path.exists(path):
        return None
    try:
        with open(path) as fh:
            return json.load(fh)
    except Exception:
        return None

data = {
    "generated_at": datetime.now().isoformat(),
    "routing": load_routing(root),
    "edges": load_edges(root),
    "lookups": load_lookups(root),
    "starRing": load_star_ring(root),
    "serialization": load_serialization(root),
}

# Читаем D3.
with open(d3_path) as fh:
    d3_js = fh.read()

# Читаем шаблон.
with open(template_path) as fh:
    html = fh.read()

# Инжектим.
html = html.replace("__D3_INLINE__", d3_js)
html = html.replace("__REPORT_DATA__", json.dumps(data, ensure_ascii=False))

with open(output_path, "w") as fh:
    fh.write(html)

print(f"[report] routing snapshots: {len(data['routing'])}")
print(f"[report] edges: {len(data['edges'])}")
print(f"[report] lookups: {len(data['lookups'])}")
print(f"[report] star/ring: {'yes' if data['starRing'] else 'no'}")
print(f"[report] serialization: {'yes' if data['serialization'] else 'no'}")
print(f"[report] wrote {output_path}")
PYEOF

echo "[report] done. Open: $OUTPUT"
