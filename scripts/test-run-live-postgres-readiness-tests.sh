#!/usr/bin/env bash
# Hermetic RED/GREEN checks for the dedicated PostgreSQL readiness runner.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
runner="${repo_root}/scripts/run-live-postgres-readiness-tests.sh"
seed_dir="$(mktemp -d)"
trap 'rm -rf "${seed_dir}"' EXIT

fail() { printf 'test-run-live-postgres-readiness-tests: %s\n' "$*" >&2; exit 1; }

mkdir -p "${seed_dir}/bin"
cat >"${seed_dir}/bin/go" <<'EOF'
#!/usr/bin/env bash
[[ "$*" == *"-json"* ]] || { echo 'missing -json' >&2; exit 9; }
[[ "$*" == *"-count=1"* ]] || { echo 'missing -count=1' >&2; exit 9; }
[[ "$*" == *"./internal/query/supply/chain/impact"* ]] || { echo 'wrong package' >&2; exit 9; }
EOF
cat >>"${seed_dir}/bin/go" <<'EOF'
[[ "$*" == *"TestSupplyChainImpactReadinessPackageManifestRepoScopeQueryPlanLive"* ]] || { echo 'missing package-manifest test' >&2; exit 9; }
[[ "$*" == *"TestSupplyChainImpactReadinessRepoArmScopeLive"* ]] || { echo 'missing repo-arm test' >&2; exit 9; }
[[ "$*" == *"TestSupplyChainImpactReadinessScanTierQueryPlanLive"* ]] || { echo 'missing scan-tier plan test' >&2; exit 9; }
EOF
cat >>"${seed_dir}/bin/go" <<'EOF'
[[ "$*" == *"TestSupplyChainImpactReadinessScanTierOSPackageCountDoesNotFanOutLive"* ]] || { echo 'missing fan-out test' >&2; exit 9; }
[[ "${ESHU_PACKAGE_MANIFEST_REPO_SCOPE_EXPLAIN_PROOF_DSN:-}" == "${ESHU_EXPECTED_DSN:-}" ]] || { echo 'wrong manifest DSN' >&2; exit 9; }
[[ "${ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DSN:-}" == "${ESHU_EXPECTED_DSN:-}" ]] || { echo 'wrong scan-tier DSN' >&2; exit 9; }
EOF
cat >>"${seed_dir}/bin/go" <<'EOF'
[[ "$*" == *"TestSupplyChainImpactReadinessPackageConsumptionScopeLive"* ]] || { echo 'missing package-consumption test' >&2; exit 9; }
[[ "${ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN:-}" == "${ESHU_EXPECTED_DSN:-}" ]] || { echo 'wrong package-consumption DSN' >&2; exit 9; }
[[ "${ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE:-}" == "1" ]] || { echo 'missing package-consumption opt-in' >&2; exit 9; }
EOF
cat >>"${seed_dir}/bin/go" <<'EOF'
[[ "$*" == *"TestSupplyChainImpactReadinessMutableRefIncludesEveryCurrentDigestLive"* ]] || { echo 'missing mutable-ref test' >&2; exit 9; }
[[ "${ESHU_READINESS_CONTAINER_IDENTITY_PROOF_DSN:-}" == "${ESHU_EXPECTED_DSN:-}" ]] || { echo 'wrong mutable-ref DSN' >&2; exit 9; }
[[ "${ESHU_READINESS_CONTAINER_IDENTITY_PROOF_DISPOSABLE:-}" == "1" ]] || { echo 'missing mutable-ref opt-in' >&2; exit 9; }
EOF
cat >>"${seed_dir}/bin/go" <<'EOF'
[[ "${ESHU_PACKAGE_MANIFEST_REPO_SCOPE_EXPLAIN_PROOF_DISPOSABLE:-}" == "1" ]] || { echo 'missing manifest opt-in' >&2; exit 9; }
[[ "${ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DISPOSABLE:-}" == "1" ]] || { echo 'missing scan-tier opt-in' >&2; exit 9; }
[[ -n "${ESHU_FAKE_GO_JSON:-}" ]] || exit 9
cp "${ESHU_FAKE_GO_JSON}" /dev/stdout
exit "${ESHU_FAKE_GO_EXIT:-0}"
EOF
chmod +x "${seed_dir}/bin/go"

export ESHU_EXPECTED_DSN='postgres://postgres:local-test@127.0.0.1:15432/postgres?sslmode=disable'
export ESHU_PACKAGE_MANIFEST_REPO_SCOPE_EXPLAIN_PROOF_DSN="${ESHU_EXPECTED_DSN}"
export ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DSN="${ESHU_EXPECTED_DSN}"
export ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN="${ESHU_EXPECTED_DSN}"
export ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE=1
export ESHU_READINESS_CONTAINER_IDENTITY_PROOF_DSN="${ESHU_EXPECTED_DSN}"
export ESHU_READINESS_CONTAINER_IDENTITY_PROOF_DISPOSABLE=1
export ESHU_PACKAGE_MANIFEST_REPO_SCOPE_EXPLAIN_PROOF_DISPOSABLE=1
export ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DISPOSABLE=1
export PATH="${seed_dir}/bin:${PATH}"
export ESHU_FAKE_GO_JSON="${seed_dir}/events.jsonl"

names=(
  TestSupplyChainImpactReadinessPackageManifestRepoScopeQueryPlanLive
  TestSupplyChainImpactReadinessRepoArmScopeLive
  TestSupplyChainImpactReadinessScanTierQueryPlanLive
  TestSupplyChainImpactReadinessScanTierOSPackageCountDoesNotFanOutLive
  TestSupplyChainImpactReadinessPackageConsumptionScopeLive
  TestSupplyChainImpactReadinessMutableRefIncludesEveryCurrentDigestLive
)

write_events() {
  : >"${ESHU_FAKE_GO_JSON}"
  local name
  for name in "${names[@]}"; do
    printf '{"Action":"run","Test":"%s"}\n' "$name" >>"${ESHU_FAKE_GO_JSON}"
    printf '{"Action":"pass","Test":"%s","Elapsed":0.25}\n' "$name" >>"${ESHU_FAKE_GO_JSON}"
  done
  printf '{"Action":"pass","Package":"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact","Elapsed":1.0}\n' >>"${ESHU_FAKE_GO_JSON}"
}

run_runner() { bash "${runner}" 2>&1; }

write_events
out="$(run_runner)" || fail "six PASS events rejected: ${out}"
[[ "${out}" == *"6/6 PASS"* ]] || fail "pass summary missing: ${out}"
[[ "${out}" == *"suite_elapsed="* ]] || fail "suite timing missing: ${out}"

# A successful go test exit is insufficient when a selected test skips.
write_events
sed -i.bak 's/"Action":"pass","Test":"TestSupplyChainImpactReadinessRepoArmScopeLive"/"Action":"skip","Test":"TestSupplyChainImpactReadinessRepoArmScopeLive"/' "${ESHU_FAKE_GO_JSON}"
out="$(run_runner)" && fail "SKIP event passed"
[[ "${out}" == *"SKIP"* ]] || fail "SKIP failure not named: ${out}"

# Likewise, a stale -run expression must not allow zero or missing tests.
printf '{"Action":"pass","Package":"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact","Elapsed":0.1}\n' >"${ESHU_FAKE_GO_JSON}"
out="$(run_runner)" && fail "zero matched tests passed"
[[ "${out}" == *"missing"* ]] || fail "zero-test failure not named: ${out}"

write_events
sed -i.bak '/"Action":"pass","Test":"TestSupplyChainImpactReadinessScanTierQueryPlanLive"/d' "${ESHU_FAKE_GO_JSON}"
out="$(run_runner)" && fail "missing terminal event passed"
[[ "${out}" == *"missing"* ]] || fail "missing-event failure not named: ${out}"

write_events
sed -i.bak 's/"Action":"pass","Test":"TestSupplyChainImpactReadinessScanTierQueryPlanLive"/"Action":"fail","Test":"TestSupplyChainImpactReadinessScanTierQueryPlanLive"/' "${ESHU_FAKE_GO_JSON}"
out="$(run_runner)" && fail "FAIL event passed"
[[ "${out}" == *"FAIL"* ]] || fail "FAIL event not named: ${out}"

write_events
ESHU_FAKE_GO_EXIT=1 out="$(ESHU_FAKE_GO_EXIT=1 run_runner)" && fail "go nonzero exit passed"
[[ "${out}" == *"go test exited 1"* ]] || fail "go exit not reported: ${out}"

out="$(env -u ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DSN bash "${runner}" 2>&1)" && fail "unset DSN passed"
[[ "${out}" == *"ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DSN"* ]] || fail "unset DSN not named: ${out}"

out="$(env -u ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN bash "${runner}" 2>&1)" && fail "unset package-consumption DSN passed"
[[ "${out}" == *"ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN"* ]] || fail "unset package-consumption DSN not named: ${out}"

out="$(env ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE=0 bash "${runner}" 2>&1)" && fail "missing package-consumption opt-in passed"
[[ "${out}" == *"ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE"* ]] || fail "missing package-consumption opt-in not named: ${out}"

out="$(env -u ESHU_READINESS_CONTAINER_IDENTITY_PROOF_DSN bash "${runner}" 2>&1)" && fail "unset mutable-ref DSN passed"
[[ "${out}" == *"ESHU_READINESS_CONTAINER_IDENTITY_PROOF_DSN"* ]] || fail "unset mutable-ref DSN not named: ${out}"

out="$(env ESHU_READINESS_CONTAINER_IDENTITY_PROOF_DISPOSABLE=0 bash "${runner}" 2>&1)" && fail "missing mutable-ref opt-in passed"
[[ "${out}" == *"ESHU_READINESS_CONTAINER_IDENTITY_PROOF_DISPOSABLE"* ]] || fail "missing mutable-ref opt-in not named: ${out}"

# The new proof needs its own pass event: a skip or a missing event fails.
write_events
sed -i.bak 's/"Action":"pass","Test":"TestSupplyChainImpactReadinessMutableRefIncludesEveryCurrentDigestLive"/"Action":"skip","Test":"TestSupplyChainImpactReadinessMutableRefIncludesEveryCurrentDigestLive"/' "${ESHU_FAKE_GO_JSON}"
out="$(run_runner)" && fail "mutable-ref SKIP event passed"
[[ "${out}" == *"SKIP"* ]] || fail "mutable-ref SKIP not named: ${out}"
write_events
sed -i.bak '/"Test":"TestSupplyChainImpactReadinessMutableRefIncludesEveryCurrentDigestLive"/d' "${ESHU_FAKE_GO_JSON}"
out="$(run_runner)" && fail "missing mutable-ref events passed"
[[ "${out}" == *"missing"* ]] || fail "missing mutable-ref events not named: ${out}"

out="$(env ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DISPOSABLE=0 bash "${runner}" 2>&1)" && fail "missing opt-in passed"
[[ "${out}" == *"ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DISPOSABLE"* ]] || fail "missing opt-in not named: ${out}"

# The ledger mapping is part of the gate: a changed classification cannot
# leave the live job green with six hard-coded test names.
ledger="${repo_root}/specs/live-tests.v1.yaml"
checker="${repo_root}/scripts/lib/live_postgres_readiness_results.py"
selection="$(python3 "${checker}" verify-ledger "${ledger}" "${repo_root}")" ||
  fail "clean postgres_ci ledger mapping rejected"
[[ "${selection}" == *"6 tests selected"* && "${selection}" != *"PASS"* ]] ||
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
