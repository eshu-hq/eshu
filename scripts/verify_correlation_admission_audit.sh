#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

(
    cd "$REPO_ROOT/go"
    go test ./internal/admissionaudit -count=1
)

echo "Correlation admission audit fixture verified."
