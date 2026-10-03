#!/usr/bin/env bash
# Run the nine readiness and dead-code incoming plan/correctness proofs on
# disposable PostgreSQL 18 (eight in the impact package, one in internal/query),
# one go test per package.
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
  ESHU_RUNTIME_ENVIRONMENT_EVIDENCE_POSTGRES_DSN \
  ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DSN \
  ESHU_DEAD_CODE_INCOMING_BOUND_PROOF_DSN; do
  [[ -n "${!name:-}" ]] || die "${name} must name the administrative postgres database"
  [[ "${!name}" == */postgres\?* || "${!name}" == */postgres ]] ||
    die "${name} must target the administrative postgres database"
done
for name in \
  ESHU_PACKAGE_MANIFEST_REPO_SCOPE_EXPLAIN_PROOF_DISPOSABLE \
  ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE \
  ESHU_READINESS_CONTAINER_IDENTITY_PROOF_DISPOSABLE \
  ESHU_RUNTIME_ENVIRONMENT_EVIDENCE_POSTGRES_DISPOSABLE \
  ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DISPOSABLE \
  ESHU_DEAD_CODE_INCOMING_BOUND_PROOF_DISPOSABLE; do
  [[ "${!name:-}" == "1" ]] || die "${name} must be 1"
done

python3 "${results}" verify-ledger "${ledger}" "${repo_root}" ||
  die "postgres_ci ledger selection is invalid"

impact_pattern='^(TestSupplyChainImpactReadinessPackageManifestRepoScopeQueryPlanLive|TestSupplyChainImpactReadinessRepoArmScopeLive|TestSupplyChainImpactReadinessScanTierQueryPlanLive|TestSupplyChainImpactReadinessScanTierOSPackageCountDoesNotFanOutLive|TestSupplyChainImpactReadinessPackageConsumptionScopeLive|TestSupplyChainImpactReadinessMutableRefIncludesEveryCurrentDigestLive|TestRuntimeEnvironmentEvidenceHotDigestUsesArtifactIndexLive|TestRuntimeEnvironmentEvidenceCurrentAuthorizedTruthMatrixLive)$'
# Parallel lists: package path (relative to go/) and its -run pattern. The
# package paths must match the keys of PACKAGES in the results verifier.
query_pattern='^(TestDeadCodeIncomingEntityIDsActiveRunBoundLive)$'
packages=(./internal/query/supply/chain/impact ./internal/query)
patterns=("${impact_pattern}" "${query_pattern}")

scratch="$(mktemp -d)"
trap 'rm -rf "${scratch}"' EXIT
started="${SECONDS}"
failed_packages=()

# Every package runs even after an earlier one fails, so one CI run reports
# every broken proof. The final status fails closed if any package failed.
for i in "${!packages[@]}"; do
  package="${packages[$i]}"
  events="${scratch}/events-${i}.jsonl"
  if (cd "${repo_root}/go" && go test -json -count=1 -timeout=15m \
    "${package}" -run "${patterns[$i]}") \
    >"${events}" 2>"${scratch}/stderr-${i}"; then
    go_status=0
  else
    go_status=$?
  fi
  if python3 "${results}" verify-results "${events}" "${package}"; then
    results_status=0
  else
    results_status=$?
  fi
  if [[ "${go_status}" -ne 0 ]]; then
    # Compiler and runtime diagnostics may be on stderr instead of in the JSON
    # stream. Limit them here so a failed proof cannot flood the CI log.
    sed -n '1,20p' "${scratch}/stderr-${i}" | cut -c 1-300 >&2
    printf 'live-postgres-readiness: %s: go test exited %s\n' "${package}" "${go_status}" >&2
    failed_packages+=("${package}")
  elif [[ "${results_status}" -ne 0 ]]; then
    printf 'live-postgres-readiness: %s: expected tests did not all pass\n' "${package}" >&2
    failed_packages+=("${package}")
  fi
done
printf 'live-postgres-readiness: suite_elapsed=%ss\n' "$((SECONDS - started))"
if [[ "${#failed_packages[@]}" -ne 0 ]]; then
  die "failed packages: ${failed_packages[*]}"
fi
python3 "${results}" summary
