#!/usr/bin/env bash
# Drift gate for the derived live-postgres-readiness registration (#7859
# P1): regenerate every surface from the test-file enrollment markers and
# fail on any difference, tracked or untracked; then run the ledger and
# suite verifiers. A seeded unregistered test fails the ledger validator
# as unclassified, an orphan row fails it as a missing file, and stale
# generated output fails the diff. Hermetic: bash, python3, git, and grep
# only — no Go toolchain, Postgres, NornicDB, Docker, or network.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

GENERATED=(
  specs/live-tests.v1.yaml
  specs/live-tests.d/live-tests-readiness.generated.yaml
  scripts/lib/live_postgres_readiness_inventory_generated.py
  scripts/lib/live_postgres_readiness_inventory_gen_*.py
  scripts/lib/live_postgres_readiness_inventory_helpers.py
  scripts/lib/live_postgres_readiness_env.sh
  .github/workflows/live-postgres-readiness.yml
)

python3 "${repo_root}/scripts/generate-live-postgres-readiness.py"
# Tracked drift first: regenerating must leave the tree byte-identical.
git -C "${repo_root}" diff --exit-code -- "${GENERATED[@]}"
# Untracked drift second: a new package's generated module is a new file,
# which the diff above cannot see.
! git -C "${repo_root}" status --porcelain -- "${GENERATED[@]}" | grep -q .
bash "${repo_root}/scripts/verify-live-tests-ledger.sh"
python3 "${repo_root}/scripts/lib/live_postgres_readiness_results.py" \
  verify-ledger "${repo_root}/specs/live-tests.v1.yaml" "${repo_root}"
