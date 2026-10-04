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
import hashlib
from datetime import datetime

root = sys.argv[1]
template_path = sys.argv[2]
output_path = sys.argv[3]
d3_path = sys.argv[4]


def _glob(patterns):
    out = []
    for p in patterns:
        out.extend(sorted(glob.glob(p)))
    return out


def _primary_or_fallback(primary_patterns, fallback_patterns):
    """
    Возвращает primary, если он непуст.
    Иначе — fallback. Никогда не смешивает оба источника.
    """
    primary = _glob(primary_patterns)
    if primary:
        return primary
    return _glob(fallback_patterns)


# ---------- routing ----------
def load_routing(root):
    files = _primary_or_fallback(
        [os.path.join(root, "metrics/collected/routing-*.json")],
        [
            os.path.join(root, "metrics/routing/routing-*.json"),
            "/tmp/p2pnode-demo-*/export/*/routing-*.json",
            "/tmp/p2pnode-demo-*/export/routing-*.json",
        ],
    )
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
    files = _primary_or_fallback(
        [os.path.join(root, "metrics/collected/routing-*.json")],
        [
            os.path.join(root, "metrics/routing/routing-*.json"),
            "/tmp/p2pnode-demo-*/export/*/routing-*.json",
            "/tmp/p2pnode-demo-*/export/routing-*.json",
        ],
    )
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


# ---------- lookups ----------
def load_lookups(root):
    files = _primary_or_fallback(
        [os.path.join(root, "metrics/lookups/lookup-*.json")],
        [
            "/tmp/p2pnode-demo-*/export/*/lookup-*.json",
            "/tmp/p2pnode-demo-*/state/*/lookup-*.json",
        ],
    )
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
                "iterations_log": s.get("iterations_log", []),
                "final_contacts": finals,
                "file": os.path.basename(f),
            })
        except Exception as e:
            print(f"[report] skip {f}: {e}", file=sys.stderr)
    return out


def summarize_lookups(lookups):
    if not lookups:
        return None
    rpcs = sorted(l["rpc"] for l in lookups)
    iters = sorted(l["iterations"] for l in lookups)
    durs = sorted(l["duration_ms"] for l in lookups)
    n = len(lookups)

    def quantile(sorted_arr, q):
        if not sorted_arr:
            return 0
        idx = int(q * (len(sorted_arr) - 1))
        return sorted_arr[idx]

    return {
        "count": n,
        "found": sum(1 for l in lookups if l["found"]),
        "absent": sum(1 for l in lookups if l["target_absent_at_start"]),
        "rpc_min": rpcs[0] if rpcs else 0,
        "rpc_max": rpcs[-1] if rpcs else 0,
        "rpc_mean": sum(rpcs) / n if n else 0,
        "rpc_median": quantile(rpcs, 0.5),
        "rpc_p95": quantile(rpcs, 0.95),
        "iter_mean": sum(iters) / n if n else 0,
        "iter_max": iters[-1] if iters else 0,
        "dur_mean": sum(durs) / n if n else 0,
        "dur_max": durs[-1] if durs else 0,
        "dur_median": quantile(durs, 0.5),
    }


# ---------- degeneracy ----------
def compute_degeneracy(routing, lookups):
    if not routing:
        return None
    n = len(routing)
    sizes = [r["size"] for r in routing]
    full_registry = n - 1
    with_less = sum(1 for s in sizes if s < full_registry)
    pct_less = 100.0 * with_less / n if n else 0.0

    has_lookups = len(lookups) > 0

    if has_lookups:
        absent_ok = all(l["target_absent_at_start"] for l in lookups)
        interm_ok = all(l["rpc"] >= 2 for l in lookups)
    else:
        absent_ok = None
        interm_ok = None

    crit1 = pct_less >= 80.0
    crit1b = all(s < full_registry for s in sizes)
    crit2 = absent_ok
    crit3 = interm_ok

    all_ok = (
        crit1 and crit1b
        and (crit2 is not False)
        and (crit3 is not False)
    )

    return {
        "n": n,
        "full_registry_size": full_registry,
        "size_min": min(sizes),
        "size_max": max(sizes),
        "size_mean": sum(sizes) / n,
        "nodes_with_less": with_less,
        "pct_less": pct_less,
        "crit1_ok": crit1,
        "crit1b_ok": crit1b,
        "crit2_ok": crit2,
        "crit3_ok": crit3,
        "has_lookups": has_lookups,
        "all_ok": all_ok,
        "per_node": [
            {
                "node_id": r["node_id"],
                "size": r["size"],
                "pct_of_full": 100.0 * r["size"] / full_registry if full_registry else 0,
                "ok": r["size"] < full_registry,
            }
            for r in sorted(routing, key=lambda x: -x["size"])
        ],
    }


# ---------- star/ring ----------
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


# ---------- traces ----------
def load_traces(root):
    """
    Приоритет: metrics/trace/ и metrics/collected/trace-*.json.
    Fallback: /tmp/p2pnode-demo-*/. Не смешивает.
    """
    files = _primary_or_fallback(
        [
            os.path.join(root, "metrics/trace/*.json"),
            os.path.join(root, "metrics/collected/trace-*.json"),
        ],
        [
            "/tmp/p2pnode-demo-*/export/trace.json",
            "/tmp/p2pnode-demo-*/export/*/trace.json",
            "/tmp/p2pnode-demo-*/state/*/trace.json",
        ],
    )
    out = []
    seen_hashes = set()
    for f in files:
        try:
            with open(f) as fh:
                s = json.load(fh)
            events = s.get("events", [])
            if not events:
                continue
            content_hash = hashlib.md5(
                json.dumps(events, sort_keys=True, ensure_ascii=False).encode()
            ).hexdigest()
            if content_hash in seen_hashes:
                continue
            seen_hashes.add(content_hash)

            parent = os.path.basename(os.path.dirname(f))
            grandparent = os.path.basename(os.path.dirname(os.path.dirname(f)))
            if grandparent in ("state", "export"):
                run_dir = os.path.basename(
                    os.path.dirname(os.path.dirname(os.path.dirname(f)))
                )
                src = f"{run_dir}/{parent}"
            else:
                src = parent

            out.append({
                "source": src,
                "path": f,
                "events": events,
                "count": len(events),
            })
        except Exception as e:
            print(f"[report] skip trace {f}: {e}", file=sys.stderr)
    out.sort(key=lambda t: t["source"])
    return out


def load_verify(root):
    candidates = _primary_or_fallback(
        [os.path.join(root, "metrics/collected/verify.json")],
        ["/tmp/p2pnode-demo-capture/verify.json"],
    )
    for f in candidates:
        if os.path.exists(f):
            try:
                with open(f) as fh:
                    return json.load(fh)
            except Exception as e:
                print(f"[report] skip verify {f}: {e}", file=sys.stderr)
    return None


# ---------- overview ----------
def compute_overview(routing, edges, lookups, traces):
    n = len(routing)
    sizes = [r["size"] for r in routing]
    mean_size = sum(sizes) / n if n else 0
    max_size = max(sizes) if sizes else 0
    min_size = min(sizes) if sizes else 0

    out_deg = {}
    in_deg = {}
    for e in edges:
        out_deg[e["source"]] = out_deg.get(e["source"], 0) + 1
        in_deg[e["target"]] = in_deg.get(e["target"], 0) + 1

    max_out = max(out_deg.values()) if out_deg else 0
    max_in = max(in_deg.values()) if in_deg else 0
    mean_out = sum(out_deg.values()) / len(out_deg) if out_deg else 0

    edge_set = set((e["source"], e["target"]) for e in edges)
    mutual = sum(1 for (a, b) in edge_set if (b, a) in edge_set)
    asym = len(edge_set) - mutual

    return {
        "nodes": n,
        "edges": len(edge_set),
        "mean_size": mean_size,
        "min_size": min_size,
        "max_size": max_size,
        "max_out_degree": max_out,
        "max_in_degree": max_in,
        "mean_out_degree": mean_out,
        "mutual_edges": mutual,
        "asym_edges": asym,
        "lookups_total": len(lookups),
        "lookups_ok": sum(1 for l in lookups if l["found"]),
        "lookups_available": len(lookups) > 0,
        "traces_total": len(traces),
        "trace_events": sum(t["count"] for t in traces),
    }


# ---------- загрузка ----------
routing = load_routing(root)
edges = load_edges(root)
lookups = load_lookups(root)
traces = load_traces(root)

data = {
    "generated_at": datetime.now().isoformat(),
    "routing": routing,
    "edges": edges,
    "lookups": lookups,
    "lookups_summary": summarize_lookups(lookups),
    "degeneracy": compute_degeneracy(routing, lookups),
    "starRing": load_star_ring(root),
    "serialization": load_serialization(root),
    "traces": traces,
    "verify": load_verify(root),
    "overview": compute_overview(routing, edges, lookups, traces),
}

with open(d3_path) as fh:
    d3_js = fh.read()

with open(template_path) as fh:
    html = fh.read()

data_json = json.dumps(data, ensure_ascii=False).replace("<", "\\u003c")
html = html.replace("__D3_INLINE__", d3_js)
html = html.replace("__REPORT_DATA__", data_json)

with open(output_path, "w") as fh:
    fh.write(html)

print(f"[report] routing snapshots: {len(data['routing'])}")
print(f"[report] edges: {len(data['edges'])}")
print(f"[report] lookups: {len(data['lookups'])}")
print(f"[report] traces: {len(data['traces'])} (dedup by content)")
print(f"[report] verify: {'yes' if data['verify'] else 'no'}")
print(f"[report] degeneracy: {'yes' if data['degeneracy'] else 'no'}")
print(f"[report] starRing: {'yes' if data['starRing'] else 'no'}")
print(f"[report] serialization: {'yes' if data['serialization'] else 'no'}")
print(f"[report] wrote {output_path}")
PYEOF

echo "[report] done. Open: $OUTPUT"
