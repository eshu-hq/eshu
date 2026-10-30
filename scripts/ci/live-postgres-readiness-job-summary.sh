#!/usr/bin/env bash
# Job-summary classifier for the live-postgres-readiness hermetic job (#8241)
# and the shard legs (#9582).
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
# Every verdict names its job (READINESS_JOB_LABEL, default hermetic) and the
# runner CPU model, so the hermetic wall reads next to each shard wall in the
# run summary and a slow job can be matched to the hardware it drew (#9582).
#
# Environment (finish): JOB_STATUS, READINESS_JOB_STARTED_EPOCH, CAP_MINUTES
# (must equal the job's timeout-minutes; the cigates readiness test pins it),
# optional READINESS_JOB_LABEL, optional READINESS_RUNNER_CPU (tests; default
# from lscpu and nproc), optional READINESS_JOB_NOW_EPOCH (tests), optional
# GITHUB_STEP_SUMMARY.
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

# runner_cpu prints the CPU model and vCPU count, or unknown off Linux.
runner_cpu() {
  local model="" count=""
  if [[ -n "${READINESS_RUNNER_CPU:-}" ]]; then
    printf '%s' "${READINESS_RUNNER_CPU//[^[:alnum:] @()._,\/-]/}"
    return 0
  fi
  model="$(lscpu 2>/dev/null | sed -n 's/^Model name:[[:space:]]*//p' | head -n 1)" || model=""
  model="${model//[^[:alnum:] @()._,\/-]/}"
  count="$(nproc 2>/dev/null)" || count=""
  [[ -n "${model}" ]] || model=unknown
  if [[ "${count}" =~ ^[0-9]+$ ]]; then printf '%s, %s vCPU' "${model}" "${count}"; else printf '%s' "${model}"; fi
}

finish() {
  local status="${JOB_STATUS:-}" started="${READINESS_JOB_STARTED_EPOCH:-}"
  local cap="${CAP_MINUTES:-}" now cap_s threshold elapsed percent label cpu head cpu_line
  label="${READINESS_JOB_LABEL:-hermetic}"
  [[ "${label}" =~ ^[a-z0-9][a-z0-9\ /()._-]*$ ]] || fail "READINESS_JOB_LABEL is not a plain job name: '${label}'"
  [[ "${cap}" =~ ^[1-9][0-9]*$ ]] || fail "CAP_MINUTES must be a positive integer, got '${cap}'"
  cpu="$(runner_cpu)"
  head="### live-postgres-readiness ${label}"
  cpu_line="Runner CPU: ${cpu}."
  cap_s=$((cap * 60))
  threshold=$((cap_s - grace_s))
  if [[ -z "${started}" ]]; then
    case "${status}" in
      cancelled) emit "${head}: CANCELLED (start time unknown)" \
        "The job was cancelled before its first step recorded a start time, so the cause (time cap or early cancel) cannot be told from elapsed time. Read the job annotations." "${cpu_line}" ;;
      success | failure) emit "${head}: ${status^^} (start time unknown)" "${cpu_line}" ;;
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
        emit "${head}: PASSED, over budget" \
          "Ran ${elapsed}s, ${percent}% of the ${cap}-minute cap (budget ${budget_percent}%). Split or trim the longest step before the cap cancels a run." \
          "${cpu_line}" "::warning title=${label} job near its cap::${elapsed}s is ${percent}% of the ${cap}-minute cap"
      else
        emit "${head}: PASSED" \
          "Ran ${elapsed}s, ${percent}% of the ${cap}-minute cap." "${cpu_line}"
      fi
      ;;
    failure)
      emit "${head}: FAILED" \
        "A step exited nonzero after ${elapsed}s (${percent}% of the ${cap}-minute cap). This is a failure, not a timeout: read the failing step's log." "${cpu_line}"
      ;;
    cancelled)
      if [[ "${elapsed}" -ge "${threshold}" ]]; then
        emit "${head}: CANCELLED BY THE TIME CAP" \
          "The job ran ${elapsed}s and was cancelled at its ${cap}-minute cap. No test failed: the run was cut off. Compare the step durations and the runner CPU and rerun; a repeat means the job outgrew the cap (#8241, #9582)." \
          "${cpu_line}" "::error title=${label} job hit its ${cap}-minute cap::cancelled after ${elapsed}s, not failed by a test"
      else
        emit "${head}: CANCELLED BEFORE THE CAP" \
          "The job was cancelled after ${elapsed}s, ${percent}% of the ${cap}-minute cap. Not a timeout and not a test failure: a newer run in the same concurrency group or a manual cancel." "${cpu_line}"
      fi
      ;;
    *) fail "unknown job status '${status}'" ;;
  esac
}

case "${1:-}" in
  finish) finish ;;
  *) fail "usage: live-postgres-readiness-job-summary.sh finish" ;;
esac
