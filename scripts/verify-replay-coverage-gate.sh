#!/usr/bin/env bash
# C-1 replay coverage manifest + lockstep gate (#4173, epic #4172). Enumerates
# every surface Eshu claims to support from the four source-of-truth registries
# (surface-inventory implemented-lane collectors, fact-kind-registry read
# surfaces, parser-backing-ledger parsers, capability-matrix claims) and reports
# any surface lacking a green replay scenario.
#
# This gate is static: registries + the curated coverage manifest + on-disk
# artifact existence + the committed B-12 snapshot. It is credential-free,
# Docker-free, and needs no running service — unlike the golden-corpus-gate it
# composes with, which actually replays the scenarios this gate counts.
#
# Ships ADVISORY: by default a coverage gap is reported but the gate still
# succeeds, so its red output is the C-2..C-6 backfill worklist. Pass --blocking
# to fail on any uncovered/unresolved/stale surface — the flip happens after
# C-2..C-6 burn the gaps down.
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

# Run the gate over the real registries with absolute paths (robust regardless of
# the working directory go run resolves refs from).
(cd go && go run ./cmd/replay-coverage-gate \
	-specs-dir "${repo_root}/specs" \
	-snapshot "${repo_root}/testdata/golden/e2e-20repo-snapshot.json" \
	-repo-root "${repo_root}" \
	-report-out "${report_abs}" \
	${blocking})

printf '\nPASS: C-1 replay coverage gate (%s); report at %s\n' "${blocking:-advisory}" "${report_abs}"
