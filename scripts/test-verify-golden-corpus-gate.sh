#!/usr/bin/env bash
# Static structural test for verify-golden-corpus-gate.sh. The verifier itself
# needs Docker + a built toolchain to run end to end (exercised by the
# golden-corpus-gate CI workflow), so this mirror validates the contract that
# cannot silently drift: the script parses, sets strict mode, drives every
# pipeline stage and drain, honours the B-13 shared_projection_intents gate, and
# leaks no private data.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-golden-corpus-gate.sh"
fixture_lib="${repo_root}/scripts/lib/golden-corpus-fixtures.sh"

fail() { printf 'test-verify-golden-corpus-gate: %s\n' "$*" >&2; exit 1; }

[[ -f "${script}" ]] || fail "missing ${script}"
[[ -x "${script}" ]] || fail "verify-golden-corpus-gate.sh must be executable"
[[ -f "${fixture_lib}" ]] || fail "missing ${fixture_lib}"

# Parses under bash -n.
bash -n "${script}" || fail "verify-golden-corpus-gate.sh has a syntax error"
bash -n "${fixture_lib}" || fail "golden-corpus-fixtures.sh has a syntax error"

require() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${script}" || fail "missing ${label}: ${needle}"
}

# Strict mode and self-cleanup.
require "strict mode" "set -euo pipefail"
require "exit trap" "trap cleanup EXIT"
# Background pids must be recorded in the PARENT shell (printf -v), or the cleanup
# trap reaps nothing on a failure path and leaks host processes.
require "parent-shell pid capture" "printf -v"
# Failure must surface the host-binary logs before the work dir is removed.
require "failure log dump" "host binary logs (failure)"
# A collector that no-ops must not let the gate pass: liveness + facts-landed.
require "collector liveness check" "exited during settle"
require "cassette facts landed check" "credentialed collector source"

# Drives every pipeline stage end to end.
require "bootstrap stage" "eshu-bootstrap-index"
require "cassette replay" "-mode=cassette"
require "projector drain" "eshu-projector"
require "reducer drain" "eshu-reducer"
require "api for query truth" "eshu-api"
require "gate binary" "eshu-golden-corpus-gate"
require "corpus fixture inventory source" "golden-corpus-fixtures.sh"
rg --fixed-strings --quiet -- $'\tsql_comprehensive' "${fixture_lib}" \
	|| fail "missing SQL relationship corpus fixture in ${fixture_lib}"

# Asserts all four B-7 buckets.
require "drains phase" "-phase=drains"
require "graph+query+timing phase" "-phase=graph,query,timing"
require "snapshot contract" "testdata/golden/e2e-20repo-snapshot.json"
require "timing budget" "-budget-multiplier"
# #4596: the blocking-correlation set must be single-sourced from the
# snapshot's own required_correlations ids via the "all" sentinel, not a
# second, hand-maintained comma-separated id list duplicated here.
require "single-sourced required-correlations" '-required-correlations="all"'
if rg --pcre2 --quiet -- '-required-correlations="rc-[0-9]+,rc-' "${script}"; then
	fail "-required-correlations reverted to a hand-maintained comma-separated id list (#4596 regression)"
fi
# B-11 (#3804) macro per-phase wall-clock: the orchestrator sources the timing
# helper lib and invokes it; the emission + gate wiring live in that lib chunk
# (extracted to keep this orchestrator under the 500-line cap).
require "phase-timing lib source" "golden-corpus-phase-timings.sh"
require "phase-timing invocation" "emit_phase_timings_and_flags"
require "passes phase flags to gate" "phase_flags"
require "cross-repo dead-code fixture source" "golden-corpus-dead-code-fixtures.sh"
require "cross-repo dead-code fixture invocation" "seed_cross_repo_dead_code_fixture"

timing_lib="${repo_root}/scripts/lib/golden-corpus-phase-timings.sh"
[[ -f "${timing_lib}" ]] || fail "missing phase-timing lib: ${timing_lib}"
bash -n "${timing_lib}" || fail "phase-timing lib has a syntax error"
dead_code_lib="${repo_root}/scripts/lib/golden-corpus-dead-code-fixtures.sh"
[[ -f "${dead_code_lib}" ]] || fail "missing dead-code fixture lib: ${dead_code_lib}"
bash -n "${dead_code_lib}" || fail "dead-code fixture lib has a syntax error"
require_lib() {
	rg --fixed-strings --quiet -- "$2" "${timing_lib}" || fail "missing $1 in phase-timing lib: $2"
}
require_lib "phase-timings emission" "phase-timings.json"
require_lib "phase baseline default" "e2e-baseline.json"
require_lib "per-phase gate flag" "-phase-timings-file="
# The per-phase check must default to advisory on shared CI runners (hardware
# variance exceeds the band); a controlled host flips it blocking.
require_lib "per-phase advisory default" "-phase-regression-advisory"
# Minimal-corpus posture: graph-populated smoke is required. Every
# shared_projection_intents domain (incl. code_calls, #3865) must drain — no
# domain is quarantined as advisory.
require "graph-populated smoke" "-required-node-labels"
if rg --quiet --fixed-strings -- 'drain-advisory-domains="code_calls"' "${script}"; then
	fail "code_calls must no longer be quarantined as an advisory drain domain (#3865 fixed)"
fi

# Wires all nine B-10 cassette collectors.
for collector in \
	collector-kubernetes-live collector-aws-cloud collector-azure-cloud \
	collector-gcp-cloud collector-vault-live collector-oci-registry \
	collector-package-registry collector-terraform-state collector-prometheus-mimir; do
	require "collector ${collector}" "${collector}"
done

# The B-13 (#3859) drain gate lives in the gate binary; the orchestrator must run
# the drains phase against the snapshot whose shared_projection_intents bound is
# the real signal. Guard against someone reducing the drain check to a sleep.
if rg --quiet --pcre2 'sleep\s+\$\{?GATE_DRAIN' "${script}"; then
	fail "drain must be polled by the gate, not slept"
fi

# Premature-convergence guard: the drain must require the reducer to be observed
# populated before accepting a drained reading, or it can pass on an unreduced
# pipeline (the 0/0-before-the-reducer-runs race).
require "populated-then-drained guard" 'require-populated-domains="repo_dependency"'

# No private data: hostnames, IPs, cloud account IDs, keys, internal paths.
private_pattern='ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|arn:aws:|(^|[^0-9])[0-9]{12}([^0-9]|$)|/Users/|/home/[a-z]'
if rg --pcre2 --quiet -- "${private_pattern}" "${script}"; then
	fail "verify-golden-corpus-gate.sh looks like it contains private data"
fi

printf 'test-verify-golden-corpus-gate: pass\n'
