#!/usr/bin/env bash
# Seeded-violation checks for test-run-live-postgres-readiness-tests.sh
# (#8241). Each seed breaks one fail-closed property of the results verifier
# or the runner inside a throwaway copy of scripts/ and specs/. The self-test
# run against that copy must exit nonzero, and its first failure line must
# name the broken property. The deep seed stops the verifier from checking
# one proof deep in the list; the per-proof direct check must reach it. The
# runner-truncates seed hides most proof names of a large package from the
# runner output; the all-names runner check must notice. The clean-tree GREEN
# run is the self-test itself (the step before this one in the hermetic job).
#
# Each seed runs in a private copy of the pristine tree, so the seeds share
# only read-only inputs and run in the bounded case pool (#9629, see
# lib/test-run-live-postgres-readiness-seed-pool.sh). Every seed is checked in
# the order below after the drain, so the report does not depend on the pool
# width or on which seed finished first. LIVE_READINESS_SEED_JOBS sets the
# width (default: the case pool's, the CPU count capped at 4). The case pool's
# own self-test runs here once, beside the seeds.
# shellcheck disable=SC2034,SC2154  # the sourced seed pool reads prefix and sets seed_root.
set -euo pipefail

command -v rg >/dev/null 2>&1 || {
  printf 'test-run-live-postgres-readiness-selftest-seeds: requires rg\n' >&2
  exit 1
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
selftest="test-run-live-postgres-readiness-tests.sh"
prefix="test-run-live-postgres-readiness-tests: "
events_py="scripts/lib/live_postgres_readiness_events.py"
runner_sh="scripts/run-live-postgres-readiness-tests.sh"
# tail_keep is how many verifier output lines the runner-truncates seed keeps.
tail_keep=60
work="$(mktemp -d)"
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "${work}"' EXIT
step_started="$(date +%s)"

fail() { printf 'test-run-live-postgres-readiness-selftest-seeds: %s\n' "$*" >&2; exit 1; }

# The case pool self-test is mostly sleeps: it runs beside the seeds. The
# pool's drain waits for every child of this shell, this one too, and reaps
# it, so its status goes to a file instead of a later wait.
(
  rc=0
  bash "${repo_root}/scripts/test-live-postgres-readiness-pool.sh" >"${work}/pool-selftest.out" 2>&1 || rc=$?
  printf '%s\n' "${rc}" >"${work}/pool-selftest.rc"
) &

bash "${repo_root}/scripts/test-live-postgres-readiness-seed-pool.sh" >/dev/null || fail "seed pool self-test failed"

pristine="${work}/tree"
mkdir -p "${pristine}"
cp -R "${repo_root}/scripts" "${repo_root}/specs" "${pristine}/"
ln -s "${repo_root}/go" "${pristine}/go"
seed_work="${work}/seeds"
mkdir -p "${seed_work}"
# shellcheck source=lib/test-run-live-postgres-readiness-pool.sh
source "${repo_root}/scripts/lib/test-run-live-postgres-readiness-pool.sh"
# shellcheck source=lib/test-run-live-postgres-readiness-seed-pool.sh
source "${repo_root}/scripts/lib/test-run-live-postgres-readiness-seed-pool.sh"

# The expected names come from the verifier's list-packages, the same source
# the self-test reads, so this file carries no test-name copy:
#   first_proof    the first enrolled proof (the first one the loop checks);
#   unsorted_proof the first proof that is not the sorted-first test of its
#                  package (the first one a verifier that checks only the
#                  sorted-first name misses);
#   deep_name      the sorted-last test of the last package whose first
#                  proof is a different test (deep in the direct phase);
#   first_pkg      the first package (the first runner-phase check);
#   big_pkg        the first package with more proofs than the runner
#                  output keeps under the runner-truncates seed;
#   hidden_name    the first proof of big_pkg, in list order, that the
#                  truncated output drops.
python3 "${repo_root}/scripts/lib/live_postgres_readiness_results.py" list-packages >"${work}/packages.tsv" ||
  fail "list-packages rejected"
read -r first_proof unsorted_proof deep_pkg deep_name first_pkg big_pkg hidden_name < <(
  python3 - "${work}/packages.tsv" "${tail_keep}" <<'PY'
import sys
rows = [(p, n.split()) for p, _, n in (x.rstrip("\n").split("\t") for x in open(sys.argv[1]))]
keep = int(sys.argv[2])
deep = [r for r in rows if max(r[1]) != r[1][0]][-1]
big = next(r for r in rows if len(r[1]) > keep)
shown = set(sorted(big[1])[-keep:])
print(rows[0][1][0], next(n for _, ns in rows for n in ns if n != min(ns)),
      deep[0], max(deep[1]), rows[0][0], big[0], next(n for n in big[1] if n not in shown))
PY
)
[[ -n "${hidden_name:-}" ]] || fail "package listing yielded no seed targets"

# apply_seed ID FILE OLD NEW makes seed ID's private tree and one exact source
# replacement in it. A seed whose anchor is gone fails here, so a drifted
# source cannot turn a seed into a no-op.
apply_seed() {
  seed_tree "$1"
  replace_once "${seed_root}/$2" "$3" "$4"
}

replace_once() {
  python3 - "$1" "$2" "$3" <<'PY' || fail "seed anchor missing in $1: $2"
import pathlib
import sys

path, old, new = pathlib.Path(sys.argv[1]), sys.argv[2], sys.argv[3]
text = path.read_text(encoding="utf-8")
if text.count(old) != 1:
    sys.exit(1)
path.write_text(text.replace(old, new), encoding="utf-8")
PY
}

# Only this mutant needs the large package first. The clean self-test and all
# other mutants keep the verifier's package order. It edits the self-test in
# seed_root, the tree seed_tree made last.
prioritize_truncation_target() {
  local target="$1" pkg seen=0 quoted_target replacement
  while IFS=$'\t' read -r pkg _; do
    [[ "${pkg}" == "${target}" ]] && seen=$((seen + 1))
  done <"${work}/packages.tsv"
  [[ "${seen}" -eq 1 ]] || fail "runner-truncates target package missing or duplicate: ${target}"
  printf -v quoted_target '%q' "${target}"
  replacement="target_pkg=${quoted_target}
priority_packages=(\"\${target_pkg}\")
seen_target=0
for pkg in \"\${packages[@]}\"; do
  if [[ \"\${pkg}\" == \"\${target_pkg}\" ]]; then
    seen_target=\$((seen_target + 1))
  else
    priority_packages+=(\"\${pkg}\")
  fi
done
[[ \"\${seen_target}\" -eq 1 ]] || fail \"runner-truncates target absent from self-test: \${target_pkg}\"
packages=(\"\${priority_packages[@]}\")
check_each_proof
"
  replace_once "${seed_root}/scripts/${selftest}" $'check_each_proof\n' "${replacement}"
}

# Mutants activate only when all package streams carry the same batched fault.
# The old one-package-at-a-time checks cannot catch them; the grouped checks
# must retain the middle package's verifier status, diagnostic, and callback.
read -r middle_pkg < <(python3 - "${work}/packages.tsv" <<'PY'
import pathlib
import sys

packages = [line.split("\t", 1)[0] for line in pathlib.Path(sys.argv[1]).read_text().splitlines()]
print(packages[len(packages) // 2])
PY
)
apply_batch_mutant() {
  local kind="$1" target
  seed_tree "batch-${kind}"
  target="${seed_root}/scripts/${selftest}"
  python3 "${repo_root}/scripts/lib/live_postgres_readiness_batch_mutant.py" \
    mutate-target "${target}" "${kind}" || fail "batch mutant anchor missing"
}

# Source drift or a stale package inventory must fail before the costly test.
seed_tree guard-anchor
replace_once "${seed_root}/scripts/${selftest}" $'check_each_proof\n' $'missing_proof_anchor\n'
out="$(prioritize_truncation_target "${big_pkg}" 2>&1)" && fail "missing priority anchor passed"
[[ "${out}" == *"seed anchor missing"* ]] || fail "missing priority anchor not named: ${out}"
seed_tree guard-package
out="$(prioritize_truncation_target './package/not-enrolled' 2>&1)" && fail "missing priority package passed"
[[ "${out}" == *"target package missing or duplicate"* ]] || fail "missing priority package not named: ${out}"
printf 'seed runner-truncates setup guards: caught missing anchor and package\n'

# The batch mutants read SEED_MUTANT_KIND, so each seed carries its own.
apply_batch_mutant status
seed_queue batch-status \
  "batched terminal did not report verifier failure for ${middle_pkg}: " prefix "" SEED_MUTANT_KIND=status
apply_batch_mutant diagnostic
seed_queue batch-diagnostic \
  "batched terminal package terminal of ${middle_pkg} not named: " prefix "" SEED_MUTANT_KIND=diagnostic
apply_batch_mutant callback
seed_queue batch-callback \
  "batched zero callback count for ${middle_pkg}: 0" exact "" SEED_MUTANT_KIND=callback

apply_seed skip-as-pass "${events_py}" '        if action != "pass":
' '        if action not in ("pass", "skip"):
'
seed_queue skip-as-pass "${first_proof} skip event passed"

apply_seed missing-ignored "${events_py}" '        terminal = terminals[name]
' '        terminal = terminals[name]
        if runs[name] == 0 and not terminal:
            continue
'
seed_queue missing-ignored "missing ${first_proof} events passed"

apply_seed first-name-only "${events_py}" '    for name in sorted(expected_tests):
' '    for name in sorted(expected_tests)[:1]:
'
seed_queue first-name-only "${unsorted_proof} skip event passed"

apply_seed unnamed "${events_py}" '        print(f"{name}: {action.upper()} elapsed={elapsed:.3f}s")
' '        print(f"test {action.upper()} elapsed={elapsed:.3f}s")
'
seed_queue unnamed "${first_proof} skip not named: " prefix

apply_seed runner-ignores-results "${runner_sh}" '  elif [[ "${results_status}" -ne 0 ]]; then
' '  elif false; then
'
seed_queue runner-ignores-results "${first_pkg} skip events passed the runner" exact heavy

apply_seed runner-truncates "${runner_sh}" ' </dev/null; then
    results_status=0
' " </dev/null | if [[ \"\${shard}\" == all ]]; then tail -n ${tail_keep}; else cat; fi; then
    results_status=0
"
prioritize_truncation_target "${big_pkg}"
seed_queue runner-truncates "${big_pkg} skip events: ${hidden_name} not named by the runner: " prefix heavy

apply_seed fail-as-pass "${events_py}" '        if action != "pass":
' '        if action not in ("pass", "fail"):
'
seed_queue fail-as-pass "${first_proof} fail event passed"

apply_seed deep-proof "${events_py}" '    for name in sorted(expected_tests):
' "    for name in (sorted(expected_tests)[:-1] if package == \"${deep_pkg}\" else sorted(expected_tests)):
"
seed_queue deep-proof "${deep_name} skip event passed"

seed_run_all
seed_check_all
wait || true
[[ "$(cat "${work}/pool-selftest.rc" 2>/dev/null)" == 0 ]] ||
  fail "case pool self-test failed: $(cat "${work}/pool-selftest.out")"
seed_report_timing "$(($(date +%s) - step_started))"
printf 'test-run-live-postgres-readiness-selftest-seeds: PASS\n'
