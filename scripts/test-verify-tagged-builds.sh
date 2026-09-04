#!/usr/bin/env bash
#
# test-verify-tagged-builds.sh - the test mirror and BITES proof for
# scripts/verify-tagged-builds.sh.
#
# It builds throwaway Go modules under a temp root, points the gate at each one
# through TAGGED_BUILDS_REPO_ROOT, and asserts the exit code. The fixtures are
# chosen so the gate cannot pass by accident:
#
#   - a clean package with a single-tag file AND a compound-constraint file,
#     which must pass;
#   - a break behind a single-tag constraint, which must fail (the defect that
#     motivated the gate: a helper deleted elsewhere, a tagged file left
#     uncompilable, nothing reporting it);
#   - a break behind a `&&` constraint, which must also fail. This is the one
#     that separates this gate from the hand-written loop it replaced: that
#     loop took only the first token of a constraint, so it would have run
#     `go vet -tags a` against a `a && b` file, excluded it, and reported
#     success while the file did not compile;
#   - a negated constraint, whose tag must NOT be enabled -- passing it would
#     exclude the very file the gate is trying to compile.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-tagged-builds.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

# Keep the throwaway builds out of the caller's cache, but honour an explicit
# one: parallel agents in this repo set a per-worktree GOCACHE and sharing it
# across concurrent go commands is what corrupts incremental builds.
export GOCACHE="${GOCACHE:-${tmp_root}/gocache}"

failures=0

note() { printf 'test-verify-tagged-builds: %s\n' "$*"; }

fail() {
	printf 'FAIL: %s\n' "$*" >&2
	failures=$((failures + 1))
}

# init_module writes a module whose internal/query package holds one untagged
# file plus the tagged files named by the caller. Each tagged file is given as
# "<name>@@<constraint>@@<body>" -- `@@` rather than a single character, because
# a constraint can contain `|`, `&` and `!`.
init_module() {
	local name="$1"
	shift
	local dir="${tmp_root}/${name}"
	local pkg="${dir}/go/internal/query"
	mkdir -p "${pkg}"
	cat >"${dir}/go/go.mod" <<-EOF
		module example.invalid/tagged

		go 1.22
	EOF
	cat >"${pkg}/plain.go" <<-EOF
		package query

		// Helper is the symbol the tagged files reach for.
		func Helper() int { return 1 }
	EOF
	local spec file constraint body rest
	for spec in "$@"; do
		file="${spec%%@@*}"
		rest="${spec#*@@}"
		constraint="${rest%%@@*}"
		body="${rest#*@@}"
		cat >"${pkg}/${file}" <<-EOF
			//go:build ${constraint}

			package query

			${body}
		EOF
	done
	printf '%s\n' "${dir}"
}

run_gate() {
	local dir="$1"
	TAGGED_BUILDS_REPO_ROOT="${dir}" bash "${verifier}" \
		>"${tmp_root}/gate.out" 2>"${tmp_root}/gate.err"
}

expect_pass() {
	local dir="$1" what="$2"
	if run_gate "${dir}"; then
		note "PASS ${what}"
		return 0
	fi
	fail "${what}: expected exit 0"
	sed -n '1,40p' "${tmp_root}/gate.out" >&2
	sed -n '1,40p' "${tmp_root}/gate.err" >&2
}

expect_fail() {
	local dir="$1" what="$2" needle="$3"
	if run_gate "${dir}"; then
		fail "${what}: expected a non-zero exit"
		sed -n '1,40p' "${tmp_root}/gate.out" >&2
		return 0
	fi
	if ! rg -q -- "${needle}" "${tmp_root}/gate.out"; then
		fail "${what}: expected the report to name ${needle}"
		sed -n '1,40p' "${tmp_root}/gate.out" >&2
		return 0
	fi
	note "PASS ${what}"
}

clean="$(init_module clean \
	"single_tagged_test.go@@tag_one@@func UsesHelperOne() int { return Helper() }" \
	"compound_tagged_test.go@@tag_one && tag_two@@func UsesHelperTwo() int { return Helper() }" \
	"negated_test.go@@!tag_one@@func UsesHelperThree() int { return Helper() }")"
expect_pass "${clean}" "a package whose tagged files all compile"

# The gate has to report what it compiled AND what it did not, or a run that
# compiled nothing reads exactly like a clean one.
if ! rg -q 'vetted 2 build configuration\(s\), skipped 1' "${tmp_root}/gate.out"; then
	fail "expected the clean run to report two vetted configurations and one skip"
	sed -n '1,40p' "${tmp_root}/gate.out" >&2
fi
# A negated constraint must not have its own tag enabled -- turning it on would
# exclude the file. The default build already compiles it, so it is a skip.
if ! rg -q 'SKIP.*\[!tag_one\].*no selectable tags' "${tmp_root}/gate.out"; then
	fail "expected !tag_one to be skipped rather than enabled"
	sed -n '1,40p' "${tmp_root}/gate.out" >&2
fi

broken_single="$(init_module broken_single \
	"single_tagged_test.go@@tag_one@@func UsesHelperOne() int { return MissingHelper() }" \
	"compound_tagged_test.go@@tag_one && tag_two@@func UsesHelperTwo() int { return Helper() }")"
expect_fail "${broken_single}" "a break behind a single-tag constraint" 'FAIL'

broken_compound="$(init_module broken_compound \
	"single_tagged_test.go@@tag_one@@func UsesHelperOne() int { return Helper() }" \
	"compound_tagged_test.go@@tag_one && tag_two@@func UsesHelperTwo() int { return MissingHelper() }")"
expect_fail "${broken_compound}" "a break behind a compound && constraint" 'tag_one,tag_two'

# An `||` alternation gets one run per alternative. Enabling every alternative
# at once is not what the constraint means, and where the alternatives each
# define the same symbol -- the shape cmd/reducer's perf5854 files have -- it
# does not compile, so the gate would report a break of its own making.
alternation="$(init_module alternation \
	"head_test.go@@variant_head@@func Variant() string { return \"head\" }" \
	"main_test.go@@variant_main@@func Variant() string { return \"main\" }" \
	"either_test.go@@variant_head || variant_main@@func UsesVariant() string { return Variant() }")"
expect_pass "${alternation}" "an || alternation vetted one alternative at a time"
if ! rg -q 'tags=variant_head$' "${tmp_root}/gate.out" ||
	! rg -q 'tags=variant_main$' "${tmp_root}/gate.out"; then
	fail "expected the alternation to be vetted once per alternative"
	sed -n '1,40p' "${tmp_root}/gate.out" >&2
fi
if rg -q 'tags=variant_head,variant_main' "${tmp_root}/gate.out"; then
	fail "the alternation was vetted with both alternatives enabled at once"
	sed -n '1,40p' "${tmp_root}/gate.out" >&2
fi

# A GOOS term cannot be selected with -tags: forcing one pulls two copies of the
# standard library's platform files into the same build and fails inside
# internal/goos, which would read as a break in the package under test.
platform="$(init_module platform \
	"windows_test.go@@windows@@func WindowsOnly() int { return Helper() }")"
expect_pass "${platform}" "a GOOS-gated constraint is skipped, not forced"
if ! rg -q 'SKIP.*\[windows\].*platform-gated' "${tmp_root}/gate.out"; then
	fail "expected the windows constraint to be reported as platform-gated"
	sed -n '1,40p' "${tmp_root}/gate.out" >&2
fi

# A package with no tagged files at all is not a failure, but it must say so
# rather than exiting 0 silently.
empty="$(init_module empty)"
expect_pass "${empty}" "a package with no build constraints"
if ! rg -q 'has no //go:build constraints' "${tmp_root}/gate.out"; then
	fail "expected the empty run to say it found no constraints"
	sed -n '1,40p' "${tmp_root}/gate.out" >&2
fi

# A package path that does not exist is an operator error, not a pass.
missing="$(init_module missing)"
if TAGGED_BUILDS_REPO_ROOT="${missing}" bash "${verifier}" ./internal/nope \
	>"${tmp_root}/gate.out" 2>"${tmp_root}/gate.err"; then
	fail "expected a nonexistent package path to fail"
else
	note "PASS a nonexistent package path is refused"
fi

if [[ ${failures} -ne 0 ]]; then
	printf 'test-verify-tagged-builds: %d assertion(s) failed\n' "${failures}" >&2
	exit 1
fi
printf 'test-verify-tagged-builds: OK\n'
