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
# heading and its page. A heading inside a fenced code block does not count.
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

# has_heading <file> <heading>: succeeds when a line equals <heading> (trailing
# whitespace ignored) outside a fenced code block.
has_heading() {
	awk -v want="$2" '
		{ line = $0; sub(/[ \t\r]+$/, "", line) }
		line ~ /^(```|~~~)/ { fenced = !fenced; next }
		!fenced && line == want { found = 1; exit }
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
