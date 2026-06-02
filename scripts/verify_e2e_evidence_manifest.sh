#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="${SCRIPT_DIR}/lib/e2e_evidence_manifest.sh"

# shellcheck source=scripts/lib/e2e_evidence_manifest.sh
source "${LIB}"

validate_e2e_evidence_manifest "${1:-}"
