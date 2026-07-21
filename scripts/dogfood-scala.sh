#!/usr/bin/env bash
#
# dogfood-scala.sh - offline-reproducible real-repo dogfood check for Scala
# (#5399, spun off from #5336). Runs TestDogfoodScalaRealRepoSnapshot
# (go/internal/parser/scala/dogfood_real_repo_test.go) against the
# committed tests/fixtures/dogfood/scala_real_repo corpus and diffs the
# parser's bucket counts against the checked-in snapshot at
# go/internal/parser/scala/testdata/dogfood_real_repo_snapshot.txt. Zero
# network calls. playframework/playframework (pinned SHA
# bcdc682de2250bbd0f2788bc5acc06f6d66ad5a7) and scala/scala (pinned SHA
# 25075e9b9b79954a0f99de515618901818822e62), which informed this fixture's
# shape and match the historical Issue #105 dogfood run cited in
# docs/public/languages/scala.md, are recorded only as provenance in the
# fixture and test file header comments -- neither is fetched here.
#
# To regenerate the snapshot after an intentional parser change:
#   DOGFOOD_UPDATE_SNAPSHOT=1 bash scripts/dogfood-scala.sh
#
# Usage: bash scripts/dogfood-scala.sh
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/dogfood-real-repo.sh
. "${script_dir}/lib/dogfood-real-repo.sh"

run_dogfood_real_repo "scala" "TestDogfoodScalaRealRepoSnapshot"
