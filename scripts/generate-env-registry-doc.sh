#!/usr/bin/env bash
#
# generate-env-registry-doc.sh - regenerate the checked-in environment variable
# reference from go/internal/envregistry.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

(
  cd "${repo_root}/go"
  ESHU_UPDATE_ENV_DOC=1 go test ./internal/envregistry \
    -run '^TestEnvRegistryReferenceDocUpToDate$' -count=1
)
