#!/usr/bin/env bash
# Core `nancy sleuth` logic (#5791, #5804), factored out of the `nancy` case
# in scripts/dev/precommit-go.sh so it is unit-testable with fake `go`/`nancy`
# binaries on PATH (see scripts/test-nancy-local.sh) without needing network
# access, a real nancy install, or OSS Index credentials.
#
# Usage: scripts/dev/nancy-local.sh <go_dir> <cache_dir> <nancy_bin>
#   go_dir    - directory to run `go list -json -deps ./...` from
#   cache_dir - worktree-local scratch dir for intermediate files
#   nancy_bin - path to the nancy binary to invoke
#
# `nancy sleuth` reads the dependency graph from stdin -- its documented
# usage is `go list -json -deps ./... | nancy sleuth`. This script pipes the
# real dependency graph in (never a bare, unpiped `nancy sleuth`, which fails
# "StdIn is invalid or empty" on a TTY or the empty-but-valid pipe bash gives
# a non-interactive script's stdin on macOS -- the original #5791 bug).
#
# `go list` runs as its OWN step, checked independently of nancy's result: a
# `go list` failure (broken module, unresolved import, bad go.sum) is a build
# problem, not a vulnerability finding, and must never be reported as one.
#
# Exit status: nancy is declared ADVISORY (specs/ci-gates.v1.yaml,
# security-scan.yml -- see #5791/#5804), the same shape as
# scripts/dev/trivy-fs-local.sh's precedent for a security scanner that
# cannot always run locally. This script exits 0 ONLY for:
#   (a) a genuinely clean scan ("no vulnerable dependencies found"), or
#   (b) an OSS Index 401 Unauthorized rejection (D6, PR #10561): Sonatype's
#       OSS Index currently rejects every anonymous request with 401
#       (verified directly against the API, no nancy involved), and neither
#       this repo's CI nor local dev has OSS_INDEX_USERNAME/OSS_INDEX_TOKEN
#       configured (#5804), so an unconditional non-zero exit here would
#       make this advisory gate permanently red for every contributor, for
#       a reason outside their control, with no local action able to fix
#       it. The 401 exits as skip-with-ticket: a `::warning::` annotation
#       plus the #5804 reference, neutral, no merge block.
# This script exits NON-ZERO for everything else:
#   (a) a `go list` failure (a real build problem in THIS repo),
#   (b) a genuine vulnerability finding reported by nancy, or
#   (c) any other hard nancy error (non-401 transport/auth failure, bad
#       input): without a real scan there is no clean bill to report, so
#       the wrapper fails closed instead of deferring. CI's identical,
#       unpiped `nancy sleuth --no-color` has never scanned anything either
#       (it silently audits 0 dependencies and reports success) -- this
#       script is strictly more honest than that baseline, not less.
#
# Case (a) vs (b) vs a hard nancy error are told apart by nancy's own output:
# verified against nancy v1.2.0 (`internal/cmd/sleuth.go:64-73`), a genuine
# vulnerability finding returns a `customerrors.ErrorExit` and calls
# `os.Exit(errExit.ExitCode)` directly, bypassing cobra's error-printing path
# entirely -- only a transport/auth/input failure (panic -> recovered ->
# returned error -> cobra's default RunE error handling) produces output
# whose first line starts with "Error:". A 401 is matched before that
# check and skips with a ticket reference (D6, PR #10561). Re-verify this discriminator against
# the installed nancy version on any nancy bump; a future release changing
# its error output shape (e.g. structured JSON, or a cobra upgrade that
# prepends a banner) could invalidate it.
set -euo pipefail

go_dir="$1"
cache_dir="$2"
bin="$3"

note() { printf 'nancy-local: %s\n' "$*" >&2; }
die() { printf 'nancy-local: %s\n' "$*" >&2; exit 1; }

deps_json="${cache_dir}/nancy-deps.json"
deps_err="${cache_dir}/nancy-deps.err"
out="${cache_dir}/nancy.out"

if ! ( cd "${go_dir}" && go list -json -deps ./... ) >"${deps_json}" 2>"${deps_err}"; then
	cat "${deps_err}" >&2
	die "'go list -json -deps ./...' failed — fix the module/build error above before nancy can scan (see ${deps_err})"
fi

if "${bin}" sleuth --no-color <"${deps_json}" >"${out}" 2>&1; then
	note "no vulnerable dependencies found"
	exit 0
fi

# D6 (PR #10561): only a 401 skips. The annotation goes to stdout
# because GitHub reads workflow commands from stdout, not stderr.
if rg -q '401 Unauthorized' "${out}"; then
	printf '::warning::nancy-local: OSS Index returned 401 Unauthorized (anonymous quota/auth failure, not a vulnerability finding); skipping scan, see #5804\n'
	note "OSS Index returned 401 Unauthorized (anonymous quota/auth failure, not a vulnerability finding) -- skipping scan, see #5804"
	note "full diagnostic in ${out}"
	exit 0
fi

if head -n1 "${out}" | rg -q '^Error:'; then
	note "could not complete a real OSS Index scan (transport/auth failure, not the ticketed 401 skip -- failing closed, see #5804):"
	head -n2 "${out}" >&2
	note "full diagnostic in ${out}"
	die "nancy scan incomplete (see output above)"
fi

cat "${out}" >&2
die "vulnerable dependencies found (see output above)"
