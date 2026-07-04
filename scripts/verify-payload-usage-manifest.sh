#!/usr/bin/env bash
# Contract System v1 §6 enforcement gate 2: the payload-usage manifest
# (issue #4573). Runs the reducer-side drift-lock test, which derives the
# manifest from the typed factschema.Decode* calls in
# go/internal/reducer/factschema_decode.go and compares every field a
# handler reads against the checked-in JSON Schemas under
# sdk/go/factschema/schema/, failing when a handler reads a field no
# declared schema covers.
set -euo pipefail

repo_root="${ESHU_PAYLOAD_USAGE_MANIFEST_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
    || (cd "$(dirname "$0")/.." && pwd))"
fi

go_dir="${ESHU_PAYLOAD_USAGE_MANIFEST_GO_DIR:-${repo_root}/go}"

cd "$go_dir"
go test ./internal/reducer -run TestPayloadUsageManifest -count=1 "$@"
