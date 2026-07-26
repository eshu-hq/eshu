#!/usr/bin/env bash
#
# Shared scanner-pipe-tee-status helper for security-scan.yml's govulncheck
# and nancy steps (#5813).
#
# The false-green bug this closes: both steps ran `<scanner> 2>&1 | tee
# <artifact>` under `set -euo pipefail`. When the scanner SUCCEEDED but `tee`
# FAILED to write its artifact (full disk, an unwritable working directory),
# the `if ! ...` branch fired -- but the original inline code read only
# ${PIPESTATUS[0]} (the scanner's own status, 0) and exited 0, reporting the
# scan green with no output file ever written. Fixed once by 41e77ee33
# directly in the workflow YAML; this script is the delegated, independently
# testable home for that logic (scripts/test-security-scan-tee-status.sh runs
# it end-to-end with stub scanners), following the same shape as
# scripts/dev/nancy-local.sh / scripts/test-nancy-local.sh: a source-text-only
# assertion over the YAML "could not tell a working pipeline from a broken
# one" (rejected in review on PR #5806) -- the logic has to live in a script
# with its own executable regression suite.
#
# Usage:
#   run-scan-with-tee.sh <label> <artifact-path> <scanner-fail-fmt> -- <scanner-cmd> [args...]
#
#   label             - short scanner name (e.g. "govulncheck", "nancy"),
#                       used only to build the tee-failure diagnostic below.
#   artifact-path     - file the scanner's combined stdout+stderr is teed to.
#   scanner-fail-fmt  - printf format string printed to stderr when the
#                       scanner itself fails (PIPESTATUS[0] != 0). May
#                       contain a %s for the scanner's exit status (nancy
#                       does; govulncheck's fixed wording does not -- printf
#                       silently drops an unused trailing argument when the
#                       format has no conversions, so one calling convention
#                       covers both). Callers pass their EXACT current
#                       wording here so both retain it unchanged.
#                       CONTRACT: this value is passed straight to `printf` as
#                       its format string (see the `printf -- "${scanner_fail_fmt}"`
#                       call below, shellcheck SC2059 intentionally
#                       suppressed there). Callers MUST pass a static string
#                       literal and MUST NOT interpolate scanner output or any
#                       other dynamic/untrusted text into this argument -- a
#                       literal `%` in dynamic text would be interpreted as a
#                       printf conversion and corrupt or crash the diagnostic
#                       output. Both current call sites (govulncheck, nancy)
#                       pass static literals; keep it that way.
#   --                - literal separator before the scanner command.
#   scanner-cmd [...] - the scanner invocation to run; its combined
#                       stdout+stderr is piped through `tee artifact-path`.
#                       Any stdin the scanner needs (e.g. nancy's dependency
#                       graph) must be redirected onto THIS script's
#                       invocation by the caller -- it is inherited by the
#                       pipeline's first command unchanged.
#
# Exit status: the scanner's exit status when non-zero; otherwise tee's exit
# status (non-zero only when the artifact failed to write, e.g. ENOTDIR/ENOSPC).
# A genuine scanner finding with a writable artifact still exits with the
# scanner's own status, unchanged from before this helper existed.
set -euo pipefail

if [ "$#" -lt 4 ]; then
  echo "usage: $0 <label> <artifact-path> <scanner-fail-fmt> -- <scanner-cmd> [args...]" >&2
  exit 64
fi

label="$1"
artifact="$2"
scanner_fail_fmt="$3"
shift 3

if [ "$1" != "--" ]; then
  printf 'run-scan-with-tee: expected "--" before the scanner command, got %s\n' "$1" >&2
  exit 64
fi
shift

if [ "$#" -eq 0 ]; then
  echo "run-scan-with-tee: no scanner command given after --" >&2
  exit 64
fi

if ! "$@" 2>&1 | tee "${artifact}"; then
  # Capture BOTH pipeline statuses in the statement immediately after the
  # pipeline runs -- PIPESTATUS is clobbered by the very next command, even a
  # bare assignment, so it cannot be read piecemeal.
  statuses=("${PIPESTATUS[@]}")
  scanner_status=${statuses[0]}
  tee_status=${statuses[1]}
  if [ "${scanner_status}" -ne 0 ]; then
    # shellcheck disable=SC2059 # scanner_fail_fmt is a caller-supplied printf
    # format string (may contain %s for the exit status); this is intentional.
    printf -- "${scanner_fail_fmt}" "${scanner_status}" >&2
    exit "${scanner_status}"
  fi
  # A scanner success (0) with a tee failure must not report success: pipefail
  # makes the pipeline's overall status the rightmost non-zero code (tee's),
  # but only checking PIPESTATUS[0] misses it -- this is the false-green bug.
  printf '%s: failed to write %s (tee exited %s)\n' "${label}" "${artifact}" "${tee_status}" >&2
  exit "${tee_status}"
fi
