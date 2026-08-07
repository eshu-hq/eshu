#!/usr/bin/env bash
# test-pre-pr-fixture-consumers.sh — behavioural tests for fixture_consumer_dirs
# (scripts/lib/pre-pr-fixture-consumers.sh, #5721).
#
# scripts/test-pre-pr-whole-module-gates.sh used to pin fixture_consumer_dirs's
# two branches with a require_block text match against scripts/dev/pre-pr.sh.
# A text match can only prove the mapping's SOURCE is present; it cannot prove
# the mapping is REACHABLE. An early `return` (or any wrapper) placed above
# both branches left that pin green while fixture_consumer_dirs emitted
# nothing for every input -- the exact regression #5935 shipped to fix,
# reopened one call deeper (eshu-hq/eshu#5938 review).
#
# This suite calls the real fixture_consumer_dirs against a throwaway git
# repository and asserts what it actually prints, so a neutralized branch
# fails here regardless of how it was neutralized. It replaces the two
# require_block pins that used to cover this in
# scripts/test-pre-pr-whole-module-gates.sh, which now invokes this suite
# directly instead of text-matching pre-pr.sh.
#
# Run with:
#   bash scripts/lib/test-pre-pr-fixture-consumers.sh
set -uo pipefail

suite_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# Same isolation as scripts/lib/test-pre-pr-lane-fixtures.sh: these fixtures
# `git init`/`git commit` throwaway repos, and git reads the developer's
# ~/.gitconfig in every one of them. A global commit.gpgsign or a rejecting
# core.hooksPath would red this suite for a reason that has nothing to do with
# fixture_consumer_dirs.
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1

for _lib in pre-pr-lane pre-pr-fixture-consumers; do
	if [[ ! -f "${suite_root}/scripts/lib/${_lib}.sh" ]]; then
		echo "test-pre-pr-fixture-consumers: missing library at ${suite_root}/scripts/lib/${_lib}.sh" >&2
		exit 1
	fi
	# shellcheck source=/dev/null
	source "${suite_root}/scripts/lib/${_lib}.sh"
done

failures=0
cases_run=0
scratch="$(mktemp -d)"
trap 'rm -rf "${scratch}"' EXIT

fail() {
	echo "FAIL $*" >&2
	failures=$((failures + 1))
}

# assert_contains <case-name> <needle> <haystack>
assert_contains() {
	local name="$1" needle="$2" haystack="$3"
	cases_run=$((cases_run + 1))
	if [[ "${haystack}" != *"${needle}"* ]]; then
		fail "${name}: want output containing '${needle}', got '${haystack}'"
		return
	fi
	echo "PASS ${name}"
}

# assert_not_contains <case-name> <needle> <haystack>
assert_not_contains() {
	local name="$1" needle="$2" haystack="$3"
	cases_run=$((cases_run + 1))
	if [[ "${haystack}" == *"${needle}"* ]]; then
		fail "${name}: want output NOT containing '${needle}', got '${haystack}'"
		return
	fi
	echo "PASS ${name}"
}

# assert_empty <case-name> <got>
assert_empty() {
	local name="$1" got="$2"
	cases_run=$((cases_run + 1))
	if [[ -n "${got}" ]]; then
		fail "${name}: want empty output, got '${got}'"
		return
	fi
	echo "PASS ${name}"
}

# changed_repo <name> <path...> — a throwaway repo with one commit on `main`
# tagged as origin/main, then a second commit on top that adds every <path>
# given (empty file, parent dirs created as needed). Mirrors
# scripts/lib/test-pre-pr-lane-fixtures.sh's new_repo/feature-branch shape, so
# collect_changed_paths sees these paths the same way it sees a real PR
# branch: through `git diff --name-only origin/main...HEAD`.
changed_repo() {
	local dir="${scratch}/$1" p
	shift
	mkdir -p "${dir}"
	git init -q -b main "${dir}"
	git -C "${dir}" config user.email test@example.invalid
	git -C "${dir}" config user.name test
	: >"${dir}/README.md"
	git -C "${dir}" add README.md
	git -C "${dir}" commit -qm base
	git -C "${dir}" update-ref refs/remotes/origin/main HEAD
	for p in "$@"; do
		mkdir -p "${dir}/$(dirname "${p}")"
		: >"${dir}/${p}"
		git -C "${dir}" add "${p}"
	done
	git -C "${dir}" commit -qm changed
	printf '%s\n' "${dir}"
}

# fixture_consumer_dirs_for <repo-dir> — sets the two globals
# fixture_consumer_dirs reads (repo_root, base) the same way pre-pr.sh does,
# then calls the real function and prints what it emits.
fixture_consumer_dirs_for() {
	# shellcheck disable=SC2034  # repo_root/base are read by fixture_consumer_dirs
	# and collect_changed_paths via bash's dynamic scoping, not by this function.
	local repo_root="$1" base="origin/main"
	fixture_consumer_dirs
}

# ─── cases ─────────────────────────────────────────────────────────────────

_repo="$(changed_repo claude_md CLAUDE.md)"
_got="$(fixture_consumer_dirs_for "${_repo}")"
assert_contains "claude_md_selects_internal_runtime" "./internal/runtime" "${_got}"
assert_not_contains "claude_md_does_not_select_golden_corpus_gate" "./cmd/golden-corpus-gate" "${_got}"

_repo="$(changed_repo agents_md AGENTS.md)"
_got="$(fixture_consumer_dirs_for "${_repo}")"
assert_contains "agents_md_selects_internal_runtime" "./internal/runtime" "${_got}"

_repo="$(changed_repo golden_snapshot testdata/golden/e2e-20repo-snapshot.json)"
_got="$(fixture_consumer_dirs_for "${_repo}")"
assert_contains "golden_snapshot_selects_golden_corpus_gate" "./cmd/golden-corpus-gate" "${_got}"
assert_not_contains "golden_snapshot_does_not_select_internal_runtime" "./internal/runtime" "${_got}"

# #5944 makes ci-gate-registry's local.test_command execute its self-tests for
# registry and workflow changes. The focused Go-package fixture mapping must
# not also select internal/cigates, or make pre-pr runs the same package suite
# twice.
_repo="$(changed_repo registry_only specs/ci-gates.v1.yaml)"
_got="$(fixture_consumer_dirs_for "${_repo}")"
assert_empty "registry_only_uses_ci_gate_test_command" "${_got}"

_repo="$(changed_repo workflow_only .github/workflows/verify-ci-gate-registry.yml)"
_got="$(fixture_consumer_dirs_for "${_repo}")"
assert_empty "workflow_only_uses_ci_gate_test_command" "${_got}"

# Mirrors nested_claude_md_is_not_the_root_canon_file below: the golden branch
# had no case pinning its leading `^`, so dropping it (making the pattern match
# testdata/golden/ anywhere in the path, not just at the repo root) passed all
# 9 cases -- see the commit that added this case for the mutation proof.
_repo="$(changed_repo nested_golden_dir vendor/testdata/golden/x.json)"
_got="$(fixture_consumer_dirs_for "${_repo}")"
assert_empty "nested_golden_dir_is_not_the_root_testdata_golden" "${_got}"

_repo="$(changed_repo both_fixtures CLAUDE.md testdata/golden/e2e-20repo-snapshot.json)"
_got="$(fixture_consumer_dirs_for "${_repo}")"
assert_contains "both_changed_selects_internal_runtime" "./internal/runtime" "${_got}"
assert_contains "both_changed_selects_golden_corpus_gate" "./cmd/golden-corpus-gate" "${_got}"

_repo="$(changed_repo unrelated_go_change go/internal/query/handler.go)"
_got="$(fixture_consumer_dirs_for "${_repo}")"
assert_empty "unrelated_go_change_selects_nothing" "${_got}"

_repo="$(changed_repo nested_claude_md nested/CLAUDE.md)"
_got="$(fixture_consumer_dirs_for "${_repo}")"
assert_empty "nested_claude_md_is_not_the_root_canon_file" "${_got}"

# The trailing `$` and the escaped `\.` are the CLAUDE/AGENTS pattern's other
# two boundaries. Checked the same way as the leading `^` above: each of these
# two cases passed all 9 existing cases when its own boundary was loosened
# (dropping `$`, or unescaping `.` to match any character) before this case
# was added.
_repo="$(changed_repo claude_md_suffix CLAUDE.md.bak)"
_got="$(fixture_consumer_dirs_for "${_repo}")"
assert_empty "claude_md_with_trailing_suffix_is_not_the_root_canon_file" "${_got}"

_repo="$(changed_repo claude_md_wrong_separator CLAUDEXmd)"
_got="$(fixture_consumer_dirs_for "${_repo}")"
assert_empty "claude_md_requires_a_literal_dot_not_any_character" "${_got}"

echo ""
echo "test-pre-pr-fixture-consumers: ${cases_run} case(s) run, ${failures} failure(s)"
if [[ "${failures}" -ne 0 ]]; then
	exit 1
fi
echo "test-pre-pr-fixture-consumers: all tests passed"
