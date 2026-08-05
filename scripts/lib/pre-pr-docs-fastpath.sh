#!/usr/bin/env bash
# pre-pr-docs-fastpath.sh — documentation/specs-only fast-path classifier for
# scripts/dev/pre-pr.sh (#5721).
#
# Sourced, never executed. Provides pre_pr_path_is_fastpath_safe and
# pre_pr_classify_docs_fastpath. It lives in its own file so the real
# classification logic is sourceable by
# scripts/lib/test-pre-pr-docs-fastpath.sh -- a test that re-implements or
# text-extracts the classifier proves nothing about the classifier.
#
# make pre-pr always ran go build ./..., whole-module golangci-lint, the
# changed-package go test lane, and the race lane, even for a diff that
# touches no Go/build-affecting file at all. On a docs/specs-only PR those
# lanes cost 15-25 minutes and, under concurrent-worktree CPU load, can fail
# spuriously on unrelated packages hitting per-test timeouts -- a red gate
# with nothing to do with the diff (#5721). This library decides, from a list
# of changed repo-relative paths, whether the diff is provably
# documentation/specs-only (the FAST lane, which skips the four Go
# build/lint/test/race lanes) or must take the FULL lane.
#
# Fail-closed by design: pre_pr_path_is_fastpath_safe is an ALLOWLIST, not a
# denylist. Every path takes the full lane UNLESS it matches one of a short
# list of explicitly recognized documentation/specs patterns. A path this
# function has never seen -- a new file extension, a renamed directory, a
# typo -- defaults to FULL. A default-ALLOW classifier here would silently
# skip Go verification on a change that touches compiled code, which is
# exactly the defect class this fast path must never introduce. See
# scripts/lib/test-pre-pr-docs-fastpath.sh for the seeded-defect proof
# (a leaked .go file, a leaked go.mod, and an unrecognized path each flipping
# the default to fast must all fail the suite loudly).

# pre_pr_path_is_fastpath_safe <path>
# Returns 0 (true) when <path> is a repo-relative path this classifier
# explicitly recognizes as safe to skip the Go build/lint/test/race lanes
# for; returns 1 (false) for everything else, including paths it does not
# recognize at all.
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
#   - go/internal/capabilitycatalog/data/*.generated.json                --
#     the embedded catalog/surface-inventory data capability-inventory
#     verifies. It is go:embed'd as raw bytes, never parsed at compile time,
#     so its content cannot break `go build`, `go vet`, or golangci-lint.
#
# Everything else defaults to full, including every other specs/*.yaml (most
# feed codegen or a fact-kind/contract build), every go/**/*.go file
# (including a generated file such as openapi*.go), go.mod, go.sum, Makefile,
# Dockerfile*, anything under scripts/, and .github/workflows/**.
pre_pr_path_is_fastpath_safe() {
	local path="$1"
	case "${path}" in
		docs/*) return 0 ;;
		specs/capability-matrix.v1.yaml) return 0 ;;
		specs/capability-matrix/*) return 0 ;;
		go/internal/capabilitycatalog/data/*.generated.json) return 0 ;;
	esac
	# Root-anchored markdown only: no path separator anywhere in the path, so
	# a nested README (e.g. go/internal/foo/README.md) does NOT match here --
	# only a file that sits directly at the repo root does.
	case "${path}" in
		*/*) return 1 ;;
		*.md) return 0 ;;
	esac
	return 1
}

# pre_pr_classify_docs_fastpath <path...>
# Classifies a full changed-path set against pre_pr_path_is_fastpath_safe.
# Sets two globals:
#   PRE_PR_FASTPATH_LANE      "fast" or "full"
#   PRE_PR_FASTPATH_TRIGGERS  array of the specific paths that forced "full"
#                             (empty when the lane is "fast")
# An empty path list classifies as "fast": there is nothing to build.
# shellcheck disable=SC2034  # PRE_PR_FASTPATH_LANE/TRIGGERS are globals for
# the sourcing caller (scripts/dev/pre-pr.sh) and this file's own test
# suite to read; this file never reads them back itself.
pre_pr_classify_docs_fastpath() {
	PRE_PR_FASTPATH_LANE="fast"
	PRE_PR_FASTPATH_TRIGGERS=()
	local p
	for p in "$@"; do
		[[ -n "${p}" ]] || continue
		if ! pre_pr_path_is_fastpath_safe "${p}"; then
			PRE_PR_FASTPATH_TRIGGERS+=("${p}")
		fi
	done
	if [[ ${#PRE_PR_FASTPATH_TRIGGERS[@]} -gt 0 ]]; then
		PRE_PR_FASTPATH_LANE="full"
	fi
}
