#!/usr/bin/env bash
#
# dogfood-swift.sh - offline-reproducible real-repo dogfood check for Swift
# (#5399, spun off from #5336). Runs TestDogfoodSwiftRealRepoSnapshot
# (go/internal/parser/swift/dogfood_real_repo_test.go) against the
# committed tests/fixtures/dogfood/swift_real_repo corpus and diffs the
# parser's bucket counts against the checked-in snapshot at
# go/internal/parser/swift/testdata/dogfood_real_repo_snapshot.txt. Zero
# network calls. No external repository or pinned SHA is cited as
# provenance for this fixture: docs/public/languages/swift.md never carried
# a specific external-repo dogfood claim, so none is fabricated here.
#
# To regenerate the snapshot after an intentional parser change:
#   DOGFOOD_UPDATE_SNAPSHOT=1 bash scripts/dogfood-swift.sh
#
# Usage: bash scripts/dogfood-swift.sh
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/dogfood-real-repo.sh
. "${script_dir}/lib/dogfood-real-repo.sh"

run_dogfood_real_repo "swift" "TestDogfoodSwiftRealRepoSnapshot"
