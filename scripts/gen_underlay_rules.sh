#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

N="${1:-7}"
SCHEME="${2:-star}"
OUT="${3:-$ROOT_DIR/visualization/underlay/rules.json}"

mkdir -p "$(dirname "$OUT")"

case "$SCHEME" in
    star|ring|tree|multiseed) ;;
    *) echo "[underlay] unknown scheme: $SCHEME" >&2; exit 1 ;;
esac

python3 - "$N" "$SCHEME" "$OUT" <<'PYEOF'
import json
import sys

n = int(sys.argv[1])
scheme = sys.argv[2]
out_path = sys.argv[3]

pairs = []

if scheme == "star":
    # Все (i>=2) <-> seed (1)
    for i in range(2, n + 1):
        pairs.append((i, 1))
        pairs.append((1, i))

elif scheme == "ring":
    # i <-> i-1 (для i >= 2)
    for i in range(2, n + 1):
        pairs.append((i, i - 1))
        pairs.append((i - 1, i))

elif scheme == "tree":
    # i <-> i/2 (для i >= 2)
    for i in range(2, n + 1):
        parent = i // 2
        pairs.append((i, parent))
        pairs.append((parent, i))

elif scheme == "multiseed":
    # seed'ы (1,2,3) — полносвязные между собой
    seeds = [1, 2, 3]
    seeds = [s for s in seeds if s <= n]
    for a in seeds:
        for b in seeds:
            if a != b:
                pairs.append((a, b))
    # Остальные (i > 3) <-> все seed'ы
    for i in range(4, n + 1):
        for s in seeds:
            pairs.append((i, s))
            pairs.append((s, i))

# Уникализуем.
pairs = sorted(set(pairs))

# Внутренние порты: node-i на 9000+i.
def port(i):
    return 9000 + i

allowed = []
for src, dst in pairs:
    allowed.append({
        "src": src,
        "dst": dst,
        "src_port": port(src),
        "dst_port": port(dst),
    })

out = {
    "n": n,
    "scheme": scheme,
    "allowed_pairs": allowed,
    "total_pairs": len(allowed),
    "all_possible": n * (n - 1),
    "blocked_pairs": n * (n - 1) - len(allowed),
}

with open(out_path, "w") as f:
    json.dump(out, f, indent=2)

print(f"[underlay] scheme={scheme} N={n}: allowed={len(allowed)} "
      f"blocked={out['blocked_pairs']} (из {out['all_possible']} возможных)")
PYEOF

echo "[underlay] rules: $OUT"
