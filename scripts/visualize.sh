#!/usr/bin/env bash
# Генерирует граф «кто кого знает» из routing-снапшотов.
#
# Режимы (последний аргумент):
#   overlay  — все рёбра (по умолчанию)
#   mutual   — только взаимные рёбра (A↔B), рисуется одно ребро
#   asym     — только несимметричные рёбра (A знает B, но B не знает A)
#   topk     — только 3 ближайших по XOR ребра на узел
#   tree     — иерархия: seed → его контакты, сгруппированные по bucket
#
# Использование:
#   ./scripts/visualize.sh [input_dir] [output_prefix] [title] [mode]
#
# Примеры:
#   ./scripts/visualize.sh
#   ./scripts/visualize.sh metrics/collected visualization/star "Star N=10" mutual
#   ./scripts/visualize.sh metrics/collected visualization/tree "Tree N=15" tree
#   ./scripts/visualize.sh metrics/collected visualization/topk "TopK N=15" topk

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

INPUT_DIR="${1:-$ROOT_DIR/metrics/collected}"
OUTPUT_PREFIX="${2:-$ROOT_DIR/visualization/overlay}"
TITLE="${3:-}"
MODE="${4:-overlay}"

OUTPUT_DIR="$(dirname "$OUTPUT_PREFIX")"
mkdir -p "$OUTPUT_DIR"

if [ ! -d "$INPUT_DIR" ]; then
    echo "[visualize] input dir $INPUT_DIR not found" >&2
    exit 1
fi

if ! command -v fdp >/dev/null 2>&1; then
    echo "[visualize] graphviz 'fdp' not found" >&2
    echo "[visualize] установите: sudo dnf install graphviz   # или apt install graphviz" >&2
    exit 1
fi

DOT_FILE="${OUTPUT_PREFIX}.dot"
PNG_FILE="${OUTPUT_PREFIX}.png"
SVG_FILE="${OUTPUT_PREFIX}.svg"

python3 - "$INPUT_DIR" "$TITLE" "$MODE" > "$DOT_FILE" <<'PYEOF'
import json, glob, os, sys

input_dir = sys.argv[1]
title = sys.argv[2] if len(sys.argv) > 2 else ""
mode = sys.argv[3] if len(sys.argv) > 3 else "overlay"

files = sorted(glob.glob(os.path.join(input_dir, "routing-*.json")))
if not files:
    print("// no routing snapshots found")
    sys.exit(0)

# Загружаем снапшоты.
nodes = {}        # id -> snapshot
edges_all = set() # (src, dst)
# Для режима tree: контакты seed'а, сгруппированные по bucket.
seed_id = None
seed_buckets = {}  # bucket_index -> [contact_ids]

for f in files:
    with open(f) as fh:
        s = json.load(fh)
    nid = s["node_id"][:8]
    nodes[nid] = s
    for bucket in s.get("buckets", []):
        for c in bucket.get("contacts", []):
            dst = c["node_id"][:8]
            edges_all.add((nid, dst))

# Находим seed (узел с максимальным out-degree).
out_deg_all = {}
for (a, b) in edges_all:
    out_deg_all[a] = out_deg_all.get(a, 0) + 1
if out_deg_all:
    seed_id = max(out_deg_all, key=out_deg_all.get)
    # Собираем контакты seed'а по bucket'ам.
    if seed_id in nodes:
        for bucket in nodes[seed_id].get("buckets", []):
            bidx = bucket.get("index", 0)
            contacts = [c["node_id"][:8] for c in bucket.get("contacts", [])]
            if contacts:
                seed_buckets[bidx] = contacts

# Фильтрация рёбер в зависимости от режима.
edges = set()
if mode == "overlay":
    edges = edges_all
elif mode == "mutual":
    seen = set()
    for (a, b) in edges_all:
        if (b, a) in edges_all and (b, a) not in seen:
            edges.add((a, b))
            seen.add((a, b))
elif mode == "asym":
    for (a, b) in edges_all:
        if (b, a) not in edges_all:
            edges.add((a, b))
elif mode == "topk":
    K = 3
    by_src = {}
    for (a, b) in edges_all:
        by_src.setdefault(a, []).append(b)

    def xor_dist(a_hex, b_hex):
        return int(a_hex, 16) ^ int(b_hex, 16)

    for src, dsts in by_src.items():
        dsts_sorted = sorted(dsts, key=lambda d: xor_dist(src, d))
        for d in dsts_sorted[:K]:
            edges.add((src, d))
elif mode == "tree":
    # Никаких edges — генерируем кластеры ниже.
    pass

# ============================================================
# Генерация DOT
# ============================================================

if mode == "tree":
    # ==== TREE MODE ====
    if not seed_id:
        print("// no seed found")
        sys.exit(0)

    print("digraph overlay {")
    print('  rankdir=TB;')
    print('  graph [overlap=false, splines=polyline, bgcolor="white", nodesep=0.3, ranksep=0.5];')
    if title:
        print(f'  label="{title}";')
        print('  labelloc="t";')
        print('  fontsize=16;')
    print('  node [shape=box, style=filled, fontname="Helvetica", fontsize=10];')
    print('  edge [color="#888888", arrowsize=0.5];')

    # Seed.
    seed_size = nodes[seed_id].get("size", 0)
    print(f'  "{seed_id}" [label="{seed_id}\\n(seed)\\nsize={seed_size}", fillcolor="#ffd6a5"];')

    # Для каждого bucket seed'а — узел-«bucket», и от него — рёбра к контактам.
    for bidx, contacts in sorted(seed_buckets.items()):
        bucket_node = f"bucket_{bidx}"
        print(f'  "{bucket_node}" [label="bucket {bidx}\\n({len(contacts)})", shape=ellipse, fillcolor="#e0e0e0"];')
        print(f'  "{seed_id}" -> "{bucket_node}";')
        for c in contacts:
            # Если контакт есть в nodes — используем его, иначе создаём «внешний».
            if c in nodes:
                size = nodes[c].get("size", 0)
                print(f'  "{c}" [label="{c}\\nsize={size}", fillcolor="#cfe2ff"];')
            else:
                print(f'  "{c}" [label="{c}\\n(unknown)", fillcolor="#f0f0f0"];')
            print(f'  "{bucket_node}" -> "{c}";')

    # Остальные узлы, не вошедшие ни в один bucket seed'а — отдельным рядом.
    bucket_contacts = set()
    for contacts in seed_buckets.values():
        bucket_contacts.update(contacts)
    others = sorted(set(nodes.keys()) - {seed_id} - bucket_contacts)
    if others:
        print(f'  "others" [label="others\\n({len(others)})", shape=ellipse, fillcolor="#e0e0e0"];')
        print(f'  "{seed_id}" -> "others" [style=dashed];')
        for nid in others:
            size = nodes[nid].get("size", 0)
            print(f'  "{nid}" [label="{nid}\\nsize={size}", fillcolor="#cfe2ff"];')
            print(f'  "others" -> "{nid}";')

    print("}")
else:
    # ==== ОБЫЧНЫЕ РЕЖИМЫ (fdp) ====
    print("digraph overlay {")
    print('  layout=fdp;')
    print('  graph [overlap=false, splines=true, bgcolor="white", K=2.0, sep="+20"];')
    if title:
        print(f'  label="{title}";')
        print('  labelloc="t";')
        print('  fontsize=16;')
    print('  node [shape=circle, style=filled, fontname="Helvetica", fontsize=10, width=0.9];')
    print('  edge [color="#888888", arrowsize=0.5, penwidth=0.8];')

    max_out = max(out_deg_all.values()) if out_deg_all else 0

    for nid, s in sorted(nodes.items()):
        size = s.get("size", 0)
        fillcolor = "#ffd6a5" if out_deg_all.get(nid, 0) == max_out else "#cfe2ff"
        label = f"{nid}\\nsize={size}"
        print(f'  "{nid}" [label="{label}", fillcolor="{fillcolor}"];')

    for src, dst in sorted(edges):
        print(f'  "{src}" -> "{dst}";')

    print("}")
PYEOF

echo "[visualize] mode=$MODE, DOT file: $DOT_FILE"

# Рендерим.
if [ "$MODE" = "tree" ]; then
    # Для tree — иерархический dot.
    dot -Tpng "$DOT_FILE" -o "$PNG_FILE"
    dot -Tsvg "$DOT_FILE" -o "$SVG_FILE"
else
    # Для остальных — force-directed fdp.
    fdp -Tpng "$DOT_FILE" -o "$PNG_FILE"
    fdp -Tsvg "$DOT_FILE" -o "$SVG_FILE"
fi

echo "[visualize] PNG file: $PNG_FILE"
echo "[visualize] SVG file: $SVG_FILE"

# Статистика.
python3 - "$INPUT_DIR" "$MODE" <<'PYEOF'
import json, glob, os, sys
input_dir = sys.argv[1]
mode = sys.argv[2] if len(sys.argv) > 2 else "overlay"

files = sorted(glob.glob(os.path.join(input_dir, "routing-*.json")))
edges_all = set()
nodes = {}
for f in files:
    with open(f) as fh:
        s = json.load(fh)
    src = s["node_id"][:8]
    nodes[src] = s
    for bucket in s.get("buckets", []):
        for c in bucket.get("contacts", []):
            dst = c["node_id"][:8]
            edges_all.add((src, dst))
            nodes.setdefault(dst, {})

print()
print(f"[visualize] mode: {mode}")
print(f"[visualize] nodes: {len(nodes)}, edges_total: {len(edges_all)}")

if mode == "mutual":
    mutual = sum(1 for (a, b) in edges_all if (b, a) in edges_all) // 2
    print(f"[visualize] mutual pairs: {mutual}")
elif mode == "asym":
    asym = sum(1 for (a, b) in edges_all if (b, a) not in edges_all)
    print(f"[visualize] asymmetric edges: {asym}")
elif mode == "topk":
    K = 3
    topk_count = min(K * len(nodes), len(edges_all))
    print(f"[visualize] topk edges (≤ {K} per node): ~{topk_count}")

out_deg = {}
in_deg = {}
for (a, b) in edges_all:
    out_deg[a] = out_deg.get(a, 0) + 1
    in_deg[b] = in_deg.get(b, 0) + 1

if out_deg:
    print(f"[visualize] max out-degree: {max(out_deg.values())} ({max(out_deg, key=out_deg.get)})")
if in_deg:
    print(f"[visualize] max in-degree:  {max(in_deg.values())} ({max(in_deg, key=in_deg.get)})")
PYEOF
