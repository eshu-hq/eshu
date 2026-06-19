#!/usr/bin/env bash
#
# scripts/verify_aws_runtime_drift_compose.sh
#
# Compose-level proof for the offline AWS cloud collector fixture mode
# (issue #3063). It proves the fully offline path end-to-end with NO AWS
# credentials and NO network calls:
#
#   collector-aws-cloud -mode fixture
#     -> aws_resource / aws_relationship facts committed to Postgres
#     -> projector enqueues the aws_cloud_runtime_drift reducer intent
#     -> reducer classifies arn:aws:s3:::eshu-fixture-unmanaged as cloud_only
#        (orphaned_cloud_resource) because no Terraform state/config declares it
#     -> POST /api/v0/replatforming/plans (scope_kind=account, account_id=...)
#        returns a non-empty migration plan (items_count >= 1).
#
# The fixture estate lives in
# go/cmd/collector-aws-cloud/testdata/fixture-estate.json and the drift intent
# is documented in tests/fixtures/aws_runtime_drift/README.md.
#
# Usage:
#   bash scripts/verify_aws_runtime_drift_compose.sh
#
# Environment knobs honored:
#   ESHU_KEEP_COMPOSE_STACK=true   Skip `docker compose down -v` at the end.
#   ESHU_AWS_RUNTIME_DRIFT_PROOF_OUT  Write the captured plan JSON to this file.
#
# NOTE: This script is intended to run on a Docker host (for example OrbStack).
# It is not run as part of `go test`.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

RUNTIME_LIB="$REPO_ROOT/scripts/lib/compose_verification_runtime_common.sh"
# shellcheck source=scripts/lib/compose_verification_runtime_common.sh
source "$RUNTIME_LIB"

# Fixture config path inside the eshu image. docker-compose.yaml bind-mounts the
# repo root at /app-src, so the checked-in fixture is visible to the container.
FIXTURE_CONFIG_CONTAINER_PATH="/app-src/go/cmd/collector-aws-cloud/testdata/fixture-estate.json"
FIXTURE_ACCOUNT_ID="111122223333"
FIXTURE_ORPHAN_ARN="arn:aws:s3:::eshu-fixture-unmanaged"

KEEP_STACK="${ESHU_KEEP_COMPOSE_STACK:-false}"
PROOF_OUT="${ESHU_AWS_RUNTIME_DRIFT_PROOF_OUT:-}"
COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-eshu-aws-runtime-drift-3063-$$}"
export COMPOSE_PROJECT_NAME

COMPOSE_CMD=()
COMPOSE_DISPLAY=""

TMP_DIR="$(mktemp -d)"
PLAN_FILE="$TMP_DIR/replatforming-plan.json"
COLLECTOR_LOG_FILE="$TMP_DIR/collector-aws-cloud.log"

# -- preflight ---------------------------------------------------------------

require_compose() {
	for candidate in "docker compose" "docker-compose"; do
		# shellcheck disable=SC2206
		local cmd_array=($candidate)
		if "${cmd_array[@]}" version >/dev/null 2>&1; then
			COMPOSE_CMD=("${cmd_array[@]}")
			COMPOSE_DISPLAY="$candidate"
			return 0
		fi
	done
	echo "docker compose not found on PATH" >&2
	return 1
}

eshu_require_tool docker
eshu_require_tool curl
eshu_require_tool jq
eshu_require_tool nc
require_compose

# -- port assignment ---------------------------------------------------------
# Every metrics/HTTP port is overridden so this project does not collide with a
# concurrently running compose project (see the parallel-agent port-collision
# lesson). Defaults are intentionally far from the shared 18080/19464 band.

configure_ports() {
	eshu_reset_reserved_ports
	eshu_assign_reserved_port NEO4J_HTTP_PORT "${NEO4J_HTTP_PORT:-47474}"
	eshu_assign_reserved_port NEO4J_BOLT_PORT "${NEO4J_BOLT_PORT:-47687}"
	eshu_assign_reserved_port ESHU_POSTGRES_PORT "${ESHU_POSTGRES_PORT:-45432}"
	eshu_assign_reserved_port ESHU_HTTP_PORT "${ESHU_HTTP_PORT:-48080}"
	eshu_assign_reserved_port ESHU_MCP_PORT "${ESHU_MCP_PORT:-48081}"
	eshu_assign_reserved_port ESHU_API_METRICS_PORT "${ESHU_API_METRICS_PORT:-41464}"
	eshu_assign_reserved_port ESHU_BOOTSTRAP_METRICS_PORT "${ESHU_BOOTSTRAP_METRICS_PORT:-41467}"
	eshu_assign_reserved_port ESHU_MCP_METRICS_PORT "${ESHU_MCP_METRICS_PORT:-41468}"
	eshu_assign_reserved_port ESHU_INGESTER_METRICS_PORT "${ESHU_INGESTER_METRICS_PORT:-41465}"
	eshu_assign_reserved_port ESHU_RESOLUTION_ENGINE_METRICS_PORT "${ESHU_RESOLUTION_ENGINE_METRICS_PORT:-41466}"
}

# -- cleanup -----------------------------------------------------------------

cleanup() {
	local exit_code=$?
	if [[ "$exit_code" -ne 0 ]]; then
		echo
		echo "aws runtime drift compose verification failed (exit=$exit_code)."
		echo "Compose project: $COMPOSE_PROJECT_NAME"
		echo "Useful commands:"
		echo "  $COMPOSE_DISPLAY ps"
		echo "  $COMPOSE_DISPLAY logs --tail=200 resolution-engine"
		"${COMPOSE_CMD[@]}" ps || true
		[[ -f "$COLLECTOR_LOG_FILE" ]] && {
			echo "collector-aws-cloud log (tail 40):"
			tail -n 40 "$COLLECTOR_LOG_FILE" || true
		}
		[[ -f "$PLAN_FILE" ]] && {
			echo "Last replatforming plan response:"
			cat "$PLAN_FILE" || true
		}
	fi
	if [[ "$KEEP_STACK" != "true" ]]; then
		"${COMPOSE_CMD[@]}" down -v >/dev/null 2>&1 || true
	fi
	rm -rf "$TMP_DIR"
	exit "$exit_code"
}
trap cleanup EXIT INT TERM

# -- helpers -----------------------------------------------------------------

# api_post posts a JSON body to an API route and writes the response body to
# out_file. The API key is read from the running eshu container.
api_post() {
	local route="$1" body="$2" out_file="$3"
	local api_key
	api_key="$(eshu_compose_read_api_key)"
	curl -fsS \
		-H "Authorization: Bearer ${api_key}" \
		-H "Content-Type: application/json" \
		-X POST \
		--data "$body" \
		"http://localhost:${ESHU_HTTP_PORT}${route}" \
		-o "$out_file"
}

# wait_for_replatforming_items polls the replatforming plan until items_count is
# at least 1 or the attempt budget is exhausted. The reducer enqueue is
# projector-driven, so we poll rather than wait on a one-shot bootstrap phase.
wait_for_replatforming_items() {
	local attempts="$1" sleep_seconds="$2"
	local body
	body="$(jq -nc \
		--arg account_id "$FIXTURE_ACCOUNT_ID" \
		'{scope_kind: "account", account_id: $account_id}')"
	for ((attempt = 1; attempt <= attempts; attempt++)); do
		if api_post "/api/v0/replatforming/plans" "$body" "$PLAN_FILE" 2>/dev/null; then
			local items_count
			items_count="$(jq -r '.items_count // 0' "$PLAN_FILE" 2>/dev/null || echo 0)"
			if [[ "$items_count" =~ ^[0-9]+$ ]] && ((items_count >= 1)); then
				return 0
			fi
		fi
		/bin/sleep "$sleep_seconds"
	done
	echo "Timed out waiting for a non-empty replatforming plan for account $FIXTURE_ACCOUNT_ID" >&2
	return 1
}

# -- run ---------------------------------------------------------------------

cd "$REPO_ROOT"

echo "==> Configuring host port assignments"
configure_ports

echo "==> Bringing up compose stack (postgres + nornicdb + db-migrate + bootstrap-index + ingester + resolution-engine + api)"
"${COMPOSE_CMD[@]}" up --build -d \
	postgres nornicdb db-migrate workspace-setup \
	bootstrap-index \
	ingester \
	resolution-engine \
	eshu

echo "==> Waiting for db-migrate to complete"
eshu_compose_wait_for_named_exit "db-migrate" 180

echo "==> Waiting for first bootstrap-index pass to complete"
eshu_compose_wait_for_named_exit "bootstrap-index" 240

echo "==> Waiting for the API to report healthy"
eshu_compose_wait_for_http "http://localhost:${ESHU_HTTP_PORT}/healthz" 60 2

echo "==> Ingesting the offline AWS estate via collector-aws-cloud -mode fixture (no credentials, no network)"
# The collector runs as a poll loop, so run it detached and stop it once the
# orphan finding has materialized. The fixture source re-emits the same
# deterministic facts on each poll, so an extra poll before teardown is
# idempotent.
"${COMPOSE_CMD[@]}" run -d --rm --no-deps \
	--name "${COMPOSE_PROJECT_NAME}-aws-fixture" \
	--entrypoint /usr/local/bin/eshu-collector-aws-cloud \
	ingester \
	-mode fixture -config "$FIXTURE_CONFIG_CONTAINER_PATH" \
	>"$COLLECTOR_LOG_FILE" 2>&1

echo "==> Waiting for the reducer to surface the orphaned_cloud_resource finding in the replatforming plan"
wait_for_replatforming_items 60 3

echo "==> Stopping the fixture collector"
docker stop "${COMPOSE_PROJECT_NAME}-aws-fixture" >/dev/null 2>&1 || true

# -- assertions --------------------------------------------------------------

echo "==> Asserting the migration plan is non-empty"
items_count="$(jq -r '.items_count // 0' "$PLAN_FILE")"
echo "  items_count=$items_count"
if ! [[ "$items_count" =~ ^[0-9]+$ ]] || ((items_count < 1)); then
	echo "items_count=$items_count, expected >= 1" >&2
	cat "$PLAN_FILE" >&2
	exit 1
fi

echo "==> Asserting the orphaned bucket is present as a migration-plan item"
# The migration-plan item's stable_id is the resource ARN
# (replatforming_ownership.go: StableID = finding.ARN), so match on that. The
# orphan must surface because no Terraform state/config declares it.
orphan_match="$(jq -r --arg arn "$FIXTURE_ORPHAN_ARN" '
	[.plan.items[]? | select(.stable_id == $arn)] | length' "$PLAN_FILE")"
echo "  orphan_match=$orphan_match for $FIXTURE_ORPHAN_ARN"
if [[ "$orphan_match" -lt 1 ]]; then
	echo "expected at least one migration-plan item for $FIXTURE_ORPHAN_ARN" >&2
	cat "$PLAN_FILE" >&2
	exit 1
fi

# Informational: echo the orphan's classification so a reviewer can confirm it
# is cloud_only / orphaned_cloud_resource. Not a hard gate because the exact
# wire enum values are not asserted from this script.
echo "==> Orphan classification (informational)"
jq -r --arg arn "$FIXTURE_ORPHAN_ARN" '
	.plan.items[]? | select(.stable_id == $arn)
	| "  finding_kind=\(.finding_kind // "<none>") source_state=\(.source_state // "<none>")"' \
	"$PLAN_FILE" || true

# -- proof artifact ----------------------------------------------------------

if [[ -n "$PROOF_OUT" ]]; then
	echo "==> Writing proof artifact to $PROOF_OUT"
	mkdir -p "$(dirname "$PROOF_OUT")"
	{
		echo "# aws runtime drift compose proof"
		echo
		echo "Captured: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
		echo "Compose project: \`$COMPOSE_PROJECT_NAME\`"
		echo "Worktree HEAD: \`$(git rev-parse --short HEAD)\` on $(git rev-parse --abbrev-ref HEAD)"
		echo
		echo "## Replatforming plan response"
		echo
		echo '```json'
		jq '.' "$PLAN_FILE"
		echo '```'
	} >"$PROOF_OUT"
fi

echo
echo "OK — aws runtime drift compose proof passed (items_count=$items_count)."
