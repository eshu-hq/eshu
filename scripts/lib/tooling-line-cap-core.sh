#!/usr/bin/env bash
# Core bash implementation of the 500-line file cap for tracked files under
# scripts/ (issue #8221). Sibling of scripts/lib/markdown-line-cap-core.sh:
# same ledger shape (path<TAB>line_count, a ceiling that may shrink but never
# grow), same CLI shape, same test-mirror shape. Read that file alongside this
# one for the reasoning behind each rule; only the differences are explained
# here.
#
# Why a separate checker: the Go cap is the golangci-lint filelength plugin and
# the Markdown cap covers go/ and docs/. Nothing walked scripts/, so a script
# or Python module past the AGENTS.md 500-line rule failed no gate.
#
# Sourced by scripts/verify-tooling-line-cap.sh; not meant to be run directly.
#
# Portability: runs under whatever bash resolves first on PATH, including the
# macOS stock bash 3.2. No `declare -A`, no `<<<`, no unguarded "${arr[@]}".

# Base-ref fetching without shallowing the shared clone (#8078).
# shellcheck source=scripts/lib/gate-diff-base.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/gate-diff-base.sh"

TOOLING_LINE_CAP_MAX_LINES=500
TOOLING_LINE_CAP_NAME="scripts-file-cap"
TOOLING_LINE_CAP_TSV="${TOOLING_LINE_CAP_TSV:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/tooling-line-cap-grandfather.tsv}"

# slcap_skip_path is true (exit 0) when the repo-relative path is NOT governed:
# anything outside scripts/ and anything under a vendor, testdata, generated or
# hidden path segment. Every other tracked file under scripts/ is governed,
# whatever its extension, because the AGENTS.md rule is about files.
slcap_skip_path() {
	local path="$1" seg
	case "${path}" in
		scripts/*) ;;
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

# slcap_line_count prints the number of lines in file. awk's NR, not `wc -l`:
# the two disagree by one on a file whose last line has no newline, and
# undercounting is the direction that lets a violation through.
slcap_line_count() {
	local file="$1"
	[[ -f "${file}" ]] || return 1
	awk 'END { print NR }' "${file}"
}

# slcap_grandfather_lookup prints the pinned line count for a repo-relative
# path and returns 0 if the ledger has a row for it; returns 1 otherwise.
slcap_grandfather_lookup() {
	local path="$1"
	awk -F'\t' -v key="${path}" '
		/^#/ || NF != 2 { next }
		$1 == key { print $2; found = 1 }
		END { exit(found ? 0 : 1) }
	' "${TOOLING_LINE_CAP_TSV}"
}

# slcap_pin_is_canonical rejects any pin that is not a canonical decimal
# integer. A leading zero reads as octal in bash arithmetic and would abort the
# comparison, which a negated condition then reports as "no growth".
slcap_pin_is_canonical() {
	local value="$1"
	[[ -n "${value}" && -z "${value//[0-9]/}" ]] || return 1
	[[ "${value}" == "0" || "${value#0}" == "${value}" ]]
}

# slcap_evaluate_file applies the cap to one repo-relative path under
# repo_root. Prints one finding to stderr and returns 1 on a violation. A file
# that does not exist is not a violation; a ledger row pointing at it is caught
# by slcap_verify_ledger.
#
# A grandfathered file passes while `live <= pinned`. It fails when it grows
# past the pin. Below the pin it passes with a NOTE asking for a re-pin.
slcap_evaluate_file() {
	local repo_root="$1" path="$2"
	local full="${repo_root}/${path}"
	[[ -f "${full}" ]] || return 0

	local count pinned
	count="$(slcap_line_count "${full}")"
	(( count > TOOLING_LINE_CAP_MAX_LINES )) || return 0

	# A malformed pin counts as no pin here; slcap_verify_ledger reports the
	# malformed row itself.
	if ! pinned="$(slcap_grandfather_lookup "${path}")" || ! slcap_pin_is_canonical "${pinned}"; then
		printf '%s: %s has %d lines, exceeding the %d-line cap; split it by responsibility into sibling modules (see scripts/lib/live_postgres_readiness_inventory_gen_*.py for an example) -- scripts/lib/tooling-line-cap-grandfather.tsv lists the files that predate the gate\n' \
			"${TOOLING_LINE_CAP_NAME}" "${path}" "${count}" "${TOOLING_LINE_CAP_MAX_LINES}" >&2
		return 1
	fi

	if (( count > pinned )); then
		printf '%s: %s grew from its grandfathered %d lines to %d, exceeding the %d-line cap; a grandfathered file may shrink but MUST NOT grow -- move the new code into a sibling module\n' \
			"${TOOLING_LINE_CAP_NAME}" "${path}" "${pinned}" "${count}" "${TOOLING_LINE_CAP_MAX_LINES}" >&2
		return 1
	fi

	if (( count < pinned )); then
		printf '%s: NOTE %s is %d lines, below its grandfathered %d; re-pin this row in scripts/lib/tooling-line-cap-grandfather.tsv (run `bash scripts/verify-tooling-line-cap.sh --pin %s`) so later growth is measured against the smaller file\n' \
			"${TOOLING_LINE_CAP_NAME}" "${path}" "${count}" "${pinned}" "${path}" >&2
	fi
	return 0
}

# slcap_verify_ledger HARD-FAILS on a stale, duplicate or malformed ledger row.
# A row is stale when its file no longer exists or is back under the cap; the
# change that fixed the file must delete the row so it cannot rot into a
# permanent exemption for a path some later commit reuses. A rename does not
# carry the row: the new path has no pin, and a row added for it is refused by
# slcap_verify_ledger_growth as NEW, so a renamed file over the cap must be
# split below it.
slcap_verify_ledger() {
	local repo_root="$1" path pinned exit_status=0 seen="" count
	[[ -f "${TOOLING_LINE_CAP_TSV}" ]] || return 0
	while IFS=$'\t' read -r path pinned; do
		case "${path}" in
			""|\#*) continue ;;
		esac

		if ! slcap_pin_is_canonical "${pinned}"; then
			printf '%s: MALFORMED ledger row %s -- second column must be a canonical decimal line count with no leading zero, got %s\n' \
				"${TOOLING_LINE_CAP_NAME}" "${path}" "${pinned:-<empty>}" >&2
			exit_status=1
			continue
		fi

		case "${seen}" in
			*"|${path}|"*)
				printf '%s: DUPLICATE ledger row %s -- one row per file; a second row silently shadows the first\n' \
					"${TOOLING_LINE_CAP_NAME}" "${path}" >&2
				exit_status=1
				continue
				;;
		esac
		seen="${seen}|${path}|"

		if (( pinned <= TOOLING_LINE_CAP_MAX_LINES )); then
			printf '%s: INVALID ledger row %s pinned at %d, at or below the %d-line cap -- a row is only meaningful for a file that exceeds the cap; remove it\n' \
				"${TOOLING_LINE_CAP_NAME}" "${path}" "${pinned}" "${TOOLING_LINE_CAP_MAX_LINES}" >&2
			exit_status=1
			continue
		fi

		if [[ ! -f "${repo_root}/${path}" ]]; then
			printf '%s: STALE ledger row %s -- file no longer exists; remove this row in the same change that removed the file or brought it under the cap (a renamed file that still exceeds the cap cannot take over the row: split it below the cap)\n' \
				"${TOOLING_LINE_CAP_NAME}" "${path}" >&2
			exit_status=1
			continue
		fi

		count="$(slcap_line_count "${repo_root}/${path}")"
		if (( count <= TOOLING_LINE_CAP_MAX_LINES )); then
			printf '%s: STALE ledger row %s -- the file is now %d lines and no longer needs grandfathering; remove this row from scripts/lib/tooling-line-cap-grandfather.tsv\n' \
				"${TOOLING_LINE_CAP_NAME}" "${path}" "${count}" >&2
			exit_status=1
		fi
	done < "${TOOLING_LINE_CAP_TSV}"
	return "${exit_status}"
}

# slcap_resolve_base_sha prints the commit SHA the growth check measures
# against, fetching origin/<branch> first when the checkout lacks it (CI
# checks out with fetch-depth 2). Returns 1 with no output when the baseline
# cannot be resolved at all.
slcap_resolve_base_sha() {
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
	eshu_gate_fetch_base "${repo_root}" \
		"${branch}:refs/remotes/origin/${branch}" || return 1
	git -C "${repo_root}" rev-parse --verify --quiet "${base_ref}^{commit}"
}

# slcap_verify_ledger_growth rejects a ledger that GREW against its committed
# baseline. Every other check reads the working tree alone, and a change can
# add an over-cap file together with a row pinning it at its own length and
# satisfy all of them. Only two ledger edits are legitimate: removing a row, or
# lowering a pin. A new row or a raised pin is refused.
#
# The baseline is the ledger at TOOLING_LINE_CAP_BASE_REF (default
# origin/<PR base>, else origin/main). When the ref cannot be resolved the check
# prints a NOTE and passes, unless TOOLING_LINE_CAP_REQUIRE_BASE=1 (CI sets it)
# turns that into a failure. When the ledger is absent at the baseline, this is
# the change that introduces it and its rows are the initial baseline.
slcap_verify_ledger_growth() {
	local repo_root="$1"
	local base_ref="${TOOLING_LINE_CAP_BASE_REF:-origin/${GITHUB_BASE_REF:-main}}"
	local tsv_rel="${TOOLING_LINE_CAP_TSV_REL:-scripts/lib/tooling-line-cap-grandfather.tsv}"

	[[ -f "${TOOLING_LINE_CAP_TSV}" ]] || return 0

	local base_sha
	if ! base_sha="$(slcap_resolve_base_sha "${repo_root}" "${base_ref}")"; then
		if [[ "${TOOLING_LINE_CAP_REQUIRE_BASE:-}" == "1" ]]; then
			printf '%s: NO BASELINE -- %s does not resolve and could not be fetched, so the ledger growth check could not run; TOOLING_LINE_CAP_REQUIRE_BASE=1 refuses to report a pass this gate never performed\n' \
				"${TOOLING_LINE_CAP_NAME}" "${base_ref}" >&2
			return 1
		fi
		printf '%s: NOTE ledger growth not checked -- %s does not resolve here and could not be fetched, so there is no committed baseline to compare against\n' \
			"${TOOLING_LINE_CAP_NAME}" "${base_ref}" >&2
		return 0
	fi

	local baseline
	if ! baseline="$(git -C "${repo_root}" show "${base_sha}:${tsv_rel}" 2>/dev/null)"; then
		return 0
	fi

	local exit_status=0 path pinned base_pin
	while IFS=$'\t' read -r path pinned; do
		case "${path}" in
			""|\#*) continue ;;
		esac
		slcap_pin_is_canonical "${pinned}" || continue

		base_pin="$(printf '%s\n' "${baseline}" |
			awk -F'\t' -v want="${path}" '$1 == want { print $2; exit }')"

		if [[ -z "${base_pin}" ]]; then
			printf '%s: NEW ledger row %s pinned at %s -- the grandfather ledger freezes debt that predates the gate, and a change MUST NOT authorise its own exemption; bring the file under the %d-line cap by splitting it\n' \
				"${TOOLING_LINE_CAP_NAME}" "${path}" "${pinned}" "${TOOLING_LINE_CAP_MAX_LINES}" >&2
			exit_status=1
			continue
		fi

		slcap_pin_is_canonical "${base_pin}" || continue
		if (( 10#${pinned} > 10#${base_pin} )); then
			printf '%s: RAISED ledger pin %s from %s to %s -- a grandfathered file may shrink but MUST NOT grow, and re-pinning it upward is the same growth wearing a ledger edit; split it\n' \
				"${TOOLING_LINE_CAP_NAME}" "${path}" "${base_pin}" "${pinned}" >&2
			exit_status=1
		fi
	done < "${TOOLING_LINE_CAP_TSV}"
	return "${exit_status}"
}

# slcap_print_pin prints the ledger row to paste for one repo-relative path,
# measured rather than guessed.
slcap_print_pin() {
	local repo_root="$1" path="$2"
	local full="${repo_root}/${path}"
	if [[ ! -f "${full}" ]]; then
		printf '%s: no such file: %s\n' "${TOOLING_LINE_CAP_NAME}" "${path}" >&2
		return 1
	fi
	local count
	count="$(slcap_line_count "${full}")"
	if (( count <= TOOLING_LINE_CAP_MAX_LINES )); then
		printf '%s: %s is %d lines, within the %d-line cap -- it needs no ledger row\n' \
			"${TOOLING_LINE_CAP_NAME}" "${path}" "${count}" "${TOOLING_LINE_CAP_MAX_LINES}" >&2
		return 1
	fi
	printf '%s\t%d\n' "${path}" "${count}"
}
