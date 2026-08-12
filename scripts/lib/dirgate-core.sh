#!/usr/bin/env bash
# Core bash implementation of the dirgate directory-size and naming gate
# (issue #6054). This is the bash MIRROR of
# tools/golangci-lint-dirgate/{dirgate,naming,nolint,grandfather_eval}.go:
# the two implementations must report the same verdict for the same tree,
# and this file's function names/comments point at the Go function each
# one mirrors so a future change can be kept in lockstep on both sides.
#
# Sourced by scripts/verify-dirgate.sh; not meant to be run directly.
#
# Portability: this file is sourced by scripts/dev/precommit-go.sh, which
# runs under whatever bash resolves first on PATH -- including macOS's
# stock bash 3.2 (no associative arrays) and Homebrew bash 5.3.15 (heredoc
# and `<<<` here-string bodies over ~512 bytes deadlock on that version's
# pipe-buffer behavior). This file therefore:
#   - never uses `declare -A` / associative arrays,
#   - never uses `<<<` or `<<EOF` to move data through a pipe,
#     using process substitution (`< <(...)`) or plain command
#     substitution (`$(...)`) instead, and
#   - guards every `"${arr[@]}"` expansion as `"${arr[@]:-}"` so an empty
#     array does not abort a `set -u` caller (macOS bash 3.2 bug).

DIRGATE_MAX_FILES=40
DIRGATE_NAME="dirgate"
DIRGATE_GRANDFATHER_TSV="${DIRGATE_GRANDFATHER_TSV:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/dirgate-grandfather.tsv}"
# DIRGATE_NAMING_EXEMPT_TSV is the SEPARATE per-file naming-exemption
# ledger (one row per exempt file, joined to DIRGATE_GRANDFATHER_TSV only
# by directory key) -- see that file's header for why it is not a column
# on the cap ledger.
DIRGATE_NAMING_EXEMPT_TSV="${DIRGATE_NAMING_EXEMPT_TSV:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/dirgate-naming-exempt.tsv}"

# dirgate_sha256 prints the lowercase hex sha256 of stdin.
dirgate_sha256() {
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 | awk '{print $1}'
	else
		sha256sum | awk '{print $1}'
	fi
}

# dirgate_skip_dir mirrors skipDir() in dirgate.go: true (exit 0) if any
# path segment of the go/-relative directory is vendor, testdata,
# generated, or hidden.
dirgate_skip_dir() {
	local dir="$1" seg
	local oldifs="${IFS}"
	IFS='/'
	# shellcheck disable=SC2086
	set -- ${dir}
	IFS="${oldifs}"
	for seg in "$@"; do
		case "${seg}" in
			vendor|testdata|generated) return 0 ;;
			.*) [[ "${seg}" != "." ]] && return 0 ;;
		esac
	done
	return 1
}

# dirgate_qualifying_files mirrors qualifyingFiles() in dirgate.go: the
# sorted, non-test .go basenames directly inside dir (non-recursive).
# Prints nothing (and returns 1) if dir does not exist.
dirgate_qualifying_files() {
	local dir="$1" f base
	[[ -d "${dir}" ]] || return 1
	for f in "${dir}"/*.go; do
		[[ -e "${f}" ]] || continue
		# Parameter expansion, not `basename` -- a directory with hundreds
		# of qualifying files (internal/query has 879) would otherwise fork
		# one `basename` process per file, which measurably slowed --all
		# down against the real tree during #6054 development.
		base="${f##*/}"
		case "${base}" in
			*_test.go) continue ;;
		esac
		printf '%s\n' "${base}"
	done | LC_ALL=C sort
}

# dirgate_naming_subpackages mirrors namingSubpackages() in dirgate.go: the
# sorted immediate subdirectories of dir that contain at least one .go
# file, excluding vendor/testdata/generated/hidden trees.
dirgate_naming_subpackages() {
	local dir="$1" d name gf has_go
	[[ -d "${dir}" ]] || return 1
	for d in "${dir}"/*/; do
		[[ -e "${d}" ]] || continue
		name="${d%/}"
		name="${name##*/}"
		case "${name}" in
			vendor|testdata|generated) continue ;;
			.*) continue ;;
		esac
		has_go=0
		for gf in "${d}"*.go; do
			[[ -e "${gf}" ]] || continue
			has_go=1
			break
		done
		[[ "${has_go}" -eq 1 ]] && printf '%s\n' "${name}"
	done | LC_ALL=C sort
}

# dirgate_representative_file mirrors representativeFile() in dirgate.go.
# Expects its arguments already sorted (dirgate_qualifying_files' output).
dirgate_representative_file() {
	local f
	for f in "$@"; do
		if [[ "${f}" == "doc.go" ]]; then
			printf '%s\n' "doc.go"
			return 0
		fi
	done
	[[ $# -gt 0 ]] && printf '%s\n' "$1"
}

# dirgate_naming_violation_subpkg mirrors the per-file loop body inside
# detectNamingViolations() in naming.go: given a qualifying file basename
# and the candidate subpackage names, prints the FIRST subpackage it
# violates against (empty stdout + exit 1 if none). The word-boundary
# rule matches naming.go exactly: an exact stem match, or the stem
# starting with "<subpkg>_".
dirgate_naming_violation_subpkg() {
	local file="$1"
	shift
	local stem="${file%.go}" sub
	for sub in "$@"; do
		if [[ "${stem}" == "${sub}" || "${stem}" == "${sub}_"* ]]; then
			printf '%s\n' "${sub}"
			return 0
		fi
	done
	return 1
}

# dirgate_nolint_justified mirrors nolintJustification() in nolint.go: a
# `//nolint:<gate>` marker on path's `package` declaration line, followed
# immediately by a second `//` comment with non-empty (post-trim) text,
# suppresses the finding. A bare marker (no justification) is rejected --
# see the "Escape hatch" section of tools/golangci-lint-dirgate/README.md
# for why this does NOT rely on golangci-lint's own nolint processor.
dirgate_nolint_justified() {
	local path="$1" gate="$2" line marker after reason
	[[ -f "${path}" ]] || return 1
	marker="//nolint:${gate}"

	while IFS= read -r line; do
		case "${line}" in
			package\ *) ;;
			*) continue ;;
		esac
		case "${line}" in
			*"${marker}"*) ;;
			*) continue ;;
		esac
		after="${line#*"${marker}"}"
		case "${after}" in
			"") return 1 ;;                 # bare marker at end of line
			" "*|$'\t'*|"//"*) ;;            # a real comment boundary
			*) continue ;;                  # e.g. matched "//nolint:dirgateway"
		esac
		case "${after}" in
			*"//"*) ;;
			*) return 1 ;;                  # no second // -> no justification
		esac
		reason="${after#*//}"
		reason="$(printf '%s' "${reason}" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
		if [[ -n "${reason}" ]]; then
			return 0
		fi
		return 1
	done < "${path}"
	return 1
}

# dirgate_digest mirrors qualifyingDigest() in dirgate.go: sha256 hex of
# the sorted, newline-joined basenames given as arguments.
dirgate_digest() {
	printf '%s\n' "$@" | LC_ALL=C sort | dirgate_sha256
}

# dirgate_grandfather_lookup mirrors a map lookup against the
# FileCount/Digest half of grandfatheredDirectories in grandfather.go,
# reading it from scripts/lib/dirgate-grandfather.tsv instead of an
# embedded Go literal. Prints "<file_count><TAB><digest>" and returns 0 if
# dirkey has a row; returns 1 with no output otherwise. See
# dirgate_naming_is_exempt for the SEPARATE per-file naming-exemption
# lookup (scripts/lib/dirgate-naming-exempt.tsv) that joins with this one
# only via the shared dirkey.
dirgate_grandfather_lookup() {
	local dirkey="$1"
	awk -F'\t' -v key="${dirkey}" '
		/^#/ || NF != 3 { next }
		$1 == key { print $2"\t"$3; found=1 }
		END { exit(found ? 0 : 1) }
	' "${DIRGATE_GRANDFATHER_TSV}"
}

# dirgate_naming_is_exempt mirrors namingExemptSet's per-file lookup in
# grandfather_eval.go: true (exit 0) if the (dirkey, file) pair has a row
# in scripts/lib/dirgate-naming-exempt.tsv. A file's exemption depends only
# on its own name being pinned in that SEPARATE ledger -- NEVER on the
# directory's aggregate file count in dirgate-grandfather.tsv (see that
# file and dirgate-naming-exempt.tsv's header for why the earlier
# aggregate-count gate was a bug).
dirgate_naming_is_exempt() {
	local file="$1" dirkey="$2"
	[[ -f "${DIRGATE_NAMING_EXEMPT_TSV}" ]] || return 1
	awk -F'\t' -v dirkey="${dirkey}" -v file="${file}" '
		/^#/ || NF != 2 { next }
		$1 == dirkey && $2 == file { found=1; exit }
		END { exit(found ? 0 : 1) }
	' "${DIRGATE_NAMING_EXEMPT_TSV}"
}

# dirgate_evaluate_dir mirrors evaluateDirectory() in grandfather_eval.go.
# dirkey is the grandfather-ledger key (go/-relative directory path); dir
# is the real filesystem path to evaluate. Prints one line per surviving
# finding to stderr and returns 1 if any survive, 0 otherwise (including
# when dir does not exist, mirroring run()'s "nothing to check" no-op).
dirgate_evaluate_dir() {
	local dirkey="$1" dir="$2"
	[[ -d "${dir}" ]] || return 0

	local -a files=()
	local f
	while IFS= read -r f; do
		[[ -n "${f}" ]] && files+=("${f}")
	done < <(dirgate_qualifying_files "${dir}")
	[[ ${#files[@]} -eq 0 ]] && return 0

	local -a subs=()
	local s
	while IFS= read -r s; do
		[[ -n "${s}" ]] && subs+=("${s}")
	done < <(dirgate_naming_subpackages "${dir}")

	local count="${#files[@]}"
	local digest
	digest="$(dirgate_digest "${files[@]:-}")"

	local pinned_count="" pinned_digest="" gf_line
	if gf_line="$(dirgate_grandfather_lookup "${dirkey}")"; then
		pinned_count="${gf_line%%$'\t'*}"
		pinned_digest="${gf_line#*$'\t'}"
	fi

	local cap_violates=0 cap_note=""
	if (( count > DIRGATE_MAX_FILES )); then
		if [[ -z "${pinned_count}" ]]; then
			cap_violates=1
		elif (( count < pinned_count )); then
			cap_violates=0
		elif [[ "${digest}" == "${pinned_digest}" ]]; then
			cap_violates=0
		elif (( count == pinned_count )); then
			cap_violates=1
			cap_note="file set changed at its pinned count of ${pinned_count} (one file was swapped for another); the grandfathered digest no longer matches"
		else
			cap_violates=1
			cap_note="grew from its grandfathered count of ${pinned_count} to ${count}"
		fi
	fi

	local exit_status=0

	if (( cap_violates == 1 )); then
		local rep
		rep="$(dirgate_representative_file "${files[@]:-}")"
		# Mirrors evaluateCapViolation's caller in grandfather_eval.go: a cap
		# nolint sits on the directory's representative file and suppresses the
		# cap for EVERY file in that directory, indefinitely. One marker on
		# internal/query's doc.go would un-gate 880 files for good, and "split it
		# into a subpackage" does not compile for query, reducer, projector or
		# mcp until the acyclic boundary lands. A grandfathered directory's only
		# exit is a reviewed pin bump in dirgate-grandfather.tsv, which a
		# reviewer can see as a one-line diff.
		if [[ -n "${pinned_count}" ]]; then
			local msg="package directory ${dirkey} has ${count} non-test .go files, exceeding the ${DIRGATE_MAX_FILES}-file cap"
			[[ -n "${cap_note}" ]] && msg="${msg} (${cap_note})"
			msg="${msg}; this directory is grandfathered, so //nolint:${DIRGATE_NAME} will NOT suppress it -- split it into a subpackage, or bump its row in scripts/lib/dirgate-grandfather.tsv in a reviewed commit"
			printf '%s\n' "${msg}" >&2
			exit_status=1
		elif ! dirgate_nolint_justified "${dir}/${rep}" "${DIRGATE_NAME}"; then
			local msg="package directory ${dirkey} has ${count} non-test .go files, exceeding the ${DIRGATE_MAX_FILES}-file cap"
			[[ -n "${cap_note}" ]] && msg="${msg} (${cap_note})"
			msg="${msg}; split it into a subpackage, or add //nolint:${DIRGATE_NAME} // <reason> to ${dirkey}/${rep}'s package line"
			printf '%s\n' "${msg}" >&2
			exit_status=1
		fi
	fi

	# Naming exemption is checked PER FILE against
	# scripts/lib/dirgate-naming-exempt.tsv, never gated by the directory's
	# aggregate count -- see dirgate_naming_is_exempt.
	if [[ ${#subs[@]} -gt 0 ]]; then
		local sub
		for f in "${files[@]:-}"; do
			sub="$(dirgate_naming_violation_subpkg "${f}" "${subs[@]:-}")" || continue
			if dirgate_naming_is_exempt "${f}" "${dirkey}"; then
				continue
			fi
			if dirgate_nolint_justified "${dir}/${f}" "${DIRGATE_NAME}"; then
				continue
			fi
			printf '%s should move into the sibling subpackage "%s" (its name matches that package); move it, or add //nolint:%s // <reason> to its package line\n' \
				"${dirkey}/${f}" "${sub}" "${DIRGATE_NAME}" >&2
			exit_status=1
		done
	fi

	return "${exit_status}"
}

# dirgate_dir_needs_grandfather reports (via exit status) whether dir
# still needs its grandfather-ledger protection: it does if the live
# qualifying count exceeds DIRGATE_MAX_FILES, or if any qualifying file
# still trips the naming rule. A directory that has been fully cleaned up
# (moved below the cap AND every naming collision resolved) does not need
# it, and dirgate_report_removable_grandfathers uses this to nudge its row
# out of the TSV. A directory that no longer exists at all trivially does
# not need it either.
dirgate_dir_needs_grandfather() {
	local dir="$1"
	[[ -d "${dir}" ]] || return 1

	local -a files=()
	local f
	while IFS= read -r f; do
		[[ -n "${f}" ]] && files+=("${f}")
	done < <(dirgate_qualifying_files "${dir}")
	(( ${#files[@]} > DIRGATE_MAX_FILES )) && return 0

	local -a subs=()
	local s
	while IFS= read -r s; do
		[[ -n "${s}" ]] && subs+=("${s}")
	done < <(dirgate_naming_subpackages "${dir}")
	[[ ${#subs[@]} -eq 0 ]] && return 1

	for f in "${files[@]:-}"; do
		if dirgate_naming_violation_subpkg "${f}" "${subs[@]:-}" >/dev/null; then
			return 0
		fi
	done
	return 1
}

# dirgate_report_removable_grandfathers prints a non-fatal NOTE (to
# stderr) for every ledger row whose directory no longer needs
# grandfathering, per the #6054 acceptance requirement that the gate
# nudges a fully-cleaned-up directory out of the list rather than
# silently tolerating a stale row forever. Never affects the caller's
# exit status -- callers should ignore this function's own return value.
dirgate_report_removable_grandfathers() {
	local go_dir="$1" line dirkey
	[[ -f "${DIRGATE_GRANDFATHER_TSV}" ]] || return 0
	while IFS= read -r line; do
		case "${line}" in
			""|\#*) continue ;;
		esac
		dirkey="${line%%$'\t'*}"
		[[ -n "${dirkey}" ]] || continue
		if ! dirgate_dir_needs_grandfather "${go_dir}/${dirkey}"; then
			printf 'dirgate: NOTE %s no longer needs grandfathering -- remove its row from scripts/lib/dirgate-grandfather.tsv\n' "${dirkey}" >&2
		fi
	done < "${DIRGATE_GRANDFATHER_TSV}"
	return 0
}

# dirgate_verify_naming_exempt_ledger HARD-FAILS -- unlike
# dirgate_report_removable_grandfathers' soft NOTE above -- when
# scripts/lib/dirgate-naming-exempt.tsv pins a (dir, file) row whose file
# either no longer exists in dir, or exists but no longer collides with any
# of dir's sibling subpackages (it was renamed, or its colliding
# subpackage was removed). A stale row means the violation it was pinned
# for has ALREADY been fixed; the PR that fixed it must delete the row in
# the same change, not leave it to rot -- see dirgate-naming-exempt.tsv's
# header for why this is a hard fail rather than the cap ledger's nudge.
# Prints one line per stale row to stderr; returns 1 if any are stale, 0
# otherwise (including when the ledger file is absent).
dirgate_verify_naming_exempt_ledger() {
	local go_dir="$1" line dirkey file dir_path exit_status=0
	[[ -f "${DIRGATE_NAMING_EXEMPT_TSV}" ]] || return 0
	while IFS=$'\t' read -r dirkey file; do
		case "${dirkey}" in
			""|\#*) continue ;;
		esac
		[[ -n "${file}" ]] || continue
		dir_path="${go_dir}/${dirkey}"

		if [[ ! -f "${dir_path}/${file}" ]]; then
			printf 'dirgate: STALE naming-exempt row %s\t%s -- file no longer exists; remove this row from scripts/lib/dirgate-naming-exempt.tsv in the same change that moved/renamed/removed it\n' \
				"${dirkey}" "${file}" >&2
			exit_status=1
			continue
		fi

		local -a subs=()
		local s
		while IFS= read -r s; do
			[[ -n "${s}" ]] && subs+=("${s}")
		done < <(dirgate_naming_subpackages "${dir_path}")

		if ! dirgate_naming_violation_subpkg "${file}" "${subs[@]:-}" >/dev/null; then
			printf 'dirgate: STALE naming-exempt row %s\t%s -- the file no longer collides with any sibling subpackage; remove this row from scripts/lib/dirgate-naming-exempt.tsv\n' \
				"${dirkey}" "${file}" >&2
			exit_status=1
		fi
	done < "${DIRGATE_NAMING_EXEMPT_TSV}"
	return "${exit_status}"
}

# dirgate_changed_dirs prints the unique go/-relative directories that own
# at least one of the given repo-root-relative file paths (as pre-commit
# passes them), filtering to paths under go/ ending in .go. Mirrors
# precommit-go.sh's go_dirs() helper but returns bare directory paths
# (matching the grandfather-ledger key format) instead of "./"-prefixed
# golangci-lint package arguments.
dirgate_changed_dirs() {
	local f rel
	for f in "$@"; do
		case "${f}" in
			go/*.go) ;;
			*) continue ;;
		esac
		rel="${f#go/}"
		dirname "${rel}"
	done | LC_ALL=C sort -u
}

# dirgate_print_digest prints "count<TAB><N>", "digest<TAB><sha256>", and
# one "naming_violation<TAB><file><TAB><subpackage>" line per CURRENT
# naming violation for dir. The human-facing helper for authoring or
# updating a dirgate-grandfather.tsv row (scripts/dev/precommit-go.sh
# dirgate-digest) AND for authoring a dirgate-naming-exempt.tsv row
# honestly instead of guessing which files actually collide.
dirgate_print_digest() {
	local dir="$1"
	if [[ ! -d "${dir}" ]]; then
		printf 'dirgate: no such directory: %s\n' "${dir}" >&2
		return 1
	fi
	local -a files=()
	local f
	while IFS= read -r f; do
		[[ -n "${f}" ]] && files+=("${f}")
	done < <(dirgate_qualifying_files "${dir}")
	printf 'count\t%d\n' "${#files[@]}"
	printf 'digest\t%s\n' "$(dirgate_digest "${files[@]:-}")"

	local -a subs=()
	local s
	while IFS= read -r s; do
		[[ -n "${s}" ]] && subs+=("${s}")
	done < <(dirgate_naming_subpackages "${dir}")
	[[ ${#subs[@]} -eq 0 ]] && return 0

	local sub
	for f in "${files[@]:-}"; do
		sub="$(dirgate_naming_violation_subpkg "${f}" "${subs[@]:-}")" || continue
		printf 'naming_violation\t%s\t%s\n' "${f}" "${sub}"
	done
}
