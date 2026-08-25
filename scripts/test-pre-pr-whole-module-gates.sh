#!/usr/bin/env bash
# Regression tests for pre-PR scheduling and the fast local test runner.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/dev/pre-pr.sh"
precommit_script="${repo_root}/scripts/dev/precommit-go.sh"
fast_runner="${repo_root}/tests/run_tests.sh"
parser_agent_guidance="${repo_root}/go/internal/parser/AGENTS.md"
canonical_parser_test_docs=(
	"${repo_root}/AGENTS.md"
	"${repo_root}/CLAUDE.md"
	"${repo_root}/CONTRIBUTING.md"
	"${repo_root}/docs/public/contributing-language-support.md"
	"${repo_root}/docs/public/guides/fixture-ecosystems.md"
	"${repo_root}/docs/public/reference/local-testing/quick-verification-matrix.md"
	"${repo_root}/docs/public/reference/local-testing/verification-gates.md"
	"${repo_root}/specs/product-claims.v1.yaml"
)

fail() {
	printf 'test-pre-pr-whole-module-gates: %s\n' "$*" >&2
	exit 1
}

require() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${script}" || \
		fail "missing ${label}: ${needle}"
}

require_precommit() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${precommit_script}" || \
		fail "missing ${label}: ${needle}"
}

require_block() {
	local label="$1" needle="$2"
	rg --multiline --fixed-strings --quiet -- "${needle}" "${script}" || \
		fail "missing ${label}: ${needle}
(this needle is matched whitespace-exact: a reformat -- tabs-to-spaces, a trailing-whitespace cleanup, CRLF line endings -- trips this the same as a real deletion. Before assuming the code is gone, diff ${script} for a reformat first.)"
}

reject() {
	local label="$1" needle="$2"
	if rg --fixed-strings --quiet -- "${needle}" "${script}"; then
		fail "unexpected ${label}: ${needle}"
	fi
}

reject_precommit() {
	local label="$1" needle="$2"
	if rg --fixed-strings --quiet -- "${needle}" "${precommit_script}"; then
		fail "unexpected ${label}: ${needle}"
	fi
}

[[ -f "${script}" ]] || fail "missing ${script}"
bash -n "${script}" || fail "pre-pr.sh has a syntax error"
[[ -f "${precommit_script}" ]] || fail "missing ${precommit_script}"
bash -n "${precommit_script}" || fail "precommit-go.sh has a syntax error"
[[ -f "${fast_runner}" ]] || fail "missing ${fast_runner}"
bash -n "${fast_runner}" || fail "tests/run_tests.sh has a syntax error"

cache_paths="$("${precommit_script}" cache-paths)" ||
	fail "precommit-go.sh cache-paths failed"
tool_cache_dir="$(printf '%s\n' "${cache_paths}" | rg '^tool_cache_dir=' | cut -d= -f2-)"
worktree_cache_dir="$(printf '%s\n' "${cache_paths}" | rg '^worktree_cache_dir=' | cut -d= -f2-)"
golangci_cache_dir="$(printf '%s\n' "${cache_paths}" | rg '^golangci_cache_dir=' | cut -d= -f2-)"
expected_git_common="$(git -C "${repo_root}" rev-parse --git-common-dir)"
case "${expected_git_common}" in
	/*) ;;
	*) expected_git_common="${repo_root}/${expected_git_common}" ;;
esac
expected_git_dir="$(git -C "${repo_root}" rev-parse --git-dir)"
case "${expected_git_dir}" in
	/*) ;;
	*) expected_git_dir="${repo_root}/${expected_git_dir}" ;;
esac
expected_tool_cache="${expected_git_common}/eshu-precommit"
expected_worktree_cache="${expected_git_dir}/eshu-precommit-state"

[[ "${tool_cache_dir}" == "${expected_tool_cache}" ]] ||
	fail "tool cache = ${tool_cache_dir}, want ${expected_tool_cache}"
[[ "${worktree_cache_dir}" == "${expected_worktree_cache}" ]] ||
	fail "worktree cache = ${worktree_cache_dir}, want ${expected_worktree_cache}"
[[ "${golangci_cache_dir}" == "${worktree_cache_dir}/golangci-lint" ]] ||
	fail "golangci cache = ${golangci_cache_dir}, want ${worktree_cache_dir}/golangci-lint"

temp_root="$(mktemp -d)"
trap 'rm -rf "${temp_root}"' EXIT
mini_repo="${temp_root}/repo"
linked_worktree="${temp_root}/linked"
git init -q "${mini_repo}"
canonical_mini_repo="$(git -C "${mini_repo}" rev-parse --show-toplevel)"
mkdir -p "${mini_repo}/scripts/dev" "${mini_repo}/.github/workflows"
cp "${precommit_script}" "${mini_repo}/scripts/dev/precommit-go.sh"
printf '%s\n' 'run: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2' > "${mini_repo}/.github/workflows/test.yml"
printf '%s\n' 'run: go install github.com/securego/gosec/v2/cmd/gosec@v2.27.1' > "${mini_repo}/.github/workflows/security-scan.yml"
git -C "${mini_repo}" add scripts .github
git -C "${mini_repo}" -c user.name=cache-test -c user.email=cache-test@example.invalid commit -qm init
git -C "${mini_repo}" worktree add --detach -q "${linked_worktree}" HEAD

main_paths="$(cd "${mini_repo}" && scripts/dev/precommit-go.sh cache-paths)"
linked_paths="$(cd "${linked_worktree}" && scripts/dev/precommit-go.sh cache-paths)"
main_tool_cache="$(printf '%s\n' "${main_paths}" | rg '^tool_cache_dir=' | cut -d= -f2-)"
linked_tool_cache="$(printf '%s\n' "${linked_paths}" | rg '^tool_cache_dir=' | cut -d= -f2-)"
main_worktree_cache="$(printf '%s\n' "${main_paths}" | rg '^worktree_cache_dir=' | cut -d= -f2-)"
linked_worktree_cache="$(printf '%s\n' "${linked_paths}" | rg '^worktree_cache_dir=' | cut -d= -f2-)"

[[ "${main_tool_cache}" == "${linked_tool_cache}" ]] ||
	fail "linked worktrees did not share the tool cache"
[[ "${main_tool_cache}" == "${canonical_mini_repo}/.git/eshu-precommit" ]] ||
	fail "normal-checkout tool cache = ${main_tool_cache}, want ${canonical_mini_repo}/.git/eshu-precommit"
[[ "${main_worktree_cache}" == "${canonical_mini_repo}/.git/eshu-precommit-state" ]] ||
	fail "normal-checkout mutable cache = ${main_worktree_cache}, want ${canonical_mini_repo}/.git/eshu-precommit-state"
[[ "${main_tool_cache}" != "${main_worktree_cache}" ]] ||
	fail "normal checkout unexpectedly mixed tool binaries and mutable state"
[[ "${main_worktree_cache}" != "${linked_worktree_cache}" ]] ||
	fail "linked worktrees unexpectedly shared mutable precommit state"

# Keep the production consumers tied to the isolated paths. Calculating the
# right directories is insufficient if a linter or report still writes to the
# shared tool cache.
# shellcheck disable=SC2016 # The needles must stay literal shell source.
require_precommit "worktree-local stripped config" 'local out="${worktree_cache_dir}/golangci-nocustom.yml"'
# shellcheck disable=SC2016
require_precommit "worktree-local golangci cache" 'GOLANGCI_LINT_CACHE="${golangci_cache_dir}" "$@"'
# shellcheck disable=SC2016
reject_precommit "ambient golangci cache override" 'GOLANGCI_LINT_CACHE:-'
# shellcheck disable=SC2016
require_precommit "disabled ambient golangci cache program" 'GOLANGCI_LINT_CACHEPROG='
# shellcheck disable=SC2016
parallel_run_count="$(rg --fixed-strings -c -- '--allow-parallel-runners --config "${cfg}"' "${precommit_script}")"
[[ "${parallel_run_count}" == "2" ]] ||
	fail "parallel-runner flag count = ${parallel_run_count}, want 2 lint entrypoints"
# shellcheck disable=SC2016
require_precommit "worktree-local changed-package SARIF" 'out="${worktree_cache_dir}/gosec.sarif"'
# shellcheck disable=SC2016
require_precommit "worktree-local whole-module SARIF" 'out="${worktree_cache_dir}/gosec-all.sarif"'
# shellcheck disable=SC2016
reject_precommit "mutable SARIF in shared tool cache" 'out="${tool_cache_dir}/gosec'

# #5791/#5804: the nancy case must delegate its sleuth/classification logic
# to scripts/dev/nancy-local.sh, which has its own executable regression
# suite (scripts/test-nancy-local.sh) exercising real exit codes for the
# empty-stdin, transport/auth-failure, clean-scan, genuine-finding, and
# go-list-failure cases with fake `go`/`nancy` binaries — a source-text-only
# assertion here previously let a broken pipeline (missing stdin redirect)
# pass silently (PR #5806 review), so this file only checks the delegation
# shape, not the classification logic itself.
# shellcheck disable=SC2016
require_precommit "nancy delegates to nancy-local.sh" 'bash "${repo_root}/scripts/dev/nancy-local.sh"'
# shellcheck disable=SC2016
reject_precommit "nancy no longer runs unpiped inline" '&& "${bin}" sleuth --no-color ) \'

require "serial precommit lane" "run_precommit_gates_serial()"
require "captured gate helper" "capture_whole_module_gate()"
# shellcheck disable=SC2016 # The needles must stay literal shell source.
require "fmt capture" 'capture_whole_module_gate "${tmpdir}" fmt "gofumpt (whole module)" step_fmt'
# shellcheck disable=SC2016
require "lint capture" 'capture_whole_module_gate "${tmpdir}" lint "golangci-lint (whole module)" step_lint'
# shellcheck disable=SC2016
require "build capture" 'capture_whole_module_gate "${tmpdir}" build "go build ./..." step_build'
# shellcheck disable=SC2016
require "vet capture" 'capture_whole_module_gate "${tmpdir}" vet "go vet ./..." step_vet'
# shellcheck disable=SC2016
require "stored duration readback" 'duration="$(cat "${tmpdir}/${n}.duration" 2>/dev/null || printf "0")"'

reject "shared parallel launcher state" "starts=()"
reject "wait-time duration accounting" 'SECONDS - starts[i]'

awk '
	/^run_precommit_gates_serial\(\)/ { in_func=1 }
	in_func && /capture_whole_module_gate .* fmt / { saw_fmt=NR }
	in_func && /capture_whole_module_gate .* lint / {
		if (saw_fmt == 0) {
			print "lint is captured before fmt in run_precommit_gates_serial" > "/dev/stderr"
			exit 1
		}
		saw_lint=NR
	}
	in_func && /^}/ { in_func=0 }
	END {
		if (saw_fmt == 0 || saw_lint == 0) {
			print "run_precommit_gates_serial must capture fmt then lint" > "/dev/stderr"
			exit 1
		}
	}
' "${script}" || fail "fmt/lint are not serialized in the precommit lane"

# ─── #5721 documentation fast-path wiring ────────────────────────────────────
# The lane decision itself lives in scripts/lib/pre-pr-lane.sh and has its own
# executable suites, which drive pre_pr_decide_lane directly. What those suites
# cannot reach is the WIRING in this file: which function supplies the changed
# paths, and which lane value gates the Go lanes. Each is a one-line edit, none
# of them shows up in a green run, and together they decide whether a push was
# verified at all. Pin them here, where the blocking ci-gate-registry gate --
# which triggers on scripts/dev/pre-pr.sh -- already runs.

# The lane decision is the ONLY consumer of untracked paths: the FULL lane's
# `go build ./...` compiles a file nobody ran `git add` on, and the FAST lane
# skips that build. Drop either collector and a forgotten `git add` takes a
# green docs-only stamp on a tree that does not compile.
# shellcheck disable=SC2016 # The needles must stay literal shell source.
require_block "untracked paths in the lane input" 'lane_input_paths() {
	{
		collect_changed_paths
		git_untracked_names
	} | sort -u
}'
# shellcheck disable=SC2016
require "lane decision delegated to the tested function" \
	'pre_pr_decide_lane "${repo_root}" "${base}" "${PRE_PR_FASTPATH_BASE_STATUS}" lane_input_paths'
# A literal verdict skips every gate on every run, forever, and no test of the
# decision function can see it because the decision function is never called.
reject "hardcoded lane verdict" 'PRE_PR_FASTPATH_LANE=fast'

# A self-check that failed has to fail the RUN, not just print a FAIL line: the
# run's exit status is what withholds the per-SHA stamp the push requires.
# shellcheck disable=SC2016
require_block "a red self-check fails the run" \
	'	results+=("FAIL  docs fast-path classifier self-check (${PRE_PR_LANE_SELFCHECK_SECONDS}s)")
	overall=1'

# Both Go-lane gates ask whether the lane is NOT "fast". Asking whether it IS
# "full" instead means any third value would skip the Go lanes while the banner
# said FULL and the run still stamped the SHA. Inverting either one swaps the
# lanes outright -- FULL skipping the gates, FAST running them.
# shellcheck disable=SC2016
require_block "module gates gated on a non-fast lane" \
	'if [[ "${PRE_PR_FASTPATH_LANE}" != "fast" ]]; then
	run_whole_module_gates_parallel
else'
# shellcheck disable=SC2016
require_block "race lane gated on a non-fast lane" \
	'if [[ "${PRE_PR_FASTPATH_LANE}" != "fast" ]]; then
	run_step "race lane (Go changes)" step_race'
# shellcheck disable=SC2016
lane_gate_count="$(rg --fixed-strings -c -- 'if [[ "${PRE_PR_FASTPATH_LANE}" != "fast" ]]; then' "${script}")"
[[ "${lane_gate_count}" == "2" ]] ||
	fail "lane gate count = ${lane_gate_count}, want 2 (whole-module gates + race lane)"
# shellcheck disable=SC2016
reject "lane gate asking whether the lane IS fast" '"${PRE_PR_FASTPATH_LANE}" == "fast"'
# shellcheck disable=SC2016
reject "lane gate asking whether the lane IS full" '"${PRE_PR_FASTPATH_LANE}" == "full"'

# eshu-hq/eshu#5935 review: `go test (changed packages)` must be reached on
# BOTH lanes, unconditionally -- its own scope (changed-Go-package dirs plus
# fixture_consumer_dirs) is what still runs TestRepositoryDocumentationStandards-
# AreEnforced for a root AGENTS.md/CLAUDE.md-only diff. A future edit that
# re-wraps the `run_step "go test (changed packages)" step_test` line back
# inside the whole-module-gates if/fi block -- restoring the shape just above,
# where it used to sit right after run_whole_module_gates_parallel -- would
# silently reopen that bug: the classifier still says FAST, but step_test (and
# the fixture_consumer_dirs mapping inside it) would only run on FULL again.
# Neither scripts/lib/test-pre-pr-lane.sh nor scripts/lib/test-pre-pr-docs-
# fastpath.sh can catch this: neither execs pre-pr.sh or step_test, both only
# drive the classifier and pre_pr_decide_lane in isolation. This is a purely
# static, line-position check -- it locates the first fast/full `if` (the
# whole-module-gates one), its matching top-level `fi`, and the one `run_step
# "go test (changed packages)" step_test` call, and asserts the call's line is
# outside the `[if, fi]` span. It cannot see whether step_test is *reachable*
# at runtime (that would need to execute pre-pr.sh, which this suite
# deliberately does not do), only whether it is textually gated by this
# specific if/fi -- which is exactly the shape the regression takes.
awk '
	/if \[\[ "\$\{PRE_PR_FASTPATH_LANE\}" != "fast" \]\]; then/ && !if_line { if_line = NR }
	if_line && !fi_line && /^fi$/ { fi_line = NR }
	/run_step "go test \(changed packages\)" step_test/ { test_count++; test_line = NR }
	END {
		if (!if_line) {
			print "could not find the whole-module-gates fast/full if" > "/dev/stderr"
			exit 1
		}
		if (!fi_line) {
			print "could not find that if'"'"'s matching fi" > "/dev/stderr"
			exit 1
		}
		if (test_count != 1) {
			print "found " test_count " `go test (changed packages)` run_step call(s), want exactly 1" > "/dev/stderr"
			exit 1
		}
		if (test_line > if_line && test_line < fi_line) {
			print "go test (changed packages) is gated INSIDE the whole-module-gates if/fi (line " test_line ", block " if_line "-" fi_line "); it must run unconditionally on both lanes" > "/dev/stderr"
			exit 1
		}
	}
' "${script}" || fail "go test (changed packages) is not reached on both lanes"

# ─── fixture_consumer_dirs branch coverage (#5721 follow-up) ────────────────
# fixture_consumer_dirs maps two non-Go fixture changes to the Go package
# whose tests actually load them: the B-12 golden snapshot to
# ./cmd/golden-corpus-gate, and a root CLAUDE.md/AGENTS.md edit to
# ./internal/runtime (TestRepositoryDocumentationStandardsAreEnforced).
# Registry and workflow self-tests now run through ci-gate-registry's
# local.test_command (#5944), so they deliberately have no duplicate focused
# package mapping here.
# This used to be two require_block text pins against pre-pr.sh here, plus a
# third mechanism #5939 added directly in this file: an awk extraction of
# fixture_consumer_dirs's source with a stubbed changed_all_files. Both were
# replaced by the single behavioural suite below when fixture_consumer_dirs
# moved into its own sourced file (eshu-hq/eshu#5938 review) -- a text match or
# an awk-extracted stub can only prove the mapping's source is present, not
# that it is reachable: an early `return` (or any wrapper) placed above the
# branches left the old pins green while fixture_consumer_dirs emitted nothing
# for every input, the same failure mode #5935 shipped to close, reopened one
# call deeper. The awk-extraction mechanism has the same blind spot and an
# extra footgun: it re-derives the function body from this file at test time,
# so it silently stops testing anything the moment the function is defined
# elsewhere (as it now is, in scripts/lib/pre-pr-fixture-consumers.sh).
#
# fixture_consumer_dirs now lives in its own sourced-only file,
# scripts/lib/pre-pr-fixture-consumers.sh, so
# scripts/lib/test-pre-pr-fixture-consumers.sh can call it directly against a
# throwaway repository and assert what it actually emits for both mappings,
# plus the absence of the retired #5939 registry/workflow workaround. That
# behavioural check is strictly stronger than the text pins and the awk
# extraction it replaces -- it fails on the exact early-return mutation that
# left both green -- so keeping either alongside it would only be a second
# thing to maintain for coverage the behavioural check already provides. The
# one thing worth still pinning here is the WIRING: that pre-pr.sh actually
# sources the file the behavioural check exercises, so a deleted `source` line
# fails loudly here rather than only at the next real `make pre-pr` run.
# shellcheck disable=SC2016 # The needle must stay literal shell source.
require "fixture-consumer mapping sourced from its own file" \
	'source "${repo_root}/scripts/lib/pre-pr-fixture-consumers.sh"'
# shellcheck disable=SC2016 # The needle must stay literal shell source.
require "focused test selector sourced from its own file" \
	'source "${repo_root}/scripts/lib/pre-pr-test-selection.sh"'
require "focused test step applies the behavioural selector" \
	'done < <({ changed_go_dirs; fixture_consumer_dirs; } | pre_pr_select_test_dirs)'

fixture_consumers_suite="${repo_root}/scripts/lib/test-pre-pr-fixture-consumers.sh"
[[ -f "${fixture_consumers_suite}" ]] || fail "missing ${fixture_consumers_suite}"
bash "${fixture_consumers_suite}" || fail "fixture_consumer_dirs behavioural suite failed -- see its output above"

test_selection_suite="${repo_root}/scripts/lib/test-pre-pr-test-selection.sh"
[[ -f "${test_selection_suite}" ]] || fail "missing ${test_selection_suite}"
bash "${test_selection_suite}" || fail "focused Go test selection behavioural suite failed -- see its output above"

assert_canonical_parser_commands_recursive() {
	local files=("$@") file matches rg_status failure_message
	for file in "${files[@]}"; do
		[[ -f "${file}" ]] || fail "missing canonical parser test guidance: ${file}"
	done
	if matches="$(
		rg --line-number --multiline --pcre2 \
			'(?s)go test(?:(?!\n[[:space:]]*\n).){0,500}?\./internal/parser(?=[[:space:]"`]|$)' \
			"${files[@]}"
	)"; then
		:
	else
		rg_status=$?
		[[ ${rg_status} -eq 1 ]] || fail "canonical parser command audit failed with rg exit ${rg_status}"
	fi
	if [[ -n "${matches}" ]]; then
		printf -v failure_message \
			'canonical parser test commands must select ./internal/parser/...:\n%s' "${matches}"
		fail "${failure_message}"
	fi
}

if (assert_canonical_parser_commands_recursive "${temp_root}/missing-parser-guidance.md") 2>/dev/null; then
	fail "canonical parser command audit accepted a missing input"
fi
assert_canonical_parser_commands_recursive "${canonical_parser_test_docs[@]}"
[[ -f "${parser_agent_guidance}" ]] ||
	fail "missing canonical parser test guidance: ${parser_agent_guidance}"
rg --multiline --fixed-strings --quiet -- \
	'  5. Add fixtures in the parser fixture corpus and run
     `go test ./internal/parser/... -count=1` so language-owned package tests
     and external parent-engine regressions are included.' \
	"${parser_agent_guidance}" ||
	fail "parser package guidance no longer requires recursive parent-engine proof"

fake_go_dir="${temp_root}/run-tests-bin"
mkdir -p "${fake_go_dir}"
cat > "${fake_go_dir}/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
: "${RUN_TESTS_GO_ARGS_LOG:?}"
printf '%s\n' "$@" >> "${RUN_TESTS_GO_ARGS_LOG}"
EOF
chmod +x "${fake_go_dir}/go"

assert_fast_runner_parser_tree() {
	local mode="$1" log_file parser_tree_count
	log_file="${temp_root}/run-tests-${mode}.args"
	RUN_TESTS_GO_ARGS_LOG="${log_file}" PATH="${fake_go_dir}:${PATH}" \
		bash "${fast_runner}" "${mode}" >/dev/null ||
		fail "tests/run_tests.sh ${mode} failed with the fake Go command"
	parser_tree_count="$(rg --fixed-strings --line-regexp -c -- './internal/parser/...' "${log_file}" || printf '0\n')"
	[[ "${parser_tree_count}" == "1" ]] ||
		fail "tests/run_tests.sh ${mode} selected ./internal/parser/... ${parser_tree_count} time(s), want 1"
	if rg --fixed-strings --line-regexp --quiet -- './internal/parser' "${log_file}"; then
		fail "tests/run_tests.sh ${mode} still selects only the parent parser package"
	fi
}

assert_fast_runner_parser_tree unit
assert_fast_runner_parser_tree fast

printf 'PASS: pre-pr scheduling and fast local parser selection are pinned\n'
