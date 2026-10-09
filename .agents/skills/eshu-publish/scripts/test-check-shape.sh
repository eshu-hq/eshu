#!/usr/bin/env bash
#
# test-check-shape.sh - hermetic tests for check-shape.sh, the advisory shape
# check that an agent runs on a PR body or issue draft before it publishes.
#
# Four fixtures are real texts. pr-bad.md is the body of PR #7838 as filed
# (old shape). pr-good.md is the same content in the eshu-publish shape.
# issue-bad.md and issue-good.md are issue #7778 before and after. Every other
# case seeds exactly one violation into a good fixture, so each rule is proven
# to fail on its own and to pass on the clean text.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
checker="${here}/check-shape.sh"
fixtures="${here}/testdata"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

pass=0
fail=0

# expect <name> <want-exit> <want-substring-or-empty> <mode> <file>
expect() {
  local name="$1" want_rc="$2" want_text="$3" mode="$4" file="$5" out rc
  set +e
  out="$(bash "${checker}" "--${mode}" "${file}" 2>&1)"
  rc=$?
  set -e
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

command -v rg >/dev/null 2>&1 || { echo "test-check-shape: rg is required" >&2; exit 2; }
[ -f "${checker}" ] || { echo "FAIL checker missing: ${checker}"; exit 1; }

# Clean text passes, old-shape text fails.
expect "pr-good passes"        0 ""                  pr    "${fixtures}/pr-good.md"
expect "pr-bad fails"          1 "FAIL"             pr    "${fixtures}/pr-bad.md"
expect "issue-good passes"     0 ""                  issue "${fixtures}/issue-good.md"
expect "issue-bad fails"       1 "FAIL"             issue "${fixtures}/issue-bad.md"

# One seeded violation per rule, each cut from the good fixture.
sed '1d' "${fixtures}/pr-good.md" > "${tmp}/no-ref.md"
expect "pr: missing Refs line"    1 "FAIL ref"        pr "${tmp}/no-ref.md"

sed -e 's/^\*\*\(.*\)\*\*$/\1/' "${fixtures}/pr-good.md" > "${tmp}/no-lead.md"
expect "pr: missing bold lead"    1 "FAIL lead"       pr "${tmp}/no-lead.md"

rg -v -F -e '| At a glance |' -e '|---|---|' -e '| Behavior change |' -e '| Size |' -e '| Proof |' -e '| Review |' \
  "${fixtures}/pr-good.md" > "${tmp}/no-glance.md" || true
expect "pr: missing glance table" 1 "FAIL glance"      pr "${tmp}/no-glance.md"

{ cat "${fixtures}/pr-good.md"; echo; printf 'Filler sentence for length. %.0s' $(seq 1 30); echo; } > "${tmp}/long-para.md"
expect "pr: paragraph over 600"   1 "FAIL paragraph"  pr "${tmp}/long-para.md"

rg -v -F -e '<details>' -e '</details>' "${fixtures}/pr-good.md" > "${tmp}/det.md" || true
{ cat "${tmp}/det.md"; echo; printf -- '- line %s\n' $(seq 1 14); } > "${tmp}/long-list.md"
expect "pr: 14-line list in the open" 0 "WARN block" pr "${tmp}/long-list.md"

rg -v -F '## Problem' "${fixtures}/issue-good.md" > "${tmp}/no-problem.md" || true
expect "issue: missing Problem"   1 "FAIL problem"    issue "${tmp}/no-problem.md"

rg -v -F '## Acceptance criteria' "${fixtures}/issue-good.md" > "${tmp}/no-accept.md" || true
expect "issue: missing Acceptance" 1 "FAIL acceptance" issue "${tmp}/no-accept.md"

# A fenced block and a details block are data, not prose: a long line inside
# them must not trip the paragraph rule.
{ cat "${fixtures}/pr-good.md"; echo; echo '```text'; printf 'x%.0s' $(seq 1 900); echo; echo '```'; } > "${tmp}/fenced.md"
expect "pr: long line inside a fence is fine" 0 "" pr "${tmp}/fenced.md"

# A Mermaid fence is a picture, not data: a long diagram gets no WARN.
{ cat "${fixtures}/pr-good.md"; echo; echo '```mermaid'; echo 'flowchart LR'; printf '  A%s --> B%s\n' $(seq 1 14 | sed 's/.*/& &/'); echo '```'; } > "${tmp}/diagram.md"
set +e; out="$(bash "${checker}" --pr "${tmp}/diagram.md" 2>&1)"; set -e
if printf '%s' "${out}" | rg -q 'WARN block'; then echo "FAIL pr: long mermaid fence got a WARN"; fail=$((fail + 1)); else echo "ok   pr: long mermaid fence gets no WARN"; pass=$((pass + 1)); fi

# The documented templates and worked examples must pass the checker they
# point to, so the docs cannot drift from the rules. Each 4-backtick markdown
# block is cut out into its own file; the placeholder issue number is filled.
refs="$(cd "${here}/../references" && pwd)"
cut_blocks() { # cut_blocks <markdown-file> <out-prefix>
  awk -v prefix="$2" '
    /^````markdown/ { k++; out = prefix k ".md"; inblock = 1; next }
    /^````$/ { inblock = 0; next }
    inblock { sub(/#NNNN/, "#1234"); print > out }
  ' "$1"
}
cut_blocks "${refs}/pr-templates.md" "${tmp}/tpl-"
tpl_count=0
for f in "${tmp}"/tpl-*.md; do
  tpl_count=$((tpl_count + 1))
  expect "template block ${tpl_count} of pr-templates.md passes" 0 "" pr "${f}"
done
if [ "${tpl_count}" -ne 4 ]; then echo "FAIL expected 4 PR templates, found ${tpl_count}"; fail=$((fail + 1)); fi
cut_blocks "${refs}/pull-request.md" "${tmp}/ex-pr-"
expect "worked example in pull-request.md passes" 0 "" pr "${tmp}/ex-pr-1.md"
cut_blocks "${refs}/issue.md" "${tmp}/ex-issue-"
expect "worked example in issue.md passes" 0 "" issue "${tmp}/ex-issue-1.md"

# Seeded drift: a template with its glance table removed must fail.
rg -v -F -e '| At a glance |' -e '|---|---|' -e '| Broken |' -e '| Fix |' -e '| Risk |' "${tmp}/tpl-1.md" > "${tmp}/tpl-drift.md" || true
expect "seeded drift: template without a glance table fails" 1 "FAIL glance" pr "${tmp}/tpl-drift.md"

echo
echo "test-check-shape: ${pass} passed, ${fail} failed"
[ "${fail}" -eq 0 ]
