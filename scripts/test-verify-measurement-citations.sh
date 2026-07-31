#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-measurement-citations.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

# init_repo seeds a throwaway git repo with a ledger carrying exactly one
# known row, "9999-known-row", so tests can prove citation-id validation
# without depending on this repo's own ledger contents.
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
  printf '# Historical\n\nAn old finding: 3/10 trials failed (ledger:9999-known-row).\n' \
    >"${dir}/docs/internal/evidence/9999-historical.md"
  git -C "${dir}" add .
  git -C "${dir}" commit -q -m initial
  printf '%s\n' "${dir}"
}

run_verifier() {
  local dir="$1"
  ESHU_MEASUREMENT_CITATIONS_REPO_ROOT="${dir}" \
    ESHU_MEASUREMENT_CITATIONS_BASE=HEAD~1 \
    "${verifier}" >/tmp/eshu-measurement-gate.out 2>/tmp/eshu-measurement-gate.err
}

expect_pass() {
  local dir="$1"
  local label="$2"
  if ! run_verifier "${dir}"; then
    printf '[%s] expected verifier to pass in %s\n' "${label}" "${dir}" >&2
    sed -n '1,120p' /tmp/eshu-measurement-gate.err >&2
    exit 1
  fi
}

expect_fail() {
  local dir="$1"
  local label="$2"
  if run_verifier "${dir}"; then
    printf '[%s] expected verifier to fail in %s\n' "${label}" "${dir}" >&2
    sed -n '1,120p' /tmp/eshu-measurement-gate.out >&2
    exit 1
  fi
}

commit_change() {
  local dir="$1"
  local message="$2"
  git -C "${dir}" add .
  git -C "${dir}" commit -q -m "${message}"
}

# --- Case 1: induced failure -- an added line states a measurement (trial
# shape) with NO citation at all. This is the "watch it fail" proof: a gate
# never observed to fail is a decoration, not a control.
uncited_repo="$(init_repo uncited)"
printf '# New Finding\n\nA deadlock check ran clean: 0/30 trials failed.\n' \
  >"${uncited_repo}/docs/internal/evidence/new-finding.md"
commit_change "${uncited_repo}" "add uncited measurement claim"
expect_fail "${uncited_repo}" "uncited trial-shape claim must fail"

# --- Case 1b: the SAME shape via "runs" instead of "trials". The gate's own
# failure message and the agent guide both advertise `<N>/<M> runs` as a
# recognized claim, but nothing tested it -- a mutation deleting the `runs`
# alternative from claim_pattern passed the entire suite. An advertised branch
# with no coverage is a promise the gate does not keep.
uncited_runs_repo="$(init_repo uncited-runs)"
printf '# New Finding\n\nThe retry probe was stable: 4/25 runs flaked.\n' \
  >"${uncited_runs_repo}/docs/internal/evidence/new-finding-runs.md"
commit_change "${uncited_runs_repo}" "add uncited runs-shape claim"
expect_fail "${uncited_runs_repo}" "uncited runs-shape claim must fail"
rg -q "cites no ledger row" /tmp/eshu-measurement-gate.err \
  || { printf 'expected failure message to explain the missing citation\n' >&2; exit 1; }

# --- Case 2: induced failure -- an added line cites a "ledger:<id>" token
# that does not exist in the ledger.
unknown_id_repo="$(init_repo unknown-id)"
printf '# New Finding\n\nA deadlock check ran clean: 0/30 trials failed (ledger:no-such-row).\n' \
  >"${unknown_id_repo}/docs/internal/evidence/new-finding.md"
commit_change "${unknown_id_repo}" "cite a ledger id that does not exist"
expect_fail "${unknown_id_repo}" "citation to unknown ledger id must fail"
rg -q "which is not in docs/internal/measurements.jsonl" /tmp/eshu-measurement-gate.err \
  || { printf 'expected failure message to name the unknown id\n' >&2; exit 1; }

# --- Case 3: the same claim, now fixed with a valid citation, passes. This is
# the "then show it passing" half of the induced-failure proof.
fixed_repo="$(init_repo fixed)"
printf '# New Finding\n\nA deadlock check ran clean: 0/30 trials failed (ledger:9999-known-row).\n' \
  >"${fixed_repo}/docs/internal/evidence/new-finding.md"
commit_change "${fixed_repo}" "add properly cited measurement claim"
expect_pass "${fixed_repo}" "properly cited trial-shape claim must pass"

# --- Case 4: an explicit "Measurement:" marker line with a valid citation
# passes -- the second recognized shape, independent of the trial/rate regex.
marker_repo="$(init_repo marker)"
printf '# New Finding\n\nMeasurement: bounded pass 5.9s (ledger:9999-known-row)\n' \
  >"${marker_repo}/docs/internal/evidence/new-finding.md"
commit_change "${marker_repo}" "add Measurement: marker line with citation"
expect_pass "${marker_repo}" "cited Measurement: marker must pass"

# --- Case 5: an explicit "Measurement:" marker line with NO citation fails.
marker_uncited_repo="$(init_repo marker-uncited)"
printf '# New Finding\n\nMeasurement: bounded pass 5.9s, no attribution here.\n' \
  >"${marker_uncited_repo}/docs/internal/evidence/new-finding.md"
commit_change "${marker_uncited_repo}" "add uncited Measurement: marker line"
expect_fail "${marker_uncited_repo}" "uncited Measurement: marker must fail"

# --- Case 6: the gate must NOT fire on the ledger's own rows. Appending a new
# valid ledger row (itself containing digits like "trials":10) must pass even
# though the ledger file is excluded from scanning by path.
ledger_row_repo="$(init_repo ledger-row)"
printf '%s\n' '{"id":"9999-second-row","date":"2026-07-27","issue":9999,"metric":"example_metric","variant":"variant-b","value":5,"unit":"trials","trials":30,"host":"local-dev","backend":"postgresql","backend_version":"16.14","commit":"deadbee0001","command":"go test ./example -run TestExample","note":"second fixture row"}' \
  >>"${ledger_row_repo}/docs/internal/measurements.jsonl"
commit_change "${ledger_row_repo}" "append a new ledger row"
expect_pass "${ledger_row_repo}" "appending a new ledger row must not self-trigger the gate"

# --- Case 7: the gate must NOT fire on an existing historical doc left
# untouched. init_repo's own initial commit already ships a cited trial claim
# in docs/internal/evidence/9999-historical.md; touching an unrelated file
# must not cause that file to be rescanned or flagged.
untouched_repo="$(init_repo untouched)"
printf '# Unrelated\n\nNo measurement language here.\n' \
  >"${untouched_repo}/docs/internal/evidence/unrelated.md"
commit_change "${untouched_repo}" "add unrelated file, leave historical doc alone"
expect_pass "${untouched_repo}" "untouched historical doc must not trigger the gate"

# --- Case 8: a properly cited line elsewhere in the diff (mixed with
# unrelated added lines) still passes -- proves the gate does not over-fire on
# every added line in a file that happens to also contain a citation.
mixed_repo="$(init_repo mixed)"
{
  printf '# Mixed\n\n'
  printf 'Some unrelated prose with no numbers at all.\n'
  printf 'A deadlock check ran clean: 0/30 trials failed (ledger:9999-known-row).\n'
  printf 'More unrelated prose.\n'
} >"${mixed_repo}/docs/internal/evidence/mixed.md"
commit_change "${mixed_repo}" "add mixed file with one cited claim among plain prose"
expect_pass "${mixed_repo}" "mixed file with one properly cited claim must pass"

# The gate's own script and mirror test necessarily CONTAIN claim-shaped text --
# regex literals and these very fixtures. They are exempt, and that exemption was
# untested: a mutation removing it survived the whole suite, so nothing proved
# the gate could be edited at all without blocking its own commit.
self_edit_repo="$(init_repo self-edit)"
mkdir -p "${self_edit_repo}/scripts"
printf '%s\n' \
  '#!/bin/bash' \
  '# A gate edit that mentions 7/9 trials in prose, and a Measurement: line,' \
  '# both uncited -- exempt because this file IS the gate.' \
  >"${self_edit_repo}/scripts/verify-measurement-citations.sh"
commit_change "${self_edit_repo}" "edit the gate itself"
expect_pass "${self_edit_repo}" "editing the gate's own script must not trip the gate on its own regex literals"

# The same must hold for prose ABOUT the gate that lives outside those two files.
# An unanchored `Measurement:` alternative matched the substring anywhere on a
# line, so a comment in .pre-commit-config.yaml describing the gate tripped it
# and the branch could not pass its own pre-push hook.
describing_repo="$(init_repo describing)"
printf '%s\n' \
  'repos:' \
  '  - repo: local' \
  '    hooks:' \
  '      # A claim in "Measurement:" shape must cite a ledger row.' \
  >"${describing_repo}/.pre-commit-config.yaml"
commit_change "${describing_repo}" "describe the gate in a config comment"
expect_pass "${describing_repo}" "prose describing the gate mid-line must not trip it"

printf 'verify-measurement-citations tests passed\n'
