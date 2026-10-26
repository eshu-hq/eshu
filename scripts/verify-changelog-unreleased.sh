#!/usr/bin/env bash
#
# verify-changelog-unreleased.sh - fail a CHANGELOG.md that a keep-both merge
# left broken (#9096, part of #9092).
#
# Every PR adds an entry under `## Unreleased`, and .gitattributes marks
# CHANGELOG.md `merge=union` so parallel PRs rebase without a conflict. Union
# keeps both sides without looking at them, so this gate catches what it can
# leave behind:
#
#   1. exactly one `## Unreleased` heading;
#   2. no git conflict marker (`<<<<<<<`, `=======`, `>>>>>>>` at line start);
#   3. no duplicate entry. An entry is a `- **Title.**` list item. The key is
#      the bold title joined across wrapped lines with runs of whitespace
#      collapsed, so a title wrapped over two lines is one key. A first-line
#      key is wrong both ways: it reports two different titles that share an
#      opening line, and it misses one title wrapped at two different columns;
#   4. no duplicate `### ` heading;
#   5. no unterminated code fence, which would hide every later check.
#
# Fenced code blocks (``` or ~~~) are skipped by checks 1 to 4, so a changelog
# entry may show a conflict marker or an example list as text.
#
# Usage: verify-changelog-unreleased.sh [path/to/CHANGELOG.md]
# The default is the CHANGELOG.md at the repository root. Exit 0 when clean,
# 1 when any rule fails (one line per failure on stderr), 2 on a usage error.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
file="${1:-${repo_root}/CHANGELOG.md}"

if [[ ! -f "${file}" ]]; then
  printf 'verify-changelog-unreleased: no such file: %s\n' "${file}" >&2
  exit 2
fi

awk -v file="${file}" '
function fail(msg) {
  printf "verify-changelog-unreleased: FAIL: %s: %s\n", file, msg > "/dev/stderr"
  failed = 1
}
function entry_closed(text,   t) {
  t = text
  sub(/^- \*\*/, "", t)
  return index(t, "**") > 0
}
function finish_entry(   key) {
  if (!pending) return
  pending = 0
  key = buf
  sub(/^- \*\*/, "", key)
  sub(/\*\*.*/, "", key)
  gsub(/[ \t]+/, " ", key)
  sub(/^ /, "", key)
  sub(/ $/, "", key)
  if (key in entry_line) {
    fail("duplicate entry \"" key "\" at lines " entry_line[key] " and " pending_line)
  } else {
    entry_line[key] = pending_line
    entries++
  }
}
/^ ? ? ?(```|~~~)/ {
  finish_entry()
  if (fenced) { fenced = 0 } else { fenced = 1; fence_line = NR }
  next
}
fenced { next }
{
  if (pending) {
    if ($0 ~ /^[ \t]*$/ || $0 ~ /^#/ || $0 ~ /^- /) {
      finish_entry()
    } else {
      buf = buf " " $0
      if (entry_closed(buf)) finish_entry()
    }
  }
}
/^(<<<<<<<|>>>>>>>)( |$)/ || /^=======$/ { fail("conflict marker at line " NR) }
/^## Unreleased([ \t]|$)/ { unreleased++ }
/^### / {
  hkey = $0
  sub(/[ \t]+$/, "", hkey)
  if (hkey in heading_line) {
    fail("duplicate heading \"" hkey "\" at lines " heading_line[hkey] " and " NR)
  } else {
    heading_line[hkey] = NR
    headings++
  }
}
/^- \*\*/ {
  buf = $0
  pending_line = NR
  pending = 1
  if (entry_closed(buf)) finish_entry()
}
END {
  finish_entry()
  if (fenced) fail("unterminated code fence opened at line " fence_line)
  if (unreleased != 1) fail("expected exactly one '"'"'## Unreleased'"'"' heading, found " unreleased + 0)
  if (failed) exit 1
  printf "verify-changelog-unreleased: OK: %s (%d entry titles, %d ### headings)\n", file, entries, headings
}
' "${file}"
