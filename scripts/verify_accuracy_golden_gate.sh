#!/usr/bin/env bash

# verify_accuracy_golden_gate.sh runs the continuous accuracy golden gate
# (issue #3499): the single gate that fails on accuracy regressions across
# cyclomatic complexity, cross-repo call resolvers, and correlation precision.
#
# The gate is a static-fixture Go test (no live services). It measures real
# values from the shipped parser, reducer, and admission-audit harnesses and
# asserts each dimension meets or exceeds the published floor in
# go/internal/accuracygate/testdata/baseline.json. A regression below the floor,
# a scored language reverting to a constant, a dropped resolver, or a missing
# measurement fails here.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

(
    cd "$REPO_ROOT/go"
    go test ./internal/accuracygate -count=1
)

echo "Accuracy golden gate verified across complexity, resolvers, and correlation."
