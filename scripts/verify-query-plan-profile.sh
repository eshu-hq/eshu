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
	ESHU_QUERYPLAN_PROFILE_LIVE=1 \
	ESHU_QUERYPLAN_PROFILE_ISOLATED=1 \
	ESHU_NEO4J_URI="bolt://127.0.0.1:${port}" \
	ESHU_NEO4J_USERNAME=neo4j \
	ESHU_NEO4J_PASSWORD="$password" \
	ESHU_NEO4J_DATABASE=neo4j \
	go test -tags queryplan_profile_live ./internal/query \
		-run '^(TestQueryplanBoundedAnchorOperatorPolicyIsClosed|TestQueryplanForbiddenOperatorPolicyIsClosed|TestProductionQueryplanProfilesRejectWholeGraphScans)$' \
		-count=1
)

printf 'verify-query-plan-profile: pass\n'
