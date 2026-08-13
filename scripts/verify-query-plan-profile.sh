#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
image="${ESHU_QUERYPLAN_PROFILE_IMAGE:-neo4j@sha256:6c162e2432f861f2c4e3da77a6ba478e7f10e2160b870541f85294532bc6ff5f}"
container="eshu-queryplan-profile-${$}-${RANDOM}"
password="queryplan-profile-${RANDOM}-${$}"

cleanup() {
	docker rm -f "$container" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

if ! command -v docker >/dev/null 2>&1; then
	printf 'verify-query-plan-profile: docker is required for the isolated planner gate\n' >&2
	exit 1
fi

docker run --rm -d \
	--name "$container" \
	-p 127.0.0.1::7687 \
	-e "NEO4J_AUTH=neo4j/${password}" \
	"$image" >/dev/null

ready=false
for _ in {1..45}; do
	if docker logs "$container" 2>&1 | rg -q 'Started\.'; then
		ready=true
		break
	fi
	sleep 2
done
if [ "$ready" != true ]; then
	printf 'verify-query-plan-profile: isolated Neo4j did not become ready\n' >&2
	docker logs "$container" 2>&1 | tail -80 >&2
	exit 1
fi

port="$(docker port "$container" 7687/tcp | awk -F: 'NR == 1 {print $NF}')"
if [ -z "$port" ]; then
	printf 'verify-query-plan-profile: could not resolve the isolated Bolt port\n' >&2
	exit 1
fi

(
	cd "${repo_root}/go"
	# shellcheck source=scripts/lib/go-test-run-guard.sh
	. "${repo_root}/scripts/lib/go-test-run-guard.sh"
	# go_test_run_guard (#6055) asserts the pattern still matches all 3 named
	# tests before running them, so a rename or move that drops the match
	# count to zero fails loudly instead of the bare `go test -run` exiting 0
	# on nothing.
	#
	# -timeout is explicit and deliberately above the gate's own 6-minute
	# wall-clock backstop (queryplanProfileTotalBudget, in
	# go/internal/query/queryplan_profile_deadlines_test.go): when a run is
	# pathologically slow the gate should stop itself with a message naming the
	# budget it blew, not die on a `go test` panic that names nothing.
	#
	# Nothing may go between these env assignments and the command they prefix:
	# a comment line here ends the continuation, the variables never reach
	# `go test`, and the live test then SKIPS while this gate still exits 0.
	# The post-run schema assertion below exists because that happened.
	ESHU_QUERYPLAN_PROFILE_LIVE=1 \
	ESHU_QUERYPLAN_PROFILE_ISOLATED=1 \
	ESHU_NEO4J_URI="bolt://127.0.0.1:${port}" \
	ESHU_NEO4J_USERNAME=neo4j \
	ESHU_NEO4J_PASSWORD="$password" \
	ESHU_NEO4J_DATABASE=neo4j \
	go_test_run_guard 3 '^(TestQueryplanBoundedAnchorOperatorPolicyIsClosed|TestQueryplanForbiddenOperatorPolicyIsClosed|TestProductionQueryplanProfilesRejectWholeGraphScans)$' \
		-- -tags queryplan_profile_live ./internal/query -count=1 -timeout=12m
)

# The live PROFILE test skips itself when ESHU_QUERYPLAN_PROFILE_LIVE is unset,
# and a skipped Go test exits 0 and prints nothing without -v: this gate would
# report "pass" having profiled no query at all. So assert the run left its
# fingerprint on the container. A fresh Neo4j 5 database has only its two
# built-in token-lookup indexes; the gate's schema phase creates many more, so
# an index count still at or below that floor means the profiles never ran.
#
# The last line must be the count and nothing else. An earlier version stripped
# every non-digit out of it instead, which turned any line carrying a digit --
# a warning, a footer, a driver message on stdout -- into a plausible count and
# would have passed the gate on it. cypher-shell's stderr also stays on this
# script's stderr rather than being folded into the value, for the same reason.
# Anything that is not a bare integer fails here: an unreadable count is not
# evidence that the profiles ran.
readonly fresh_database_index_count=2
index_output="$(docker exec "$container" cypher-shell \
	-u neo4j -p "$password" --format plain \
	'SHOW INDEXES YIELD name RETURN count(name) AS indexes' || true)"
index_count="$(printf '%s\n' "$index_output" | tail -1 | tr -d '[:space:]')"
case "$index_count" in
	'' | *[!0-9]*)
		printf 'verify-query-plan-profile: reading the index count back from the isolated database gave "%s", which is not a whole number. That is a failed read, not a count, and the live PROFILE test cannot be shown to have run — full output:\n%s\n' \
			"$index_count" "$index_output" >&2
		exit 1
		;;
esac
if [ "$index_count" -le "$fresh_database_index_count" ]; then
	printf 'verify-query-plan-profile: the isolated database holds %s index(es), at or below the %s a fresh database starts with — the live PROFILE test did not run (it most likely skipped because its environment did not reach `go test`)\n' \
		"$index_count" "$fresh_database_index_count" >&2
	exit 1
fi

printf 'verify-query-plan-profile: pass (%s indexes created, profiles ran)\n' "$index_count"
