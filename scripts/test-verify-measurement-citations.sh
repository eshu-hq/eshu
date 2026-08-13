#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-measurement-citations.sh"

tmp_root="$(mktemp -d)"
# gate_out/gate_err are this run's own scratch files, not the hardcoded
# /tmp/eshu-measurement-gate.{out,err} an earlier version of this test used.
# Two concurrent runs of this suite (this Mac has had many concurrent agents
# and worktrees active) would otherwise clobber each other's captured
# output and could make one run assert against the OTHER run's result.
gate_out="$(mktemp "${TMPDIR:-/tmp}/eshu-measurement-gate-out.XXXXXX")"
gate_err="$(mktemp "${TMPDIR:-/tmp}/eshu-measurement-gate-err.XXXXXX")"
# shim_dir is created much later (the PATH-shim case) and is empty until then.
# The trap body is single-quoted so it expands at exit, not here, and rm on an
# empty operand is harmless under the existing suppression.
trap 'rm -rf "${tmp_root}" "${gate_out}" "${gate_err}" "${shim_dir}" 2>/dev/null || true' EXIT
shim_dir=""

# init_repo seeds a throwaway git repo with a ledger carrying exactly one
# known row, "9999-known-row" (value:0, unit:trials, trials:10), so tests can
# prove citation-id AND value validation without depending on this repo's
# own ledger contents.
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
rg -q "cites no ledger row" "${gate_err}" \
  || { printf 'expected failure message to explain the missing citation\n' >&2; exit 1; }

# --- Case 2: induced failure -- an added line cites a "ledger:<id>" token
# that does not exist in the ledger.
unknown_id_repo="$(init_repo unknown-id)"
printf '# New Finding\n\nA deadlock check ran clean: 0/30 trials failed (ledger:no-such-row).\n' \
  >"${unknown_id_repo}/docs/internal/evidence/new-finding.md"
commit_change "${unknown_id_repo}" "cite a ledger id that does not exist"
expect_fail "${unknown_id_repo}" "citation to unknown ledger id must fail"
rg -q "which is not in docs/internal/measurements.jsonl" "${gate_err}" \
  || { printf 'expected failure message to name the unknown id\n' >&2; exit 1; }

# --- Case 3: the same claim, now fixed with a valid citation AND the figure
# the row actually states (value:0, trials:10 -> "0/10"), passes. This is the
# "then show it passing" half of the induced-failure proof.
fixed_repo="$(init_repo fixed)"
printf '# New Finding\n\nA deadlock check ran clean: 0/10 trials failed (ledger:9999-known-row).\n' \
  >"${fixed_repo}/docs/internal/evidence/new-finding.md"
commit_change "${fixed_repo}" "add properly cited measurement claim"
expect_pass "${fixed_repo}" "properly cited trial-shape claim must pass"

# --- Case 3v: membership is not enough -- citing a REAL row for the WRONG
# figure must be rejected, not merely a citation to an unknown id. Citing
# 9999-known-row (value:0, trials:10) for "0/30" is exactly the defect the
# ledger exists to prevent: a wrong number now carries the ledger's
# authority, and a reader who sees "(ledger:...)" stops checking. This case
# must fail against an unfixed verifier that only checks id membership -- if
# it does not fail there, the value-mismatch bug has not been reproduced.
value_mismatch_repo="$(init_repo value-mismatch)"
printf '# New Finding\n\nA deadlock check ran clean: 0/30 trials failed (ledger:9999-known-row).\n' \
  >"${value_mismatch_repo}/docs/internal/evidence/new-finding.md"
commit_change "${value_mismatch_repo}" "cite a real row for the wrong figure"
expect_fail "${value_mismatch_repo}" "citing a real row for a disagreeing figure must fail"
rg -q "but that row states value=0 trials=10" "${gate_err}" \
  || { printf 'expected failure message to name the row disagreement\n' >&2; \
       sed -n '1,20p' "${gate_err}" >&2; exit 1; }

# --- Case 3w: same defect, but the denominator alone disagrees (numerator
# happens to match). Proves both value and trials are compared, not just one.
trials_mismatch_repo="$(init_repo trials-mismatch)"
printf '# New Finding\n\nA deadlock check ran clean: 0/99 trials failed (ledger:9999-known-row).\n' \
  >"${trials_mismatch_repo}/docs/internal/evidence/new-finding.md"
commit_change "${trials_mismatch_repo}" "cite a real row with the wrong denominator"
expect_fail "${trials_mismatch_repo}" "matching numerator but disagreeing denominator must fail"

# --- Case 4: an explicit "Measurement:" marker line with a valid citation
# and NO restated figure passes -- the second recognized shape, independent
# of the trial/rate regex. The gate cannot verify an arbitrary duration
# against the row's structured value/trials, so the marker must omit the
# figure rather than restate one unverifiably.
marker_repo="$(init_repo marker)"
printf '# New Finding\n\nMeasurement: bounded pass, no regression detected (ledger:9999-known-row)\n' \
  >"${marker_repo}/docs/internal/evidence/new-finding.md"
commit_change "${marker_repo}" "add Measurement: marker line with citation, no figure"
expect_pass "${marker_repo}" "cited Measurement: marker with no figure must pass"

# --- Case 4v: a "Measurement:" marker whose figure IS the recognized ratio
# shape is verified exactly like a bare "<N>/<M> trials" claim.
marker_ratio_repo="$(init_repo marker-ratio)"
printf '# New Finding\n\nMeasurement: 0/10 trials (ledger:9999-known-row)\n' \
  >"${marker_ratio_repo}/docs/internal/evidence/new-finding.md"
commit_change "${marker_ratio_repo}" "add Measurement: marker with a matching ratio"
expect_pass "${marker_ratio_repo}" "Measurement: marker with a matching ratio must pass"

marker_ratio_mismatch_repo="$(init_repo marker-ratio-mismatch)"
printf '# New Finding\n\nMeasurement: 0/30 trials (ledger:9999-known-row)\n' \
  >"${marker_ratio_mismatch_repo}/docs/internal/evidence/new-finding.md"
commit_change "${marker_ratio_mismatch_repo}" "add Measurement: marker with a disagreeing ratio"
expect_fail "${marker_ratio_mismatch_repo}" "Measurement: marker with a disagreeing ratio must fail"

# --- Case 4w: a "Measurement:" marker restating an unverifiable figure (a
# duration, not the ratio shape) is rejected even though the citation is
# real and correct-looking. This is the second half of the value-check
# reproduction: an unfixed verifier that only checks membership passes this,
# because it never inspects the figure for the Measurement: shape at all.
marker_duration_repo="$(init_repo marker-duration)"
printf '# New Finding\n\nMeasurement: bounded pass 5.9s (ledger:9999-known-row)\n' \
  >"${marker_duration_repo}/docs/internal/evidence/new-finding.md"
commit_change "${marker_duration_repo}" "restate an unverifiable duration alongside a real citation"
expect_fail "${marker_duration_repo}" "Measurement: marker restating an unverifiable figure must fail"
rg -q "restates a figure this gate cannot verify" "${gate_err}" \
  || { printf 'expected failure message to explain the unverifiable figure\n' >&2; \
       sed -n '1,20p' "${gate_err}" >&2; exit 1; }

# --- Case 5: an explicit "Measurement:" marker line with NO citation fails.
marker_uncited_repo="$(init_repo marker-uncited)"
printf '# New Finding\n\nMeasurement: bounded pass 5.9s, no attribution here.\n' \
  >"${marker_uncited_repo}/docs/internal/evidence/new-finding.md"
commit_change "${marker_uncited_repo}" "add uncited Measurement: marker line"
expect_fail "${marker_uncited_repo}" "uncited Measurement: marker must fail"

# --- Case 5h/5c: the "Measurement:" trigger is prose-only by design -- a
# Markdown heading (any level) or a source-code comment marker is excluded,
# not merely unhandled. Each fixture below states an uncited DURATION (not
# the "<N>/<M> trials|runs" ratio shape, which is unanchored and would match
# regardless of the Measurement: prefix) so the only way it could be flagged
# at all is via the "Measurement:" marker path; PASS here proves the line is
# not recognized as a claim, not that some other pattern missed it.
heading1_repo="$(init_repo heading-h1)"
printf '%s\n' '# Measurement: bounded pass 5.9s' \
  >"${heading1_repo}/docs/internal/evidence/heading.md"
commit_change "${heading1_repo}" "add a level-1 Markdown heading naming Measurement"
expect_pass "${heading1_repo}" "a level-1 Measurement: heading must not be scanned as a claim"

heading2_repo="$(init_repo heading-h2)"
printf '%s\n' '## Measurement: bounded pass 5.9s' \
  >"${heading2_repo}/docs/internal/evidence/heading.md"
commit_change "${heading2_repo}" "add a level-2 Markdown heading naming Measurement"
expect_pass "${heading2_repo}" "a level-2 Measurement: heading must not be scanned as a claim"

comment_repo="$(init_repo comment-slash)"
printf '%s\n' '// Measurement: bounded pass 5.9s' \
  >"${comment_repo}/docs/internal/evidence/comment.md"
commit_change "${comment_repo}" "add a slash-comment naming Measurement"
expect_pass "${comment_repo}" "a // Measurement: comment marker must not be scanned as a claim"

# --- Case 6: the gate must NOT fire on the ledger's own rows. Appending a new
# valid ledger row (itself containing digits like "trials":10) must pass even
# though the ledger file is excluded from scanning by path, and the
# append-only check must not fire on a pure addition.
ledger_row_repo="$(init_repo ledger-row)"
printf '%s\n' '{"id":"9999-second-row","date":"2026-07-27","issue":9999,"metric":"example_metric","variant":"variant-b","value":5,"unit":"trials","trials":30,"host":"local-dev","backend":"postgresql","backend_version":"16.14","commit":"deadbee0001","command":"go test ./example -run TestExample","note":"second fixture row"}' \
  >>"${ledger_row_repo}/docs/internal/measurements.jsonl"
commit_change "${ledger_row_repo}" "append a new ledger row"
expect_pass "${ledger_row_repo}" "appending a new ledger row must not self-trigger the gate"

# --- Case 6e: the ledger's append-only contract is enforced independent of
# any added prose claim. Editing an existing row's value in place, with no
# new measurement-shaped line added anywhere, must still fail: a later
# commit could otherwise silently change what a live citation to that row
# means, and nothing scanning only ADDED prose lines would ever notice,
# because the ledger file itself is (correctly, for a different reason)
# exempt from that scan.
ledger_edit_repo="$(init_repo ledger-edit)"
printf '%s\n' '{"id":"9999-known-row","date":"2026-07-27","issue":9999,"metric":"example_metric","variant":"baseline","value":1,"unit":"trials","trials":10,"host":"local-dev","backend":"postgresql","backend_version":"16.14","commit":"deadbee0000","command":"go test ./example -run TestExample","note":"fixture row for the citation gate test mirror"}' \
  >"${ledger_edit_repo}/docs/internal/measurements.jsonl"
commit_change "${ledger_edit_repo}" "edit an existing ledger row in place"
expect_fail "${ledger_edit_repo}" "editing an existing ledger row must fail even with no new claim"
rg -q "row '9999-known-row' was modified" "${gate_err}" \
  || { printf 'expected failure message to name the modified row\n' >&2; \
       sed -n '1,20p' "${gate_err}" >&2; exit 1; }

# --- Case 6d: deleting an existing row (not editing it) must also fail, with
# a message that says "deleted" rather than "modified" so the two failure
# modes are distinguishable.
ledger_delete_repo="$(init_repo ledger-delete)"
: >"${ledger_delete_repo}/docs/internal/measurements.jsonl"
commit_change "${ledger_delete_repo}" "delete the only existing ledger row"
expect_fail "${ledger_delete_repo}" "deleting an existing ledger row must fail even with no new claim"
rg -q "row '9999-known-row' was deleted" "${gate_err}" \
  || { printf 'expected failure message to name the deleted row\n' >&2; \
       sed -n '1,20p' "${gate_err}" >&2; exit 1; }

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
  printf 'A deadlock check ran clean: 0/10 trials failed (ledger:9999-known-row).\n'
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

# The mirror test's exemption is the second half of that case statement and was
# itself untested: the suite only ever runs against throwaway repos, never
# against this file, so deleting the mirror's `continue` arm survived the whole
# suite. That arm is load-bearing -- this file is full of claim-shaped fixture
# text like the "0/30 trials" strings above -- so without a case, the exemption
# that lets this branch commit its own tests had nothing proving it works.
self_edit_test_repo="$(init_repo self-edit-test)"
mkdir -p "${self_edit_test_repo}/scripts"
printf '%s\n' \
  '#!/bin/bash' \
  '# A mirror-test edit carrying fixture text: 0/30 trials failed, and a' \
  '# Measurement: line, both uncited -- exempt because this file IS the mirror.' \
  >"${self_edit_test_repo}/scripts/test-verify-measurement-citations.sh"
commit_change "${self_edit_test_repo}" "edit the mirror test itself"
expect_pass "${self_edit_test_repo}" "editing the gate's own mirror test must not trip the gate on its fixture text"

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

# The self-exemption must match ONLY the gate's own two files by exact path, not
# by prefix. A mutation widening the case-statement patterns from
# "scripts/verify-measurement-citations.sh)" to "scripts/verify-measurement-citations*)"
# (and the test-mirror equivalent) passed the entire suite above -- every
# existing case either targets the exact exempted filename or a file that
# shares no prefix with it, so none of them can tell an exact match from a
# glob. A real, unrelated file that merely shares the exempted script's name
# PREFIX -- a hypothetical v2 draft or backup left mid-refactor -- would then
# carry an uncited claim through silently.
nearmiss_repo="$(init_repo nearmiss)"
mkdir -p "${nearmiss_repo}/scripts"
printf '# Not the gate\n\nDrain retry probe: 3/40 trials failed.\n' \
  >"${nearmiss_repo}/scripts/verify-measurement-citations-v2-notes.md"
commit_change "${nearmiss_repo}" "add a same-prefix, non-exempt file with an uncited claim"
expect_fail "${nearmiss_repo}" "a file merely sharing the exempted script's name prefix must still be scanned"

# --- Case 9 (diff-header forgery): an added line whose own CONTENT reads
# "++ b/scripts/verify-measurement-citations.sh" renders in the raw diff as
# "+++ b/scripts/verify-measurement-citations.sh" -- indistinguishable BY
# CONTENT from a genuine file-header line. A parser that matches "+++ b/"
# anywhere in the stream would switch current_file to the exempt gate script
# at that point and silently exempt every claim after it, in ANY file, not
# just this one. The uncited claim on the next line must still be caught.
header_forge_repo="$(init_repo header-forge)"
printf '%s\n' \
  '++ b/scripts/verify-measurement-citations.sh' \
  'A deadlock check ran clean: 0/30 trials failed.' \
  >"${header_forge_repo}/docs/internal/evidence/bypass-attempt.md"
commit_change "${header_forge_repo}" "attempt diff-header forgery to smuggle an uncited claim"
expect_fail "${header_forge_repo}" "a forged +++ b/ header line in added content must not exempt the following uncited claim"

# --- Case 9b: a genuine claim line whose own content happens to START WITH
# "++ " (so its raw diff form is "+++ ...") must still be scanned as body
# content -- not skipped merely because it is header-SHAPED. This is the
# narrower residual of the same bug: a fix that special-cases skipping any
# line starting with "+++ " (rather than tracking header/body state
# positionally) would still let this exact line hide an uncited claim, even
# though it never touches current_file.
header_shaped_body_repo="$(init_repo header-shaped-body)"
printf '%s\n' '++ 0/30 trials failed with no citation' \
  >"${header_shaped_body_repo}/docs/internal/evidence/header-shaped.md"
commit_change "${header_shaped_body_repo}" "add a claim whose content is itself header-shaped"
expect_fail "${header_shaped_body_repo}" "a claim line that is merely header-SHAPED must still be scanned"

# A read failure on the ledger during id extraction must be a loud error
# (exit 2), never a silent "citation unknown" against a row that is actually
# present. The extraction pipeline used to swallow every failure with
# `|| true`; an unreadable ledger file then produced an EMPTY id list, and a
# perfectly valid, already-committed citation was reported as missing from
# the ledger -- indistinguishable from a genuinely uncited claim. This is
# not a hypothetical: it produced exactly that false report against a real
# push on this branch, non-reproducibly, and the root cause traced to a
# transient read failure under the extraction's swallowed error path.
#
# The failure is induced with a PATH shim rather than `chmod 000`: as UID 0
# (normal inside many dev containers) permission bits do not stop a process
# from reading its own files, so `chmod 000` silently fails to induce
# anything there and this test would wrongly report the verifier itself as
# broken. A shim that exits 2 whenever it is invoked against the ledger path
# behaves identically regardless of the invoking user -- the same fix as the
# GIT_DIR bug above: assert against a controlled mechanism, not an ambient
# environment condition.
real_rg="$(command -v rg)"
shim_dir="$(mktemp -d)"
cat >"${shim_dir}/rg" <<SHIM
#!/usr/bin/env bash
for arg in "\$@"; do
  case "\$arg" in
    *measurements.jsonl) exit 2 ;;
  esac
done
exec "${real_rg}" "\$@"
SHIM
chmod +x "${shim_dir}/rg"

unreadable_repo="$(init_repo unreadable)"
printf '# New Finding\n\nA deadlock check ran clean: 0/10 trials failed (ledger:9999-known-row).\n' \
  >"${unreadable_repo}/docs/internal/evidence/new-finding.md"
commit_change "${unreadable_repo}" "add a properly cited claim"
if PATH="${shim_dir}:${PATH}" \
    ESHU_MEASUREMENT_CITATIONS_REPO_ROOT="${unreadable_repo}" \
    ESHU_MEASUREMENT_CITATIONS_BASE=HEAD~1 \
    "${verifier}" >"${gate_out}" 2>"${gate_err}"; then
  printf '[unreadable ledger must fail loudly] expected the verifier to fail\n' >&2
  exit 1
fi
rg -q "failed to read .*measurements\.jsonl" "${gate_err}" \
  || { printf 'expected an explicit read-failure message, not a citation-unknown report\n' >&2; \
       sed -n '1,20p' "${gate_err}" >&2; exit 1; }
rg -q "cites ledger id" "${gate_err}" \
  && { printf 'read failure must not be reported as an unknown citation\n' >&2; exit 1; }

# --- Case 10 (repo-root under GIT_DIR): the verifier must derive repo_root
# from its own location, not `git rev-parse --show-toplevel`. Git hooks
# (pre-commit/pre-push) export GIT_DIR, and with GIT_DIR set
# `git -C scripts rev-parse --show-toplevel` returns <repo>/scripts instead of
# the repo root, so `${repo_root}/docs/internal/measurements.jsonl` resolves
# to a path that does not exist, silently emptying the ledger-id index and
# reporting an already-present, correctly cited row as unknown. This is not
# hypothetical: it produced exactly that false report against a real `git
# push` on this branch (GIT_DIR exported by the real hook process), while
# every manual reproduction of the same hook script (no GIT_DIR exported)
# passed -- the mismatch between "manual invocation passes" and "real push
# fails" was the whole symptom. Run a COPY of the verifier from the fixture's
# scripts/ with GIT_DIR set and ESHU_MEASUREMENT_CITATIONS_REPO_ROOT unset; it
# must resolve the fixture root and PASS. Mirrors
# test-verify-telemetry-coverage.sh's case 9 for the identical bug class.
case_gitdir="$(init_repo case-gitdir)"
mkdir -p "${case_gitdir}/scripts"
cp "${verifier}" "${case_gitdir}/scripts/verify-measurement-citations.sh"
printf '# New Finding\n\nA deadlock check ran clean: 0/10 trials failed (ledger:9999-known-row).\n' \
  >"${case_gitdir}/docs/internal/evidence/gitdir-finding.md"
git -C "${case_gitdir}" add .
git -C "${case_gitdir}" commit -q -m "copy verifier into fixture scripts, add cited claim"
if env -u ESHU_MEASUREMENT_CITATIONS_REPO_ROOT -u GITHUB_BASE_REF \
    GIT_DIR="${case_gitdir}/.git" ESHU_MEASUREMENT_CITATIONS_BASE=HEAD~1 \
    "${case_gitdir}/scripts/verify-measurement-citations.sh" \
    >"${gate_out}" 2>"${gate_err}"; then
  :
else
  printf '[resolves repo_root from script location under GIT_DIR] expected the verifier to pass\n' >&2
  sed -n '1,120p' "${gate_err}" >&2
  exit 1
fi

# Regression: with neither ESHU_MEASUREMENT_CITATIONS_BASE nor GITHUB_BASE_REF
# set -- the shape every test above bypasses by pinning HEAD~1 -- the base must
# be the merge base with origin/main. A HEAD~1 default scopes the gate to the
# last commit, so an uncited measurement claim added in an earlier commit
# escapes whenever the tip commit is innocuous. Gives the fixture a real
# origin/main by cloning it at the initial commit, then branching away.
case_mb="$(init_repo case-merge-base)"
git -C "${case_mb}" branch -M main
git clone -q --bare "${case_mb}" "${case_mb}-origin"
git -C "${case_mb}" remote add origin "${case_mb}-origin"
git -C "${case_mb}" fetch -q origin
git -C "${case_mb}" checkout -q -b feature
printf '# New Finding\n\nThe repro failed 7/30 trials.\n' \
  >"${case_mb}/docs/internal/evidence/uncited-finding.md"
git -C "${case_mb}" add .
git -C "${case_mb}" commit -q -m 'branch commit A: uncited trials claim'
printf '# readme\n' >"${case_mb}/README.md"
git -C "${case_mb}" add .
git -C "${case_mb}" commit -q -m 'branch commit B: readme touch'
if env -u ESHU_MEASUREMENT_CITATIONS_BASE -u GITHUB_BASE_REF \
    ESHU_MEASUREMENT_CITATIONS_REPO_ROOT="${case_mb}" "${verifier}" \
    >"${gate_out}" 2>"${gate_err}"; then
  printf '[merge-base local fallback] expected the gate to FAIL: the uncited\n' >&2
  printf 'claim is in an earlier commit and the tip commit is innocuous. A pass\n' >&2
  printf 'means the base fell back to HEAD~1 and scoped to the last commit.\n' >&2
  sed -n '1,120p' "${gate_out}" >&2
  exit 1
fi

# The widened window must not fire on a branch with no measurement claim.
case_mb_clean="$(init_repo case-merge-base-clean)"
git -C "${case_mb_clean}" branch -M main
git clone -q --bare "${case_mb_clean}" "${case_mb_clean}-origin"
git -C "${case_mb_clean}" remote add origin "${case_mb_clean}-origin"
git -C "${case_mb_clean}" fetch -q origin
git -C "${case_mb_clean}" checkout -q -b feature
printf '# docs only\n' >"${case_mb_clean}/README.md"
git -C "${case_mb_clean}" add .
git -C "${case_mb_clean}" commit -q -m 'branch commit A: docs only'
printf '# docs only, again\n' >"${case_mb_clean}/README.md"
git -C "${case_mb_clean}" add .
git -C "${case_mb_clean}" commit -q -m 'branch commit B: docs only'
if ! env -u ESHU_MEASUREMENT_CITATIONS_BASE -u GITHUB_BASE_REF \
    ESHU_MEASUREMENT_CITATIONS_REPO_ROOT="${case_mb_clean}" "${verifier}" \
    >"${gate_out}" 2>"${gate_err}"; then
  printf '[merge-base local fallback] expected a docs-only branch to PASS\n' >&2
  sed -n '1,120p' "${gate_err}" >&2
  exit 1
fi

printf 'verify-measurement-citations tests passed\n'
