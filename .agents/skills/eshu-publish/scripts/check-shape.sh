#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# check-shape.sh - advisory shape check for a PR body or issue draft.
#
# Usage: check-shape.sh [--pr|--issue] <file>
#
# Run it on the draft before you capture a review receipt. It checks the shape
# rules of the eshu-publish skill, not the wording; use ste-lint.py for wording.
#
#   FAIL: the text breaks a rule that makes it hard to read. Fix it.
#   WARN: advice. Judge it.
#
# Exit 0: no FAIL. Exit 1: at least one FAIL. Exit 2: bad usage.
#
# Prose is any block of lines that is not a list, a table, a heading, a quote,
# a fenced block, or inside <details>. Data in a fence or in <details> never
# trips the paragraph rule: that is where long data belongs. Lengths are counted
# in bytes, so a paragraph with many non-ASCII characters counts a little high.
# That is why a paragraph warns at 600 and fails only above 800.
set -euo pipefail

mode="pr"
file=""
for arg in "$@"; do
  case "${arg}" in
    --pr) mode="pr" ;;
    --issue) mode="issue" ;;
    -h | --help)
      sed -n '5,21p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) file="${arg}" ;;
  esac
done
if [ -z "${file}" ] || [ ! -f "${file}" ]; then
  echo "usage: check-shape.sh [--pr|--issue] <file>" >&2
  exit 2
fi

LC_ALL=C awk -v mode="${mode}" '
function fail(rule, msg) { print "FAIL " rule ": " msg; fails++ }
function warn(rule, msg) { print "WARN " rule ": " msg }
# fencemark sets mk_c, mk_n, mk_rest when s starts with 3 or more of ` or ~.
function fencemark(s,   c, n) {
  mk_c = ""; mk_n = 0; mk_rest = ""
  c = substr(s, 1, 1)
  if (c != "`" && c != "~") return 0
  n = 0
  while (substr(s, n + 1, 1) == c) n++
  if (n < 3) return 0
  mk_c = c; mk_n = n; mk_rest = substr(s, n + 1)
  return 1
}
function flush(   first, w, len) {
  if (n > 0) {
    first = firstline
    if (first ~ /^(\||- |\* |\+ |[0-9]+[.)] |#|<|>)/) {
      if (nlist > 10) warn("block", "a list or table of " nlist " lines sits outside <details>; collapse it")
    } else {
      len = length(buf)
      if (len > 800) fail("paragraph", len " bytes in one paragraph, over 800; it starts: " substr(buf, 1, 50))
      else if (len > 600) warn("paragraph", len " bytes in one paragraph, over 600; split it or use a list")
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
  sub(/[ \t]+$/, "", line)
  if (!started && line !~ /^[ \t]*$/) { started = 1; firstreal = line }
  t = line
  gsub(/`[^`]*`/, "", t)
  if (t ~ /REPLACE:/) replace++
  t = line
  sub(/^[ \t]+/, "", t)
  if (infence) {
    if (fencemark(t) && mk_c == fch && mk_n >= fn && mk_rest == "") {
      infence = 0
      if (!indet && !diagram && fencelines > 10) warn("block", "a fenced block of " fencelines " lines sits outside <details>; collapse it")
    } else fencelines++
    next
  }
  if (fencemark(t)) {
    flush(); infence = 1; fch = mk_c; fn = mk_n; fencelines = 0
    diagram = (mk_rest ~ /^[ \t]*mermaid/)
    next
  }
  if (line ~ /^[ \t]*<details/) {
    flush()
    if (line !~ /<\/details>/) indet = 1
    next
  }
  if (line ~ /^[ \t]*<\/details>[ \t]*$/) { flush(); indet = 0; next }
  if (indet) next
  if (line ~ /^#+[ \t]/) {
    flush()
    if (line ~ /^## /) {
      h2++; seenh2 = 1
      if (line ~ /^## Problem/) problem = 1
      if (line ~ /^## Acceptance criteria/) accept = 1
    }
    next
  }
  if (line ~ /^\|[ \t]*At a glance[ \t]*\|/) glance = 1
  if (line ~ /NOT_CHECKED/) notchecked = 1
  total += length(line) + 1
  if (line ~ /^[ \t]*$/) { flush(); next }
  # A list may follow its intro line with no blank line, so a list item that
  # starts while a prose block is open closes that block first.
  if (n > 0 && line ~ /^(- |\+ |\* |[0-9]+[.)] )/ && firstline !~ /^(\||- |\* |\+ |[0-9]+[.)] |#|<|>)/) flush()
  if (n == 0) firstline = line
  buf = (n == 0) ? line : buf " " line
  n++
  if (line ~ /^(\||- |\+ |\* |[0-9]+[.)] )/) nlist++
}
END {
  flush()
  if (infence) warn("fence", "a code fence was never closed; the text after it was not checked")
  if (indet) warn("details", "a <details> block was never closed; the text after it was not checked")
  if (mode == "pr" &&
      firstreal !~ /^(Refs|Fixes|Closes|Resolves|Partial-closes) #[0-9]+/ &&
      firstreal !~ /^Refs [A-Za-z0-9][A-Za-z0-9-]*\/[A-Za-z0-9_.-]+#[0-9]+\./)
    fail("ref", "the first line must start with Refs #N., Fixes #N., or Refs owner/repo#N.")
  if (!leadseen) fail("lead", "no lead: the first paragraph after the Refs line must be bold from start to end")
  else if (leadwords > 45) warn("lead", "the lead has " leadwords " words; aim for 45 or fewer")
  if (mode == "pr") {
    if (!glance) {
      if (total < 600 && h2 == 0) warn("glance", "no At a glance table; it is optional for a body under 600 characters with no ## heading")
      else fail("glance", "no table whose header starts with | At a glance |")
    }
    if (total > 1200 && h2 < 2) warn("headings", "a body over 1200 characters should have at least 2 ## headings")
    if (!notchecked) warn("notchecked", "no NOT_CHECKED line; say what you did not run")
  } else {
    if (!problem) fail("problem", "no ## Problem heading")
    if (!accept) fail("acceptance", "no ## Acceptance criteria heading")
  }
  if (replace > 0) warn("placeholder", replace " line(s) still contain REPLACE:; fill or delete them")
  if (fails == 0) print "OK shape check passed (" mode ")"
  exit (fails > 0)
}
' "${file}"
