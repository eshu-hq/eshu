#!/usr/bin/env bash
set -euo pipefail

# Companion to scripts/test-verify-measurement-citations.sh, split out to
# keep that file under the repo's 500-line cap.
#
# Workflow-artifact citations (#9315): a measurement claim may cite the CI
# run that recorded it via "artifact:<run-id>/<artifact-name>" (or a full
# Actions run URL) instead of a committed ledger row. The committed
# "ledger:<id>" path keeps working unchanged. Malformed artifact pointers
# (no namespace) still fail.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-measurement-citations.sh"

tmp_root="$(mktemp -d)"
# #8673: keep fixture repos out of the host git-ai daemon's Trace2 tracking.
export GIT_TRACE2_EVENT=0 GIT_TRACE2_PERF=0
gate_out="$(mktemp "${TMPDIR:-/tmp}/eshu-measurement-artifact-gate-out.XXXXXX")"
gate_err="$(mktemp "${TMPDIR:-/tmp}/eshu-measurement-artifact-gate-err.XXXXXX")"
trap 'rm -rf "${tmp_root}" "${gate_out}" "${gate_err}" 2>/dev/null || true' EXIT

# init_repo seeds a throwaway git repo with a ledger carrying exactly one
# known row, "9999-known-row" (value:0, unit:trials, trials:10), mirroring
# the main suite's fixture.
init_repo() {
  local name="$1"
  local dir="${tmp_root}/${name}"
  mkdir -p "${dir}/docs/internal"
  git -C "${dir}" init -q
  git -C "${dir}" config user.email "test@example.invalid"
  git -C "${dir}" config user.name "Eshu Test"
  printf '%s\n' '{"id":"9999-known-row","date":"2026-07-27","issue":9999,"metric":"example_metric","variant":"baseline","value":0,"unit":"trials","trials":10,"host":"local-dev","backend":"postgresql","backend_version":"16.14","commit":"deadbee0000","command":"go test ./example -run TestExample","note":"fixture row for the citation gate test mirror"}' \
    >"${dir}/docs/internal/measurements.jsonl"
  mkdir -p "${dir}/docs/internal/evidence"
  git -C "${dir}" add .
  git -C "${dir}" commit -q -m initial
  printf '%s\n' "${dir}"
}

run_verifier() {
  local dir="$1"
  ESHU_MEASUREMENT_CITATIONS_REPO_ROOT="${dir}" \
    ESHU_MEASUREMENT_CITATIONS_BASE=HEAD~1 \
    "${verifier}" >"${gate_out}" 2>"${gate_err}"
}

expect_pass() {
  local dir="$1"
  local label="$2"
  if ! run_verifier "${dir}"; then
    printf '[%s] expected verifier to pass in %s\n' "${label}" "${dir}" >&2
    sed -n '1,120p' "${gate_err}" >&2
    exit 1
  fi
}

expect_fail() {
  local dir="$1"
  local label="$2"
  if run_verifier "${dir}"; then
    printf '[%s] expected verifier to fail in %s\n' "${label}" "${dir}" >&2
    sed -n '1,120p' "${gate_out}" >&2
    exit 1
  fi
}

commit_change() {
  local dir="$1"
  local message="$2"
  git -C "${dir}" add .
  git -C "${dir}" commit -q -m "${message}"
}

# A namespaced artifact pointer satisfies a trial-shape claim with no ledger row.
artifact_repo="$(init_repo artifact-cited)"
printf '# New Finding\n\nA deadlock check ran clean: 0/30 trials failed (artifact:39421484741/deadlock-matrix).\n' \
  >"${artifact_repo}/docs/internal/evidence/new-finding.md"
commit_change "${artifact_repo}" "add artifact-cited measurement claim"
expect_pass "${artifact_repo}" "namespaced artifact pointer satisfies the claim"

# A full run URL is a namespaced pointer too.
artifact_url_repo="$(init_repo artifact-url)"
printf '# New Finding\n\nMeasurement: canary bound held (artifact:https://github.com/eshu-hq/eshu/actions/runs/39421484741).\n' \
  >"${artifact_url_repo}/docs/internal/evidence/new-finding.md"
commit_change "${artifact_url_repo}" "add run-URL artifact claim"
expect_pass "${artifact_url_repo}" "run-URL artifact pointer satisfies the claim"

# A bare word is not a resolvable pointer and must still fail.
artifact_bare_repo="$(init_repo artifact-bare)"
printf '# New Finding\n\nA deadlock check ran clean: 0/30 trials failed (artifact:somerun).\n' \
  >"${artifact_bare_repo}/docs/internal/evidence/new-finding.md"
commit_change "${artifact_bare_repo}" "add bare artifact claim"
expect_fail "${artifact_bare_repo}" "bare artifact pointer must fail"

# A slash-bearing non-pointer names no run and must still fail: the ledger
# promises artifact:<run-id>/<artifact-name> or a run URL, and a bare slash
# is not a namespace (eshu-hq/eshu#9368).
artifact_slash_repo="$(init_repo artifact-slash)"
printf '# New Finding\n\nA deadlock check ran clean: 0/30 trials failed (artifact:see/above).\n' \
  >"${artifact_slash_repo}/docs/internal/evidence/new-finding.md"
commit_change "${artifact_slash_repo}" "add slash-bearing non-pointer claim"
expect_fail "${artifact_slash_repo}" "slash-bearing non-pointer must fail"

# A bare http(s) host names no run and must fail: the URL arm requires a
# registry host plus /actions/runs/ or a numeric run segment (eshu-hq/eshu#9370).
artifact_bare_url_repo="$(init_repo artifact-bare-url)"
printf '# New Finding\n\nA deadlock check ran clean: 0/30 trials failed (artifact:http://x).\n' \
  >"${artifact_bare_url_repo}/docs/internal/evidence/new-finding.md"
commit_change "${artifact_bare_url_repo}" "add bare-http artifact claim"
expect_fail "${artifact_bare_url_repo}" "bare http URL must fail"

artifact_bare_url_s_repo="$(init_repo artifact-bare-url-s)"
printf '# New Finding\n\nA deadlock check ran clean: 0/30 trials failed (artifact:https://y).\n' \
  >"${artifact_bare_url_s_repo}/docs/internal/evidence/new-finding.md"
commit_change "${artifact_bare_url_s_repo}" "add bare-https artifact claim"
expect_fail "${artifact_bare_url_s_repo}" "bare https URL must fail"

# The committed ledger path keeps working through the transition.
ledger_repo="$(init_repo ledger-still-accepted)"
printf '# New Finding\n\nA deadlock check ran clean: 0/10 trials failed (ledger:9999-known-row).\n' \
  >"${ledger_repo}/docs/internal/evidence/new-finding.md"
commit_change "${ledger_repo}" "add ledger-cited measurement claim"
expect_pass "${ledger_repo}" "committed ledger citation still satisfies the claim"

printf 'verify-measurement-citations workflow-artifact test passed\n'
