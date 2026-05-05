#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$REPO_ROOT"

echo "=== 1. Go integration slice ==="
./tests/run_tests.sh integration

echo
echo "=== 2. Neo4j-backed compose proof ==="
./scripts/verify_relationship_platform_compose.sh

echo
echo "Neo4j-backed verification completed."
