#!/usr/bin/env bash
#
# test-refresh-e2e-baseline.sh — static contract mirror for refresh-e2e-baseline.sh.
# The refresh itself needs Docker + a full gate run, so this validates the
# script's contract cheaply and checks the committed baseline's shape and the
# jq merge expression in isolation.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/refresh-e2e-baseline.sh"
baseline="${repo_root}/testdata/golden/e2e-baseline.json"

fail() { printf 'test-refresh-e2e-baseline: %s\n' "$*" >&2; exit 1; }

command -v jq >/dev/null 2>&1 || fail "missing required tool: jq"
command -v rg >/dev/null 2>&1 || fail "missing required tool: rg"

[[ -f "${script}" ]] || fail "missing ${script}"
[[ -x "${script}" ]] || fail "refresh-e2e-baseline.sh must be executable"
bash -n "${script}" || fail "refresh-e2e-baseline.sh has a syntax error"

require() {
	rg --fixed-strings --quiet -- "$2" "${script}" || fail "missing $1: $2"
}
require "strict mode" "set -euo pipefail"
require "runs the gate" "verify-golden-corpus-gate.sh"
require "parses the timings line" "per-phase timings:"
require "preserves non-timing fields" "baseline_seconds ="
require "writes the baseline" "e2e-baseline.json"

# The committed baseline must be valid JSON with the fields the gate binary reads.
[[ -f "${baseline}" ]] || fail "missing ${baseline}"
jq -e 'type == "object"' "${baseline}" >/dev/null || fail "baseline is not a JSON object"
jq -e '.regression_band > 0' "${baseline}" >/dev/null || fail "baseline regression_band must be > 0"
jq -e '.phases | type == "object" and length > 0' "${baseline}" >/dev/null || fail "baseline must have phases"
jq -e '.phases | to_entries | all(.value | has("baseline_seconds") and has("gated"))' "${baseline}" >/dev/null \
	|| fail "every phase must carry baseline_seconds and gated"

# Exercise the jq merge in isolation: observed seconds overwrite baseline_seconds
# while gated and notes are preserved.
merged="$(jq --argjson obs '{"phases":{"bootstrap":99}}' '
  .phases |= with_entries(
    .value.baseline_seconds = (($obs.phases[.key]) // .value.baseline_seconds)
  )
' "${baseline}")"
got="$(jq -r '.phases.bootstrap.baseline_seconds' <<<"${merged}")"
[[ "${got}" == "99" ]] || fail "merge did not update bootstrap baseline_seconds (got ${got})"
gated="$(jq -r '.phases.bootstrap.gated' <<<"${merged}")"
[[ "${gated}" == "true" ]] || fail "merge did not preserve bootstrap gated flag (got ${gated})"

printf 'test-refresh-e2e-baseline: pass\n'
