#!/usr/bin/env bash
set -euo pipefail

VICTIM="${1:?usage: $0 <victim_id_hex> <output_dir>}"
OUT_DIR="${2:?usage: $0 <victim_id_hex> <output_dir>}"

if [ ${#VICTIM} -ne 64 ]; then
    echo "[sybil] victim must be 64 hex chars, got ${#VICTIM}" >&2
    exit 1
fi

PREFIX=$(echo "${VICTIM:0:2}" | tr 'A-Z' 'a-z')
mkdir -p "$OUT_DIR"

echo "[sybil] victim prefix: $PREFIX"

python3 - "$VICTIM" "$OUT_DIR" <<'PYEOF'
import hashlib
import os
import sys

victim = sys.argv[1].lower()
out_dir = sys.argv[2]
prefix = victim[:2]

try:
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
    from cryptography.hazmat.primitives import serialization
except ImportError:
    print("[sybil] ERR: python cryptography not installed", file=sys.stderr)
    print("[sybil] use: pip install cryptography", file=sys.stderr)
    sys.exit(1)

tries = 0
max_tries = 100000

while tries < max_tries:
    tries += 1
    priv = Ed25519PrivateKey.generate()
    pub_bytes = priv.public_key().public_bytes(
        encoding=serialization.Encoding.Raw,
        format=serialization.PublicFormat.Raw,
    )
    node_id = hashlib.sha256(pub_bytes).hexdigest()

    if node_id.startswith(prefix):
        priv_bytes = priv.private_bytes(
            encoding=serialization.Encoding.Raw,
            format=serialization.PrivateFormat.Raw,
            encryption_algorithm=serialization.NoEncryption(),
        )
        # identity.key = seed (32) || pubkey (32) = 64 байта.
        key_bytes = priv_bytes + pub_bytes
        with open(os.path.join(out_dir, "identity.key"), "wb") as f:
            f.write(key_bytes)
        with open(os.path.join(out_dir, "identity.pub"), "wb") as f:
            f.write(pub_bytes)

        print(f"[sybil] found in {tries} tries")
        print(f"[sybil] node_id: {node_id}")
        print(f"[sybil] prefix match: {node_id[:2]} == {prefix}")
        sys.exit(0)

print(f"[sybil] ERR: no match in {max_tries} tries", file=sys.stderr)
sys.exit(1)
PYEOF
