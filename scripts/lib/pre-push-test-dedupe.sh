#!/usr/bin/env bash
# pre-push-test-dedupe.sh - run each changed package's tests once in pre-push.
#
# Sourced by scripts/dev/pre-push.sh. `go test -race` runs every test that the
# plain `go test` runs, so a plain pass over a package that the race pass also
# covers only repeats work. The one exception is a file constrained
# `//go:build !race`: it is compiled out of the race binary, so its tests run
# only in the plain pass (internal/graph, whispercpp and toml/nesting have such
# files). A package with one keeps its plain run.
#
# Expects go_dir (the repo's go/ directory) from the caller.

# pre_push_has_norace_file reports (exit 0) whether a ./-relative package dir
# holds a .go file that a race build leaves out. An unreadable directory counts
# as "has one": keeping a plain run is the safe answer when we cannot look.
#
# The pattern accepts leading whitespace, because Go still honors an indented
# `//go:build` line, and a `!` anywhere before `race`, so a negated group such as
# `!(race || linux)` counts. It can also match a constraint that only mentions
# race after an unrelated `!` (`!windows && race`); that errs toward keeping the
# plain run, which only costs time.
pre_push_has_norace_file() {
	local dir="${go_dir}/${1#./}" status=0
	rg -q --no-ignore --max-depth 1 --glob '*.go' \
		'^[[:space:]]*(//go:build|// \+build) .*!.*\brace\b' "${dir}" 2>/dev/null || status=$?
	[[ "${status}" -ne 1 ]]
}

# pre_push_plain_test_dirs reads the ./-relative dirs selected for the plain
# `go test` on stdin and prints those that still need it. $1 names a file listing
# the dirs the race pass covers (the changed packages). A selected dir the race
# pass does not cover, such as a fixture consumer or a parser child package,
# stays; so does any dir with a !race file.
pre_push_plain_test_dirs() {
	local race_list="$1" d
	while IFS= read -r d; do
		[[ -n "${d}" ]] || continue
		if rg -Fxq -- "${d}" "${race_list}" 2>/dev/null && ! pre_push_has_norace_file "${d}"; then
			continue
		fi
		printf '%s\n' "${d}"
	done
}
