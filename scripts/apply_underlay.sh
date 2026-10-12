#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

N="${1:-7}"
SCHEME="${2:-star}"
RULES="${3:-$ROOT_DIR/visualization/underlay/rules.json}"

if [ ! -f "$RULES" ]; then
    echo "[underlay] rules not found: $RULES; generating..."
    "$ROOT_DIR/scripts/gen_underlay_rules.sh" "$N" "$SCHEME" "$RULES"
fi

if ! command -v jq >/dev/null 2>&1; then
    echo "[underlay] jq required" >&2
    exit 1
fi

echo "[underlay] applying scheme=$SCHEME N=$N"

declare -A NODE_IP
for i in $(seq 1 "$N"); do
    svc=$(printf 'node-%02d' "$i")
    container="p2pnode-$svc"
    ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$container" 2>/dev/null || echo "")
    if [ -z "$ip" ]; then
        echo "[underlay] $svc: no IP, skip"
        continue
    fi
    NODE_IP[$i]="$ip"
    echo "[underlay] $svc -> $ip"
done

for i in $(seq 1 "$N"); do
    svc=$(printf 'node-%02d' "$i")
    container="p2pnode-$svc"
    pid=$(docker inspect -f '{{.State.Pid}}' "$container" 2>/dev/null || echo "")
    if [ -z "$pid" ] || [ "$pid" = "0" ]; then
        echo "[underlay] $svc: not running, skip"
        continue
    fi

    # Собираем список разрешённых IP.
    allowed_addrs=""
    while IFS= read -r row; do
        dst_idx=$(echo "$row" | jq -r '.dst')
        dst_ip="${NODE_IP[$dst_idx]:-}"
        if [ -n "$dst_ip" ]; then
            allowed_addrs="$allowed_addrs $dst_ip"
        fi
    done < <(jq -c --argjson src "$i" '.allowed_pairs[] | select(.src == $src)' "$RULES")

    # Формируем полный набор nftables команд.
    # Используем add table (delete + add — для идемпотентности).
    # Политика input/output — drop по умолчанию.
    #
    # Разрешаем:
    #   - loopback
    #   - established/related
    #   - ICMP
    #   - DNS к 127.0.0.11 (Docker DNS)
    #   - TCP-порт узла (9000+i) от любого (нужно для входящих bootstrap-связей)
    #   - TCP-выход к разрешённым IP
    {
        echo "table inet filter {}"
        echo "delete table inet filter"
        echo "table inet filter {"
        echo "  chain input {"
        echo "    type filter hook input priority 0; policy drop;"
        echo "    iif lo accept"
        echo "    ct state established,related accept"
        echo "    ip protocol icmp accept"
        echo "    ip daddr 127.0.0.11 udp dport 53 accept"
        echo "    ip daddr 127.0.0.11 tcp dport 53 accept"
        echo "    tcp dport $((9000 + i)) accept"
        echo "  }"
        echo "  chain output {"
        echo "    type filter hook output priority 0; policy drop;"
        echo "    oif lo accept"
        echo "    ct state established,related accept"
        echo "    ip protocol icmp accept"
        echo "    ip daddr 127.0.0.11 udp dport 53 accept"
        echo "    ip daddr 127.0.0.11 tcp dport 53 accept"
        for addr in $allowed_addrs; do
            echo "    ip daddr $addr tcp dport 9000-$((9000 + N)) accept"
        done
        echo "  }"
        echo "}"
    } | sudo nsenter -t "$pid" -n nft -f - || \
        echo "[underlay] WARN: nft failed for $svc"

    echo "[underlay] $svc: applied ($(echo "$allowed_addrs" | wc -w) allowed peers)"
done

echo "[underlay] applied to $N containers"
