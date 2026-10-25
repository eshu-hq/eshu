#!/usr/bin/env bash
#
# verify-cgo-cflags-guard.sh - fail when a place that compiles Go (and so cgo)
# does not carry the C dialect flag from #9066.
#
# go-sitter-forest/perl v1.9.9 does not compile as C23, and GCC 15 on glibc 2.43
# defaults to C23, so a cold Go build cache fails with
# "bsearch.h:72:7: error: expected identifier or '(' before '_Generic'".
# scripts/lib/cgo-cflags.sh holds the full reason and the removal condition.
#
# Checks:
#   1. Every .github/workflows/*.yml that provisions Go (actions/setup-go, a
#      ci-tools container) or runs go build/test/vet/run/install sets
#      CGO_CFLAGS with -std=gnu17 in its workflow-level env. GitHub passes no
#      env between workflow files, so each file must carry its own.
#   2. No CGO_CFLAGS assignment anywhere in a workflow drops -std=gnu17, so a
#      later job or step cannot clobber the flag.
#   3. Every script under scripts/ that runs `CGO_ENABLED=1 go ...` sources
#      scripts/lib/cgo-cflags.sh and calls eshu_export_cgo_cflags.
#   4. The developer entrypoints (--list-entrypoints) do the same, and the
#      Makefile exports CGO_CFLAGS with -std=gnu17.
#
# Checks 3 and 4 read code only: whole-line comments are removed first, so a
# comment that names the library (as the "see cgo-cflags.sh" notes do) cannot
# satisfy them. Sourcing the library without calling the export does nothing,
# so both must be present.
#
# Coverage is exactly the files named above. A script that runs go without the
# `CGO_ENABLED=1 go` prefix gets the flag from a workflow, make, or one of the
# entrypoints, or from `go env -w CGO_CFLAGS=...` on the developer's host (see
# docs/public/reference/local-testing/cgo-cflags.md).
#
# Usage: scripts/verify-cgo-cflags-guard.sh [--root DIR] | --list-entrypoints
# Mirror test: scripts/test-verify-cgo-cflags-guard.sh
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${script_dir}/.." && pwd)"

# Entrypoints a developer runs directly. Each must source scripts/lib/
# cgo-cflags.sh (the Makefile sets the same value itself).
entrypoints="Makefile
scripts/dev/pre-push.sh
scripts/dev/pre-pr.sh
scripts/dev/prove.sh
scripts/dev/precommit-go.sh
scripts/dev/run-selected-gates.sh
scripts/install-local-binaries.sh"

while [ $# -gt 0 ]; do
	case "$1" in
	--root)
		root="$(cd "$2" && pwd)"
		shift 2
		;;
	--list-entrypoints)
		printf '%s\n' "${entrypoints}"
		exit 0
		;;
	*)
		printf 'usage: %s [--root DIR] | --list-entrypoints\n' "$0" >&2
		exit 2
		;;
	esac
done

failures=0
fail() {
	printf 'FAIL: %s\n' "$1" >&2
	failures=$((failures + 1))
}

# Matches a workflow line that provisions Go or compiles with it. Comment
# lines are removed first.
go_user_pattern='actions/setup-go|container:.*ci-tools|(^|[^A-Za-z0-9_./-])go( -C [^ ]+)? (build|test|vet|run|install)([^A-Za-z0-9_-]|$)'

# code_of prints a file without whole-line comments.
code_of() {
	sed -e '/^[[:space:]]*#/d' "$1"
}

# sources_lib succeeds when the file has a real source (or ".") line naming
# cgo-cflags.sh; exports_lib succeeds when it calls eshu_export_cgo_cflags.
sources_lib() {
	code_of "$1" | rg -q -e '(^|[^[:alnum:]_])(source|\.)[[:space:]]+[^#]*cgo-cflags\.sh'
}
exports_lib() {
	code_of "$1" | rg -q -e '(^|[^[:alnum:]_])eshu_export_cgo_cflags([^[:alnum:]_]|$)'
}

# require_lib_use REL describes what is missing for the script at ROOT/REL.
require_lib_use() {
	local rel="$1" why="$2"
	if ! sources_lib "${root}/${rel}"; then
		fail "${rel} ${why} but has no non-comment line that sources scripts/lib/cgo-cflags.sh (#9066)"
	elif ! exports_lib "${root}/${rel}"; then
		fail "${rel} ${why} and sources scripts/lib/cgo-cflags.sh but never calls eshu_export_cgo_cflags (#9066)"
	fi
}

# workflow_env_has_flag prints "yes" when the workflow-level env block (a
# column-zero "env:" key) sets CGO_CFLAGS to a value containing -std=gnu17.
workflow_env_has_flag() {
	awk '
		/^env:[[:space:]]*$/ { inenv = 1; next }
		/^[^[:space:]#]/ { inenv = 0 }
		inenv && /^[[:space:]]+CGO_CFLAGS:/ && /-std=gnu17/ { found = 1 }
		END { print found ? "yes" : "no" }
	' "$1"
}

if [ -d "${root}/.github/workflows" ]; then
	for wf in "${root}"/.github/workflows/*.yml "${root}"/.github/workflows/*.yaml; do
		[ -f "${wf}" ] || continue
		rel="${wf#"${root}"/}"
		body="$(sed -e '/^[[:space:]]*#/d' "${wf}")"
		if printf '%s\n' "${body}" | rg -q -e "${go_user_pattern}"; then
			if [ "$(workflow_env_has_flag "${wf}")" != "yes" ]; then
				fail "${rel} compiles Go but has no workflow-level env CGO_CFLAGS containing -std=gnu17 (#9066)"
			fi
		fi
		# Any assignment that lacks the flag would clobber the workflow value.
		clobbers="$(printf '%s\n' "${body}" | rg -e 'CGO_CFLAGS[:=]' | rg -v -e '-std=gnu17' || true)"
		if [ -n "${clobbers}" ]; then
			fail "${rel} assigns CGO_CFLAGS without -std=gnu17 (#9066): $(printf '%s\n' "${clobbers}" | head -1)"
		fi
	done
fi

if [ -d "${root}/scripts" ]; then
	while IFS= read -r sh_file; do
		[ -n "${sh_file}" ] || continue
		case "$(basename "${sh_file}")" in
		test-* | verify-cgo-cflags-guard.sh) continue ;;
		esac
		require_lib_use "${sh_file}" "runs CGO_ENABLED=1 go"
	done < <(cd "${root}" && { rg -l -e '^[[:space:]]*CGO_ENABLED=1 go ' scripts --glob '*.sh' || true; } | sort)
fi

while IFS= read -r entry; do
	[ -n "${entry}" ] || continue
	if [ ! -f "${root}/${entry}" ]; then
		fail "${entry} is missing; it must carry the #9066 CGO_CFLAGS flag"
	elif [ "${entry}" = "Makefile" ]; then
		if ! { code_of "${root}/${entry}" | rg -q -e '^export CGO_CFLAGS' && code_of "${root}/${entry}" | rg -q -e '-std=gnu17'; }; then
			fail "${entry} has no non-comment export of CGO_CFLAGS with -std=gnu17 (#9066)"
		fi
	else
		require_lib_use "${entry}" "is a developer entrypoint"
	fi
done <<EOF
${entrypoints}
EOF

if [ "${failures}" -ne 0 ]; then
	printf '%s cgo-cflags violation(s); see scripts/lib/cgo-cflags.sh\n' "${failures}" >&2
	exit 1
fi
printf 'cgo-cflags guard: ok\n'
