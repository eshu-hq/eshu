#!/usr/bin/env bash
# RED/GREEN checks for live-postgres-readiness-job-summary.sh (#8241): a
# hermetic job cancelled by its time cap must read differently from a failed
# job and from a cancel that came early (a newer run in the same concurrency
# group, or a manual cancel). Each case feeds the job status and elapsed
# seconds that the workflow step would see and checks the summary text.
# The seeded section then breaks the classifier in a throwaway copy and
# requires the same cases to fail, so a classifier that cannot tell the three
# outcomes apart does not pass.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
real="${root}/scripts/ci/live-postgres-readiness-job-summary.sh"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

fail() { printf 'test-live-postgres-readiness-job-summary: %s\n' "$*" >&2; exit 1; }

# run_case prints the summary the script writes for one status and elapsed
# time. Output goes to the step summary file and to stdout; both must agree.
run_case() {
  local script="$1" status="$2" elapsed="$3" cap="${4:-30}" started=1000000
  local summary="${work}/summary.md" out
  : >"${summary}"
  out="$(JOB_STATUS="${status}" READINESS_JOB_STARTED_EPOCH="${started}" \
    READINESS_JOB_NOW_EPOCH="$((started + elapsed))" CAP_MINUTES="${cap}" \
    GITHUB_STEP_SUMMARY="${summary}" bash "${script}" finish 2>&1)" || return 1
  [[ "$(cat "${summary}")" == *"$(printf '%s' "${out}" | rg -v '^::' || true)"* ]] || return 2
  printf '%s\n' "${out}"
}

has() { [[ "$1" == *"$2"* ]]; }

# check_cases returns 0 when every outcome is classified as required.
check_cases() {
  local script="$1" out
  out="$(run_case "${script}" success 300)" || return 1
  has "${out}" "PASSED" && has "${out}" "16% of the 30-minute cap" && ! has "${out}" "::warning" || return 1
  out="$(run_case "${script}" success 1500)" || return 1
  has "${out}" "PASSED" && has "${out}" "::warning" && has "${out}" "83%" || return 1
  out="$(run_case "${script}" failure 120)" || return 1
  has "${out}" "FAILED" && has "${out}" "not a timeout" && ! has "${out}" "TIME CAP" || return 1
  out="$(run_case "${script}" failure 1795)" || return 1
  has "${out}" "FAILED" && ! has "${out}" "TIME CAP" || return 1
  out="$(run_case "${script}" cancelled 1795)" || return 1
  has "${out}" "CANCELLED BY THE TIME CAP" && has "${out}" "::error" && ! has "${out}" "FAILED" || return 1
  out="$(run_case "${script}" cancelled 1710)" || return 1
  has "${out}" "CANCELLED BY THE TIME CAP" || return 1
  out="$(run_case "${script}" cancelled 1709)" || return 1
  has "${out}" "CANCELLED BEFORE THE CAP" && ! has "${out}" "TIME CAP" || return 1
  out="$(run_case "${script}" cancelled 89)" || return 1
  has "${out}" "CANCELLED BEFORE THE CAP" && has "${out}" "concurrency" || return 1
  out="$(run_case "${script}" cancelled 599 10)" || return 1
  has "${out}" "CANCELLED BY THE TIME CAP" && has "${out}" "10-minute cap" || return 1
  return 0
}

check_cases "${real}" || fail "classifier does not separate success, failure, cap cancel, and early cancel"

# An unknown status and a malformed start time are refused.
JOB_STATUS=skipped READINESS_JOB_STARTED_EPOCH=1 CAP_MINUTES=30 bash "${real}" finish >/dev/null 2>&1 &&
  fail "unknown job status accepted"
JOB_STATUS=cancelled READINESS_JOB_STARTED_EPOCH=abc CAP_MINUTES=30 bash "${real}" finish >/dev/null 2>&1 &&
  fail "malformed start epoch accepted"
# A cancel before the start step ran says so instead of guessing a cause.
out="$(JOB_STATUS=cancelled READINESS_JOB_STARTED_EPOCH= CAP_MINUTES=30 bash "${real}" finish 2>&1)" ||
  fail "cancel without a start time was refused"
has "${out}" "start time unknown" && ! has "${out}" "TIME CAP" || fail "cancel without a start time was classified"

# The start mode records the epoch for later steps.
env_file="${work}/env"
: >"${env_file}"
GITHUB_ENV="${env_file}" bash "${real}" start || fail "start mode failed"
rg -q '^READINESS_JOB_STARTED_EPOCH=[0-9]+$' "${env_file}" || fail "start mode wrote no epoch"

# Seeds: each mutation must make check_cases fail.
seed() {
  local name="$1" old="$2" new="$3" copy="${work}/seed-${1}.sh"
  python3 - "${real}" "${copy}" "${old}" "${new}" <<'PY' || fail "seed ${name}: anchor missing"
import pathlib
import sys

src, dst, old, new = sys.argv[1:5]
text = pathlib.Path(src).read_text(encoding="utf-8")
if text.count(old) != 1:
    sys.exit(1)
pathlib.Path(dst).write_text(text.replace(old, new), encoding="utf-8")
PY
  if check_cases "${copy}"; then fail "seed ${name} passed the cases"; fi
  printf 'seed %s: caught\n' "${name}"
}
seed cancel-as-failure 'if [[ "${elapsed}" -ge "${threshold}" ]]; then' 'if false; then'
seed failure-as-cap '    failure)
      emit' '    failure|cancelled)
      emit'
seed every-cancel-is-cap 'if [[ "${elapsed}" -ge "${threshold}" ]]; then' 'if true; then'
seed grace-dropped 'threshold=$((cap_s - grace_s))' 'threshold=$((cap_s + 1))'

printf 'test-live-postgres-readiness-job-summary: PASS\n'
