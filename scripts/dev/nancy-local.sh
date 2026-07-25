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
# cannot always run locally (that script also prints guidance and exits 0
# when trivy itself is unavailable, deferring to CI, rather than failing a
# gate no local environment can satisfy). This script exits non-zero ONLY
# for:
#   (a) a `go list` failure (a real build problem in THIS repo), or
#   (b) a genuine vulnerability finding reported by nancy.
# A hard nancy error (auth, network, or bad input) exits 0 with a clear,
# non-silent diagnostic instead, because neither this repo's CI nor local dev
# has OSS_INDEX_USERNAME/OSS_INDEX_TOKEN configured (#5804): Sonatype's OSS
# Index currently rejects every anonymous request with 401 Unauthorized
# (verified directly against the API, no nancy involved), so an unconditional
# non-zero exit here would make this advisory gate permanently red for every
# contributor, for a reason outside their control, with no local action able
# to fix it. CI's identical, unpiped `nancy sleuth --no-color` has never
# scanned anything either (it silently audits 0 dependencies and reports
# success) -- this script is strictly more honest than that baseline, not
# less.
#
# Case (a) vs (b) vs a hard nancy error are told apart by nancy's own output:
# verified against nancy v1.2.0 (`internal/cmd/sleuth.go:64-73`), a genuine
# vulnerability finding returns a `customerrors.ErrorExit` and calls
# `os.Exit(errExit.ExitCode)` directly, bypassing cobra's error-printing path
# entirely -- only a transport/auth/input failure (panic -> recovered ->
# returned error -> cobra's default RunE error handling) produces output
# whose first line starts with "Error:". Re-verify this discriminator against
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

if head -n1 "${out}" | rg -q '^Error:'; then
	note "could not complete a real OSS Index scan (transport/auth failure, not a vulnerability finding) -- deferring non-fatally (nancy is advisory, see #5804):"
	head -n2 "${out}" >&2
	note "full diagnostic in ${out}; CI's nancy job has the same limitation today (#5804)."
	exit 0
fi

cat "${out}" >&2
die "vulnerable dependencies found (see output above)"
