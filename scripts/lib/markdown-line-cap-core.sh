#!/usr/bin/env bash
# Core bash implementation of the Markdown 500-line file cap under go/
# (issue #6187). Sibling of scripts/lib/dirgate-core.sh: same ledger shape,
# same CLI shape, same test-mirror shape. Read that file alongside this one.
#
# Why this exists as its own checker rather than a config change: the Go cap
# is the tools/golangci-lint-filelength plugin, which walks the Go AST and
# cannot see a .md file at all, and .pre-commit-config.yaml's go-file-cap
# hook declares `types: [go]`. There is no setting that widens either to
# Markdown, so Markdown needs its own gate.
#
# Sourced by scripts/verify-markdown-line-cap.sh; not meant to be run
# directly.
#
# Portability: this file is sourced by a pre-commit hook, so it runs under
# whatever bash resolves first on PATH -- including macOS's stock bash 3.2.
# It therefore never uses `declare -A` / associative arrays, never moves data
# through a pipe with `<<<` or `<<EOF`, and guards every `"${arr[@]}"`
# expansion as `"${arr[@]:-}"`. Same constraints as dirgate-core.sh; see that
# file's header for the incidents behind each one.

MARKDOWN_LINE_CAP_MAX_LINES=500
MARKDOWN_LINE_CAP_NAME="markdown-file-cap"
MARKDOWN_LINE_CAP_TSV="${MARKDOWN_LINE_CAP_TSV:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/markdown-line-cap-grandfather.tsv}"

# mdcap_skip_path is the bash analogue of dirgate_skip_dir: true (exit 0) if
# the repo-relative path is NOT a Markdown file this gate governs. That is
# anything outside go/, anything that is not *.md, and anything under a
# vendor, testdata, generated, or hidden path segment. go/**/testdata/*.md
# really exists (go/cmd/audit-preflight/testdata carries fixture Markdown
# whose length is an input to a test, not a document a human reads), so the
# testdata skip is load-bearing, not defensive.
mdcap_skip_path() {
	local path="$1" seg
	case "${path}" in
		go/*.md) ;;
		*) return 0 ;;
	esac
	local oldifs="${IFS}"
	IFS='/'
	# shellcheck disable=SC2086
	set -- ${path}
	IFS="${oldifs}"
	for seg in "$@"; do
		case "${seg}" in
			vendor|testdata|generated) return 0 ;;
			.*) [[ "${seg}" != "." ]] && return 0 ;;
		esac
	done
	return 1
}

# mdcap_line_count prints the number of lines in file.
#
# awk's NR, not `wc -l`. They disagree by one on a file whose last line has
# no terminating newline: `wc -l` counts newline BYTES and reports 499 for a
# 500-line file, awk counts RECORDS and reports 500. The repo's end-of-file-
# fixer hook makes the unterminated case rare but not impossible (an excluded
# fixture path, or a `--no-verify`-free commit of a file the hook skipped),
# and undercounting is the direction that lets a violation through. The issue
# #6187 census used awk for the same reason; this keeps the gate's arithmetic
# identical to the numbers the ledger was pinned from.
mdcap_line_count() {
	local file="$1"
	[[ -f "${file}" ]] || return 1
	awk 'END { print NR }' "${file}"
}

# mdcap_grandfather_lookup mirrors dirgate_grandfather_lookup: prints the
# pinned line count for a repo-relative path and returns 0 if the path has a
# row in the ledger; returns 1 with no output otherwise.
mdcap_grandfather_lookup() {
	local path="$1"
	awk -F'\t' -v key="${path}" '
		/^#/ || NF != 2 { next }
		$1 == key { print $2; found = 1 }
		END { exit(found ? 0 : 1) }
	' "${MARKDOWN_LINE_CAP_TSV}"
}

# mdcap_evaluate_file applies the cap to one repo-relative Markdown path.
# repo_root is the tree to resolve it against. Prints one finding to stderr
# and returns 1 when the file violates the cap; returns 0 otherwise
# (including when the file does not exist -- a deleted path is not a
# violation, and a ledger row pointing at it is caught by
# mdcap_verify_ledger instead).
#
# The ledger pins the LINE COUNT, and the comparison it is checked with is
# `live <= pinned`, NOT dirgate's `live == pinned`. Be precise about which:
# a count match is not a pin match, and these two ledgers deliberately assert
# different things.
#
#   - `live > pinned` FAILS. This is the whole point of pinning a number
#     rather than allowlisting a filename: a bare filename allowlist would
#     let go/internal/storage/postgres/README.md drift from 3766 lines to
#     5000 without a single gate ever going red.
#   - `live <= pinned` PASSES, with a non-fatal NOTE below the pin nudging a
#     re-pin. dirgate hard-FAILS a shrink to force the ratchet down, and that
#     is right for it: a directory shrinks when files MOVE, which is a
#     deliberate structural change already touching the ledger. Markdown
#     shrinks a line at a time, on ordinary prose edits. Requiring a re-pin
#     for each one would make every documentation edit under go/ re-touch a
#     single shared file, which serialises them against each other exactly
#     the way #6187 warned a second dirgate-shaped ledger would -- and
#     documentation edits are otherwise among the cheapest changes to land.
#     The cost of the softer rule is bounded and stated: a file that shrinks
#     to 600 lines may regrow to its pin before failing. It can never exceed
#     the pin, which is the number this gate exists to hold.
#
# The pin is a line count only. No content digest, deliberately: a digest
# would fail on every edit that leaves the line count alone, which is most of
# them, and would turn the ledger into a merge-conflict magnet for no
# additional protection against the growth this gate measures.
mdcap_evaluate_file() {
	local repo_root="$1" path="$2"
	local full="${repo_root}/${path}"
	[[ -f "${full}" ]] || return 0

	local count pinned
	count="$(mdcap_line_count "${full}")"
	(( count > MARKDOWN_LINE_CAP_MAX_LINES )) || return 0

	# A malformed pin is treated as NO pin: report the plain over-cap
	# finding here and let mdcap_verify_ledger report the malformed row
	# itself. Without this guard the `(( count > pinned ))` below evaluates
	# the pin's text as an arithmetic expression, so a row reading
	# "nine hundred" aborted the whole run with `nine: unbound variable`
	# under `set -u` before the ledger check that explains it ever ran.
	# Pinned by test_malformed_ledger_row_hard_fails.
	if ! pinned="$(mdcap_grandfather_lookup "${path}")" || [[ -z "${pinned}" || -n "${pinned//[0-9]/}" ]]; then
		printf '%s: %s has %d lines, exceeding the %d-line cap; split it by audience (doc.go for the godoc contract, README.md for human architecture, AGENTS.md for scoped agent instructions) rather than cutting at an arbitrary line -- see scripts/lib/markdown-line-cap-grandfather.tsv if this file predates the gate\n' \
			"${MARKDOWN_LINE_CAP_NAME}" "${path}" "${count}" "${MARKDOWN_LINE_CAP_MAX_LINES}" >&2
		return 1
	fi

	if (( count > pinned )); then
		printf '%s: %s grew from its grandfathered %d lines to %d, exceeding the %d-line cap; a grandfathered file may shrink but MUST NOT grow -- split it, or move the new content into a sibling document\n' \
			"${MARKDOWN_LINE_CAP_NAME}" "${path}" "${pinned}" "${count}" "${MARKDOWN_LINE_CAP_MAX_LINES}" >&2
		return 1
	fi

	if (( count < pinned )); then
		printf '%s: NOTE %s is %d lines, below its grandfathered %d; re-pin this row in scripts/lib/markdown-line-cap-grandfather.tsv (run `bash scripts/verify-markdown-line-cap.sh --pin %s`) so later growth is measured against the smaller file\n' \
			"${MARKDOWN_LINE_CAP_NAME}" "${path}" "${count}" "${pinned}" "${path}" >&2
	fi
	return 0
}

# mdcap_verify_ledger HARD-FAILS on a stale or malformed ledger row, the way
# dirgate_verify_naming_exempt_ledger does (and unlike
# dirgate_report_removable_grandfathers' soft NOTE). A row is stale when its
# file no longer exists, or when the file has been brought under the cap: in
# both cases the reason the row was written has already been dealt with, and
# the change that dealt with it must delete the row rather than leave it to
# rot into a permanent exemption for a path some later commit might reuse.
#
# Prints one line per bad row to stderr; returns 1 if any are bad, 0
# otherwise (including when the ledger file is absent).
mdcap_verify_ledger() {
	local repo_root="$1" path pinned exit_status=0 seen=""
	[[ -f "${MARKDOWN_LINE_CAP_TSV}" ]] || return 0
	while IFS=$'\t' read -r path pinned; do
		case "${path}" in
			""|\#*) continue ;;
		esac

		if [[ -z "${pinned}" || -n "${pinned//[0-9]/}" ]]; then
			printf '%s: MALFORMED ledger row %s -- second column must be a decimal line count, got %s\n' \
				"${MARKDOWN_LINE_CAP_NAME}" "${path}" "${pinned:-<empty>}" >&2
			exit_status=1
			continue
		fi

		case "${seen}" in
			*"|${path}|"*)
				printf '%s: DUPLICATE ledger row %s -- one row per file; a second row silently shadows the first\n' \
					"${MARKDOWN_LINE_CAP_NAME}" "${path}" >&2
				exit_status=1
				continue
				;;
		esac
		seen="${seen}|${path}|"

		if (( pinned <= MARKDOWN_LINE_CAP_MAX_LINES )); then
			printf '%s: INVALID ledger row %s pinned at %d, at or below the %d-line cap -- a row is only meaningful for a file that exceeds the cap; remove it\n' \
				"${MARKDOWN_LINE_CAP_NAME}" "${path}" "${pinned}" "${MARKDOWN_LINE_CAP_MAX_LINES}" >&2
			exit_status=1
			continue
		fi

		if [[ ! -f "${repo_root}/${path}" ]]; then
			printf '%s: STALE ledger row %s -- file no longer exists; remove this row in the same change that moved/renamed/removed it\n' \
				"${MARKDOWN_LINE_CAP_NAME}" "${path}" >&2
			exit_status=1
			continue
		fi

		local count
		count="$(mdcap_line_count "${repo_root}/${path}")"
		if (( count <= MARKDOWN_LINE_CAP_MAX_LINES )); then
			printf '%s: STALE ledger row %s -- the file is now %d lines and no longer needs grandfathering; remove this row from scripts/lib/markdown-line-cap-grandfather.tsv\n' \
				"${MARKDOWN_LINE_CAP_NAME}" "${path}" "${count}" >&2
			exit_status=1
		fi
	done < "${MARKDOWN_LINE_CAP_TSV}"
	return "${exit_status}"
}

# mdcap_print_pin prints the ledger row a human should paste for one
# repo-relative path, mirroring dirgate_print_digest's role: authoring a row
# by measuring the file rather than by guessing at its length.
mdcap_print_pin() {
	local repo_root="$1" path="$2"
	local full="${repo_root}/${path}"
	if [[ ! -f "${full}" ]]; then
		printf '%s: no such file: %s\n' "${MARKDOWN_LINE_CAP_NAME}" "${path}" >&2
		return 1
	fi
	local count
	count="$(mdcap_line_count "${full}")"
	if (( count <= MARKDOWN_LINE_CAP_MAX_LINES )); then
		printf '%s: %s is %d lines, within the %d-line cap -- it needs no ledger row\n' \
			"${MARKDOWN_LINE_CAP_NAME}" "${path}" "${count}" "${MARKDOWN_LINE_CAP_MAX_LINES}" >&2
		return 1
	fi
	printf '%s\t%d\n' "${path}" "${count}"
}
