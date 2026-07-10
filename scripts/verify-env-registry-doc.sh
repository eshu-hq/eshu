#!/usr/bin/env bash
#
# verify-env-registry-doc.sh - fail when the committed environment variable
# reference doc drifts from go/internal/envregistry.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

(
  cd "${repo_root}/go"
  go test ./internal/envregistry -run '^TestEnvRegistryReferenceDocUpToDate$' -count=1
)
