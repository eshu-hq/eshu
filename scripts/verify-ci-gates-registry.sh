#!/usr/bin/env bash
# CI gate registry integrity checker (#4213, drift extension #4220). Verifies
# that every entry in specs/ci-gates.v1.yaml references a script and workflow
# file that exists on disk and that every gate trigger still names something
# real — a literal trigger must name a tracked FILE (#6055, #6159), and a glob
# trigger must select at least one, because a trigger matching nothing stops
# selecting its gate without ever failing. The universe is files only: the
# paths a change hands the selector are files, from a git diff or a GitHub
# pull-files list, so a trigger that stops at a directory can never select.
# Directories are derived on the FAILURE path alone, to name a working "dir/**"
# spelling in the error. Re-adding them to the universe would re-introduce the
# defect this checks for. AND (unconditionally as of #6055 — see below)
# checks pre-commit-hook and workflow registry completeness against
# .pre-commit-config.yaml and .github/workflows/, including
# checkPathFilterCoverage (go/internal/cigates/pathfilter.go): every literal
# registry trigger of a gate whose CI workflow uses a dorny/paths-filter must
# actually be selected by that filter. Credential-free, Docker-free,
# network-free.
#
# #6055: the drift/path-filter-coverage check used to be opt-in behind an
# explicit --drift flag, so a bare local run only proved script/workflow
# existence and silently skipped the check that catches a registry trigger a
# CI workflow's dorny filter can no longer select -- exactly the "gate goes
# dark after a move" risk #6055 exists to close. Every wired invocation
# (the pre-commit gate-registry-drift hook, and this gate's own
# specs/ci-gates.v1.yaml local.command) already passed --drift; the only gap
# was a developer or agent running this script bare and getting a false
# sense of full coverage. --drift is still accepted, for callers that still
# pass it, but is now a no-op: this script always runs the full check.
#
# Usage:
#   scripts/verify-ci-gates-registry.sh          # full check (integrity + drift)
#   scripts/verify-ci-gates-registry.sh --drift  # same; flag kept for existing callers
#
# Exit codes:
#   0 — registry is consistent with the repository.
#   1 — one or more integrity/drift errors found; details printed to stderr.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
registry="${repo_root}/specs/ci-gates.v1.yaml"

case "${1:-}" in
	""|--drift) ;;
	*)
		printf 'verify-ci-gates-registry: unknown argument %s\n' "${1}" >&2
		exit 2
		;;
esac

(cd "${repo_root}/go" && go run ./cmd/ci-gates validate \
	--registry "${registry}" --repo-root "${repo_root}" --drift)
