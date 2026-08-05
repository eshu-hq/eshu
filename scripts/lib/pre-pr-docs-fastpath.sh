#!/usr/bin/env bash
# pre-pr-docs-fastpath.sh — documentation/specs-only fast-path classifier for
# scripts/dev/pre-pr.sh (#5721).
#
# Sourced, never executed. Provides pre_pr_resolve_lane_base,
# pre_pr_path_is_fastpath_safe, and pre_pr_classify_docs_fastpath. It lives in
# its own file so the real classification logic is sourceable by
# scripts/lib/test-pre-pr-docs-fastpath.sh -- a test that re-implements or
# text-extracts the classifier proves nothing about the classifier.
#
# make pre-pr always ran go build ./..., whole-module golangci-lint, the
# changed-package go test lane, and the race lane, even for a diff that
# touches no Go/build-affecting file at all. On a docs/specs-only PR those
# lanes cost real wall time and, under concurrent-worktree CPU load, can fail
# spuriously on unrelated packages hitting per-test timeouts -- a red gate
# with nothing to do with the diff (#5721). This library decides, from a list
# of changed repo-relative paths, whether the diff is provably
# documentation/specs-only (the FAST lane, which skips the whole-module Go
# build/lint/vet/race lanes; the changed-package go test lane stays narrowly
# scoped rather than skipping, so a fixture-consumer mapping such as root
# AGENTS.md/CLAUDE.md -> ./internal/runtime still runs -- see
# fixture_consumer_dirs and pre_pr_fast_lane_skip_steps) or must take the FULL
# lane.
#
# Two independent fail-closed rules, because the allowlist alone is not
# enough:
#
#   1. THE ALLOWLIST is a positive list, not a denylist. A path this file has
#      never seen -- a new extension, a renamed directory, a typo -- takes the
#      FULL lane. A default-ALLOW classifier here would skip Go verification
#      on a change that touches compiled code.
#   2. THE INPUT is treated as guilty until proven innocent. Every allowlist
#      guarantee is conditional on the caller handing over the real changed-path
#      set, and an empty list is indistinguishable from "git blew up and printed
#      nothing" unless the caller says which it was. So the classifier refuses
#      to consider FAST at all until the caller states that the list came from
#      git commands that exited 0, against a base that resolved. A wrong FULL
#      verdict costs wall time; a wrong FAST verdict stamps the SHA and lets a
#      broken build through, and nothing downstream re-checks it.
#
# See scripts/lib/test-pre-pr-docs-fastpath.sh for the seeded-defect proof.

# pre_pr_resolve_lane_base <repo-root>
# Resolves the base ref the lane decision (and every other pre-pr lane) diffs
# against, and says whether that base can carry a lane-SKIP decision.
#
# Sets:
#   PRE_PR_FASTPATH_BASE         ref to diff against: "origin/main" when it
#                                resolves, else "HEAD~1"
#   PRE_PR_FASTPATH_BASE_STATUS  "ok" only when the base is a resolved
#                                origin/main that shares a merge base with
#                                HEAD; otherwise a reason string for the
#                                operator
#
# HEAD~1 is a usable base for "which packages should I test" and a useless one
# for "may I skip the Go lanes entirely": on a multi-commit branch it shows
# only the last commit, so a branch whose final commit is docs-only classifies
# FAST while its real diff touches Go. The no-merge-base case is live, not
# hypothetical -- worktrees here are often shallow clones, and `git diff
# origin/main...HEAD` exits 128 with "fatal: no merge base" when the fetched
# origin/main does not reach the branch.
#
# shellcheck disable=SC2034  # PRE_PR_FASTPATH_BASE/BASE_STATUS are globals for
# the sourcing caller (scripts/dev/pre-pr.sh) and this file's own test suite to
# read; this file never reads them back itself.
pre_pr_resolve_lane_base() {
	local root="$1"
	PRE_PR_FASTPATH_BASE="HEAD~1"
	if ! git -C "${root}" rev-parse --verify "origin/main" >/dev/null 2>&1; then
		PRE_PR_FASTPATH_BASE_STATUS="origin/main does not resolve, so the base fell back to HEAD~1 -- that shows only the last commit of a multi-commit branch"
		return 0
	fi
	PRE_PR_FASTPATH_BASE="origin/main"
	if ! git -C "${root}" merge-base "origin/main" HEAD >/dev/null 2>&1; then
		PRE_PR_FASTPATH_BASE_STATUS="origin/main and HEAD share no merge base (a shallow clone that was never deepened), so the branch diff cannot be computed"
		return 0
	fi
	PRE_PR_FASTPATH_BASE_STATUS="ok"
	return 0
}

# pre_pr_path_is_fastpath_safe <path>
# Returns 0 (true) when <path> is a repo-relative path this classifier
# explicitly recognizes as safe to skip the Go build/lint/test/race lanes
# for; returns 1 (false) for everything else, including paths it does not
# recognize at all, and including an allowlisted path that no longer exists.
#
# The allowlist intentionally covers only the doc/spec examples #5721 names
# as fast-path-safe, not every path that merely "looks like" documentation:
#
#   - docs/**                                                    the docs tree
#   - a root-level *.md file (README.md, CLAUDE.md, AGENTS.md, ...) --
#     root-anchored: a nested path/*.md (e.g. a package README under go/) is
#     NOT covered, matching the root-anchor precedent already established by
#     scripts/verify-docs-build-changed.sh's is_docs_trigger.
#   - specs/capability-matrix.v1.yaml and specs/capability-matrix/**    --
#     capability-matrix rows are data the capability-inventory tool verifies
#     as part of the fast path itself (`-mode verify` / `-mode docs`); they
#     do not feed a Go build.
#   - go/internal/capabilitycatalog/data/*.generated.json, one level only --
#     the embedded catalog/surface-inventory data capability-inventory
#     verifies. Its CONTENT is go:embed'd as raw bytes and never parsed at
#     compile time, so editing it cannot break `go build`. Deleting or
#     renaming it can and does. Two directives embed two files by their
#     literal names, with no wildcard in either:
#     go/internal/capabilitycatalog/load.go embeds data/catalog.generated.json
#     and surfaces_load.go embeds data/surface-inventory.generated.json.
#     Removing either fails the build with "pattern ...: no matching files
#     found" -- go:embed calls a literal name a pattern in its error text.
#     `git diff --name-only` prints a deleted path exactly like a modified one,
#     so the existence check below is the only thing that tells them apart. The
#     allowlist is wider than the two embedded names on purpose: a third
#     *.generated.json in that directory is data nobody embeds yet, and
#     treating it the same way costs nothing.
#
# Everything else defaults to full, including every other specs/*.yaml (most
# feed codegen or a fact-kind/contract build), every go/**/*.go file
# (including a generated file such as openapi*.go), go.mod, go.sum, Makefile,
# Dockerfile*, anything under scripts/, and .github/workflows/**.
#
# Path existence is resolved against ${PRE_PR_FASTPATH_ROOT:-.}; pre-pr.sh
# sets that to the repo root, and the test suite points it at a fixture tree.
pre_pr_path_is_fastpath_safe() {
	local path="$1"
	# An empty argument is not a path. Fail closed rather than let a stray
	# blank line contribute a free "fast" vote; pre-pr.sh strips blank lines
	# before it calls, so this never fires on a normal run.
	[[ -n "${path}" ]] || return 1

	# Build-affecting file types are never fast-path safe, whichever directory
	# they sit in. docs/** is an allowlisted PREFIX, so without this a
	# docs/examples/main.go or a docs/tools/gen/go.mod would take the fast path
	# and skip the build it can break. There are none under docs/ today
	# (`rg --files docs -g '*.go'` and `-g 'go.mod'` both return 0 files); this
	# keeps it that way without depending on nobody ever adding one.
	case "${path}" in
		*.go | go.mod | go.sum | */go.mod | */go.sum) return 1 ;;
	esac

	case "${path}" in
		docs/*) : ;;
		specs/capability-matrix.v1.yaml) : ;;
		specs/capability-matrix/*) : ;;
		# A bash `case` glob crosses `/`, so `data/*.generated.json` alone would
		# also match data/nested/dir/x.generated.json. Only the single level the
		# two go:embed directives actually read is allowlisted, so reject the
		# deeper shape first -- code and docs agree on "one level".
		go/internal/capabilitycatalog/data/*/*) return 1 ;;
		go/internal/capabilitycatalog/data/*.generated.json) : ;;
		# Anything else with a path separator is nested and unrecognized.
		*/*) return 1 ;;
		# Root-anchored markdown: no separator anywhere in the path, so only a
		# file sitting directly at the repo root reaches this.
		*.md) : ;;
		*) return 1 ;;
	esac

	# The path matched the allowlist. It is only fast-path safe if it still
	# exists: a deletion or a rename reaches here as an ordinary changed path,
	# and a deleted go:embed'd file breaks `go build` on the next compile.
	local root="${PRE_PR_FASTPATH_ROOT:-.}"
	[[ -e "${root}/${path}" ]] || return 1
	return 0
}

# pre_pr_classify_docs_fastpath <paths-status> <path...>
#
# <paths-status> is the caller's statement about where <path...> came from:
#   "ok"        every git command that produced the list exited 0, against a
#               base that resolved. Only this value makes FAST reachable.
#   any other   the list is not trustworthy -- a diff that failed, or a base
#   value       that is not a resolved origin/main. The value is kept verbatim
#               as the operator-facing reason and the lane is FULL.
#
# Sets three globals:
#   PRE_PR_FASTPATH_LANE                "fast" or "full"
#   PRE_PR_FASTPATH_TRIGGERS            the specific paths that forced "full"
#                                       (empty when the lane is "fast", and
#                                       also empty when the status forced full)
#   PRE_PR_FASTPATH_FORCED_FULL_REASON  non-empty only when the status, not a
#                                       path, forced "full"
#
# An empty path list with status "ok" classifies as "fast" -- git looked and
# found nothing to build. An empty path list with any other status classifies
# as "full", because then the emptiness is an absence of information rather
# than an observation. Keeping those two apart is the whole point of the
# status argument.
#
# shellcheck disable=SC2034  # PRE_PR_FASTPATH_* are globals for the sourcing
# caller (scripts/dev/pre-pr.sh) and this file's own test suite to read; this
# file never reads them back itself.
pre_pr_classify_docs_fastpath() {
	local status="${1-}"
	[[ $# -gt 0 ]] && shift

	# Start at "full" and earn "fast", so no early return can leave a stale
	# permissive verdict behind.
	PRE_PR_FASTPATH_LANE="full"
	PRE_PR_FASTPATH_TRIGGERS=()
	PRE_PR_FASTPATH_FORCED_FULL_REASON=""

	if [[ "${status}" != "ok" ]]; then
		PRE_PR_FASTPATH_FORCED_FULL_REASON="${status:-the caller did not say where the changed-path list came from}"
		return 0
	fi

	local p
	for p in "$@"; do
		pre_pr_path_is_fastpath_safe "${p}" || PRE_PR_FASTPATH_TRIGGERS+=("${p}")
	done
	if [[ ${#PRE_PR_FASTPATH_TRIGGERS[@]} -eq 0 ]]; then
		PRE_PR_FASTPATH_LANE="fast"
	fi
	return 0
}
