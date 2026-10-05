#!/usr/bin/env bash

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

COLLECTED_DIR="${1:-$METRICS_DIR/collected}"
LOOKUP_DIR="${2:-$METRICS_DIR/lookups}"

if [ ! -d "$COLLECTED_DIR" ]; then
    echo "[check] $COLLECTED_DIR not found" >&2
    exit 1
fi

python3 - "$COLLECTED_DIR" "$LOOKUP_DIR" <<'PYEOF'
import json, sys, os, glob

collected_dir = sys.argv[1]
lookup_dir = sys.argv[2] if len(sys.argv) > 2 else ""

# ---------- 1. Routing snapshots ----------

files = sorted(glob.glob(os.path.join(collected_dir, "routing-*.json")))
if not files:
    print("[check] no routing snapshots")
    sys.exit(1)

nodes = []
for f in files:
    try:
        with open(f) as fh:
            nodes.append(json.load(fh))
    except Exception as e:
        print(f"[check] skip {f}: {e}")

n = len(nodes)
if n == 0:
    print("[check] no valid routing snapshots")
    sys.exit(1)

sizes = [s.get("size", 0) for s in nodes]
full_registry = [s["node_id"][:8] for s in nodes if s.get("size", 0) >= n - 1]
less_than = sum(1 for s in nodes if s.get("size", 0) < n - 1)
pct = less_than / n * 100.0

print(f"[check] N = {n}")
print(f"[check] table sizes: min={min(sizes)} max={max(sizes)} mean={sum(sizes)/n:.2f}")
print(f"[check] nodes with < N-1 contacts: {less_than}/{n} ({pct:.1f}%)")
print(f"[check] nodes with full registry (size >= N-1): {len(full_registry)}")
if full_registry:
    print(f"[check]   full-registry nodes: {', '.join(full_registry)}")

# Criterion 1: >=80% узлов имеют < N-1 контактов.
c1 = pct >= 80.0

# Criterion 1b: "нет скрытого полного каталога" — не более 1 узла с полным реестром
# (в star-схеме seed всегда знает всех — это не вырождение).
# Если 0 или 1 узел — OK.
c1b = len(full_registry) <= 1

print(f"[check] criterion 1 (>=80% < N-1):        {c1}")
print(f"[check] criterion 1b (≤1 full registry):   {c1b}")

# Buckets статистика.
bucket_counts = [s.get("bucket_count", 0) for s in nodes]
max_fills = [s.get("max_bucket_fill", 0) for s in nodes]
print(f"[check] buckets per node: min={min(bucket_counts)} "
      f"max={max(bucket_counts)} mean={sum(bucket_counts)/n:.2f}")
print(f"[check] max bucket fill:  min={min(max_fills)} "
      f"max={max(max_fills)}")

# ---------- 2. Lookups ----------

if not lookup_dir or not os.path.isdir(lookup_dir):
    print()
    print("[check] no lookup dir, skipping lookup criteria")
    all_ok = c1 and c1b
    if all_ok:
        print("[check] RESULT: non-degenerate (routing criteria only)")
        sys.exit(0)
    print("[check] RESULT: DEGENERATE")
    sys.exit(2)

lookup_files = sorted(glob.glob(os.path.join(lookup_dir, "lookup-*.json")))
lookups = []
for f in lookup_files:
    try:
        with open(f) as fh:
            lookups.append(json.load(fh))
    except Exception as e:
        print(f"[check] skip {f}: {e}")

total_lookups = len(lookups)
absent_at_start = 0
with_intermediate = 0
successful = 0

for l in lookups:
    if l.get("target_absent_at_start"):
        absent_at_start += 1
    if (l.get("rpc") or 0) >= 2:
        with_intermediate += 1
    target = l.get("target")
    finals = l.get("final_contacts") or []
    if target and any(c.get("node_id") == target for c in finals):
        successful += 1

print()
print(f"[check] lookup'ов всего:               {total_lookups}")
print(f"[check] цель отсутствовала до старта:  {absent_at_start}")
print(f"[check] с промежуточным узлом (RPC≥2): {with_intermediate}")
print(f"[check] успешных (target в final):     {successful}")

# Criterion 2: >=30 absent_at_start (по чек-листу).
c2 = absent_at_start >= 30

# Criterion 3: не менее 80% lookup'ов с RPC>=2.
# (100% невыполнимо: в star-схеме часть lookup'ов из seed'а всегда 1-RPC.)
pct_interm = (with_intermediate / total_lookups * 100.0) if total_lookups else 0.0
c3 = total_lookups > 0 and (pct_interm >= 80.0)
print(f"[check] criterion 2 (>=30 absent):              {c2}")
print(f"[check] criterion 3 (≥80% w/ interm, {pct_interm:.1f}%): {c3}")

print()
all_ok = c1 and c1b and c2 and c3
if all_ok:
    print("[check] RESULT: NON-DEGENERATE (все критерии выполнены)")
    sys.exit(0)
else:
    print("[check] RESULT: DEGENERATE (не все критерии выполнены)")
    sys.exit(2)
PYEOF
