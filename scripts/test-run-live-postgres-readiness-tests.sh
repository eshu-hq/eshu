#!/usr/bin/env bash
# Hermetic RED/GREEN checks for the dedicated PostgreSQL readiness runner:
# a fake go supplies the per-package event streams of every enrolled proof.
# The package list and test names come from the results verifier's PACKAGES
# through list-packages, the same source the runner reads, so this file
# carries no copy of them.
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
while [[ $# -gt 0 ]]; do [[ "$1" == -run ]] && pattern="$2"; shift; done
[[ -n "${pattern:-}" ]] || { echo 'missing -run' >&2; exit 9; }
printf '%s\n' "${pkg}" >>"${ESHU_FAKE_DIR}/calls"
while read -r name; do
  [[ "${name}" =~ ${pattern} ]] || { echo "pattern misses test ${name}" >&2; exit 9; }
done <"${ESHU_FAKE_DIR}/${key}.names"
[[ "TestZzzDecoyNotEnrolled" =~ ${pattern} ]] && { echo 'pattern is not anchored' >&2; exit 9; }
EOF
cat >>"${seed_dir}/bin/go" <<'EOF'
while read -r var; do
  case "${var}" in
    *_DSN) want="${ESHU_EXPECTED_DSN:-}" ;;
    ESHU_NEO4J_URI) want="${ESHU_EXPECTED_NEO4J_URI:-}" ;;
    ESHU_NEO4J_USERNAME) want="${ESHU_EXPECTED_NEO4J_USERNAME:-}" ;;
    ESHU_NEO4J_PASSWORD) want="${ESHU_EXPECTED_NEO4J_PASSWORD:-}" ;;
    ESHU_GRAPH_BACKEND) want=neo4j ;;
    *) want=1 ;;
  esac
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
  ESHU_RUNTIME_ENVIRONMENT_EVIDENCE_POSTGRES ESHU_DEAD_CODE_INCOMING_BOUND_PROOF \
  ESHU_GENERATION_RETENTION_PROOF ESHU_STATUS_TERRAFORM_SELECTION_PROOF \
  ESHU_DEFERRED_PARTITION_PROOF ESHU_TARGETED_MAINTENANCE_PROOF \
  ESHU_STATUS_SUMMARY_PROOF ESHU_ADMIN_REOPEN_PROOF ESHU_FLUX_EVIDENCE_IDENTITY_PROOF \
  ESHU_REACHABILITY_EDGES_SCOPE_PROOF \
  ESHU_DRIFTED_BUCKET_SKIP_PROOF \
  ESHU_CONTAINER_IMAGE_IDENTITY_EPOCH_PROOF \
  ESHU_PROJECTOR_SUPERSESSION_PROOF \
  ESHU_PREFETCH_BATCH_PLAN_PROOF; do
  printf '%s_DSN\n%s_DISPOSABLE\n' "${var}" "${var}" >>"${fake}/envs"
done
for var in ESHU_NEO4J_URI ESHU_NEO4J_USERNAME ESHU_NEO4J_PASSWORD \
  ESHU_GRAPH_BACKEND; do
  printf '%s\n' "${var}" >>"${fake}/envs"
done

export ESHU_FAKE_DIR="${fake}"
export ESHU_EXPECTED_DSN='postgres://postgres:local-test@127.0.0.1:15432/postgres?sslmode=disable'
export ESHU_EXPECTED_NEO4J_URI='neo4j://127.0.0.1:17687'
export ESHU_EXPECTED_NEO4J_USERNAME='neo4j'
export ESHU_EXPECTED_NEO4J_PASSWORD='local-test-neo4j'
while read -r var; do
  case "${var}" in
    *_DSN) export "${var}=${ESHU_EXPECTED_DSN}" ;;
    ESHU_NEO4J_URI) export "${var}=${ESHU_EXPECTED_NEO4J_URI}" ;;
    ESHU_NEO4J_USERNAME) export "${var}=${ESHU_EXPECTED_NEO4J_USERNAME}" ;;
    ESHU_NEO4J_PASSWORD) export "${var}=${ESHU_EXPECTED_NEO4J_PASSWORD}" ;;
    ESHU_GRAPH_BACKEND) export "${var}=neo4j" ;;
    *) export "${var}=1" ;;
  esac
done <"${fake}/envs"
export PATH="${seed_dir}/bin:${PATH}"

checker="${repo_root}/scripts/lib/live_postgres_readiness_results.py"
packages=()
proofs=()
# load_proofs fills packages and "package|test" entries, in the runner's order,
# from the list-packages output of the given verifier.
load_proofs() {
  local pkg pattern names name
  packages=()
  proofs=()
  while IFS=$'\t' read -r pkg pattern names; do
    packages+=("${pkg}")
    for name in ${names}; do proofs+=("${pkg}|${name}"); done
  done < <(python3 "$1" list-packages)
  [[ "${#packages[@]}" -gt 0 && "${#proofs[@]}" -gt 0 ]] || fail "list-packages produced no proofs from $1"
}
load_proofs "${checker}"
first_pkg="${packages[0]}"
last_pkg="${packages[$((${#packages[@]} - 1))]}"
total="${#proofs[@]}"
first_proof="${proofs[0]##*|}"
last_proof="${proofs[$((total - 1))]##*|}"
count_in() { local e n=0; for e in "${proofs[@]}"; do [[ "${e%%|*}" == "$1" ]] && n=$((n + 1)); done; printf '%s' "${n}"; }

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
  for pkg in "${packages[@]}"; do
    printf '{"Action":"pass","Package":"github.com/eshu-hq/eshu/go/%s","Elapsed":1.0}\n' \
      "${pkg#./}" >>"$(events_of "${pkg}")"
  done
}

run_runner() { bash "${runner}" 2>&1; }

write_events
out="$(run_runner)" || fail "${total} PASS events rejected: ${out}"
[[ "${out}" == *"${total}/${total} PASS"* ]] || fail "pass summary missing: ${out}"
[[ "${out}" == *"suite_elapsed="* ]] || fail "suite timing missing: ${out}"
for pkg in "${packages[@]}"; do
  n="$(count_in "${pkg}")"
  [[ "${out}" == *"${pkg} ${n}/${n} PASS"* ]] || fail "per-package summary of ${pkg} missing: ${out}"
done

# Every enrolled proof needs its own pass event in its own package: a skip, a
# failure, or a missing event for any one of them fails the run.
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
for pkg in "${packages[@]}"; do
  write_events
  printf '{"Action":"pass","Package":"github.com/eshu-hq/eshu/go/%s","Elapsed":0.1}\n' "${pkg#./}" >"$(events_of "${pkg}")"
  out="$(run_runner)" && fail "zero matched tests in ${pkg} passed"
  [[ "${out}" == *"missing"* ]] || fail "zero-test failure in ${pkg} not named: ${out}"
done

# A failed package terminal fails the run even when every test passed.
for pkg in "${packages[@]}"; do
  write_events
  sed -i.bak 's/"Action":"pass","Package"/"Action":"fail","Package"/' "$(events_of "${pkg}")"
  out="$(run_runner)" && fail "failed package terminal of ${pkg} passed"
  [[ "${out}" == *"package terminal (${pkg})"* ]] || fail "package terminal of ${pkg} not named: ${out}"
done

# A package that exits nonzero fails the run and names that package; the
# other packages still run so one CI run reports every broken proof.
for pkg in "${packages[@]}"; do
  write_events
  printf '1\n' >"${fake}/$(pkg_key "${pkg}").exit"
  out="$(run_runner)" && fail "go nonzero exit in ${pkg} passed"
  [[ "${out}" == *"${pkg}: go test exited 1"* ]] || fail "go exit in ${pkg} not reported: ${out}"
done
write_events
sed -i.bak "s/\"Action\":\"pass\",\"Test\":\"${first_proof}\"/\"Action\":\"fail\",\"Test\":\"${first_proof}\"/" "$(events_of "${first_pkg}")"
sed -i.bak "s/\"Action\":\"pass\",\"Test\":\"${last_proof}\"/\"Action\":\"skip\",\"Test\":\"${last_proof}\"/" "$(events_of "${last_pkg}")"
out="$(run_runner)" && fail "failures in two packages passed"
[[ "${out}" == *"${first_proof}: FAIL"* && "${out}" == *"${last_proof}: SKIP"* ]] ||
  fail "an earlier package failure hid a later package failure: ${out}"

# A failing test prints the TAIL of its output stream: a long test's early
# progress lines must not crowd out the assertion (#7814).
write_events
sed -i.bak "/\"Test\":\"${first_proof}\"/d" "$(events_of "${first_pkg}")"
{
  printf '{"Action":"run","Test":"%s"}\n' "${first_proof}"
  for i in $(seq 0 19); do
    printf '{"Action":"output","Test":"%s","Output":"early line %d\\n"}\n' "${first_proof}" "${i}"
  done
  printf '{"Action":"output","Test":"%s","Output":"--- FAIL: %s (40.38s)\\n"}\n' "${first_proof}" "${first_proof}"
  printf '{"Action":"output","Test":"%s","Output":"    seeded_test.go:99: SEEDED-FAILURE-MARKER\\n"}\n' "${first_proof}"
  printf '{"Action":"fail","Test":"%s","Elapsed":40.38}\n' "${first_proof}"
} >>"$(events_of "${first_pkg}")"
out="$(run_runner)" && fail "tailed failure passed"
[[ "${out}" == *"SEEDED-FAILURE-MARKER"* ]] || fail "failure tail not printed: ${out}"
[[ "${out}" != *"early line 0"* ]] || fail "early noise crowded out the failure: ${out}"

# A missing DSN or opt-in names the variable before any package runs. The
# Neo4j variables are exempt: without a backend the runner degrades (see
# below) instead of dying.
while read -r var; do
  out="$(env -u "${var}" bash "${runner}" 2>&1)" && fail "unset ${var} passed"
  [[ "${out}" == *"${var}"* ]] || fail "unset ${var} not named: ${out}"
done < <(grep -v -E '^(ESHU_NEO4J_|ESHU_GRAPH_BACKEND)' "${fake}/envs")
out="$(env ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE=0 bash "${runner}" 2>&1)" && fail "opt-in 0 passed"
[[ "${out}" == *"ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE"* ]] || fail "opt-in 0 not named: ${out}"

# Without a Neo4j backend the runner degrades: the graph proofs are excused
# (absence or skip passes, a failure still fails) and the summary names
# them, so a developer without a container runs every other proof. The
# excused list comes from the verifier, so this file still carries no
# test-name copy. A partial backend (URI without credentials) dies loud.
graph_tests="$(python3 "${checker}" list-neo4j-tests)" || fail "list-neo4j-tests rejected"
[[ -n "${graph_tests}" ]] || fail "list-neo4j-tests is empty, the degrade checks are vacuous"
for name in ${graph_tests}; do
  found=0
  for entry in "${proofs[@]}"; do [[ "${entry##*|}" == "${name}" ]] && found=1; done
  [[ "${found}" -eq 1 ]] || fail "neo4j test ${name} is not an enrolled proof"
done
graph_count="$(printf '%s' "${graph_tests}" | wc -w | tr -d '[:space:]')"
degraded_total="$((total - graph_count))"
cp "${fake}/envs" "${fake}/envs.full"
grep -v -E '^(ESHU_NEO4J_|ESHU_GRAPH_BACKEND)' "${fake}/envs.full" >"${fake}/envs"
unset ESHU_NEO4J_URI ESHU_NEO4J_USERNAME ESHU_NEO4J_PASSWORD ESHU_GRAPH_BACKEND
write_events
for name in ${graph_tests}; do
  for entry in "${proofs[@]}"; do
    if [[ "${entry##*|}" == "${name}" ]]; then
      sed -i.bak "/\"Test\":\"${name}\"/d" "$(events_of "${entry%%|*}")"
    fi
  done
done
out="$(run_runner)" || fail "degraded run without Neo4j env failed: ${out}"
[[ "${out}" == *"no Neo4j proof backend configured"* ]] || fail "degrade notice missing: ${out}"
for name in ${graph_tests}; do
  [[ "${out}" == *"${name}"* ]] || fail "excused ${name} not named: ${out}"
done
[[ "${out}" == *"${degraded_total}/${total} PASS"* ]] || fail "degraded summary missing: ${out}"
write_events
for name in ${graph_tests}; do
  for entry in "${proofs[@]}"; do
    if [[ "${entry##*|}" == "${name}" ]]; then
      sed -i.bak "s/\"Action\":\"pass\",\"Test\":\"${name}\"/\"Action\":\"skip\",\"Test\":\"${name}\"/" "$(events_of "${entry%%|*}")"
    fi
  done
done
out="$(run_runner)" || fail "degraded run with graph skip failed: ${out}"
write_events
first_graph="${graph_tests%% *}"
for entry in "${proofs[@]}"; do
  if [[ "${entry##*|}" == "${first_graph}" ]]; then
    sed -i.bak "s/\"Action\":\"pass\",\"Test\":\"${first_graph}\"/\"Action\":\"fail\",\"Test\":\"${first_graph}\"/" "$(events_of "${entry%%|*}")"
  fi
done
out="$(run_runner)" && fail "degraded run with graph failure passed"
[[ "${out}" == *"${first_graph}: FAIL"* ]] || fail "graph failure not named: ${out}"
export ESHU_NEO4J_URI="${ESHU_EXPECTED_NEO4J_URI}"
out="$(run_runner)" && fail "partial Neo4j env passed"
[[ "${out}" == *"ESHU_NEO4J_USERNAME"* ]] || fail "partial Neo4j env not named: ${out}"
unset ESHU_NEO4J_URI
export ESHU_GRAPH_BACKEND=neo4j
write_events
for name in ${graph_tests}; do
  for entry in "${proofs[@]}"; do
    if [[ "${entry##*|}" == "${name}" ]]; then
      sed -i.bak "/\"Test\":\"${name}\"/d" "$(events_of "${entry%%|*}")"
    fi
  done
done
out="$(run_runner)" || fail "backend set without Neo4j env failed: ${out}"
unset ESHU_GRAPH_BACKEND
cp "${fake}/envs.full" "${fake}/envs"
export ESHU_NEO4J_URI="${ESHU_EXPECTED_NEO4J_URI}"
export ESHU_NEO4J_USERNAME="${ESHU_EXPECTED_NEO4J_USERNAME}"
export ESHU_NEO4J_PASSWORD="${ESHU_EXPECTED_NEO4J_PASSWORD}"
export ESHU_GRAPH_BACKEND=neo4j
write_events

# The ledger mapping is part of the gate: a changed classification cannot
# leave the live job green with hard-coded test names.
ledger="${repo_root}/specs/live-tests.v1.yaml"
selection="$(python3 "${checker}" verify-ledger "${ledger}" "${repo_root}")" ||
  fail "clean postgres_ci ledger mapping rejected"
[[ "${selection}" == *"${total} tests selected"* && "${selection}" != *"PASS"* ]] ||
  fail "ledger selection claimed a test pass before Go ran: ${selection}"
sed 's/class: postgres_ci/class: scheduled/g' "${ledger}" >"${seed_dir}/ledger-missing.yaml"
out="$(python3 "${checker}" verify-ledger "${seed_dir}/ledger-missing.yaml" "${repo_root}" 2>&1)" &&
  fail "missing postgres_ci ledger row passed"
[[ "${out}" == *"ledger files differ"* ]] || fail "ledger drift not named: ${out}"
sed 's/runner: live-postgres-readiness/runner: unrelated/g' "${ledger}" >"${seed_dir}/ledger-wrong-runner.yaml"
out="$(python3 "${checker}" verify-ledger "${seed_dir}/ledger-wrong-runner.yaml" "${repo_root}" 2>&1)" &&
  fail "wrong postgres_ci runner passed"
[[ "${out}" == *"unexpected runner"* ]] || fail "wrong runner not named: ${out}"

# The verifier is the single source of truth for the runner's package list. A
# throwaway tree runs the real runner against a verifier copy, a ledger copy
# and the real go/ sources. First, a verifier that drops a package the ledger
# and Go sources still carry must fail the run before any go test starts (RED).
tree="${seed_dir}/tree"
mkdir -p "${tree}/scripts/lib" "${tree}/specs"
cp "${runner}" "${tree}/scripts/"
ln -s "${repo_root}/go" "${tree}/go"
tree_runner="${tree}/scripts/$(basename "${runner}")"
tree_checker="${tree}/scripts/lib/$(basename "${checker}")"
cp "${ledger}" "${tree}/specs/live-tests.v1.yaml"
# Remove the last PACKAGES entry (from its "    NAME: {" line through the
# next "    }," line) and list the test files it carried in dropped-files.
awk -v list="${seed_dir}/dropped-files" '
  { line[NR] = $0 }
  /^PACKAGES = \{/ { inpkg = 1 }
  /^EXPECTED = / { inpkg = 0 }
  inpkg && /^    [A-Z_]+: \{$/ { from = NR }
  END {
    for (i = 1; i <= NR; i++) {
      if (i >= from && !done) {
        if (line[i] ~ /^        "go\//) { split(line[i], f, "\""); print f[2] >> list }
        if (line[i] ~ /^    \},$/) done = 1
        continue
      }
      print line[i]
    }
  }
' "${checker}" >"${tree_checker}"
[[ -s "${seed_dir}/dropped-files" ]] || fail "seeded verifier dropped no package files"
load_proofs "${tree_checker}"
dropped_total="${#proofs[@]}"
dropped_packages="${#packages[@]}"
[[ "${dropped_total}" -lt "${total}" ]] || fail "seeded verifier still lists the dropped package"
[[ " ${packages[*]} " != *" ${last_pkg} "* ]] || fail "seeded verifier still lists ${last_pkg}"
load_proofs "${checker}"
write_events
: >"${fake}/calls"
runner="${tree_runner}"
out="$(run_runner)" && fail "verifier dropping ${last_pkg} while the ledger carries it passed: ${out}"
[[ "${out}" == *"ledger selection is invalid"* && "${out}" == *"ledger files differ"* ]] ||
  fail "seeded package omission failed for another reason: ${out}"
[[ ! -s "${fake}/calls" ]] || fail "go test ran before the ledger rejected the omission"

# GREEN: with the dropped package's ledger rows reclassified to match, the
# runner follows the verifier: it must not invoke the dropped package and must
# report the smaller total. A runner with its own hard-coded package list fails
# here (the fake go rejects the package it has no events for).
awk -v list="${seed_dir}/dropped-files" '
  BEGIN { while ((getline f < list) > 0) drop["  - file: " f] = 1 }
  ($0 in drop) { hit = 1 }
  hit && $0 == "    class: postgres_ci" { print "    class: scheduled"; hit = 0; next }
  { print }
' "${ledger}" >"${tree}/specs/live-tests.v1.yaml"
cmp -s "${ledger}" "${tree}/specs/live-tests.v1.yaml" && fail "seeded ledger reclassification changed nothing"
load_proofs "${tree_checker}"
write_events
: >"${fake}/calls"
out="$(run_runner)" || fail "runner did not follow the verifier package list: ${out}"
[[ "${out}" == *"${dropped_total}/${dropped_total} PASS"* ]] || fail "reduced summary missing: ${out}"
grep -qxF -- "${last_pkg}" "${fake}/calls" && fail "runner invoked ${last_pkg}, dropped from PACKAGES"
[[ "$(wc -l <"${fake}/calls" | tr -d '[:space:]')" -eq "${dropped_packages}" ]] ||
  fail "runner package invocations differ from the verifier list: $(cat "${fake}/calls")"
runner="${repo_root}/scripts/run-live-postgres-readiness-tests.sh"
load_proofs "${checker}"

# list-packages fails closed on an empty or malformed PACKAGES.
awk '/^PACKAGES = \{/ { print "PACKAGES = {}"; skip = 1; next } skip && /^\}$/ { skip = 0; next } !skip' \
  "${checker}" >"${seed_dir}/empty.py"
out="$(python3 "${seed_dir}/empty.py" list-packages 2>&1)" && fail "empty PACKAGES listed"
[[ "${out}" == *"PACKAGES is empty"* ]] || fail "empty PACKAGES not named: ${out}"
sed "s/\"${last_proof}\"/\"not a test\"/" "${checker}" >"${seed_dir}/malformed.py"
cmp -s "${checker}" "${seed_dir}/malformed.py" && fail "malformed seed changed nothing"
out="$(python3 "${seed_dir}/malformed.py" list-packages 2>&1)" && fail "malformed test name listed"
[[ "${out}" == *"malformed test"* ]] || fail "malformed test not named: ${out}"

printf 'test-run-live-postgres-readiness-tests: PASS\n'
