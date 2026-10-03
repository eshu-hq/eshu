#!/usr/bin/env bash
# Hermetic RED/GREEN checks for the dedicated PostgreSQL readiness runner:
# a fake go supplies the per-package event streams of every enrolled proof.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
runner="${repo_root}/scripts/run-live-postgres-readiness-tests.sh"
seed_dir="$(mktemp -d)"
trap 'rm -rf "${seed_dir}"' EXIT

fail() { printf 'test-run-live-postgres-readiness-tests: %s\n' "$*" >&2; exit 1; }

mkdir -p "${seed_dir}/bin" "${seed_dir}/fake"
fake="${seed_dir}/fake"
# The fake go selects its events by package path, checks the -run pattern
# names every expected test of that package, and checks the opt-in env.
cat >"${seed_dir}/bin/go" <<'EOF'
#!/usr/bin/env bash
[[ "$*" == *"-json"* ]] || { echo 'missing -json' >&2; exit 9; }
[[ "$*" == *"-count=1"* ]] || { echo 'missing -count=1' >&2; exit 9; }
for arg in "$@"; do [[ "${arg}" == ./* ]] && pkg="${arg}"; done
key="${pkg//[^a-z]/_}"
[[ -f "${ESHU_FAKE_DIR}/${key}.names" ]] || { echo "unexpected package ${pkg}" >&2; exit 9; }
EOF
cat >>"${seed_dir}/bin/go" <<'EOF'
while read -r name; do
  [[ "$*" == *"${name}"* ]] || { echo "missing test ${name}" >&2; exit 9; }
done <"${ESHU_FAKE_DIR}/${key}.names"
while read -r var; do
  case "${var}" in *_DSN) want="${ESHU_EXPECTED_DSN:-}" ;; *) want=1 ;; esac
  [[ "${!var:-}" == "${want}" ]] || { echo "wrong ${var}" >&2; exit 9; }
done <"${ESHU_FAKE_DIR}/envs"
EOF
cat >>"${seed_dir}/bin/go" <<'EOF'
cat "${ESHU_FAKE_DIR}/${key}.jsonl"
if [[ -f "${ESHU_FAKE_DIR}/${key}.exit" ]]; then exit "$(cat "${ESHU_FAKE_DIR}/${key}.exit")"; fi
exit 0
EOF
chmod +x "${seed_dir}/bin/go"
for var in ESHU_PACKAGE_MANIFEST_REPO_SCOPE_EXPLAIN_PROOF ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF \
  ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF ESHU_READINESS_CONTAINER_IDENTITY_PROOF \
  ESHU_RUNTIME_ENVIRONMENT_EVIDENCE_POSTGRES ESHU_DEAD_CODE_INCOMING_BOUND_PROOF; do
  printf '%s_DSN\n%s_DISPOSABLE\n' "${var}" "${var}" >>"${fake}/envs"
done

export ESHU_FAKE_DIR="${fake}"
export ESHU_EXPECTED_DSN='postgres://postgres:local-test@127.0.0.1:15432/postgres?sslmode=disable'
while read -r var; do
  case "${var}" in *_DSN) export "${var}=${ESHU_EXPECTED_DSN}" ;; *) export "${var}=1" ;; esac
done <"${fake}/envs"
export PATH="${seed_dir}/bin:${PATH}"

impact=./internal/query/supply/chain/impact
storage=./internal/storage/postgres
query=./internal/query
# One "package|test" entry per enrolled proof, in the runner's order.
proofs=(
  "${impact}|TestSupplyChainImpactReadinessPackageManifestRepoScopeQueryPlanLive"
  "${impact}|TestSupplyChainImpactReadinessRepoArmScopeLive"
  "${impact}|TestSupplyChainImpactReadinessScanTierQueryPlanLive"
  "${impact}|TestSupplyChainImpactReadinessScanTierOSPackageCountDoesNotFanOutLive"
  "${impact}|TestSupplyChainImpactReadinessPackageConsumptionScopeLive"
  "${impact}|TestSupplyChainImpactReadinessMutableRefIncludesEveryCurrentDigestLive"
  "${impact}|TestRuntimeEnvironmentEvidenceHotDigestUsesArtifactIndexLive"
  "${impact}|TestRuntimeEnvironmentEvidenceCurrentAuthorizedTruthMatrixLive"
  "${storage}|TestPackageManifestConsumptionBackfillRepairsOldWriterAfterReadyLive"
  "${storage}|TestPackageManifestConsumptionBackfillPagesHeavyScopeLive"
  "${storage}|TestPackageManifestConsumptionBackfillWaitsForScopeWriterLive"
  "${storage}|TestPackageManifestConsumptionBackfillBoundsTwentyFiveScopePassLive"
  "${storage}|TestPackageManifestConsumptionBackfillConcurrentPassesAreIdempotentLive"
  "${storage}|TestPackageManifestConsumptionMigrationsUpgradeAfterSecretLinesLive"
  "${query}|TestDeadCodeIncomingEntityIDsActiveRunBoundLive"
)

pkg_key() { printf '%s' "${1//[^a-z]/_}"; }
events_of() { printf '%s/%s.jsonl' "${fake}" "$(pkg_key "$1")"; }

write_events() {
  local entry pkg name
  rm -f "${fake}"/*.jsonl "${fake}"/*.names "${fake}"/*.exit "${fake}"/*.bak
  for entry in "${proofs[@]}"; do
    pkg="${entry%%|*}"
    name="${entry##*|}"
    printf '%s\n' "${name}" >>"${fake}/$(pkg_key "${pkg}").names"
    printf '{"Action":"run","Test":"%s"}\n' "${name}" >>"$(events_of "${pkg}")"
    printf '{"Action":"pass","Test":"%s","Elapsed":0.25}\n' "${name}" >>"$(events_of "${pkg}")"
  done
  for pkg in "${impact}" "${storage}" "${query}"; do
    printf '{"Action":"pass","Package":"github.com/eshu-hq/eshu/go/%s","Elapsed":1.0}\n' \
      "${pkg#./}" >>"$(events_of "${pkg}")"
  done
}

run_runner() { bash "${runner}" 2>&1; }

write_events
out="$(run_runner)" || fail "fifteen PASS events rejected: ${out}"
[[ "${out}" == *"15/15 PASS"* ]] || fail "pass summary missing: ${out}"
[[ "${out}" == *"suite_elapsed="* ]] || fail "suite timing missing: ${out}"
[[ "${out}" == *"${impact} 8/8 PASS"* && "${out}" == *"${storage} 6/6 PASS"* &&
  "${out}" == *"${query} 1/1 PASS"* ]] || fail "per-package summaries missing: ${out}"

# Every enrolled proof needs its own pass event in its own package: a skip, a
# failure, or a missing event for any one of the fifteen fails the run.
for entry in "${proofs[@]}"; do
  pkg="${entry%%|*}"
  name="${entry##*|}"
  for action in skip fail; do
    write_events
    sed -i.bak "s/\"Action\":\"pass\",\"Test\":\"${name}\"/\"Action\":\"${action}\",\"Test\":\"${name}\"/" "$(events_of "${pkg}")"
    out="$(run_runner)" && fail "${name} ${action} event passed"
    want="$(printf '%s' "${action}" | tr '[:lower:]' '[:upper:]')"
    [[ "${out}" == *"${name}: ${want}"* ]] || fail "${name} ${action} not named: ${out}"
  done
  write_events
  sed -i.bak "/\"Test\":\"${name}\"/d" "$(events_of "${pkg}")"
  out="$(run_runner)" && fail "missing ${name} events passed"
  [[ "${out}" == *"${name}: missing"* ]] || fail "missing ${name} events not named: ${out}"
done

# A stale -run expression must not allow zero matched tests in any package.
for pkg in "${impact}" "${storage}" "${query}"; do
  write_events
  printf '{"Action":"pass","Package":"github.com/eshu-hq/eshu/go/%s","Elapsed":0.1}\n' "${pkg#./}" >"$(events_of "${pkg}")"
  out="$(run_runner)" && fail "zero matched tests in ${pkg} passed"
  [[ "${out}" == *"missing"* ]] || fail "zero-test failure in ${pkg} not named: ${out}"
done

# A failed package terminal fails the run even when every test passed.
for pkg in "${impact}" "${storage}" "${query}"; do
  write_events
  sed -i.bak 's/"Action":"pass","Package"/"Action":"fail","Package"/' "$(events_of "${pkg}")"
  out="$(run_runner)" && fail "failed package terminal of ${pkg} passed"
  [[ "${out}" == *"package terminal (${pkg})"* ]] || fail "package terminal of ${pkg} not named: ${out}"
done

# A package that exits nonzero fails the run and names that package; the
# other packages still run so one CI run reports every broken proof.
for pkg in "${impact}" "${storage}" "${query}"; do
  write_events
  printf '1\n' >"${fake}/$(pkg_key "${pkg}").exit"
  out="$(run_runner)" && fail "go nonzero exit in ${pkg} passed"
  [[ "${out}" == *"${pkg}: go test exited 1"* ]] || fail "go exit in ${pkg} not reported: ${out}"
done
write_events
sed -i.bak 's/"Action":"pass","Test":"TestSupplyChainImpactReadinessRepoArmScopeLive"/"Action":"fail","Test":"TestSupplyChainImpactReadinessRepoArmScopeLive"/' "$(events_of "${impact}")"
sed -i.bak 's/"Action":"pass","Test":"TestPackageManifestConsumptionBackfillPagesHeavyScopeLive"/"Action":"skip","Test":"TestPackageManifestConsumptionBackfillPagesHeavyScopeLive"/' "$(events_of "${storage}")"
out="$(run_runner)" && fail "failures in two packages passed"
[[ "${out}" == *"RepoArmScopeLive: FAIL"* && "${out}" == *"PagesHeavyScopeLive: SKIP"* ]] ||
  fail "an earlier package failure hid a later package failure: ${out}"

# A missing DSN or opt-in names the variable before any package runs.
while read -r var; do
  out="$(env -u "${var}" bash "${runner}" 2>&1)" && fail "unset ${var} passed"
  [[ "${out}" == *"${var}"* ]] || fail "unset ${var} not named: ${out}"
done <"${fake}/envs"
out="$(env ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE=0 bash "${runner}" 2>&1)" && fail "opt-in 0 passed"
[[ "${out}" == *"ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE"* ]] || fail "opt-in 0 not named: ${out}"

# The ledger mapping is part of the gate: a changed classification cannot
# leave the live job green with fifteen hard-coded test names.
ledger="${repo_root}/specs/live-tests.v1.yaml"
checker="${repo_root}/scripts/lib/live_postgres_readiness_results.py"
selection="$(python3 "${checker}" verify-ledger "${ledger}" "${repo_root}")" ||
  fail "clean postgres_ci ledger mapping rejected"
[[ "${selection}" == *"15 tests selected"* && "${selection}" != *"PASS"* ]] ||
  fail "ledger selection claimed a test pass before Go ran: ${selection}"
sed 's/class: postgres_ci/class: scheduled/g' "${ledger}" >"${seed_dir}/ledger-missing.yaml"
out="$(python3 "${checker}" verify-ledger "${seed_dir}/ledger-missing.yaml" "${repo_root}" 2>&1)" &&
  fail "missing postgres_ci ledger row passed"
[[ "${out}" == *"ledger files differ"* ]] || fail "ledger drift not named: ${out}"
sed 's/runner: live-postgres-readiness/runner: unrelated/g' "${ledger}" >"${seed_dir}/ledger-wrong-runner.yaml"
out="$(python3 "${checker}" verify-ledger "${seed_dir}/ledger-wrong-runner.yaml" "${repo_root}" 2>&1)" &&
  fail "wrong postgres_ci runner passed"
[[ "${out}" == *"unexpected runner"* ]] || fail "wrong runner not named: ${out}"

printf 'test-run-live-postgres-readiness-tests: PASS\n'
