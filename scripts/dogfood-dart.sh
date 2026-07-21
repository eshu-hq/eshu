#!/usr/bin/env bash
#
# dogfood-dart.sh - offline-reproducible real-repo dogfood check for Dart
# (#5399, spun off from #5336). Runs TestDogfoodDartRealRepoSnapshot
# (go/internal/parser/dart/dogfood_real_repo_test.go) against the committed
# tests/fixtures/dogfood/dart_real_repo corpus and diffs the parser's
# row-level output (one line per emitted entity/relationship with its
# identifying fields, not just per-bucket counts) against the checked-in
# snapshot at
# go/internal/parser/dart/testdata/dogfood_real_repo_snapshot.txt. Zero
# network calls. flutter/flutter and dart-lang/http, which informed this
# fixture's shape, are recorded only as provenance in the fixture and test
# file header comments -- neither is fetched here.
#
# To regenerate the snapshot after an intentional parser change:
#   DOGFOOD_UPDATE_SNAPSHOT=1 bash scripts/dogfood-dart.sh
#
# Usage: bash scripts/dogfood-dart.sh
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/dogfood-real-repo.sh
. "${script_dir}/lib/dogfood-real-repo.sh"

run_dogfood_real_repo "dart" "TestDogfoodDartRealRepoSnapshot"
