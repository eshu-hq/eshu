#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "${repo_root}/go"
go test ./internal/evidencecontinuity -run 'TestValidator|TestValidateRealEvidenceContinuityContract' -count=1
printf 'verify-evidence-continuity tests passed\n'
