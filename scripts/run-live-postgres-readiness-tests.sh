#!/usr/bin/env bash
# Run the readiness, dead-code incoming, status route-selection,
# quiet-generation, activation obligation, #7584 targeted-maintenance,
# reindex watermark plan/correctness, container image identity epoch-gate,
# #7209 projector zombie-heal, and #7471 marked-write-age signal proofs
# listed in scripts/lib/live_postgres_readiness_results.py on disposable
# PostgreSQL 18
# (plus a disposable Neo4j for the zombie-heal graph proof when its env is
# configured; without one the graph proofs are excused and named in the
# summary), one go test per package.
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
  ESHU_DEAD_CODE_INCOMING_BOUND_PROOF_DSN \
  ESHU_GENERATION_RETENTION_PROOF_DSN \
  ESHU_STATUS_TERRAFORM_SELECTION_PROOF_DSN \
  ESHU_DEFERRED_PARTITION_PROOF_DSN \
  ESHU_TARGETED_MAINTENANCE_PROOF_DSN \
  ESHU_STATUS_SUMMARY_PROOF_DSN \
  ESHU_ADMIN_REOPEN_PROOF_DSN \
  ESHU_FLUX_EVIDENCE_IDENTITY_PROOF_DSN \
  ESHU_REACHABILITY_EDGES_SCOPE_PROOF_DSN \
  ESHU_DRIFTED_BUCKET_SKIP_PROOF_DSN \
  ESHU_CONTAINER_IMAGE_IDENTITY_EPOCH_PROOF_DSN \
  ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN \
  ESHU_PREFETCH_BATCH_PLAN_PROOF_DSN \
  ESHU_REDUCER_FAIRNESS_PROOF_DSN \
  ESHU_RECOVERY_DELTA_ACTIVE_PROOF_DSN; do
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
  ESHU_DEAD_CODE_INCOMING_BOUND_PROOF_DISPOSABLE \
  ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE \
  ESHU_STATUS_TERRAFORM_SELECTION_PROOF_DISPOSABLE \
  ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE \
  ESHU_TARGETED_MAINTENANCE_PROOF_DISPOSABLE \
  ESHU_STATUS_SUMMARY_PROOF_DISPOSABLE \
  ESHU_ADMIN_REOPEN_PROOF_DISPOSABLE \
  ESHU_FLUX_EVIDENCE_IDENTITY_PROOF_DISPOSABLE \
  ESHU_REACHABILITY_EDGES_SCOPE_PROOF_DISPOSABLE \
  ESHU_DRIFTED_BUCKET_SKIP_PROOF_DISPOSABLE \
  ESHU_CONTAINER_IMAGE_IDENTITY_EPOCH_PROOF_DISPOSABLE \
  ESHU_PROJECTOR_SUPERSESSION_PROOF_DISPOSABLE \
  ESHU_PREFETCH_BATCH_PLAN_PROOF_DISPOSABLE \
  ESHU_REDUCER_FAIRNESS_PROOF_DISPOSABLE \
  ESHU_RECOVERY_DELTA_ACTIVE_PROOF_DISPOSABLE; do
  [[ "${!name:-}" == "1" ]] || die "${name} must be 1"
done
# Only the Neo4j graph proofs need a backend; everything else runs on plain
# Postgres. All three Neo4j variables set selects strict mode (the CI path):
# the backend must be neo4j and every enrolled proof must pass. None set
# selects degrade mode: the graph proofs are excused from verification and
# named in the summary, so a developer without a container still runs every
# other proof. A partial set dies loud instead of silently degrading.
neo4j_missing=()
for name in \
  ESHU_NEO4J_URI \
  ESHU_NEO4J_USERNAME \
  ESHU_NEO4J_PASSWORD; do
  [[ -n "${!name:-}" ]] || neo4j_missing+=("${name}")
done
skip_args=()
if [[ "${#neo4j_missing[@]}" -eq 0 ]]; then
  [[ "${ESHU_GRAPH_BACKEND:-}" == "neo4j" ]] || die "ESHU_GRAPH_BACKEND must be neo4j"
elif [[ "${#neo4j_missing[@]}" -eq 3 ]]; then
  graph_tests="$(python3 "${results}" list-neo4j-tests)" ||
    die "neo4j test list from the results verifier is invalid"
  for name in ${graph_tests}; do skip_args+=(--skip "${name}"); done
  if [[ "${#skip_args[@]}" -gt 0 ]]; then
    printf 'live-postgres-readiness: no Neo4j proof backend configured; excusing %s\n' "${graph_tests}"
  fi
else
  die "${neo4j_missing[*]} must name the Neo4j proof backend (set all three or none)"
fi

python3 "${results}" verify-ledger "${ledger}" "${repo_root}" ||
  die "postgres_ci ledger selection is invalid"

# The package list and each package's anchored -run pattern come from PACKAGES
# in the results verifier, the single source of truth, so this runner cannot
# drift from the ledger-checked expectations. Records are tab separated:
# package path (relative to go/), -run pattern, expected test names.
scratch="$(mktemp -d)"
trap 'rm -rf "${scratch}"' EXIT
python3 "${results}" list-packages >"${scratch}/packages.tsv" ||
  die "package list from the results verifier is invalid"
[[ -s "${scratch}/packages.tsv" ]] || die "results verifier listed no packages"

started="${SECONDS}"
failed_packages=()

# Every package runs even after an earlier one fails, so one CI run reports
# every broken proof. The final status fails closed if any package failed.
i=0
while IFS=$'\t' read -r package pattern _tests; do
  i=$((i + 1))
  events="${scratch}/events-${i}.jsonl"
  if (cd "${repo_root}/go" && go test -json -count=1 -timeout=15m \
    "${package}" -run "${pattern}") \
    >"${events}" 2>"${scratch}/stderr-${i}" </dev/null; then
    go_status=0
  else
    go_status=$?
  fi
  # The empty-array expansion keeps `set -u` safe on older bash: with no
  # --skip flags this is exactly the historical three-argument call.
  # shellcheck disable=SC2086
  if python3 "${results}" verify-results "${events}" "${package}" ${skip_args[@]+"${skip_args[@]}"} </dev/null; then
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
done <"${scratch}/packages.tsv"
printf 'live-postgres-readiness: suite_elapsed=%ss\n' "$((SECONDS - started))"
if [[ "${#failed_packages[@]}" -ne 0 ]]; then
  die "failed packages: ${failed_packages[*]}"
fi
# shellcheck disable=SC2086
python3 "${results}" summary ${skip_args[@]+"${skip_args[@]}"}
