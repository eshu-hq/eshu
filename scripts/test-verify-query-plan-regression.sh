#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-query-plan-regression.sh"
profile_verifier="${repo_root}/scripts/verify-query-plan-profile.sh"

bash -n "$verifier"
bash -n "$profile_verifier"

rg --quiet 'verify-query-plan-profile\.sh' "$verifier"
rg --quiet 'neo4j@sha256:[0-9a-f]{64}' "$profile_verifier"
rg --quiet 'trap cleanup EXIT INT TERM' "$profile_verifier"
rg --quiet -- '-tags queryplan_profile_live' "$profile_verifier"
rg --quiet 'verify-query-plan-profile: pass' "$profile_verifier"
rg --quiet 'verify-query-plan-regression: pass' "$verifier"

printf 'test-verify-query-plan-regression: pass\n'
