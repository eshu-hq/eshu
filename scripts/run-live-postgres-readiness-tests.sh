#!/usr/bin/env bash
# Run the six readiness plan/correctness proofs on disposable PostgreSQL 18.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
results="${repo_root}/scripts/lib/live_postgres_readiness_results.py"
ledger="${repo_root}/specs/live-tests.v1.yaml"

die() { printf 'live-postgres-readiness: %s\n' "$*" >&2; exit 1; }

[[ $# -eq 0 ]] || die "no arguments accepted"
command -v go >/dev/null 2>&1 || die "go is required"
command -v python3 >/dev/null 2>&1 || die "python3 is required"

for name in \
  ESHU_PACKAGE_MANIFEST_REPO_SCOPE_EXPLAIN_PROOF_DSN \
  ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN \
  ESHU_READINESS_CONTAINER_IDENTITY_PROOF_DSN \
  ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DSN; do
  [[ -n "${!name:-}" ]] || die "${name} must name the administrative postgres database"
  [[ "${!name}" == */postgres\?* || "${!name}" == */postgres ]] ||
    die "${name} must target the administrative postgres database"
done
for name in \
  ESHU_PACKAGE_MANIFEST_REPO_SCOPE_EXPLAIN_PROOF_DISPOSABLE \
  ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE \
  ESHU_READINESS_CONTAINER_IDENTITY_PROOF_DISPOSABLE \
  ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DISPOSABLE; do
  [[ "${!name:-}" == "1" ]] || die "${name} must be 1"
done

python3 "${results}" verify-ledger "${ledger}" "${repo_root}" ||
  die "postgres_ci ledger selection is invalid"

run_pattern='^(TestSupplyChainImpactReadinessPackageManifestRepoScopeQueryPlanLive|TestSupplyChainImpactReadinessRepoArmScopeLive|TestSupplyChainImpactReadinessScanTierQueryPlanLive|TestSupplyChainImpactReadinessScanTierOSPackageCountDoesNotFanOutLive|TestSupplyChainImpactReadinessPackageConsumptionScopeLive|TestSupplyChainImpactReadinessMutableRefIncludesEveryCurrentDigestLive)$'
scratch="$(mktemp -d)"
trap 'rm -rf "${scratch}"' EXIT
started="${SECONDS}"

if (cd "${repo_root}/go" && go test -json -count=1 -timeout=15m \
  ./internal/query/supply/chain/impact -run "${run_pattern}") \
  >"${scratch}/events.jsonl" 2>"${scratch}/stderr"; then
  go_status=0
else
  go_status=$?
fi

if python3 "${results}" verify-results "${scratch}/events.jsonl"; then
  results_status=0
else
  results_status=$?
fi
printf 'live-postgres-readiness: suite_elapsed=%ss\n' "$((SECONDS - started))"
if [[ "${go_status}" -ne 0 ]]; then
  # Compiler and runtime diagnostics may be on stderr instead of in the JSON
  # stream. Limit them here so a failed proof cannot flood the CI log.
  sed -n '1,20p' "${scratch}/stderr" | cut -c 1-300 >&2
  die "go test exited ${go_status}"
fi
[[ "${results_status}" -eq 0 ]] || die "expected tests did not all pass"
