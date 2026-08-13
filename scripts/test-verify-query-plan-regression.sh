#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-query-plan-regression.sh"
profile_verifier="${repo_root}/scripts/verify-query-plan-profile.sh"

bash -n "$verifier"
bash -n "$profile_verifier"

# require_anchor <file> <pattern> <why>
#
# Every assertion below is "this line is still in that script". A bare
# `rg --quiet` under `set -e` states the failure as an exit code and nothing
# else, which leaves whoever hits it in CI reading a red step with no text. This
# says which file lost which line and what that line was holding up.
require_anchor() {
	local file="$1" pattern="$2" why="$3"
	if rg --quiet -- "$pattern" "$file"; then
		return 0
	fi
	printf 'test-verify-query-plan-regression: %s no longer matches /%s/ — %s\n' \
		"${file#"${repo_root}/"}" "$pattern" "$why" >&2
	exit 1
}

require_anchor "$verifier" 'verify-query-plan-profile\.sh' \
	'the regression gate must run the live PROFILE verifier'
require_anchor "$verifier" 'TestHandlerQueryplanManifestBindsProductionBuilders' \
	'the gate must pin the handler manifest binding test'
require_anchor "$verifier" 'TestLegacyQueryplanManifestBindsProductionQueries' \
	'the gate must pin the legacy manifest binding test'
require_anchor "$verifier" 'verify-query-plan-regression: pass' \
	'the gate must print its own pass line'
require_anchor "$profile_verifier" 'neo4j@sha256:[0-9a-f]{64}' \
	'the profile gate must pin its Neo4j image by digest, not by tag'
require_anchor "$profile_verifier" 'TestQueryplanForbiddenOperatorPolicyIsClosed' \
	'the profile gate must run the forbidden-operator policy test'
require_anchor "$profile_verifier" 'trap cleanup EXIT INT TERM' \
	'the profile gate must remove its container however it exits'
require_anchor "$profile_verifier" '-tags queryplan_profile_live' \
	'the live PROFILE test only builds under its own tag'
require_anchor "$profile_verifier" 'verify-query-plan-profile: pass' \
	'the profile gate must print its own pass line'

# The live PROFILE test skips itself when its environment does not reach
# `go test`, and a skipped Go test exits 0 without -v: the gate then prints
# "pass" having profiled nothing, which is how two evidence runs came back
# vacuously green. The profile verifier now reads the index count back from the
# container to prove the profiles ran, and bounds the run with an explicit
# -timeout. Both are load-bearing, so both are pinned here — a guard whose
# removal no test notices is the same false green wearing a fix's shape.
require_anchor "$profile_verifier" '-timeout=12m' \
	'without an explicit -timeout a pathologically slow run dies on the default 10m panic instead of the gate naming the budget it blew'
require_anchor "$profile_verifier" 'fresh_database_index_count=2' \
	'the vacuous-skip guard compares against the index count a fresh Neo4j 5 database starts with'
require_anchor "$profile_verifier" 'SHOW INDEXES YIELD name RETURN count\(name\)' \
	'the vacuous-skip guard has to read the index count back from the container to know the profiles ran'
require_anchor "$profile_verifier" '"\$index_count" -le "\$fresh_database_index_count"' \
	'reading the count proves nothing unless the gate fails when it is still at the fresh-database floor'
require_anchor "$profile_verifier" "'' \| \*\[!0-9\]\*" \
	'the count must be validated as a whole number; stripping non-digits out of the line let anything carrying a digit parse as a count'

# The profile verifier has no registry entry of its own; it runs as part of the
# query-plan-regression gate. Without it in that gate's triggers, a change to
# that file alone — the index-count floor, the timeout — ships without the live
# gate re-running in CI.
gate_triggers="$(awk '
	$0 == "  - id: query-plan-regression" { inside = 1; next }
	inside && /^  - id: / { exit }
	inside && /^    triggers:/ { collecting = 1; next }
	collecting && /^    [a-z_]+:/ { exit }
	collecting { print }
' "${repo_root}/specs/ci-gates.v1.yaml")"
if [ -z "$gate_triggers" ]; then
	printf 'test-verify-query-plan-regression: found no triggers for the query-plan-regression gate in specs/ci-gates.v1.yaml — the gate was renamed, moved, or reshaped, and the trigger checks below would otherwise pass having read nothing\n' >&2
	exit 1
fi
for required_trigger in \
	"scripts/verify-query-plan-regression.sh" \
	"scripts/verify-query-plan-profile.sh" \
	"scripts/test-verify-query-plan-regression.sh"; do
	if ! printf '%s\n' "$gate_triggers" | rg --quiet --fixed-strings "\"${required_trigger}\""; then
		printf 'test-verify-query-plan-regression: %s is not in the query-plan-regression triggers in specs/ci-gates.v1.yaml, so a change to it alone would not re-run this gate in CI\n' \
			"$required_trigger" >&2
		exit 1
	fi
done

printf 'test-verify-query-plan-regression: pass\n'
