#!/usr/bin/env bash
#
# regen-conflicts.sh - resolve rebase conflicts on generated files by
# regenerating them instead of hand-editing them.
#
# Reads the generated path-to-command map from the gate registry's
# regenerate: fields (specs/ci-gates.v1.yaml plus its ci-gates.d fragments),
# and for every conflicted path the map covers, runs the owning command and
# stages the result. Conflicted paths the map does not cover are left alone
# and reported; the exit is nonzero while any conflict remains.
#
# Usage:
#   bash scripts/dev/regen-conflicts.sh [--base <ref>] [--registry <path>]
#   bash scripts/dev/regen-conflicts.sh --print-map [--registry <path>]
#   bash scripts/dev/regen-conflicts.sh --help
#
# --base defaults to origin/main: entries whose regeneration starts from the
# base (the collector content pin takes the base's ledger, then re-pins)
# read it from $ESHU_REGEN_BASE, which the helper exports for every command
# it runs. REPO_ROOT overrides the repository root (tests point it at a
# fixture tree); ESHU_REGEN_BASE set in the environment overrides --base.
set -euo pipefail

default_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
repo_root="${REPO_ROOT:-${default_root}}"
registry="${repo_root}/specs/ci-gates.v1.yaml"
base="origin/main"
print_map_only=0

usage() {
	printf 'usage: regen-conflicts.sh [--base <ref>] [--registry <path>] [--print-map]\n'
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	--base)
		base="${2:?--base needs a ref}"
		shift 2
		;;
	--registry)
		registry="${2:?--registry needs a path}"
		shift 2
		;;
	--print-map)
		print_map_only=1
		shift
		;;
	--help | -h)
		usage
		exit 0
		;;
	*)
		printf 'regen-conflicts: unknown argument %s\n' "$1" >&2
		usage >&2
		exit 1
		;;
	esac
done

[[ -f "${registry}" ]] || {
	printf 'regen-conflicts: registry not found: %s\n' "${registry}" >&2
	exit 1
}

tab="$(printf '\t')"

# regen_map_extract prints TSV path<tab>command rows for every regenerate:
# entry in the registry's fragments, in LC_ALL=C sorted order. A fragment
# holds whole gates, so one awk pass per fragment file is enough; the gate id
# is tracked only to scope the map (a regenerate: block always belongs to the
# gate above it). regenerate: values are double-quoted and carry no double
# quotes or backslash escapes themselves, so the raw text the awk reads is
# byte-identical to the YAML-decoded value Go's Load sees — see the field's
# doc comment in go/internal/cigates/registry.go.
regen_map_extract() {
	local root="$1" frag line
	local frags
	frags="$(awk '/^gate_fragments:/{in_list=1; next} in_list && /^  - /{line=$0; sub(/^  - /, "", line); print line; next} in_list && /^[^ ]/{exit}' "${root}")"
	[[ -n "${frags}" ]] || {
		printf 'regen-conflicts: no gate_fragments in %s\n' "${root}" >&2
		return 1
	}
	while IFS= read -r frag; do
		[[ -n "${frag}" ]] || continue
		line="$(dirname "${root}")/${frag}"
		[[ -f "${line}" ]] || {
			printf 'regen-conflicts: fragment not found: %s\n' "${line}" >&2
			return 1
		}
		awk -v TAB="${tab}" '
			/^  - id: / { in_regen = 0; next }
			/^    regenerate:/ { in_regen = 1; next }
			/^    [a-z_]+:/ { in_regen = 0; next }
			in_regen && /^      "/ {
				line = $0
				sub(/^      "/, "", line)
				i = index(line, "\": \"")
				if (i == 0) { next }
				path = substr(line, 1, i - 1)
				cmd = substr(line, i + 4)
				sub(/"$/, "", cmd)
				print path TAB cmd
				next
			}
		' "${line}"
	done <<<"${frags}" | LC_ALL=C sort -u
}

regen_map="$(regen_map_extract "${registry}")" || exit 1
if [[ "${print_map_only}" -eq 1 ]]; then
	printf '%s\n' "${regen_map}"
	exit 0
fi

git -C "${repo_root}" rev-parse --is-inside-work-tree >/dev/null 2>&1 || {
	printf 'regen-conflicts: not a git checkout: %s\n' "${repo_root}" >&2
	exit 1
}

export ESHU_REGEN_BASE="${ESHU_REGEN_BASE:-${base}}"

# match_command prints the regen command for one conflicted path: an exact
# key first, then the first glob key (in map order) the path matches under
# shell case patterns.
match_command() {
	local path="$1" map="$2" mline pat cmd
	mline="$(printf '%s\n' "${map}" | awk -F"${tab}" '$1==p{print; exit}' p="${path}")"
	if [[ -n "${mline}" ]]; then
		printf '%s' "${mline#*"$tab"}"
		return 0
	fi
	while IFS= read -r mline; do
		[[ -n "${mline}" ]] || continue
		pat="${mline%%"$tab"*}"
		cmd="${mline#*"$tab"}"
		case "${pat}" in
		*'*'* | *'?'* | *'['*)
			case "${path}" in
			${pat})
				printf '%s' "${cmd}"
				return 0
				;;
			esac
			;;
		esac
	done <<<"${map}"
	return 1
}

unmerged="$(git -C "${repo_root}" diff --name-only --diff-filter=U || true)"
if [[ -z "${unmerged}" ]]; then
	printf 'regen-conflicts: no conflicted files\n'
	exit 0
fi

covered=""
uncovered=""
while IFS= read -r path; do
	[[ -n "${path}" ]] || continue
	if match_command "${path}" "${regen_map}" >/dev/null; then
		covered="${covered}${path}"$'\n'
	else
		uncovered="${uncovered}${path}"$'\n'
	fi
done <<<"${unmerged}"

if [[ -z "${covered}" ]]; then
	printf 'regen-conflicts: no covered conflicts; resolve by hand:\n%s' "${uncovered}" >&2
	exit 1
fi

# Run each distinct command once, no matter how many conflicted paths it
# owns: the readiness generator rewrites all seven files in one pass.
commands="$(printf '%s\n' "${covered}" | while IFS= read -r path; do
	[[ -n "${path}" ]] || continue
	match_command "${path}" "${regen_map}"
	printf '\n'
done | LC_ALL=C sort -u)"
while IFS= read -r cmd; do
	[[ -n "${cmd}" ]] || continue
	printf 'regen-conflicts: run %s\n' "${cmd}"
	(cd "${repo_root}" && bash -c "${cmd}")
done <<<"${commands}"

# Reset-style entries (their command takes the base copy first) need human
# review before the staging below: re-apply any hand-owned content the
# conflict carried, and for the content pin decide -advance-content-epoch by
# hand when fixtures and output changed together. Announce every one loudly
# so the reset is never silent.
printf '%s' "${covered}" | while IFS= read -r path; do
	[[ -n "${path}" ]] || continue
	cmd="$(match_command "${path}" "${regen_map}")"
	case "${cmd}" in
	*ESHU_REGEN_BASE*)
		printf 'regen-conflicts: review %s: reset to the base copy before regenerating; re-apply hand-owned content the conflict carried (content pin: combined fixture-plus-output changes need a human -advance-content-epoch call; see the conflict recipes)\n' "${path}"
		;;
	esac
done

# Never stage conflict markers: a regen command that did not rewrite a file
# (a typo'd entry, a generator that refused) must fail loudly here instead of
# looking resolved.
marked="$(printf '%s' "${covered}" | while IFS= read -r path; do
	[[ -n "${path}" ]] || continue
	if rg -q '^<{7} |^={7}$|^>{7} |^\|{7} ' "${repo_root}/${path}" 2>/dev/null; then
		printf '%s\n' "${path}"
	fi
done || true)"
if [[ -n "${marked}" ]]; then
	printf 'regen-conflicts: markers remain after regenerating:\n%s' "${marked}" >&2
	exit 1
fi

# shellcheck disable=SC2086
git -C "${repo_root}" add -- ${covered}

remaining="$(git -C "${repo_root}" diff --name-only --diff-filter=U || true)"
printf 'regen-conflicts: regenerated and staged:\n%s' "${covered}"
if [[ -z "${remaining}" ]]; then
	exit 0
fi
printf 'regen-conflicts: still conflicted (no regenerate entry):\n%s' "${remaining}" >&2
exit 1
