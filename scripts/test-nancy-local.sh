#!/usr/bin/env bash
#
# Executable regression test for scripts/dev/nancy-local.sh (#5791, #5804).
#
# A prior version of this coverage only grepped the SOURCE TEXT of
# scripts/dev/precommit-go.sh for a `go list` redirection string. That
# assertion could not tell a working pipeline from a broken one: deleting the
# `<"${deps_json}"` redirect while leaving the producer line intact still
# matched the grep, silently restoring the #5791 empty-stdin bug (caught in
# PR #5806 review). This test instead runs the REAL script end-to-end, with
# fake `go` and `nancy` binaries prepended to PATH, and asserts actual exit
# codes and diagnostic text for each scenario nancy-local.sh must tell apart.
#
# Cases:
#   1. `go list` succeeds but yields zero dependencies -> nancy sees
#      genuinely empty stdin and reports its real "StdIn is invalid or
#      empty" error -> deferred non-fatally (advisory, exit 0).
#   2. `go list` succeeds, nancy hits an OSS Index transport/auth failure
#      ("Error:" prefix, e.g. 401 Unauthorized) -> deferred non-fatally
#      (advisory, exit 0).
#   3. `go list` succeeds, nancy performs a real scan and finds nothing ->
#      exit 0, "no vulnerable dependencies found".
#   4. `go list` succeeds, nancy finds a genuine vulnerability (nonzero exit,
#      output does NOT start with "Error:") -> the gate still BLOCKS (exit
#      1), because nancy being advisory in the CI-gate registry must not be
#      confused with silently accepting a real finding.
#   5. (bonus, beyond the four required cases) `go list` itself fails (a
#      broken module/build) -> the gate BLOCKS (exit 1) with a `go list`
#      specific diagnostic, never mislabeled as "vulnerable dependencies
#      found" — this exact conflation was a self-review catch on PR #5806.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/dev/nancy-local.sh"

if [ ! -x "${script}" ]; then
	printf 'test-nancy-local: missing executable script: %s\n' "${script}" >&2
	exit 1
fi

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fail() {
	printf 'test-nancy-local: %s\n' "$*" >&2
	exit 1
}

# write_fake_go writes a `go` shim at "<dir>/go" that only understands
# `list -json -deps ./...` (the sole invocation nancy-local.sh makes),
# printing the given stdout/stderr and exiting with the given code.
write_fake_go() {
	local dir="$1" stdout="$2" stderr="$3" exit_code="$4"
	mkdir -p "${dir}"
	cat >"${dir}/go" <<SCRIPT
#!/usr/bin/env bash
case "\$*" in
	"list -json -deps ./...") ;;
	*)
		echo "fake go: unexpected invocation: \$*" >&2
		exit 99
		;;
esac
printf '%s' $(printf '%q' "${stdout}")
printf '%s' $(printf '%q' "${stderr}") >&2
exit ${exit_code}
SCRIPT
	chmod +x "${dir}/go"
}

# write_fake_nancy writes a `nancy` shim at the given path that only
# understands `sleuth --no-color`. It actually reads stdin (like the real
# nancy does) and reproduces nancy's real empty-input error when stdin is
# empty, regardless of the canned stdout/exit_code arguments — this is what
# makes the test sensitive to nancy-local.sh's own `<"${deps_json}"`
# redirect: removing that redirect starves nancy of the real dependency
# graph, so a case whose fake `go` produced non-empty output would
# nonetheless see empty stdin here and get the empty-input error instead of
# its configured canned response, failing the case's exit/needle assertion.
# When stdin is non-empty, it prints the given canned stdout and exits with
# the given code — reproducing nancy's real output shapes (verified against
# nancy v1.2.0) without a real nancy binary, network access, or OSS Index
# credentials.
write_fake_nancy() {
	local path="$1" stdout="$2" exit_code="$3"
	cat >"${path}" <<SCRIPT
#!/usr/bin/env bash
case "\$*" in
	"sleuth --no-color") ;;
	*)
		echo "fake nancy: unexpected invocation: \$*" >&2
		exit 99
		;;
esac
input="\$(cat)"
if [ -z "\${input}" ]; then
	printf '%s\n' "Error: StdIn is invalid or empty. Did you forget to pipe 'go list' to nancy?"
	exit 1
fi
printf '%s\n' $(printf '%q' "${stdout}")
exit ${exit_code}
SCRIPT
	chmod +x "${path}"
}

# run_case wires up a scratch cache dir + fake go/nancy pair, runs
# nancy-local.sh for real, and asserts its exit code and that its combined
# output contains the expected needle.
run_case() {
	local name="$1" go_stdout="$2" go_stderr="$3" go_exit="$4" \
		nancy_stdout="$5" nancy_exit="$6" want_exit="$7" want_needle="$8"

	local case_dir="${tmp_root}/${name}"
	mkdir -p "${case_dir}/bin" "${case_dir}/gopkg" "${case_dir}/cache"
	write_fake_go "${case_dir}/bin" "${go_stdout}" "${go_stderr}" "${go_exit}"
	write_fake_nancy "${case_dir}/bin/nancy" "${nancy_stdout}" "${nancy_exit}"

	local out got_exit
	set +e
	out="$(PATH="${case_dir}/bin:${PATH}" bash "${script}" "${case_dir}/gopkg" "${case_dir}/cache" "${case_dir}/bin/nancy" 2>&1)"
	got_exit=$?
	set -e

	[ "${got_exit}" -eq "${want_exit}" ] ||
		fail "${name}: exit=${got_exit}, want ${want_exit}. output:
${out}"

	case "${out}" in
		*"${want_needle}"*) ;;
		*)
			fail "${name}: output missing expected needle '${want_needle}'. output:
${out}"
			;;
	esac

	printf 'PASS: %s\n' "${name}"
}

# --- Case 1: go list ok, empty dependency graph -> nancy's real "StdIn is
# invalid or empty" -> deferred non-fatally.
run_case "empty-stdin-defers-non-fatally" \
	"" "" 0 \
	"Error: StdIn is invalid or empty. Did you forget to pipe 'go list' to nancy?" 1 \
	0 "deferring non-fatally"

# --- Case 2: go list ok, nancy hits a transport/auth failure -> deferred
# non-fatally, never reported as a vulnerability finding.
run_case "transport-auth-failure-defers-non-fatally" \
	'{"ImportPath":"example.com/foo"}' "" 0 \
	"Error: An error occurred: [401 Unauthorized] error accessing OSS Index" 1 \
	0 "deferring non-fatally"

# --- Case 3: go list ok, nancy performs a real scan and finds nothing.
run_case "clean-scan-passes" \
	'{"ImportPath":"example.com/foo"}' "" 0 \
	"Audited Dependencies: 42
Vulnerable Dependencies: 0" 0 \
	0 "no vulnerable dependencies found"

# --- Case 4: go list ok, nancy finds a genuine vulnerability (no "Error:"
# prefix) -> the gate still blocks, regardless of nancy's advisory
# classification in the CI-gate registry.
run_case "genuine-finding-still-blocks" \
	'{"ImportPath":"example.com/foo"}' "" 0 \
	"pkg:golang/example.com/bad@v1.0.0
  CVE-2024-00000  A made-up vulnerability for this test
Vulnerable Dependencies: 1" 3 \
	1 "vulnerable dependencies found"

# --- Case 5 (bonus): go list itself fails -> the gate blocks with a
# go-list-specific message, never "vulnerable dependencies found" (this
# conflation was caught in self-review on PR #5806: a pipeline-only success
# check cannot distinguish a build failure from a real finding).
run_case "go-list-failure-blocks-distinctly" \
	"" "go: module example.com/foo: broken build constraint" 1 \
	"should not run" 0 \
	1 "'go list -json -deps ./...' failed"

printf 'PASS: nancy-local.sh distinguishes empty-stdin, transport/auth failure, clean scan, real finding, and go-list failure\n'
