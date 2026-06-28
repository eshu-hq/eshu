#!/usr/bin/env bash
set -euo pipefail

repo_root="${ESHU_EVIDENCE_CONTINUITY_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fi

cd "${repo_root}/go"
go test ./internal/evidencecontinuity -run TestValidateRealEvidenceContinuityContract -count=1
printf 'verify-evidence-continuity: contract matrix is complete\n'
