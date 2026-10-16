#!/usr/bin/env bash
# Seeded-violation checks for test-run-live-postgres-readiness-tests.sh
# (#8241). Each seed breaks one fail-closed property of the results verifier
# or the runner inside a throwaway copy of scripts/ and specs/. The self-test
# run against that copy must exit nonzero, and its first failure line must
# name the broken property. The deep seed stops the verifier from checking
# one proof that is not the first proof of its package. Only the per-proof
# direct verifier check in the self-test catches that seed, so a self-test
# that checks only sampled proofs fails here. The clean-tree GREEN run is the
# self-test itself (the step before this one in the hermetic job).
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
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

fail() { printf 'test-run-live-postgres-readiness-selftest-seeds: %s\n' "$*" >&2; exit 1; }

tree="${work}/tree"
mkdir -p "${tree}"
cp -R "${repo_root}/scripts" "${repo_root}/specs" "${tree}/"
ln -s "${repo_root}/go" "${tree}/go"

# The expected names come from the verifier's list-packages, the same source
# the self-test reads, so this file carries no test-name copy:
#   first_proof    the first enrolled proof (the first one the loop checks);
#   unsorted_proof the first proof that is not the sorted-first test of its
#                  package (the first one a verifier that checks only the
#                  sorted-first name misses);
#   deep_name      the sorted-last test of the last package whose first
#                  proof is a different test (the runner never sees it).
python3 "${repo_root}/scripts/lib/live_postgres_readiness_results.py" list-packages >"${work}/packages.tsv" ||
  fail "list-packages rejected"
read -r first_proof unsorted_proof deep_pkg deep_name < <(python3 - "${work}/packages.tsv" <<'PY'
import sys

rows = []
with open(sys.argv[1], encoding="utf-8") as listing:
    for line in listing:
        package, _pattern, names = line.rstrip("\n").split("\t")
        rows.append((package, names.split()))
first = rows[0][1][0]
unsorted = next(name for _, names in rows for name in names if name != min(names))
deep_package, deep_names = [row for row in rows if max(row[1]) != row[1][0]][-1]
print(first, unsorted, deep_package, max(deep_names))
PY
)
[[ -n "${deep_name:-}" ]] || fail "no package has a proof the runner check does not reach"

# apply_seed restores the clean verifier and runner in the tree, then makes
# one exact source replacement. A seed whose anchor is gone fails here, so a
# drifted source cannot turn a seed into a no-op.
apply_seed() {
  cp "${repo_root}/${events_py}" "${tree}/${events_py}"
  cp "${repo_root}/${runner_sh}" "${tree}/${runner_sh}"
  python3 - "${tree}/$1" "$2" "$3" <<'PY' || fail "seed anchor missing in $1: $2"
import pathlib
import sys

path, old, new = pathlib.Path(sys.argv[1]), sys.argv[2], sys.argv[3]
text = path.read_text(encoding="utf-8")
if text.count(old) != 1:
    sys.exit(1)
path.write_text(text.replace(old, new), encoding="utf-8")
PY
}

# expect_failure runs the self-test in the seeded tree and requires a nonzero
# exit whose first failure line is the wanted text (or starts with it, for a
# message that carries the runner output).
expect_failure() {
  local seed="$1" want="$2" match="${3:-exact}" out line
  out="$(bash "${tree}/scripts/${selftest}" 2>&1)" && fail "seed ${seed}: self-test passed"
  line="$(printf '%s\n' "${out}" | rg -m1 -F -- "${prefix}" || true)"
  line="${line#"${prefix}"}"
  if [[ "${match}" == prefix ]]; then
    [[ "${line}" == "${want}"* ]] || fail "seed ${seed}: wanted '${want}...', got '${line}'"
  else
    [[ "${line}" == "${want}" ]] || fail "seed ${seed}: wanted '${want}', got '${line}'"
  fi
  printf 'seed %s: caught (%s)\n' "${seed}" "${line:0:120}"
}

apply_seed "${events_py}" '        if action != "pass":
' '        if action not in ("pass", "skip"):
'
expect_failure skip-as-pass "${first_proof} skip event passed"

apply_seed "${events_py}" '        terminal = terminals[name]
' '        terminal = terminals[name]
        if runs[name] == 0 and not terminal:
            continue
'
expect_failure missing-ignored "missing ${first_proof} events passed"

apply_seed "${events_py}" '    for name in sorted(expected_tests):
' '    for name in sorted(expected_tests)[:1]:
'
expect_failure first-name-only "${unsorted_proof} skip event passed"

apply_seed "${events_py}" '        print(f"{name}: {action.upper()} elapsed={elapsed:.3f}s")
' '        print(f"test {action.upper()} elapsed={elapsed:.3f}s")
'
expect_failure unnamed "${first_proof} skip not named: " prefix

apply_seed "${runner_sh}" '  elif [[ "${results_status}" -ne 0 ]]; then
' '  elif false; then
'
expect_failure runner-ignores-results "${first_proof} skip event passed the runner"

apply_seed "${events_py}" '    for name in sorted(expected_tests):
' "    for name in (sorted(expected_tests)[:-1] if package == \"${deep_pkg}\" else sorted(expected_tests)):
"
expect_failure deep-proof "${deep_name} skip event passed"

printf 'test-run-live-postgres-readiness-selftest-seeds: PASS\n'
