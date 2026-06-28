#!/usr/bin/env bash
#
# refresh-e2e-baseline.sh — recapture the B-11 (#3804) per-phase wall-clock
# baseline by running the B-7 golden corpus gate and folding the observed
# per-phase seconds into testdata/golden/e2e-baseline.json.
#
# Run this on the enforcement host (consistent hardware) when an intentional perf
# change alters a phase's timing, or when seeding the baseline for a new host.
# Only baseline_seconds is updated; gated flags, notes, band, slack, and the
# policy blocks are preserved, so the human-curated parts of the contract survive
# a recapture. Review the resulting diff before committing — a baseline bump is a
# reviewed claim that the new timing is the expected normal.
#
# Requires Docker + jq + rg and the gate env (same as verify-golden-corpus-gate.sh).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
baseline="${repo_root}/testdata/golden/e2e-baseline.json"

die() {
	printf 'refresh-e2e-baseline: %s\n' "$*" >&2
	exit 1
}

command -v jq >/dev/null 2>&1 || die "missing required tool: jq"
command -v rg >/dev/null 2>&1 || die "missing required tool: rg"
[[ -f "${baseline}" ]] || die "baseline not found: ${baseline}"

log_out="$(mktemp)"
trap 'rm -f "${log_out}"' EXIT

printf 'refresh-e2e-baseline: running the golden corpus gate to capture per-phase timings...\n' >&2
ESHU_GRAPH_BACKEND="${ESHU_GRAPH_BACKEND:-nornicdb}" \
	ESHU_NEO4J_PASSWORD="${ESHU_NEO4J_PASSWORD:-change-me}" \
	ESHU_POSTGRES_PASSWORD="${ESHU_POSTGRES_PASSWORD:-change-me}" \
	bash "${repo_root}/scripts/verify-golden-corpus-gate.sh" 2>&1 | tee "${log_out}"

# The gate logs one line: "per-phase timings: {json}". Take the last occurrence.
timings_json="$(rg -o 'per-phase timings: (\{.*\})' -r '$1' "${log_out}" | tail -1 || true)"
[[ -n "${timings_json}" ]] || die "the gate emitted no per-phase timings line"

# Fold observed seconds into the existing baseline, preserving every other field.
updated="$(jq --argjson obs "${timings_json}" '
  .phases |= with_entries(
    .value.baseline_seconds = (($obs.phases[.key]) // .value.baseline_seconds)
  )
' "${baseline}")" || die "failed to merge observed timings into the baseline"

printf '%s\n' "${updated}" >"${baseline}"
printf 'refresh-e2e-baseline: updated %s — review the diff before committing\n' "${baseline}" >&2
