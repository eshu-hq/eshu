#!/usr/bin/env bash
#
# check-shape.sh - advisory shape check for a PR body or issue draft.
#
# Usage: check-shape.sh [--pr|--issue] <file>
#
# Run it on the exact text you are about to publish. It checks the shape rules
# of the eshu-publish skill, not the wording; use ste-lint.py for wording.
#
#   FAIL lines break a rule that makes the text hard to read. Fix them.
#   WARN lines are advice. Judge them.
#
# Exit 0: no FAIL. Exit 1: at least one FAIL. Exit 2: bad usage.
#
# Prose is any block of lines that is not a list, a table, a heading, a fenced
# block, or inside <details>. Data in a fence or in <details> never trips the
# paragraph rule: that is where long data belongs.
set -euo pipefail

mode=pr
file=""
for arg in "$@"; do
  case "${arg}" in
    --pr) mode=pr ;;
    --issue) mode=issue ;;
    -h | --help)
      sed -n '3,16p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) file="${arg}" ;;
  esac
done
if [ -z "${file}" ] || [ ! -f "${file}" ]; then
  echo "usage: check-shape.sh [--pr|--issue] <file>" >&2
  exit 2
fi

awk -v mode="${mode}" '
function fail(rule, msg) { print "FAIL " rule ": " msg; fails++ }
function warn(rule, msg) { print "WARN " rule ": " msg }
function flush(   first, words, w) {
  if (n > 0) {
    first = firstline
    if (first ~ /^(\||- |\* |[0-9]+\. |#|<|>)/) {
      if (nlist > 10) warn("block", "a list or table of " nlist " lines sits outside <details>; collapse it")
    } else {
      if (length(buf) > 600) fail("paragraph", length(buf) " characters in one paragraph, over 600; it starts: " substr(buf, 1, 50))
      if (!leadseen && !seenh2 && first ~ /^\*\*/ && buf ~ /\*\*$/) {
        leadseen = 1
        leadwords = split(buf, w, " ")
      }
    }
  }
  n = 0; nlist = 0; buf = ""; firstline = ""
}
{
  line = $0
  sub(/\r$/, "", line)
  if (!started && line !~ /^[ \t]*$/) { started = 1; firstreal = line }
  if (line ~ /^```/) {
    if (!infence) { flush(); infence = 1; fencelines = 0; diagram = (line ~ /^```mermaid/) }
    else {
      infence = 0
      if (!indet && !diagram && fencelines > 10) warn("block", "a fenced block of " fencelines " lines sits outside <details>; collapse it")
    }
    next
  }
  if (infence) { fencelines++; next }
  if (line ~ /<details>/) { flush(); indet = 1; next }
  if (line ~ /<\/details>/) { flush(); indet = 0; next }
  if (indet) next
  if (line ~ /^## /) {
    flush(); h2++; seenh2 = 1
    if (line ~ /^## Problem/) problem = 1
    if (line ~ /^## Acceptance criteria/) accept = 1
    next
  }
  if (line ~ /^\|[ \t]*At a glance[ \t]*\|/) glance = 1
  if (line ~ /NOT_CHECKED/) notchecked = 1
  total += length(line) + 1
  if (line ~ /^[ \t]*$/) { flush(); next }
  if (n == 0) firstline = line
  buf = (n == 0) ? line : buf " " line
  n++
  if (line ~ /^(\||- )/) nlist++
}
END {
  flush()
  if (mode == "pr" && firstreal !~ /^(Refs|Fixes|Closes|Resolves|Partial-closes) #[0-9]+/)
    fail("ref", "the first line must start with Refs #N. or Fixes #N.")
  if (!leadseen) fail("lead", "no bold lead paragraph before the first ## heading")
  else if (leadwords > 45) warn("lead", "the lead has " leadwords " words; aim for 45 or fewer")
  if (mode == "pr") {
    if (!glance) fail("glance", "no table whose header starts with | At a glance |")
    if (total > 1200 && h2 < 2) warn("headings", "a body over 1200 characters should have at least 2 ## headings")
    if (!notchecked) warn("notchecked", "no NOT_CHECKED line; say what you did not run")
  } else {
    if (!problem) fail("problem", "no ## Problem heading")
    if (!accept) fail("acceptance", "no ## Acceptance criteria heading")
  }
  if (fails == 0) print "OK shape check passed (" mode ")"
  exit (fails > 0)
}
' "${file}"
