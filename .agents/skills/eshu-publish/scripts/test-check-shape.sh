#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# test-check-shape.sh - hermetic tests for check-shape.sh, the advisory shape
# check that an agent runs on a PR body or issue draft before it publishes.
#
# Four fixtures are real texts. pr-bad.md is the body of PR #7838 as filed
# (old shape). pr-good.md is the same content in the eshu-publish shape.
# issue-bad.md and issue-good.md are issue #7778 before and after. Every other
# case changes one thing in a good fixture, so each rule is proven to fire on
# its own, each boundary is pinned on both sides, and each false FAIL that a
# reviewer reproduced stays fixed. The cases were chosen by mutating the script:
# a change to any rule line must make at least one case fail.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
checker="${here}/check-shape.sh"
fixtures="${here}/testdata"
good="${fixtures}/pr-good.md"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

pass=0
fail=0

run() { # run <mode> <file>: prints output, then the exit code on the last line
  local out rc
  set +e
  out="$(bash "${checker}" "--$1" "$2" 2>&1)"
  rc=$?
  set -e
  printf '%s\n%s' "${out}" "${rc}"
}

# expect <name> <want-exit> <must-contain-or-empty> <mode> <file>
expect() {
  local name="$1" want_rc="$2" want_text="$3" mode="$4" file="$5" res rc out
  res="$(run "${mode}" "${file}")"
  rc="${res##*$'\n'}"
  out="${res%$'\n'*}"
  if [ "${rc}" -ne "${want_rc}" ]; then
    echo "FAIL ${name}: exit ${rc}, want ${want_rc}"
    echo "${out}" | sed 's/^/    /'
    fail=$((fail + 1))
    return
  fi
  if [ -n "${want_text}" ] && ! printf '%s' "${out}" | rg -q -F -- "${want_text}"; then
    echo "FAIL ${name}: output lacks '${want_text}'"
    echo "${out}" | sed 's/^/    /'
    fail=$((fail + 1))
    return
  fi
  echo "ok   ${name}"
  pass=$((pass + 1))
}

# expect_not <name> <want-exit> <must-not-contain> <mode> <file>
expect_not() {
  local name="$1" want_rc="$2" bad_text="$3" mode="$4" file="$5" res rc out
  res="$(run "${mode}" "${file}")"
  rc="${res##*$'\n'}"
  out="${res%$'\n'*}"
  if [ "${rc}" -ne "${want_rc}" ] || printf '%s' "${out}" | rg -q -F -- "${bad_text}"; then
    echo "FAIL ${name}: exit ${rc} (want ${want_rc}), or output has '${bad_text}'"
    echo "${out}" | sed 's/^/    /'
    fail=$((fail + 1))
    return
  fi
  echo "ok   ${name}"
  pass=$((pass + 1))
}

ascii() { printf '%*s' "$1" '' | tr ' ' 'a'; } # ascii <n>: n letters, no spaces
words() { seq 1 "$1" | sed 's/^/w/' | paste -sd' ' -; } # words <n>: n words, no trailing space
with() { { cat "${good}"; printf '\n'; cat; } > "${tmp}/$1.md"; } # with <name> < extra text

command -v rg >/dev/null 2>&1 || { echo "test-check-shape: rg is required" >&2; exit 2; }
[ -f "${checker}" ] || { echo "FAIL checker missing: ${checker}"; exit 1; }

# --- Real texts: clean passes, old shape fails.
expect "pr-good passes"        0 ""      pr    "${good}"
expect "pr-bad fails"          1 "FAIL"  pr    "${fixtures}/pr-bad.md"
expect "issue-good passes"     0 ""      issue "${fixtures}/issue-good.md"
expect "issue-bad fails"       1 "FAIL"  issue "${fixtures}/issue-bad.md"

# --- One rule at a time.
sed '1d' "${good}" > "${tmp}/no-ref.md"
expect "ref: missing Refs line"        1 "FAIL ref"   pr "${tmp}/no-ref.md"
sed '1s/#6950/#/' "${good}" > "${tmp}/ref-no-number.md"
expect "ref: Refs with no number"      1 "FAIL ref"   pr "${tmp}/ref-no-number.md"
sed '1c\Refs eshu-hq/eshu#7033.' "${good}" > "${tmp}/ref-cross-repo.md"
expect "ref: qualified GitHub issue passes" 0 "OK shape" pr "${tmp}/ref-cross-repo.md"
sed '1c\Refs eshu-hq/eshu#.' "${good}" > "${tmp}/ref-cross-repo-no-number.md"
expect "ref: qualified issue needs a number" 1 "FAIL ref" pr "${tmp}/ref-cross-repo-no-number.md"
sed '1c\Refs eshu-hq/#7033.' "${good}" > "${tmp}/ref-cross-repo-no-repo.md"
expect "ref: qualified issue needs a repository" 1 "FAIL ref" pr "${tmp}/ref-cross-repo-no-repo.md"
sed '1c\Refs /eshu#7033.' "${good}" > "${tmp}/ref-cross-repo-no-owner.md"
expect "ref: qualified issue needs an owner" 1 "FAIL ref" pr "${tmp}/ref-cross-repo-no-owner.md"
sed '1c\Refs eshu-hq/eshu#7033' "${good}" > "${tmp}/ref-cross-repo-no-stop.md"
expect "ref: qualified issue needs a stop" 1 "FAIL ref" pr "${tmp}/ref-cross-repo-no-stop.md"
sed '1c\Fixes eshu-hq/eshu#7033.' "${good}" > "${tmp}/ref-cross-repo-close.md"
expect "ref: qualified closing reference is not in scope" 1 "FAIL ref" pr "${tmp}/ref-cross-repo-close.md"

sed -e 's/^\*\*\(.*\)\*\*$/\1/' "${good}" > "${tmp}/no-lead.md"
expect "lead: missing bold lead"       1 "FAIL lead"  pr "${tmp}/no-lead.md"
sed '3s/\*\*$//' "${good}" > "${tmp}/lead-open.md"
expect "lead: bold start, plain end"   1 "FAIL lead"  pr "${tmp}/lead-open.md"
printf 'Refs #1.\n\n## Problem\n\n**A bold line after a heading is not the lead.**\n' > "${tmp}/lead-late.md"
expect "lead: bold paragraph after a heading" 1 "FAIL lead" pr "${tmp}/lead-late.md"
{ sed '1,2!d' "${good}"; printf '**%s**\n\n**Short bold.**\n\n' "$(words 46)"; sed '1,4d' "${good}"; } > "${tmp}/lead-long.md"
expect "lead: 46 words warns"          0 "WARN lead"  pr "${tmp}/lead-long.md"
{ sed '1,2!d' "${good}"; printf '**%s**\n\n' "$(words 45)"; sed '1,4d' "${good}"; } > "${tmp}/lead-45.md"
expect_not "lead: 45 words is fine"    0 "WARN lead"  pr "${tmp}/lead-45.md"

rg -v -F -e '| At a glance |' -e '|---|---|' -e '| Behavior change |' -e '| Size |' -e '| Proof |' -e '| Review |' \
  "${good}" > "${tmp}/no-glance.md" || true
expect "glance: missing table fails"   1 "FAIL glance" pr "${tmp}/no-glance.md"
printf 'Refs #1.\n\n**Fix a typo in the README.**\n' > "${tmp}/tiny.md"
expect "glance: tiny PR only warns"    0 "WARN glance" pr "${tmp}/tiny.md"

rg -v -F '## Proof' "${good}" > "${tmp}/one-heading.md" || true
expect "headings: long body, one heading warns" 0 "WARN headings" pr "${tmp}/one-heading.md"
expect "notchecked: absent warns"      0 "WARN notchecked" pr "${good}"
printf '**NOT_CHECKED:** hosted CI.\n' | with has-notchecked
expect_not "notchecked: present is quiet" 0 "WARN notchecked" pr "${tmp}/has-notchecked.md"

rg -v -F '## Problem' "${fixtures}/issue-good.md" > "${tmp}/no-problem.md" || true
expect "issue: missing Problem"        1 "FAIL problem"    issue "${tmp}/no-problem.md"
rg -v -F '## Acceptance criteria' "${fixtures}/issue-good.md" > "${tmp}/no-accept.md" || true
expect "issue: missing Acceptance"     1 "FAIL acceptance" issue "${tmp}/no-accept.md"

# --- The paragraph boundary, pinned on both sides (bytes: warn above 600, fail above 800).
ascii 600 | with p600;  expect_not "paragraph: 600 is quiet"  0 "paragraph" pr "${tmp}/p600.md"
ascii 601 | with p601;  expect "paragraph: 601 warns"          0 "WARN paragraph" pr "${tmp}/p601.md"
ascii 800 | with p800;  expect_not "paragraph: 800 does not fail" 0 "FAIL" pr "${tmp}/p800.md"
ascii 801 | with p801;  expect "paragraph: 801 fails"          1 "FAIL paragraph" pr "${tmp}/p801.md"
{ printf '### Sub\n'; ascii 840; printf '\n'; } | with h3-para
expect "paragraph: text under a ### heading is measured" 1 "FAIL paragraph" pr "${tmp}/h3-para.md"

# --- Data is not prose: fences and <details> hide long lines.
{ printf '```text\n'; ascii 900; printf '\n```\n'; } | with fence
expect "data: long line in a fence"      0 "" pr "${tmp}/fence.md"
{ printf '<details>\n<summary>x</summary>\n\n'; ascii 900; printf '\n\n</details>\n'; } | with details
expect "data: long line in <details>"    0 "" pr "${tmp}/details.md"
{ printf '<details open>\n<summary>x</summary>\n\n'; ascii 900; printf '\n\n</details>\n'; } | with details-open
expect "data: long line in <details open>" 0 "" pr "${tmp}/details-open.md"
{ printf '<details><summary>x</summary>\n\n'; ascii 900; printf '\n\n</details>\n'; } | with details-oneline
expect "data: one-line <details><summary>" 0 "" pr "${tmp}/details-oneline.md"
{ printf -- '- step\n\n    ```bash\n'; ascii 900; printf '\n    ```\n'; } | with fence-indent
expect "data: fence indented under a list" 0 "" pr "${tmp}/fence-indent.md"
{ printf '~~~text\n'; ascii 900; printf '\n~~~\n'; } | with fence-tilde
expect "data: ~~~ fence"                 0 "" pr "${tmp}/fence-tilde.md"
{ printf '````markdown\n```text\n'; ascii 900; printf '\n```\n````\n'; } | with fence-nested
expect "data: 4-backtick fence around a 3-backtick fence" 0 "" pr "${tmp}/fence-nested.md"
# A fence ends only at a marker of the same character, at least as long, with no
# text after it. A long paragraph after the fence must still be measured.
{ printf '````markdown\n```text\n'; ascii 900; printf '\n```\n````\n\n'; ascii 900; printf '\n'; } | with fence-nested-after
expect "fence: a shorter inner fence does not end a longer one" 1 "FAIL paragraph" pr "${tmp}/fence-nested-after.md"
{ printf '~~~text\n'; ascii 900; printf '\n```\n'; ascii 900; printf '\n~~~\n'; } | with fence-mixed
expect "fence: a backtick line does not end a ~~~ fence" 0 "" pr "${tmp}/fence-mixed.md"
{ printf '```\n```text\n'; ascii 900; printf '\n```\n'; } | with fence-info
expect "fence: a marker with text after it does not close" 0 "" pr "${tmp}/fence-info.md"
{ printf '<details><summary>x</summary>y</details>\n\n'; ascii 900; printf '\n'; } | with details-closed-line
expect "details: a one-line open-and-close block hides nothing" 1 "FAIL paragraph" pr "${tmp}/details-closed-line.md"
{ sed '1,2!d' "${good}"; printf '**%s**\n\n' "$(words 20)"; ascii 650; printf '\n'; } > "${tmp}/long-no-glance.md"
expect "glance: a 650-byte body with no heading still needs the table" 1 "FAIL glance" pr "${tmp}/long-no-glance.md"
{ printf '> '; ascii 900; printf '\n'; } | with quote
expect "data: long blockquote line is not a paragraph" 0 "" pr "${tmp}/quote.md"

# --- Long data left in the open warns; collapsed data does not.
{ printf '```text\n'; seq 1 11; printf '```\n'; } | with fence11
expect "block: 11-line fence in the open warns" 0 "WARN block" pr "${tmp}/fence11.md"
{ printf '<details>\n<summary>x</summary>\n\n```text\n'; seq 1 11; printf '```\n\n</details>\n'; } | with fence11-in-details
expect_not "block: 11-line fence in <details> is quiet" 0 "WARN block" pr "${tmp}/fence11-in-details.md"
{ printf '```mermaid\nflowchart LR\n'; seq 1 11 | sed 's/.*/  A& --> B&/'; printf '```\n'; } | with diagram
expect_not "block: long Mermaid fence is quiet" 0 "WARN block" pr "${tmp}/diagram.md"
seq 1 14 | sed 's/^/- item /' | with list14
expect "block: 14-line list in the open warns" 0 "WARN block" pr "${tmp}/list14.md"
# A list may follow its intro line with no blank line (CommonMark and GitHub render it as a list).
{ echo 'What changed:'; for i in $(seq 1 11); do echo "- Item ${i}: the verb, the object, and the why, in about one sentence of text here."; done; } | with intro-list11
expect_not "list: bullets right after an intro line are not one paragraph" 0 "paragraph" pr "${tmp}/intro-list11.md"
expect "list: 11 bullets after an intro line still warn as a list" 0 "WARN block" pr "${tmp}/intro-list11.md"
{ echo 'Steps:'; echo '1. one'; echo '2. two'; echo 'Done.'; } | with intro-numbered
expect_not "list: a numbered list right after an intro line is quiet" 0 "paragraph" pr "${tmp}/intro-numbered.md"
seq 1 14 | sed 's/^/+ item /' | with plus14
expect "list: + markers count as a list, not prose" 0 "WARN block" pr "${tmp}/plus14.md"
seq 1 14 | sed 's/^/* item /' | with star14
expect "list: * markers count as a list, not prose" 0 "WARN block" pr "${tmp}/star14.md"
seq 1 14 | sed 's/$/) item/' | with paren14
expect "list: 1) markers count as a list, not prose" 0 "WARN block" pr "${tmp}/paren14.md"

# --- Inputs that look different but are good text.
sed '3s/$/ /' "${good}" > "${tmp}/lead-trailing-space.md"
expect "input: trailing space after the lead" 0 "" pr "${tmp}/lead-trailing-space.md"
sed 's/$/'"$(printf '\r')"'/' "${good}" > "${tmp}/crlf.md"
expect "input: CRLF line endings"        0 "OK shape" pr "${tmp}/crlf.md"
{ ascii 500; for _ in $(seq 1 50); do printf '\xe2\x80\x94'; done; printf '\n'; } | with non-ascii
expect "input: 550 characters of which 50 are em dashes" 0 "" pr "${tmp}/non-ascii.md"

# --- Unfilled templates and unclosed blocks.
refs="$(cd "${here}/../references" && pwd)"
cut_blocks() { # cut_blocks <markdown-file> <out-prefix>
  awk -v prefix="$2" '
    /^````markdown/ { k++; out = prefix k ".md"; inblock = 1; next }
    /^````$/ { inblock = 0; next }
    inblock { sub(/#NNNN/, "#1234"); print > out }
  ' "$1"
}
cut_blocks "${refs}/pr-templates.md" "${tmp}/tpl-"
expect "placeholder: unfilled template warns" 0 "WARN placeholder" pr "${tmp}/tpl-1.md"
expect_not "placeholder: filled text is quiet" 0 "WARN placeholder" pr "${good}"
printf 'Every placeholder starts with `REPLACE:`.\n' | with placeholder-mention
expect_not "placeholder: a mention in inline code is quiet" 0 "WARN placeholder" pr "${tmp}/placeholder-mention.md"
printf '```text\nnever closed\n' | with unclosed-fence
expect "fence: unclosed fence warns"     0 "WARN fence" pr "${tmp}/unclosed-fence.md"
printf '<details>\n<summary>x</summary>\n' | with unclosed-details
expect "details: unclosed block warns"   0 "WARN details" pr "${tmp}/unclosed-details.md"

# --- The documented templates and worked examples must pass the checker they point to.
tpl_count=0
for f in "${tmp}"/tpl-*.md; do
  tpl_count=$((tpl_count + 1))
  expect "template ${tpl_count} of pr-templates.md passes" 0 "" pr "${f}"
done
if [ "${tpl_count}" -ne 4 ]; then echo "FAIL expected 4 PR templates, found ${tpl_count}"; fail=$((fail + 1)); fi
cut_blocks "${refs}/pull-request.md" "${tmp}/ex-pr-"
expect "worked example in pull-request.md passes" 0 "" pr "${tmp}/ex-pr-1.md"
cut_blocks "${refs}/issue.md" "${tmp}/ex-issue-"
expect "worked example in issue.md passes" 0 "" issue "${tmp}/ex-issue-1.md"
rg -v -F -e '| At a glance |' -e '|---|---|' -e '| Broken |' -e '| Fix |' -e '| Risk |' "${tmp}/tpl-1.md" > "${tmp}/tpl-drift.md" || true
expect "seeded drift: template without a glance table fails" 1 "FAIL glance" pr "${tmp}/tpl-drift.md"

# --- Usage.
set +e; bash "${checker}" >/dev/null 2>&1; rc=$?; set -e
if [ "${rc}" -eq 2 ]; then echo "ok   usage: no file exits 2"; pass=$((pass + 1)); else echo "FAIL usage: no file exited ${rc}, want 2"; fail=$((fail + 1)); fi
expect "usage: --help prints the header" 0 "Usage: check-shape.sh" pr "--help"

echo
echo "test-check-shape: ${pass} passed, ${fail} failed"
[ "${fail}" -eq 0 ]
