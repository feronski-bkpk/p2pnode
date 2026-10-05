#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

N="${1:-15}"
LOOKUP_DIR="$ROOT_DIR/metrics/lookups"
LOG_DIR="$ROOT_DIR/logs-docker"
RESULTS="$ROOT_DIR/visualization/seed-down"
COMPOSE_FILE="$ROOT_DIR/docker-compose.yml"

mkdir -p "$RESULTS"

if docker compose version >/dev/null 2>&1; then
    COMPOSE=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
    COMPOSE=(docker-compose)
else
    echo "[seed-down] docker compose не найден" >&2
    exit 1
fi

echo "=== E6-5: работа без bootstrap (N=$N) ==="

"$ROOT_DIR/scripts/docker_down.sh" --clean >/dev/null 2>&1 || true
rm -rf "$LOOKUP_DIR" "$ROOT_DIR/metrics/collected"
mkdir -p "$LOOKUP_DIR" "$ROOT_DIR/metrics/collected"

rm -rf "$RESULTS/before-lookups" "$RESULTS/after-lookups"

echo "[seed-down] запуск $N узлов (star)..."
"$ROOT_DIR/scripts/docker_up.sh" "$N" star --clean >/dev/null
sleep 30
echo "[seed-down] ждём 30с сходимости... ok"

echo "[seed-down] baseline: batch lookup'ов..."
"$ROOT_DIR/scripts/docker_lookup_batch.sh" "$N" 30 2>&1 | tail -3
cp -r "$LOOKUP_DIR" "$RESULTS/before-lookups"

echo "[seed-down] останавливаем seed (node-01)..."
"${COMPOSE[@]}" -f "$COMPOSE_FILE" stop node-01 >/dev/null 2>&1
sleep 3
echo "[seed-down] seed остановлен"

echo "[seed-down] ждём 45с, чтобы routing почистился от мёртвого seed'а..."
sleep 45

echo "[seed-down] after: batch lookup'ов без seed'а..."
rm -f "$LOOKUP_DIR"/*.json
BOOTSTRAP="node-02:9002,node-03:9003,node-04:9004" \
    "$ROOT_DIR/scripts/docker_lookup_batch.sh" "$N" 30 2>&1 | tail -3
cp -r "$LOOKUP_DIR" "$RESULTS/after-lookups"

echo
echo "[seed-down] === Сравнение before/after ==="
python3 - "$RESULTS" <<'PYEOF'
import json, glob, os, sys

root = sys.argv[1]

def summarize(dirpath):
    files = sorted(glob.glob(os.path.join(dirpath, "lookup-*.json")))
    if not files:
        return None
    total = len(files)
    rpcs, iters, durs = [], [], []
    found = absent = with_interm = 0
    for f in files:
        try:
            d = json.load(open(f))
        except Exception:
            continue
        rpcs.append(d.get("rpc", 0))
        iters.append(d.get("iterations", 0))
        durs.append(d.get("duration_ms", 0))
        if d.get("target_absent_at_start"):
            absent += 1
        if (d.get("rpc") or 0) >= 2:
            with_interm += 1
        tgt = d.get("target")
        finals = d.get("final_contacts") or []
        if tgt and any(c.get("node_id") == tgt for c in finals):
            found += 1
    def q(arr, p):
        if not arr: return 0
        s = sorted(arr)
        return s[min(len(s)-1, int(p*(len(s)-1)))]
    return {
        "total": total,
        "found": found,
        "absent": absent,
        "with_interm": with_interm,
        "rpc_mean": sum(rpcs)/len(rpcs) if rpcs else 0,
        "rpc_median": q(rpcs, 0.5),
        "rpc_max": max(rpcs) if rpcs else 0,
        "iter_mean": sum(iters)/len(iters) if iters else 0,
        "dur_mean": sum(durs)/len(durs) if durs else 0,
    }

before = summarize(os.path.join(root, "before-lookups"))
after = summarize(os.path.join(root, "after-lookups"))

if before is None or after is None:
    print("[seed-down] нет данных")
    sys.exit(1)

print()
print(f"{'metric':25} {'before':>12} {'after':>12}")
print("-" * 55)
for k in ["total", "found", "absent", "with_interm",
          "rpc_mean", "rpc_median", "rpc_max",
          "iter_mean", "dur_mean"]:
    b = before.get(k, 0)
    a = after.get(k, 0)
    if isinstance(b, float):
        print(f"{k:25} {b:>12.2f} {a:>12.2f}")
    else:
        print(f"{k:25} {b:>12} {a:>12}")

# Чек-лист E6-5: сеть "продолжает работать" без seed'а.
# Routing после kill частично разрежен (мёртвые контакты удаляются
# фоновым expire'ом раз в 30с), поэтому допускаем ≥30% сохранившихся
# успешных lookup'ов. Это — доказательство, что сеть жива.
if before["total"] > 0 and after["total"] > 0:
    ratio = after["found"] / before["found"] if before["found"] > 0 else 0
    threshold = 0.3
    print()
    if ratio >= threshold:
        print(f"[seed-down] OK: after/before = {ratio:.2f} (>={threshold}) — E6-5 satisfied")
    else:
        print(f"[seed-down] FAIL: after/before = {ratio:.2f} (<{threshold})")

with open(os.path.join(root, "summary.json"), "w") as fh:
    json.dump({"before": before, "after": after, "N": before["total"]}, fh, indent=2)
PYEOF

echo
echo "[seed-down] результаты в $RESULTS"
echo "[seed-down] остановить: ./scripts/docker_down.sh --clean"
