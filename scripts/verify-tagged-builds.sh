#!/usr/bin/env bash
#
# verify-tagged-builds.sh - compile-check the Go files that no ordinary build
# ever looks at.
#
# A file whose first line is a `//go:build <constraint>` is excluded from the
# default tag set, so `go build`, `go vet` and `go test` all skip it. A helper
# it calls can be deleted somewhere else in the package and nothing reports it:
# the file simply stops compiling, silently, and stays that way through any
# number of green CI runs. That is not hypothetical here. #5167's live NornicDB
# complexity proof (`live_nornicdb_complexity_grant`) lost
# `ptrToCodeGrantAuthContext` to an unrelated refactor and sat uncompilable
# through a `make pre-pr` run, a push, a full CI run and eight review rounds.
#
# This gate closes that. It reads the constraints out of the files rather than
# from a hand-maintained list -- a tag added later is swept without anyone
# remembering to add it -- and runs one `go vet` per distinct constraint with
# every identifier that constraint needs enabled.
#
# Usage:
#   scripts/verify-tagged-builds.sh                     ./internal/query
#   scripts/verify-tagged-builds.sh --all               every tagged package
#   scripts/verify-tagged-builds.sh <pkg> [pkg...]      go-relative package dirs
#
# Package arguments are directories relative to `go/`, with or without a
# leading `./`. A trailing `/...` sweeps the directory tree beneath it.
#
# `--all` finds the packages itself: every directory under `go/` holding at
# least one `//go:build` file. That is what the local gate runs, so a tagged
# file added to a package nobody listed here is still swept.
#
# Credential-free: it compiles, it does not run anything. The tagged suites
# themselves usually need a pinned backend container; vetting them needs
# nothing, which is why this belongs in the local gate and their execution does
# not.
#
# TAGGED_BUILDS_REPO_ROOT points the gate at an isolated tree, so
# scripts/test-verify-tagged-builds.sh drives this exact CLI entry point rather
# than a reimplementation of it.
#
# LIMITATIONS, both by construction:
#   - Only `//go:build` is read. The pre-Go-1.17 `// +build` form is not, and
#     this module has none.
#   - A GOOS/GOARCH term (`linux`, `arm64`, `unix`) cannot be turned on with
#     `-tags`; the toolchain takes it from the environment. A constraint that
#     rests on one is reported as swept while its file may not have been
#     compiled on this host. `internal/query` has none, and a package that
#     grows one needs a GOOS matrix rather than a longer tag list.
#   - `ignore` is skipped. It is the conventional name for "never build this",
#     and enabling it would compile files nobody intends to.
set -euo pipefail

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# The repo root is the parent of this script's own directory, computed
# directly -- NOT via `git rev-parse --show-toplevel`. git exports GIT_DIR to
# every hook it runs, and with the absolute GIT_DIR a linked worktree has,
# rev-parse stops discovering the work tree and reports the directory git ran
# in. See verify-dirgate.sh and verify-markdown-line-cap.sh for the same note
# and the gate that silently passed everything until it was found.
repo_root="${TAGGED_BUILDS_REPO_ROOT:-$(cd "${script_root}/.." && pwd)}"
go_dir="${repo_root}/go"
gate_name="tagged-builds"

die() {
	printf '%s: %s\n' "${gate_name}" "$*" >&2
	exit 2
}

[[ -d "${go_dir}" ]] || die "no go module at ${go_dir}"

packages=()
if [[ "${1:-}" == "--all" ]]; then
	shift
	[[ $# -eq 0 ]] || die "--all takes no package arguments"
	# Discover rather than list. A package that grows its first tagged file is
	# swept without anyone editing this script or its caller, which is the
	# whole point: the defect this gate exists for is something being true only
	# while someone remembers it.
	while IFS= read -r dir; do
		[[ -n "${dir}" ]] && packages+=("./${dir}")
	done < <(
		cd "${go_dir}" &&
			rg -l '^//go:build' --glob '*.go' . |
			sed -e 's|^\./||' -e 's|/[^/]*$||' |
			LC_ALL=C sort -u
	)
	[[ ${#packages[@]} -gt 0 ]] || die "--all found no //go:build files under ${go_dir}"
else
	packages=("$@")
	[[ ${#packages[@]} -gt 0 ]] || packages=("./internal/query")
fi

# split_on prints the fields of "$1" separated by the literal operator "$2", one
# per line, with surrounding whitespace trimmed. Pure bash: no sed, no locale,
# and no replacement escape whose portability has to be argued about.
split_on() {
	local rest="$1" separator="$2" field
	while :; do
		if [[ "${rest}" == *"${separator}"* ]]; then
			field="${rest%%"${separator}"*}"
			rest="${rest#*"${separator}"}"
		else
			field="${rest}"
			rest=""
		fi
		field="${field#"${field%%[![:space:]]*}"}"
		field="${field%"${field##*[![:space:]]}"}"
		printf '%s\n' "${field}"
		[[ -n "${rest}" ]] || break
	done
}

# platform_term reports whether a term names a GOOS, GOARCH, or one of the
# toolchain's own meta-tags. These are NOT selectable with -tags: the go command
# matches them against the build environment, and forcing one on a host that is
# not it pulls two copies of the standard library's platform files into the same
# package. `go vet -tags windows ./internal/eshulocal` on macOS does not compile
# the Windows file -- it fails inside internal/goos with GOOS redeclared.
platform_term() {
	case "$1" in
	aix | android | darwin | dragonfly | freebsd | hurd | illumos | ios | js | linux | nacl | netbsd | openbsd | plan9 | solaris | wasip1 | windows | zos) return 0 ;;
	386 | amd64 | amd64p32 | arm | arm64 | arm64be | armbe | loong64 | mips* | ppc | ppc64 | ppc64le | riscv | riscv64 | s390 | s390x | sparc | sparc64 | wasm) return 0 ;;
	unix | cgo | gc | gccgo | race | msan | asan | boringcrypto) return 0 ;;
	*) return 1 ;;
	esac
}

# alternative_run prints what one conjunction of terms needs, as `TAGS:<comma
# list>`, `SKIP:<reason>`, or `ERROR:<reason>`.
#
# Negated terms are dropped: the file is built when that tag is OFF, so passing
# it would exclude the very file this gate exists to compile. `ignore` is
# dropped for the same reason it always is -- it names "never build this".
# Anything else that is not a legal tag identifier is an ERROR, not a silent
# drop: a term this function does not understand means the constraint was not
# parsed, and a gate that reports PASS on a constraint it could not read is the
# failure class it exists to remove.
alternative_run() {
	local terms=() term
	while IFS= read -r term; do
		[[ -n "${term}" ]] || continue
		if [[ "${term}" == "!"* || "${term}" == "ignore" ]]; then
			continue
		fi
		# platform_term is consulted BEFORE the identifier check, because `386`
		# is a legal GOARCH and not a legal Go identifier. With the checks the
		# other way round it was reported as an unrecognized term and failed the
		# run, which is loud but wrong: it is platform-gated like every other
		# GOARCH. No such constraint exists in the module today (reviewer's note
		# on addendum 8e).
		if platform_term "${term}"; then
			printf 'SKIP:platform-gated (%s); -tags cannot select a GOOS/GOARCH\n' "${term}"
			return
		fi
		if [[ ! "${term}" =~ ^[A-Za-z_][A-Za-z0-9_.]*$ ]]; then
			printf 'ERROR:unrecognized term %s\n' "${term}"
			return
		fi
		terms+=("${term}")
	done < <(split_on "$1" "&&")
	if [[ ${#terms[@]} -eq 0 ]]; then
		printf 'SKIP:no selectable tags; the default build already compiles this file\n'
		return
	fi
	local list
	list="$(
		IFS=,
		printf '%s' "${terms[*]}"
	)"
	printf 'TAGS:%s\n' "${list}"
}

# constraint_runs prints one line per vet run a constraint needs, or a single
# ERROR when the constraint is outside the shape this expander can represent
# exactly.
#
# The shape it accepts, and nothing wider:
#
#   * one term, optionally negated  -- live_nornicdb_dead_code_incoming, !windows
#   * terms joined only by &&       -- perf5854_ack && perf5740_completion
#   * terms joined only by ||       -- perf5854_head || perf5854_main
#
# with no parentheses anywhere. Every `//go:build` line in this module is one of
# those. Anything else is an ERROR that fails the run: not a SKIP, and never a
# PASS. An unreadable constraint means the file behind it was not compiled, and
# a gate that answers PASS for a file it never compiled is the failure class it
# exists to remove.
#
# Two shapes taught that the hard way, both reported as false greens against a
# fixture whose tagged file did not compile:
#
#   - `!(tag_a || tag_b)`. The parentheses used to be flattened to spaces before
#     anything looked at them, which turns the constraint into `! tag_a ||
#     tag_b`: the `!` binds to the first term only, so the gate dropped `!tag_a`
#     as a negation, ran `-tags tag_b`, and reported SKIP plus PASS. Neither
#     touched the file, which is built when BOTH tags are off.
#   - `tag_a && (tag_b || tag_c)`. This was a SKIP with the honest reason that
#     flattening loses the `tag_a` from one branch -- but a SKIP on a blocking
#     gate is still a green run over an uncompiled file.
#
# An `||` alternation gets one run per alternative, not one run with every
# alternative enabled. Enabling them together is not what the constraint means
# and it does not even compile: cmd/reducer's `perf5854_head || perf5854_main`
# files each declare the same symbol, so turning both on redeclares it and the
# gate reports a break that is its own.
#
# The split counts its own output. This used to be `sed 's/||/\n/g'`, whose
# `\n` is a GNU extension: on a sed that inserts a literal `n` instead,
# `perf5854_head || perf5854_main` collapses to the single token
# `perf5854_headnperf5854_main`, which passes the identifier check, vets
# trivially, and reports PASS having compiled nothing. split_on is pure bash so
# there is no such sed to depend on, and the count check below turns any future
# collapse into a FAIL rather than a silent green.
constraint_runs() {
	local constraint="$1"
	# Parentheses are refused whole. Representing them exactly needs a real
	# boolean expander, and every partial attempt at one here has produced a
	# green run over a file it never built. No constraint in this module uses
	# them; a change that adds one should extend this function deliberately and
	# bring its own test, not inherit a guess.
	if [[ "${constraint}" == *"("* || "${constraint}" == *")"* ]]; then
		printf 'ERROR:uses a parenthesised group, which this gate does not expand; the file behind it was NOT compiled\n'
		return
	fi
	if [[ "${constraint}" == *"&&"* && "${constraint}" == *"||"* ]]; then
		printf 'ERROR:mixes && and ||, which this gate does not expand; the file behind it was NOT compiled\n'
		return
	fi
	if [[ "${constraint}" == *"||"* ]]; then
		local alternatives=() alternative
		while IFS= read -r alternative; do
			[[ -n "${alternative}" ]] && alternatives+=("${alternative}")
		done < <(split_on "${constraint}" "||")
		if [[ ${#alternatives[@]} -lt 2 ]]; then
			printf 'ERROR:alternation split produced %d alternative(s); the constraint was not parsed\n' \
				"${#alternatives[@]}"
			return
		fi
		for alternative in "${alternatives[@]}"; do
			alternative_run "${alternative}"
		done
		return
	fi
	alternative_run "${constraint}"
}

# package_dir prints the directory one package argument names, and refuses a
# path that is not there. It is called from the main shell, never from a
# process substitution: a `die` inside one exits only that subshell, so an
# unchecked path would have been reported as a clean sweep of zero files.
package_dir() {
	local spec="${1#./}" dir
	if [[ "${spec}" == */... ]]; then
		dir="${go_dir}/${spec%/...}"
	else
		dir="${go_dir}/${spec}"
	fi
	[[ -d "${dir}" ]] || die "package path does not exist: ${1} (looked in ${dir})"
	printf '%s\n' "${dir}"
}

# go_files prints the .go files one package argument covers. `pkg/...` recurses;
# a bare package is that directory only. rg --files is the repo's
# file-discovery rule; -g scopes it to Go sources and --max-depth mirrors the
# non-recursive case.
go_files() {
	local spec="$1" dir="$2"
	if [[ "${spec}" == */... ]]; then
		rg --files -g '*.go' "${dir}" 2>/dev/null || true
	else
		rg --files --max-depth 1 -g '*.go' "${dir}" 2>/dev/null || true
	fi
}

# vet_package prints the package pattern go vet should be given for one
# argument, which is the argument itself once it carries a leading `./`.
vet_package() {
	local spec="$1"
	[[ "${spec}" == ./* ]] && printf '%s\n' "${spec}" || printf './%s\n' "${spec}"
}

# Resolve every path before vetting anything, so a typo fails immediately
# rather than after a dozen passing constraints. package_dir is called here, in
# the main shell: a `die` inside a process substitution exits only that
# subshell, so an unchecked path would have been reported as a clean sweep of
# zero files.
package_dirs=()
for package in "${packages[@]}"; do
	package_dirs+=("$(package_dir "${package}")")
done

exit_status=0
swept=0
skipped=0

for index in "${!packages[@]}"; do
	package="${packages[index]}"
	pattern="$(vet_package "${package}")"
	# One pass per DISTINCT constraint, not per file: two files sharing a
	# constraint compile together, and a constraint appearing twice would
	# otherwise be vetted twice.
	constraints=()
	while IFS= read -r constraint; do
		[[ -n "${constraint}" ]] && constraints+=("${constraint}")
	done < <(
		while IFS= read -r file; do
			[[ -n "${file}" ]] || continue
			sed -n 's|^//go:build[[:space:]]\{1,\}||p' "${file}"
		done < <(go_files "${package}" "${package_dirs[index]}") | LC_ALL=C sort -u
	)

	if [[ ${#constraints[@]} -eq 0 ]]; then
		printf '%s: %s has no //go:build constraints\n' "${gate_name}" "${pattern}"
		continue
	fi

	for constraint in "${constraints[@]}"; do
		while IFS= read -r run; do
			[[ -n "${run}" ]] || continue
			case "${run}" in
			SKIP:*)
				skipped=$((skipped + 1))
				printf 'SKIP  %s  [%s]  %s\n' "${pattern}" "${constraint}" "${run#SKIP:}"
				;;
			ERROR:*)
				# A constraint this gate could not read is a failure, never a
				# skip. Reporting PASS on one is the exact shape the gate exists
				# to remove.
				printf 'ERROR %s  [%s]  %s\n' "${pattern}" "${constraint}" "${run#ERROR:}"
				exit_status=1
				;;
			TAGS:*)
				tag_list="${run#TAGS:}"
				swept=$((swept + 1))
				if (cd "${go_dir}" && go vet -tags "${tag_list}" "${pattern}"); then
					printf 'PASS  %s  [%s]  tags=%s\n' "${pattern}" "${constraint}" "${tag_list}"
				else
					printf 'FAIL  %s  [%s]  tags=%s\n' "${pattern}" "${constraint}" "${tag_list}"
					exit_status=1
				fi
				;;
			esac
		done < <(constraint_runs "${constraint}")
	done
done

# Report both counts for the same reason verify-markdown-line-cap.sh reports
# its own: a run that compiled nothing exits 0 and is indistinguishable from
# "compiled everything, all clean" unless the numbers are printed. A rising
# skip count is the signal that the sweep is covering less than it looks like.
printf '%s: vetted %d build configuration(s), skipped %d, across %d package path(s)\n' \
	"${gate_name}" "${swept}" "${skipped}" "${#packages[@]}"
exit "${exit_status}"
