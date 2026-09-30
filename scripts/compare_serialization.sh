#!/usr/bin/env bash
# Сравнение форматов сериализации: msgpack, JSON, gob.
#
# Для каждого формата:
#   1. Кодирует N сообщений FIND_NODE_RESPONSE.
#   2. Замеряет средний размер.
#   3. Замеряет среднее время encode и decode.
#
# Выводит таблицу и сохраняет JSON в visualization/serialization.json.
#
# Использование:
#   ./scripts/compare_serialization.sh [N]
#
# По умолчанию N=1000.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

N="${1:-1000}"
OUT="${ROOT_DIR}/visualization/serialization.json"
mkdir -p "$(dirname "$OUT")"

# Временный каталог для бенчмарка.
BENCH_DIR="$(mktemp -d)"
trap 'rm -rf "$BENCH_DIR"' EXIT

cat > "$BENCH_DIR/main.go" <<'GOEOF'
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/vmihailenco/msgpack/v5"

	"p2pnode/internal/protocol"
)

type result struct {
	Format     string  `json:"format"`
	SizeBytes  int     `json:"size_bytes"`
	EncodeUs   float64 `json:"encode_us_per_msg"`
	DecodeUs   float64 `json:"decode_us_per_msg"`
}

func makeContact() protocol.Contact {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	var id [32]byte
	copy(id[:], pub)
	return protocol.Contact{
		NodeID:            id,
		IdentityAlgorithm: "ed25519",
		IdentityPublicKey: pub,
		Host:              "127.0.0.1",
		Port:              9001,
	}
}

func main() {
	n := 1000
	if len(os.Args) > 1 {
		if v, err := strconv.Atoi(os.Args[1]); err == nil {
			n = v
		}
	}

	msg := protocol.FindNodeResponsePayload{
		Responder:    makeContact(),
		TargetNodeID: [32]byte{1, 2, 3},
		Contacts: []protocol.Contact{
			makeContact(), makeContact(), makeContact(), makeContact(),
		},
	}

	var results []result

	// --- msgpack ---
	{
		start := time.Now()
		var total int
		var sample []byte
		for i := 0; i < n; i++ {
			b, _ := msgpack.Marshal(msg)
			total += len(b)
			sample = b
		}
		enc := time.Since(start)

		start = time.Now()
		for i := 0; i < n; i++ {
			var m protocol.FindNodeResponsePayload
			_ = msgpack.Unmarshal(sample, &m)
		}
		dec := time.Since(start)

		results = append(results, result{
			Format:    "msgpack",
			SizeBytes: total / n,
			EncodeUs:  float64(enc.Microseconds()) / float64(n),
			DecodeUs:  float64(dec.Microseconds()) / float64(n),
		})
	}

	// --- JSON ---
	{
		start := time.Now()
		var total int
		var sample []byte
		for i := 0; i < n; i++ {
			b, _ := json.Marshal(msg)
			total += len(b)
			sample = b
		}
		enc := time.Since(start)

		start = time.Now()
		for i := 0; i < n; i++ {
			var m protocol.FindNodeResponsePayload
			_ = json.Unmarshal(sample, &m)
		}
		dec := time.Since(start)

		results = append(results, result{
			Format:    "json",
			SizeBytes: total / n,
			EncodeUs:  float64(enc.Microseconds()) / float64(n),
			DecodeUs:  float64(dec.Microseconds()) / float64(n),
		})
	}

	// --- gob ---
	{
		start := time.Now()
		var total int
		var sample []byte
		for i := 0; i < n; i++ {
			var buf bytes.Buffer
			enc := gob.NewEncoder(&buf)
			_ = enc.Encode(msg)
			total += buf.Len()
			sample = buf.Bytes()
		}
		enc := time.Since(start)

		start = time.Now()
		for i := 0; i < n; i++ {
			var m protocol.FindNodeResponsePayload
			dec := gob.NewDecoder(bytes.NewReader(sample))
			_ = dec.Decode(&m)
		}
		dec := time.Since(start)

		results = append(results, result{
			Format:    "gob",
			SizeBytes: total / n,
			EncodeUs:  float64(enc.Microseconds()) / float64(n),
			DecodeUs:  float64(dec.Microseconds()) / float64(n),
		})
	}

	out, _ := json.MarshalIndent(results, "", "  ")
	fmt.Println(string(out))
}
GOEOF

# Копируем бенчмарк в проект, чтобы он видел internal/protocol.
mkdir -p "$ROOT_DIR/cmd/serialize_bench"
cp "$BENCH_DIR/main.go" "$ROOT_DIR/cmd/serialize_bench/main.go"

# Запускаем.
go run "$ROOT_DIR/cmd/serialize_bench/main.go" "$N" > "$OUT"

# Удаляем временный бенчмарк.
rm -rf "$ROOT_DIR/cmd/serialize_bench"

# Печатаем таблицу.
python3 - "$OUT" <<'PYEOF'
import json, sys
data = json.load(open(sys.argv[1]))
print()
print(f"{'format':10} {'size (bytes)':>15} {'encode (us/msg)':>18} {'decode (us/msg)':>18}")
print("-" * 65)
for r in data:
    print(f"{r['format']:10} {r['size_bytes']:>15} {r['encode_us_per_msg']:>18.3f} {r['decode_us_per_msg']:>18.3f}")
print()
PYEOF

echo "[compare_serialization] results saved to $OUT"
