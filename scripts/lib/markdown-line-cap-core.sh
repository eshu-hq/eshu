#!/usr/bin/env bash
# Core bash implementation of the Markdown 500-line file cap under go/ and docs/
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
# anything outside go/ and docs/, anything that is not *.md, and anything under a
# vendor, testdata, generated, or hidden path segment. go/**/testdata/*.md
# really exists (go/cmd/audit-preflight/testdata carries fixture Markdown
# whose length is an input to a test, not a document a human reads), so the
# testdata skip is load-bearing, not defensive.
mdcap_skip_path() {
	local path="$1" seg
	case "${path}" in
		go/*.md|docs/*.md) ;;
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
# mdcap_pin_is_canonical rejects any pin that is not a canonical decimal
# integer. Digits-only is NOT enough: bash reads a leading zero as octal, so a
# row pinned "0900" makes `(( count > pinned ))` abort with "value too great
# for base". That abort makes the arithmetic FALSE, and because the comparison
# sits inside a negated condition the growth simply goes unreported -- a
# 901-line file pinned at 0900 exited 0 with the gate believing it had checked.
# A malformed pin must fail loudly, never quietly disable the comparison it
# was supposed to feed. Pinned by test_leading_zero_ledger_pin_is_rejected.
mdcap_pin_is_canonical() {
	local value="$1"
	[[ -n "${value}" && -z "${value//[0-9]/}" ]] || return 1
	# A single "0" is canonical but can never be a valid pin (it is below the
	# cap and mdcap_verify_ledger rejects it as INVALID); any other leading
	# zero is noncanonical input.
	[[ "${value}" == "0" || "${value#0}" == "${value}" ]]
}

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
	if ! pinned="$(mdcap_grandfather_lookup "${path}")" || ! mdcap_pin_is_canonical "${pinned}"; then
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

		if ! mdcap_pin_is_canonical "${pinned}"; then
			printf '%s: MALFORMED ledger row %s -- second column must be a canonical decimal line count with no leading zero, got %s\n' \
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

# mdcap_resolve_base_sha prints the commit SHA the growth check measures
# against, FETCHING the ref first when this repository has no such ref yet.
# Returns 1 with no output when the baseline cannot be resolved at all.
#
# The fetch is load-bearing, and its destination refspec doubly so. test.yml's
# verify-contracts job -- the job that runs this gate -- checks out with
# `fetch-depth: 2`, which leaves the clone with no refs/remotes/origin/main at
# all. Without a fetch here, `rev-parse origin/main` failed, the growth check
# printed its NOTE and returned 0, and the anti-self-exemption backstop was
# silently absent while reading exactly like a pass. It resolved in CI only
# because the earlier "Verify hot-path evidence" step runs
# verify-performance-evidence.sh, whose own fetch populates the ref as a side
# effect -- an undocumented ordering dependency that any step reorder breaks.
#
# `git fetch origin <branch>` with NO `<src>:<dst>` destination updates
# FETCH_HEAD alone and never refs/remotes/origin/<branch>; that trap is what
# scripts/lib/gate-diff-base.sh was written to hold, and the refspec below
# mirrors its exactly.
#
# The fetch is attempted only after the ref fails to resolve locally, so an
# ordinary local run costs nothing and touches the network never. It is also
# skipped for any base ref that is not an `origin/<branch>` name -- a SHA or a
# local ref names nothing fetchable, and every scratch fixture repo (no origin
# remote at all) falls out here rather than waiting on git.
mdcap_resolve_base_sha() {
	local repo_root="$1" base_ref="$2" sha branch
	if sha="$(git -C "${repo_root}" rev-parse --verify --quiet "${base_ref}^{commit}")"; then
		printf '%s\n' "${sha}"
		return 0
	fi
	case "${base_ref}" in
		origin/*) branch="${base_ref#origin/}" ;;
		*) return 1 ;;
	esac
	[[ -n "${branch}" ]] || return 1
	git -C "${repo_root}" fetch --no-tags --depth=1 origin \
		"${branch}:refs/remotes/origin/${branch}" >/dev/null 2>&1 || return 1
	git -C "${repo_root}" rev-parse --verify --quiet "${base_ref}^{commit}"
}

# mdcap_verify_ledger_growth rejects a ledger that GREW against its committed
# baseline. Every other check in this file reads the working tree alone, and
# that is precisely the hole: a change may add a brand-new over-cap file
# together with a row pinning it at its own length, and every working-tree
# check agrees. count == pinned, the row is not stale, the pin exceeds the cap
# -- exit 0. The gate a change must not be able to satisfy by editing the
# ledger it is judged against was satisfiable by exactly that.
#
# The ledger freezes debt that predates the gate. So only two edits are ever
# legitimate: removing a row, or lowering a pin. Newly governed docs/ debt
# may be imported only up to its proven immutable-base count. All other new
# rows and raised pins are refused.
#
# The baseline is the ledger as committed at MARKDOWN_LINE_CAP_BASE_REF,
# defaulting to origin/<the branch the PR targets> and to origin/main outside
# CI. mdcap_resolve_base_sha fetches that ref when the checkout lacks it, so
# the two permissive cases below are now the only ways the check declines to
# run:
#
#   * the ref does not resolve AND could not be fetched -- a clone with no
#     origin remote, or an offline machine. The check reports that it could
#     not run rather than failing on a repository shape it cannot read. Set
#     MARKDOWN_LINE_CAP_REQUIRE_BASE=1 to turn that report into a failure;
#     CI does, because in CI an unreadable baseline is a missing gate, not a
#     local convenience.
#   * the ledger does not exist at the baseline. That is the commit that
#     introduces the ledger, whose rows ARE the initial baseline. Once it
#     lands, the file exists in main and every later addition is measured.
#
# Pinned by test_new_ledger_row_is_rejected, test_raised_ledger_pin_is_rejected,
# test_absent_baseline_ref_is_fetched, and
# test_unresolvable_baseline_is_red_under_require_base.
mdcap_verify_ledger_growth() {
	local repo_root="$1"
	local base_ref="${MARKDOWN_LINE_CAP_BASE_REF:-origin/${GITHUB_BASE_REF:-main}}"
	local tsv_rel="${MARKDOWN_LINE_CAP_TSV_REL:-scripts/lib/markdown-line-cap-grandfather.tsv}"

	[[ -f "${MARKDOWN_LINE_CAP_TSV}" ]] || return 0

	local base_sha
	if ! base_sha="$(mdcap_resolve_base_sha "${repo_root}" "${base_ref}")"; then
		if [[ "${MARKDOWN_LINE_CAP_REQUIRE_BASE:-}" == "1" ]]; then
			printf '%s: NO BASELINE -- %s does not resolve and could not be fetched, so the ledger growth check could not run; MARKDOWN_LINE_CAP_REQUIRE_BASE=1 refuses to report a pass this gate never performed\n' \
				"${MARKDOWN_LINE_CAP_NAME}" "${base_ref}" >&2
			return 1
		fi
		printf '%s: NOTE ledger growth not checked -- %s does not resolve here and could not be fetched, so there is no committed baseline to compare against\n' \
			"${MARKDOWN_LINE_CAP_NAME}" "${base_ref}" >&2
		return 0
	fi

	local baseline
	if ! baseline="$(git -C "${repo_root}" show "${base_sha}:${tsv_rel}" 2>/dev/null)"; then
		# The commit that introduces the ledger. Its rows are the baseline.
		return 0
	fi

	local exit_status=0 path pinned base_pin
	while IFS=$'\t' read -r path pinned; do
		case "${path}" in
			""|\#*) continue ;;
		esac
		mdcap_pin_is_canonical "${pinned}" || continue

		base_pin="$(printf '%s\n' "${baseline}" |
			awk -F'\t' -v want="${path}" '$1 == want { print $2; exit }')"

		# #6545 expands the governed tree. Import only debt already present
		# at this immutable base, never a new file or branch-authored growth.
		if [[ -z "${base_pin}" && "${path}" == docs/*.md ]]; then
			local base_count
			if base_count="$(git -C "${repo_root}" show "${base_sha}:${path}" 2>/dev/null | awk 'END { print NR }')"; then
				if (( base_count > MARKDOWN_LINE_CAP_MAX_LINES && 10#${pinned} <= base_count )); then
					continue
				fi
			fi
		fi

		if [[ -z "${base_pin}" ]]; then
			printf '%s: NEW ledger row %s pinned at %s -- the grandfather ledger freezes debt that predates the gate, and a change MUST NOT authorise its own exemption; bring the file under the %d-line cap, or split it by audience\n' \
				"${MARKDOWN_LINE_CAP_NAME}" "${path}" "${pinned}" "${MARKDOWN_LINE_CAP_MAX_LINES}" >&2
			exit_status=1
			continue
		fi

		mdcap_pin_is_canonical "${base_pin}" || continue
		if (( 10#${pinned} > 10#${base_pin} )); then
			printf '%s: RAISED ledger pin %s from %s to %s -- a grandfathered file may shrink but MUST NOT grow, and re-pinning it upward is the same growth wearing a ledger edit; split it, or move the new content into a sibling document\n' \
				"${MARKDOWN_LINE_CAP_NAME}" "${path}" "${base_pin}" "${pinned}" >&2
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
