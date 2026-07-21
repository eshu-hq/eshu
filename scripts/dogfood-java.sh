#!/usr/bin/env bash
#
# dogfood-java.sh - offline-reproducible real-repo dogfood check for Java
# (#5399, spun off from #5336). Runs TestDogfoodJavaRealRepoSnapshot
# (go/internal/parser/java/dogfood_real_repo_test.go) against the committed
# tests/fixtures/dogfood/java_real_repo corpus and diffs the parser's
# row-level output (one line per emitted entity/relationship with its
# identifying fields, not just per-bucket counts) against the checked-in
# snapshot at
# go/internal/parser/java/testdata/dogfood_real_repo_snapshot.txt. Zero
# network calls. No external repository or pinned SHA is cited as
# provenance for this fixture: docs/public/languages/java.md never carried
# a specific external-repo dogfood claim, so none is fabricated here.
#
# To regenerate the snapshot after an intentional parser change:
#   DOGFOOD_UPDATE_SNAPSHOT=1 bash scripts/dogfood-java.sh
#
# Usage: bash scripts/dogfood-java.sh
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/dogfood-real-repo.sh
. "${script_dir}/lib/dogfood-real-repo.sh"

run_dogfood_real_repo "java" "TestDogfoodJavaRealRepoSnapshot"
