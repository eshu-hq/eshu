#!/usr/bin/env bash
# RED/GREEN checks for the readiness self-test case pool (#9582):
# scripts/lib/test-run-live-postgres-readiness-pool.sh runs seeded runner cases
# concurrently, so it must stay fail closed. Each scenario runs in its own
# subshell with stand-in cases (sleeps and exits, no runner):
#
#   width    never more than the pool width at once, and more than one at once
#   order    statuses and outputs are read back per case, in start order
#   planted  a case that passes while the others still run is caught by name
#   killed   a case that dies before it writes a status fails the drain
#   hung     a hung case holds the drain; it never reports a pass
#
# The seeded section breaks the pool in throwaway copies and requires the
# scenarios to fail, so a pool that drops a status, skips the wait, or ignores
# its width does not pass.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
real="${root}/scripts/lib/test-run-live-postgres-readiness-pool.sh"
work="$(mktemp -d)"
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "${work}"' EXIT

die() { printf 'test-live-postgres-readiness-pool: %s\n' "$*" >&2; exit 1; }

# A stand-in case: mark itself running, record how many run at once, sleep,
# then exit with the given status.
probe_case() {
  local id="$1" status="$2" seen
  : >"${work}/running/${id}.${BASHPID}"
  seen="$(rg --files "${work}/running" | wc -l | tr -d '[:space:]')"
  printf '%s\n' "${seen}" >"${work}/peak/${id}"
  sleep 1
  rm -f "${work}/running/${id}.${BASHPID}"
  printf 'case %s says %s\n' "${id}" "${status}"
  return "${status}"
}

# expect_all_failed is the caller-side check the self-test uses: every seeded
# case must exit nonzero and its output must be its own.
expect_all_failed() {
  local id rc
  for id in "$@"; do
    rc="$(pool_rc "${id}")"
    [[ "${rc}" -ne 0 ]] || fail "case ${id} passed"
    [[ "$(pool_out "${id}")" == "case ${id} says ${rc}" ]] || fail "case ${id} output is not its own"
  done
}

# scenario LIB NAME runs one scenario in a subshell against pool library LIB.
scenario() {
  local lib="$1" name="$2"
  (
    set -euo pipefail
    fail() { printf 'pool: %s\n' "$*" >&2; exit 1; }
    fake="${work}/fake"
    # shellcheck source=lib/test-run-live-postgres-readiness-pool.sh
    source "${lib}"
    rm -rf "${work}/running" "${work}/peak"
    mkdir -p "${work}/running" "${work}/peak" "${fake}"
    export LIVE_READINESS_SELFTEST_JOBS=3
    pool_init "${work}/pool-${name}"
    ids=()
    case "${name}" in
      width | order)
        for i in 1 2 3 4 5 6 7; do
          pool_prepare "c${i}"
          pool_start "c${i}" probe_case "c${i}" "$((i + 1))"
          ids+=("c${i}")
        done
        pool_drain
        expect_all_failed "${ids[@]}"
        peak="$(sort -n "${work}"/peak/* | tail -n 1)"
        [[ "${peak}" -le 3 ]] || fail "ran ${peak} cases at once, width is 3"
        [[ "${name}" != width || "${peak}" -ge 2 ]] || fail "cases never overlapped (peak ${peak})"
        ;;
      planted)
        for i in 1 2 3 4 5 6 7; do
          pool_prepare "c${i}"
          status=1
          [[ "${i}" -ne 4 ]] || status=0
          pool_start "c${i}" probe_case "c${i}" "${status}"
          ids+=("c${i}")
        done
        pool_drain
        expect_all_failed "${ids[@]}"
        ;;
      killed)
        pool_prepare c1
        pool_start c1 probe_case c1 1
        pool_prepare c2
        # The case's wrapper subshell is the parent of this sh: killing it
        # stands in for a case that dies before it records a status.
        pool_start c2 sh -c 'kill -9 "${PPID}"'
        pool_drain
        ;;
      hung)
        pool_prepare c1
        pool_start c1 probe_case c1 1
        pool_prepare c2
        pool_start c2 bash -c 'printf "%s\n" "$$" >"$1"; exec sleep 300' hang "${work}/hung.pid"
        pool_drain
        printf 'drained\n'
        expect_all_failed c1
        ;;
    esac
  )
}

# scenarios LIB returns 0 when LIB keeps every property above.
scenarios() {
  local lib="$1" out hung_pid
  scenario "${lib}" width >/dev/null 2>&1 || return 1
  scenario "${lib}" order >/dev/null 2>&1 || return 1
  out="$(scenario "${lib}" planted 2>&1)" && return 1
  [[ "${out}" == *"case c4 passed"* ]] || return 1
  out="$(scenario "${lib}" killed 2>&1)" && return 1
  [[ "${out}" == *"pool case c2 left no exit status"* ]] || return 1
  rm -f "${work}/hung.pid" "${work}/hung.out"
  scenario "${lib}" hung >"${work}/hung.out" 2>&1 &
  local scenario_pid=$!
  for _ in $(seq 1 50); do [[ -s "${work}/hung.pid" ]] && break; sleep 0.1; done
  [[ -s "${work}/hung.pid" ]] || { kill "${scenario_pid}" 2>/dev/null; return 1; }
  sleep 3
  hung_pid="$(cat "${work}/hung.pid")"
  if rg -q drained "${work}/hung.out" || ! kill -0 "${scenario_pid}" 2>/dev/null; then
    kill "${hung_pid}" "${scenario_pid}" 2>/dev/null || true
    return 1
  fi
  kill "${hung_pid}"
  wait "${scenario_pid}" || true
  rg -q drained "${work}/hung.out" || return 1
  return 0
}

scenarios "${real}" || die "the pool does not keep its fail-closed properties"

# An invalid width is refused.
(
  fail() { exit 7; }
  fake="${work}/fake"
  # shellcheck source=lib/test-run-live-postgres-readiness-pool.sh
  source "${real}"
  LIVE_READINESS_SELFTEST_JOBS=0 pool_init "${work}/pool-width0"
) && die "width 0 accepted"

# Seeds: each mutation must make the scenarios fail.
seed() {
  local name="$1" old="$2" new="$3" copy="${work}/seed-${1}.sh"
  python3 - "${real}" "${copy}" "${old}" "${new}" <<'PY' || die "seed ${name}: anchor missing"
import pathlib
import sys

src, dst, old, new = sys.argv[1:5]
text = pathlib.Path(src).read_text(encoding="utf-8")
if text.count(old) != 1:
    sys.exit(1)
pathlib.Path(dst).write_text(text.replace(old, new), encoding="utf-8")
PY
  if scenarios "${copy}"; then die "seed ${name} passed the scenarios"; fi
  printf 'seed %s: caught\n' "${name}"
}
seed missing-status-ignored '[[ -f "${pool_dir}/${id}/rc" ]] || fail' 'true || fail'
seed status-forced-zero '"${rc}" >"${case_dir}/rc.tmp"' '0 >"${case_dir}/rc.tmp"'
seed no-drain-wait '  wait || true
  pool_live=()' '  pool_live=()'
seed width-ignored 'while [[ "${#pool_live[@]}" -ge "${pool_jobs}" ]]; do' 'while false; do'
seed shared-output '"$@" >"${case_dir}/out" 2>&1' '"$@" >>"${pool_dir}/out" 2>&1'

printf 'test-live-postgres-readiness-pool: PASS\n'
