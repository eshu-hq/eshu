#!/usr/bin/env bash
# Job-summary classifier for the live-postgres-readiness hermetic job (#8241).
#
# GitHub cancels a job that reaches `timeout-minutes` and marks it cancelled,
# not failed, so no check turns red and the run reads like a concurrency
# cancel. The workflow records the start epoch inline in its first step
# (READINESS_JOB_STARTED_EPOCH in $GITHUB_ENV, before checkout, so it needs no
# repo file) and runs this script as its last step, `if: always()`:
#
#   finish  classifies the job from JOB_STATUS (the job context's success,
#           failure, or cancelled) and the elapsed seconds, then writes the
#           verdict to $GITHUB_STEP_SUMMARY and stdout.
#
# A cancelled job whose elapsed time is within a short grace of the cap is
# reported as cancelled by the time cap. The grace covers runner setup before
# the first step. A cancel that arrives sooner is reported as an early cancel
# (a newer run in the same concurrency group, or a manual cancel). The elapsed
# time cannot separate a concurrency cancel that lands inside the grace from a
# real cap hit; the platform annotation on the job names the true cause.
#
# Environment (finish): JOB_STATUS, READINESS_JOB_STARTED_EPOCH, CAP_MINUTES
# (must equal the job's timeout-minutes; the cigates readiness test pins it),
# optional READINESS_JOB_NOW_EPOCH (tests), optional GITHUB_STEP_SUMMARY.
set -euo pipefail

fail() { printf 'live-postgres-readiness-job-summary: %s\n' "$*" >&2; exit 2; }

# grace_s is how far below the cap a cancelled job may stop and still read as
# a cap hit: runner setup runs before the first step starts the clock.
grace_s=90
# budget_percent is the share of the cap above which a passing job warns.
budget_percent=80

# emit prints the verdict lines to stdout and, minus the ::annotation lines,
# to the step summary.
emit() {
  local line
  for line in "$@"; do
    printf '%s\n' "${line}"
    if [[ "${line}" != ::* && -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
      printf '%s\n' "${line}" >>"${GITHUB_STEP_SUMMARY}"
    fi
  done
}

finish() {
  local status="${JOB_STATUS:-}" started="${READINESS_JOB_STARTED_EPOCH:-}"
  local cap="${CAP_MINUTES:-}" now cap_s threshold elapsed percent
  [[ "${cap}" =~ ^[1-9][0-9]*$ ]] || fail "CAP_MINUTES must be a positive integer, got '${cap}'"
  cap_s=$((cap * 60))
  threshold=$((cap_s - grace_s))
  if [[ -z "${started}" ]]; then
    case "${status}" in
      cancelled) emit "### live-postgres-readiness hermetic: CANCELLED (start time unknown)" \
        "The job was cancelled before its first step recorded a start time, so the cause (time cap or early cancel) cannot be told from elapsed time. Read the job annotations." ;;
      success | failure) emit "### live-postgres-readiness hermetic: ${status^^} (start time unknown)" ;;
      *) fail "unknown job status '${status}'" ;;
    esac
    return 0
  fi
  [[ "${started}" =~ ^[0-9]+$ ]] || fail "READINESS_JOB_STARTED_EPOCH is not an epoch: '${started}'"
  now="${READINESS_JOB_NOW_EPOCH:-$(date +%s)}"
  [[ "${now}" =~ ^[0-9]+$ && "${now}" -ge "${started}" ]] || fail "bad end epoch '${now}'"
  elapsed=$((now - started))
  percent=$((elapsed * 100 / cap_s))
  case "${status}" in
    success)
      if [[ "${percent}" -ge "${budget_percent}" ]]; then
        emit "### live-postgres-readiness hermetic: PASSED, over budget" \
          "Ran ${elapsed}s, ${percent}% of the ${cap}-minute cap (budget ${budget_percent}%). Split or trim the longest step before the cap cancels a run." \
          "::warning title=hermetic job near its cap::${elapsed}s is ${percent}% of the ${cap}-minute cap"
      else
        emit "### live-postgres-readiness hermetic: PASSED" \
          "Ran ${elapsed}s, ${percent}% of the ${cap}-minute cap."
      fi
      ;;
    failure)
      emit "### live-postgres-readiness hermetic: FAILED" \
        "A step exited nonzero after ${elapsed}s (${percent}% of the ${cap}-minute cap). This is a failure, not a timeout: read the failing step's log."
      ;;
    cancelled)
      if [[ "${elapsed}" -ge "${threshold}" ]]; then
        emit "### live-postgres-readiness hermetic: CANCELLED BY THE TIME CAP" \
          "The job ran ${elapsed}s and was cancelled at its ${cap}-minute cap. No test failed: the run was cut off. Compare the step durations and rerun; a repeat means the self-test outgrew the cap (#8241)." \
          "::error title=hermetic job hit its ${cap}-minute cap::cancelled after ${elapsed}s, not failed by a test"
      else
        emit "### live-postgres-readiness hermetic: CANCELLED BEFORE THE CAP" \
          "The job was cancelled after ${elapsed}s, ${percent}% of the ${cap}-minute cap. Not a timeout and not a test failure: a newer run in the same concurrency group or a manual cancel."
      fi
      ;;
    *) fail "unknown job status '${status}'" ;;
  esac
}

case "${1:-}" in
  finish) finish ;;
  *) fail "usage: live-postgres-readiness-job-summary.sh finish" ;;
esac
