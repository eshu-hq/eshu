#!/usr/bin/env bash
#
# test-verify-required-doc-sections.sh - hermetic RED/GREEN tests for
# scripts/verify-required-doc-sections.sh (issue #9028).
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-required-doc-sections.sh"
tab=$'\t'
page_rel="docs/public/languages/admitted-files.md"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fail() {
  echo "test-verify-required-doc-sections: $*" >&2
  exit 1
}

# run_verifier <root> <list>: captures combined output in ${out} and the exit
# code in ${rc}.
run_verifier() {
  rc=0
  out="$(ESHU_REQUIRED_DOC_SECTIONS_ROOT="$1" ESHU_REQUIRED_DOC_SECTIONS_LIST="$2" \
    bash "${verifier}" 2>&1)" || rc=$?
}

expect_fail() {
  local name="$1"
  shift
  [[ "${rc}" -ne 0 ]] || fail "${name}: expected a failure, got exit 0: ${out}"
  local needle
  for needle in "$@"; do
    [[ "${out}" == *"${needle}"* ]] || fail "${name}: output missing '${needle}': ${out}"
  done
}

write_list() {
  printf '%s\n' "$2" >"$1"
}

# GREEN: the committed list passes on the real repository.
run_verifier "${repo_root}" "${repo_root}/scripts/required-doc-sections.txt"
[[ "${rc}" -eq 0 ]] || fail "real repository: expected exit 0, got ${rc}: ${out}"

# The committed list must protect the Content store policy section.
rg -q --fixed-strings "${page_rel}${tab}## Content store policy" \
  "${repo_root}/scripts/required-doc-sections.txt" ||
  fail "committed list does not name '## Content store policy' for ${page_rel}"

# RED: a copy of the real page without the section fails and names both.
seeded="${tmp_root}/seeded"
mkdir -p "${seeded}/$(dirname "${page_rel}")"
awk '
  /^## Content store policy$/ { skip = 1; next }
  skip && /^## / { skip = 0 }
  !skip { print }
' "${repo_root}/${page_rel}" >"${seeded}/${page_rel}"
if cmp -s "${repo_root}/${page_rel}" "${seeded}/${page_rel}"; then
  fail "seed did not remove the section from the real page"
fi
run_verifier "${seeded}" "${repo_root}/scripts/required-doc-sections.txt"
expect_fail "section removed" "## Content store policy" "${page_rel}"

# A heading that only appears inside a fenced code block does not count.
fenced="${tmp_root}/fenced"
mkdir -p "${fenced}/docs"
printf '%s\n' '# Page' '' '```markdown' '## Needed' '```' >"${fenced}/docs/p.md"
write_list "${tmp_root}/fenced.txt" "docs/p.md${tab}## Needed"
run_verifier "${fenced}" "${tmp_root}/fenced.txt"
expect_fail "heading only in fence" "## Needed" "docs/p.md"

# A real heading outside the fence passes; comments and blank lines are skipped.
printf '%s\n' '# Page' '' '## Needed' '' 'body' >"${fenced}/docs/p.md"
printf '%s\n' '# comment' '' "docs/p.md${tab}## Needed" >"${tmp_root}/ok.txt"
run_verifier "${fenced}" "${tmp_root}/ok.txt"
[[ "${rc}" -eq 0 ]] || fail "heading present: expected exit 0, got ${rc}: ${out}"

# A heading that merely starts with the required text does not satisfy it.
printf '%s\n' '# Page' '## Needed more' >"${fenced}/docs/p.md"
run_verifier "${fenced}" "${tmp_root}/ok.txt"
expect_fail "prefix-only heading" "## Needed" "docs/p.md"

# page_case <name> <want> <heading> <page lines...>: writes the page, lists the
# heading, and expects pass (want=ok) or failure (want=fail).
page_case() {
  local name="$1" want="$2" heading="$3"
  shift 3
  printf '%s\n' "$@" >"${fenced}/docs/p.md"
  write_list "${tmp_root}/case.txt" "docs/p.md${tab}${heading}"
  run_verifier "${fenced}" "${tmp_root}/case.txt"
  if [[ "${want}" == ok ]]; then
    [[ "${rc}" -eq 0 ]] || fail "${name}: expected exit 0, got ${rc}: ${out}"
  else
    expect_fail "${name}" "${heading}" "docs/p.md"
  fi
}

# Backslashes in a heading match literally.
page_case "backslash heading" ok '## a\b' '# P' '## a\b'
page_case "backslash heading absent" fail '## a\b' '# P' '## ab'

# Fence rules follow CommonMark: the closer needs the same character and at
# least the opener's length; up to three spaces of indent still opens a fence.
page_case "tilde block holding backticks" fail '## H' '~~~' '```' '## H' '~~~'
page_case "four-backtick block holding three" fail '## H' '````' '```' '## H' '```' '````'
page_case "indented fence" fail '## H' '   ```' '## H' '   ```'
page_case "closer indented four spaces does not close" fail '## H' '```' '    ```' '## H'
page_case "heading after a closed longer fence" ok '## H' '````' '```' '````' '## H'
page_case "heading after a closed tilde fence" ok '## H' '~~~' 'x' '~~~' '## H'

# A heading inside an HTML comment does not count.
page_case "heading in one-line comment" fail '## H' '<!-- ## H -->'
page_case "heading in multi-line comment" fail '## H' '<!--' '## H' '-->'
page_case "heading after a closed comment" ok '## H' '<!-- note -->' '## H'

# A backtick opener whose info string holds a backtick is not a fence.
page_case "backtick info string with backtick" ok '## H' '```code``` text' '## H'
page_case "tilde info string with backtick still fences" fail '## H' '~~~ a`b' '## H'
# An indented-code line (4+ spaces) is not a fence opener.
page_case "four-space indented opener is not a fence" ok '## H' '    ```' '## H'
# A comment opener inside an inline code span is not a comment.
page_case "comment opener in code span" ok '## H' 'use `<!--` to open a comment' '## H'
page_case "comment opener in double-backtick span" ok '## H' 'use ``<!-- x`` to open' '## H'
# A trailing comment on the heading line leaves the heading.
page_case "heading with trailing comment" ok '## H' '## H <!-- note -->'
page_case "text after a closed comment is kept" ok '## H' '## <!-- c -->H'
# A multi-line comment closes at the first --> and the heading after it counts.
page_case "multi-line comment, heading after" ok '## H' '<!--' 'one' 'two' '-->' '## H'
page_case "multi-line comment swallows inner lines" fail '## H' '<!--' 'one' '## H' 'two' '-->'
# A fence closer with trailing spaces or CR still closes.
page_case "closer with trailing spaces" ok '## H' '```' 'x' '```   ' '## H'
printf '# P\r\n```\r\nx\r\n```\r\n## H\r\n' >"${fenced}/docs/p.md"
write_list "${tmp_root}/case.txt" "docs/p.md${tab}## H"
run_verifier "${fenced}" "${tmp_root}/case.txt"
[[ "${rc}" -eq 0 ]] || fail "CRLF fence closer: expected exit 0, got ${rc}: ${out}"

# Trailing spaces and CRLF on the page, and trailing spaces on the list entry,
# are ignored.
printf '# P\r\n## H  \r\n' >"${fenced}/docs/p.md"
write_list "${tmp_root}/case.txt" "docs/p.md${tab}## H   "
run_verifier "${fenced}" "${tmp_root}/case.txt"
[[ "${rc}" -eq 0 ]] || fail "CRLF and trailing spaces: expected exit 0, got ${rc}: ${out}"

# Every missing heading is reported, not only the first.
printf '%s\n' '# Page' >"${fenced}/docs/p.md"
printf '%s\n' "docs/p.md${tab}## One" "docs/p.md${tab}## Two" >"${tmp_root}/two.txt"
run_verifier "${fenced}" "${tmp_root}/two.txt"
expect_fail "two missing" "## One" "## Two"

# A listed page that does not exist fails.
write_list "${tmp_root}/nopage.txt" "docs/nope.md${tab}## X"
run_verifier "${fenced}" "${tmp_root}/nopage.txt"
expect_fail "missing page" "docs/nope.md" "page not found"

# A malformed line (no tab) fails closed.
write_list "${tmp_root}/bad.txt" "docs/p.md ## X"
run_verifier "${fenced}" "${tmp_root}/bad.txt"
expect_fail "malformed line" "malformed"

# An empty list fails closed so a truncated list cannot pass silently.
: >"${tmp_root}/empty.txt"
run_verifier "${fenced}" "${tmp_root}/empty.txt"
expect_fail "empty list" "no entries"

# A missing list file fails.
run_verifier "${fenced}" "${tmp_root}/absent.txt"
expect_fail "missing list" "absent.txt"

echo "test-verify-required-doc-sections: pass"
