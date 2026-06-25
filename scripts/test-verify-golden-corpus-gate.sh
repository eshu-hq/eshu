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

fail() { printf 'test-verify-golden-corpus-gate: %s\n' "$*" >&2; exit 1; }

[[ -f "${script}" ]] || fail "missing ${script}"
[[ -x "${script}" ]] || fail "verify-golden-corpus-gate.sh must be executable"

# Parses under bash -n.
bash -n "${script}" || fail "verify-golden-corpus-gate.sh has a syntax error"

require() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${script}" || fail "missing ${label}: ${needle}"
}

# Strict mode and self-cleanup.
require "strict mode" "set -euo pipefail"
require "exit trap" "trap cleanup EXIT"

# Drives every pipeline stage end to end.
require "bootstrap stage" "eshu-bootstrap-index"
require "cassette replay" "-mode=cassette"
require "projector drain" "eshu-projector"
require "reducer drain" "eshu-reducer"
require "api for query truth" "eshu-api"
require "gate binary" "eshu-golden-corpus-gate"

# Asserts all four B-7 buckets.
require "drains phase" "-phase=drains"
require "graph+query+timing phase" "-phase=graph,query,timing"
require "snapshot contract" "testdata/golden/e2e-20repo-snapshot.json"
require "timing budget" "-budget-multiplier"
require "minimal blocking correlations" 'rc-1,rc-3'

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

# No private data: hostnames, IPs, cloud account IDs, keys, internal paths.
private_pattern='ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|arn:aws:|(^|[^0-9])[0-9]{12}([^0-9]|$)|/Users/|/home/[a-z]'
if rg --pcre2 --quiet -- "${private_pattern}" "${script}"; then
	fail "verify-golden-corpus-gate.sh looks like it contains private data"
fi

printf 'test-verify-golden-corpus-gate: pass\n'
