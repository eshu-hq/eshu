#!/usr/bin/env bash
# Hermetic checks for the seeded-violation pool (#9629) in
# scripts/lib/test-run-live-postgres-readiness-seed-pool.sh. A stub self-test
# replays the behavior planted in its own seed tree, so a shared tree, a lost
# status, or an order that follows finish time instead of declaration shows up
# as a different report. Every scenario runs at width 1 and width 4 and must
# print the same lines and exit the same way.
# shellcheck disable=SC2016,SC2034,SC2154  # stub lines are literal; the sourced pools read and set the rest.
set -euo pipefail

command -v rg >/dev/null 2>&1 || {
  printf 'test-live-postgres-readiness-seed-pool: requires rg\n' >&2
  exit 1
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
seeds_script="${repo_root}/scripts/test-run-live-postgres-readiness-selftest-seeds.sh"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

die() { printf 'test-live-postgres-readiness-seed-pool: %s\n' "$*" >&2; exit 1; }

# The stub self-test reads <tree>/scripts/seed-behavior: sleep seconds, exit
# status, then the failure text it reports under the self-test prefix.
pristine="${work}/pristine"
mkdir -p "${pristine}/scripts" "${pristine}/specs" "${work}/module"
ln -s "${work}/module" "${pristine}/go"
printf '%s\n' '#!/usr/bin/env bash' \
  'here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"' \
  '{ read -r nap; read -r rc; read -r msg; } <"${here}/seed-behavior"' \
  'sleep "${nap}"; printf "noise before\n"' \
  'printf "stub-selftest: %s\n" "${msg}" >&2; exit "${rc}"' >"${pristine}/scripts/stub.sh"

# run_scenario WIDTH SPEC... runs one seed per SPEC (id:sleep:rc:msg:want[:heavy])
# through the seed pool at WIDTH and prints its report and exit status.
run_scenario() {
  local width="$1" spec id nap rc msg want heavy
  shift
  (
    fail() { printf 'seeds: %s\n' "$*" >&2; exit 1; }
    prefix="stub-selftest: "
    selftest="stub.sh"
    seed_work="${work}/run-${width}-${RANDOM}"
    # shellcheck source=lib/test-run-live-postgres-readiness-pool.sh
    source "${repo_root}/scripts/lib/test-run-live-postgres-readiness-pool.sh"
    # shellcheck source=lib/test-run-live-postgres-readiness-seed-pool.sh
    source "${repo_root}/scripts/lib/test-run-live-postgres-readiness-seed-pool.sh"
    for spec in "$@"; do
      IFS=: read -r id nap rc msg want heavy <<<"${spec}"
      seed_tree "${id}"
      printf '%s\n%s\n%s\n' "${nap}" "${rc}" "${msg}" >"${seed_root}/scripts/seed-behavior"
      seed_queue "${id}" "${want}" exact "${heavy}"
    done
    LIVE_READINESS_SEED_JOBS="${width}" seed_run_all
    [[ "${seed_jobs}" == "${width}" ]] || fail "width ${seed_jobs}, want ${width}"
    seed_check_all
  ) 2>&1 && printf 'exit=0\n' || printf 'exit=%s\n' "$?"
}

# same_both_widths NAME SPEC... requires width 1 and width 4 to agree and
# prints the shared report for the caller's own expectations.
same_both_widths() {
  local name="$1" one four
  shift
  one="$(run_scenario 1 "$@")"
  four="$(run_scenario 4 "$@")"
  [[ "${one}" == "${four}" ]] || die "${name}: width 1 and 4 differ:
--- width 1
${one}
--- width 4
${four}"
  printf '%s' "${one}"
}

# All caught: the heavy seed starts first and the slow seed finishes last,
# yet the report keeps declaration order and each seed's own message.
out="$(same_both_widths all-caught \
  's1:0.3:1:one:one' 's2:0:1:two:two' 's3:0.6:1:three:three:heavy' 's4:0:1:four:four')"
want=$'seed s1: caught (one)\nseed s2: caught (two)\nseed s3: caught (three)\nseed s4: caught (four)\nexit=0'
[[ "${out}" == "${want}" ]] || die "all-caught report:
${out}"

# First failure is the first declared seed, not the first finished one: s2
# passes after a delay while s3 reports the wrong text at once.
out="$(same_both_widths first-declared \
  's1:0:1:one:one' 's2:0.5:0:fine:two' 's3:0:1:other:three' 's4:0:1:four:four')"
[[ "${out}" == *"seeds: seed s2: self-test passed"*"exit=1" ]] || die "first-declared report:
${out}"
[[ "${out}" != *"s3"* ]] || die "first-declared reported a later seed: ${out}"

# A wrong first failure line is named with the wanted and the actual text.
out="$(same_both_widths wrong-line 's1:0:1:actual:expected')"
[[ "${out}" == *"seed s1: wanted 'expected', got 'actual'"*"exit=1" ]] || die "wrong-line report:
${out}"

# The real seeds script runs its seeds through this pool and checks them after
# the drain; inline expect_failure runs would serialize them again.
rg -q '^seed_run_all$' "${seeds_script}" || die "seeds script does not run seed_run_all"
rg -q '^seed_check_all$' "${seeds_script}" || die "seeds script does not run seed_check_all"
if rg -q 'expect_failure ' "${seeds_script}"; then die "seeds script still runs seeds inline"; fi

printf 'test-live-postgres-readiness-seed-pool: PASS\n'
