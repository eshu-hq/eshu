#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# verify-required-doc-sections.sh - fail when a hand-written section that must
# stay on a docs page is missing (issue #9028).
#
# Some pages mix a generated region with hand-written sections. The generator
# check (scripts/generate-parser-admission-doc.sh --check) only covers the
# generated region, so a merge or squash that deleted a hand-written section
# passed every gate (#9016 dropped "Content store policy"; #9025 restored it).
# This check reads scripts/required-doc-sections.txt, the single list of
# <page><TAB><exact heading line> entries, and fails naming each missing
# heading and its page. A heading inside a fenced code block or an HTML
# comment does not count.
#
# Environment (used by scripts/test-verify-required-doc-sections.sh):
#   ESHU_REQUIRED_DOC_SECTIONS_ROOT  repository root the page paths resolve
#                                    against (default: this checkout).
#   ESHU_REQUIRED_DOC_SECTIONS_LIST  path of the entry list (default:
#                                    scripts/required-doc-sections.txt).
#
# Exit codes: 0 all required headings present; 1 a heading, page, or list is
# missing, or an entry is malformed.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="${ESHU_REQUIRED_DOC_SECTIONS_ROOT:-$(cd "${script_dir}/.." && pwd)}"
list="${ESHU_REQUIRED_DOC_SECTIONS_LIST:-${script_dir}/required-doc-sections.txt}"
tab=$'\t'

if [[ ! -f "${list}" ]]; then
	echo "verify-required-doc-sections: entry list not found: ${list}" >&2
	exit 1
fi

# has_heading <file> <heading>: succeeds when a line equals <heading> (leading
# text exact, trailing whitespace and CR ignored) outside a fenced code block
# and outside an HTML comment (a comment opener inside an inline code span is
# not one). Fences follow CommonMark: up to three spaces of
# indent, an opener of three or more backticks or tildes, and a closer of the
# same character at least as long as the opener with nothing after it.
# The heading goes in through ENVIRON, not awk -v, which would process
# backslash escapes.
has_heading() {
	WANT_HEADING="$2" awk '
		BEGIN { want = ENVIRON["WANT_HEADING"] }
		# mask returns s with the text inside inline code spans replaced by x,
		# same length, so a comment opener shown as code is not found.
		function mask(s,   out, i, n, k, j, m, t, found) {
			out = s; n = length(s); i = 1
			while (i <= n) {
				if (substr(s, i, 1) != "`") { i++; continue }
				k = 0
				while (substr(s, i + k, 1) == "`") k++
				j = i + k; found = 0
				while (j <= n) {
					if (substr(s, j, 1) != "`") { j++; continue }
					m = 0
					while (substr(s, j + m, 1) == "`") m++
					if (m == k) { found = 1; break }
					j += m
				}
				if (!found) { i += k; continue }
				for (t = i + k; t < j; t++) out = substr(out, 1, t - 1) "x" substr(out, t + 1)
				i = j + k
			}
			return out
		}
		{
			line = $0
			sub(/[ \t\r]+$/, "", line)
			if (incomment) {
				i = index(line, "-->")
				if (i == 0) next
				incomment = 0
				next
			}
			if (fence != "") {
				if (match(line, /^ *(```+|~~~+)$/)) {
					t = line; sub(/^ */, "", t)
					if (substr(t, 1, 1) == substr(fence, 1, 1) && length(t) >= length(fence) && length(line) - length(t) <= 3) fence = ""
				}
				next
			}
			if (match(line, /^ *(```+|~~~+)/)) {
				t = line; sub(/^ */, "", t)
				if (length(line) - length(t) <= 3) {
					m = substr(t, 1, 1)
					n = 0
					while (substr(t, n + 1, 1) == m) n++
					# A backtick fence info string may not hold a backtick.
					if (m != "`" || index(substr(t, n + 1), "`") == 0) {
						fence = substr(t, 1, n)
						next
					}
				}
			}
			while ((i = index(mask(line), "<!--")) > 0) {
				rest = substr(line, i + 4)
				j = index(rest, "-->")
				if (j == 0) { line = substr(line, 1, i - 1); incomment = 1; break }
				line = substr(line, 1, i - 1) substr(rest, j + 3)
			}
			sub(/[ \t\r]+$/, "", line)
			if (line == want) { found = 1; exit }
		}
		END { exit(found ? 0 : 1) }
	' "$1"
}

entries=0
failures=0
while IFS= read -r line || [[ -n "${line}" ]]; do
	line="${line%$'\r'}"
	case "${line}" in
	'' | '#'*) continue ;;
	esac
	entries=$((entries + 1))
	if [[ "${line}" != *"${tab}"* ]]; then
		echo "verify-required-doc-sections: malformed entry (want <page><TAB><heading>): ${line}" >&2
		failures=$((failures + 1))
		continue
	fi
	page="${line%%"${tab}"*}"
	heading="${line#*"${tab}"}"
	# Trailing whitespace on the entry is not part of the heading.
	while [[ "${heading}" == *[[:space:]] ]]; do heading="${heading%?}"; done
	if [[ -z "${page}" || -z "${heading}" ]]; then
		echo "verify-required-doc-sections: malformed entry (empty page or heading): ${line}" >&2
		failures=$((failures + 1))
		continue
	fi
	if [[ ! -f "${root}/${page}" ]]; then
		echo "verify-required-doc-sections: ${page}: page not found (required heading: ${heading})" >&2
		failures=$((failures + 1))
		continue
	fi
	if ! has_heading "${root}/${page}" "${heading}"; then
		echo "verify-required-doc-sections: ${page}: required hand-written section '${heading}' is missing; restore it or remove the entry from scripts/required-doc-sections.txt in the same change" >&2
		failures=$((failures + 1))
	fi
done <"${list}"

if [[ "${entries}" -eq 0 ]]; then
	echo "verify-required-doc-sections: no entries in ${list}" >&2
	exit 1
fi
if [[ "${failures}" -ne 0 ]]; then
	exit 1
fi
echo "verify-required-doc-sections: pass (${entries} headings)"
