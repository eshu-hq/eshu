#!/usr/bin/env bash
# C-1/C-8/C-9 replay coverage manifest + lockstep gate (#4173, #4187, #4188, epic #4172).
# Enumerates every surface Eshu claims to support from the source-of-truth
# registries and reports any required surface/scenario_type pair lacking a green
# replay scenario.
#
# This gate is static: registries + the curated coverage manifest + on-disk
# artifact existence + the committed B-12 snapshot. It is credential-free,
# Docker-free, and needs no running service — unlike the golden-corpus-gate it
# composes with, which actually replays the scenarios this gate counts.
#
# Local runs default to advisory so a developer can inspect a coverage report
# without failing the command. CI passes --blocking now that C-2..C-9 burned the
# gaps down, so any uncovered, unresolved, or stale surface fails the workflow.
#
# Usage:
#   scripts/verify-replay-coverage-gate.sh [--blocking]
# Env:
#   RCOV_REPORT_OUT  report artifact path (default: replay-coverage-report.json
#                    at the repo root). Absolute paths are honoured as-is.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

blocking=""
for arg in "$@"; do
	case "${arg}" in
	--blocking) blocking="-blocking" ;;
	*)
		printf 'verify-replay-coverage-gate: unknown argument %s\n' "${arg}" >&2
		exit 2
		;;
	esac
done

report_out="${RCOV_REPORT_OUT:-replay-coverage-report.json}"
case "${report_out}" in
/*) report_abs="${report_out}" ;;
*) report_abs="${repo_root}/${report_out}" ;;
esac

# Unit proof for the gate logic before running it, so a logic regression fails
# here rather than producing a wrong coverage report.
(cd go && go test ./internal/replaycoverage/ ./cmd/replay-coverage-gate/ -count=1)

# The committed C-7 dashboard (docs-discoverable %-covered + gap list). The unit
# proof above runs TestCommittedDashboardIsCurrent, so a stale dashboard fails
# before this point. Blocking runs refresh it in place (a no-op when current);
# local advisory runs write only the JSON report so they cannot dirty the
# committed dashboard back to advisory mode. Regenerate a stale one with:
#   cd go && go test ./cmd/replay-coverage-gate/ -update-dashboard
dashboard_abs="${repo_root}/docs/public/reference/replay-coverage.md"
gate_args=(
	-specs-dir "${repo_root}/specs"
	-snapshot "${repo_root}/testdata/golden/e2e-20repo-snapshot.json"
	-repo-root "${repo_root}"
	-report-out "${report_abs}"
)
if [[ -n "${blocking}" ]]; then
	gate_args+=(-dashboard-out "${dashboard_abs}" "${blocking}")
fi

# Run the gate over the real registries with absolute paths (robust regardless of
# the working directory go run resolves refs from).
(cd go && go run ./cmd/replay-coverage-gate "${gate_args[@]}")

printf '\nPASS: C-1/C-8/C-9 replay coverage gate (%s); report at %s\n' "${blocking:-advisory}" "${report_abs}"
