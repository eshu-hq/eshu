#!/usr/bin/env bash
# pre-pr-fixture-consumers.sh — non-Go fixture-to-Go-package mapping for
# scripts/dev/pre-pr.sh (#5721).
#
# Sourced, never executed. Provides collect_changed_paths, changed_all_files,
# and fixture_consumer_dirs. It lives in its own file for the same reason
# pre-pr-lane.sh and pre-pr-docs-fastpath.sh do: a text match against
# scripts/dev/pre-pr.sh can only prove the mapping's source is present, not
# that it is reachable. An early `return` (or any wrapper) placed above both
# branches in fixture_consumer_dirs left a text-match pin green while the
# function emitted nothing for every input -- the exact regression #5935
# shipped to fix, reopened one call deeper (eshu-hq/eshu#5938 review).
#
# Sourcing scripts/dev/pre-pr.sh directly is not an option: past its function
# definitions it runs the whole gate and calls `exit`, so nothing that sources
# it survives to assert anything. Splitting the mapping into its own
# sourced-only file is what lets
# scripts/lib/test-pre-pr-fixture-consumers.sh call fixture_consumer_dirs
# directly, against a throwaway repository, and assert what it actually
# emits -- a check that survives a refactor of the branches a text match
# cannot see through.
#
# Depends on ${repo_root} and ${base} (set by the caller before invoking
# fixture_consumer_dirs) and on git_changed_names, which is defined in
# pre-pr-lane.sh and must be sourced first.

# collect_changed_paths: committed (vs base) + unstaged + staged paths. One
# implementation, because the previous two copies of the same git plumbing meant
# a fix to one silently left the other swallowing exit codes.
collect_changed_paths() {
	# shellcheck disable=SC2154  # base is set by the sourcing caller (pre-pr.sh
	# or a test), the same convention pre-pr-lane.sh's own functions rely on.
	git_changed_names "${base}...HEAD"
	git_changed_names HEAD
	git_changed_names --cached
}

# changed_all_files: every changed path, not just Go files. Used to map non-Go
# fixtures to their consumer packages and to decide the lane.
changed_all_files() {
	collect_changed_paths | sort -u
}

# fixture_consumer_dirs: ./-relative Go package dirs whose tests load a non-Go
# fixture that changed. pre-pr's focused `go test` is scoped to changed *Go*
# packages, so a fixture-only edit (e.g. the B-12 golden snapshot, which is a
# JSON file, not a Go package) would never run its consumer's tests locally — the
# exact gap that let a golden-snapshot change break go/cmd/golden-corpus-gate on
# CI only. Each entry maps a changed-path pattern to the package(s) that consume
# it; extend this table when a new fixture-backed test is added.
fixture_consumer_dirs() {
	local all
	all="$(changed_all_files)"
	# The B-12 golden snapshot is loaded by the golden-corpus-gate unit tests.
	if printf '%s\n' "${all}" | rg -q '^testdata/golden/'; then
		printf './cmd/golden-corpus-gate\n'
	fi
	# CLAUDE.md and AGENTS.md are read by TestRepositoryDocumentationStandardsAreEnforced,
	# which asserts both that they stay byte-identical and that specific rules are
	# still present. A canon-only edit changes no Go package, so without this the
	# focused test step selects nothing and the first failure is in CI -- which is
	# how a reword of two rules reached CI red on #5903.
	if printf '%s\n' "${all}" | rg -q '^(CLAUDE|AGENTS)\.md$'; then
		printf './internal/runtime\n'
	fi
}
