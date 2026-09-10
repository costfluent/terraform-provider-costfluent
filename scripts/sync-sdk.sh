#!/usr/bin/env bash
set -euo pipefail

PROVIDER_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SOURCE="$PROVIDER_ROOT/../costfluent-go/costfluent"
TARGET="$PROVIDER_ROOT/internal/costfluent"

if [[ "${1:-}" == "--check" ]]; then
  if ! diff -qr "$SOURCE" "$TARGET" >/dev/null; then
    echo "embedded provider SDK is stale; run 'make -C toolkit/terraform-provider sync-sdk'" >&2
    diff -qr "$SOURCE" "$TARGET" >&2 || true
    exit 1
  fi
  exit 0
fi

mkdir -p "$TARGET"
rsync -a --delete "$SOURCE"/ "$TARGET"/
