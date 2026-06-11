#!/usr/bin/env bash
set -euo pipefail

# End-to-end driver for the Scorecard component-extension OCI-adapter remote
# Compose proof (#1980, #1923). It produces the runnable half of the proof:
#
#   1. Stand up a local OCI registry.
#   2. Build the minimal digest-pinnable Scorecard artifact image
#      (examples/collector-extensions/scorecard/Dockerfile.oci) and push it,
#      capturing the immutable manifest digest.
#   3. Bring up the base stack + the OCI overlay; a one-shot init pins the
#      manifest artifact to the real digest and installs/enables the component
#      under the OCI adapter; the worker launches the digest-pinned artifact with
#      `docker run` under the adapter isolation flags through the mounted host
#      runtime.
#   4. Wait for the Scorecard component work item to reach terminal success.
#   5. Reuse the shared capture script for inventory/workflow/facts artifacts,
#      then record an OCI provenance artifact, then run the OCI verifier.
#
# Only normalized, bounded fields are recorded (trust/enablement booleans,
# workflow terminal states, fact-family counts, and the digest-pinned ref). Raw
# fact payloads and source URIs are never dumped, so the verifier's redaction
# canary holds by construction.
#
# Usage (from repo root):
#   scripts/run-remote-e2e-component-extension-oci.sh [--artifacts <dir>] [--keep-up]
#
# Environment overrides:
#   CE_OCI_PROJECT     docker compose project name      (default: ce-proof-oci)
#   CE_OCI_REGISTRY    local registry host:port         (default: localhost:5050)
#   CE_OCI_REPO        artifact repository path          (default: eshu-examples/scorecard-collector)
#
# The harness owns a dedicated local registry (name + port below) so it never
# entangles with any other registry an operator may already be running. The
# default port avoids 5000, which macOS reserves for AirPlay/AirTunes.

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || (cd "$(dirname "$0")/.." && pwd))"
cd "${repo_root}"

project="${CE_OCI_PROJECT:-ce-proof-oci}"
registry="${CE_OCI_REGISTRY:-localhost:5050}"
repo="${CE_OCI_REPO:-eshu-examples/scorecard-collector}"
registry_name="eshu-ce-oci-proof-registry"
registry_host_port="${registry##*:}"
component_id="dev.eshu.examples.scorecard"
collector="${project}-component-extension-collector-1"
postgres="${project}-postgres-1"
artifacts_dir=""
keep_up=false

base_compose="${repo_root}/docker-compose.yaml"
overlay="${repo_root}/docs/public/run-locally/docker-compose.component-extension-oci.yaml"

die() { printf 'run-remote-e2e-component-extension-oci: %s\n' "$*" >&2; exit 1; }
log() { printf 'run-remote-e2e-component-extension-oci: %s\n' "$*"; }

while [[ $# -gt 0 ]]; do
	case "$1" in
		--artifacts) artifacts_dir="${2:-}"; shift 2 ;;
		--keep-up) keep_up=true; shift ;;
		-h|--help) sed -n '3,40p' "$0"; exit 0 ;;
		*) die "unknown option: $1" ;;
	esac
done

command -v docker >/dev/null 2>&1 || die "docker is required"
command -v rg >/dev/null 2>&1 || die "rg is required"
docker compose version >/dev/null 2>&1 || die "docker compose v2+ is required"

if [[ -z "${artifacts_dir}" ]]; then
	artifacts_dir="$(mktemp -d "${TMPDIR:-/tmp}/ce-proof-oci-artifacts.XXXXXX")"
fi
mkdir -p "${artifacts_dir}"

compose() {
	docker compose -p "${project}" -f "${base_compose}" -f "${overlay}" \
		--profile workflow-coordinator --profile component-extension-collector "$@"
}

# Remap every published host port into a high range so the proof stack never
# collides with another Eshu stack an operator may already be running. This is
# a source-evidence-only proof: facts and work items live in Postgres, so the
# minimal service set below does not need the graph backend.
export ESHU_POSTGRES_PORT="${ESHU_POSTGRES_PORT:-25432}"
export ESHU_WORKFLOW_COORDINATOR_HTTP_PORT="${ESHU_WORKFLOW_COORDINATOR_HTTP_PORT:-28082}"
export ESHU_WORKFLOW_COORDINATOR_METRICS_PORT="${ESHU_WORKFLOW_COORDINATOR_METRICS_PORT:-29469}"
export ESHU_COMPONENT_EXTENSION_COLLECTOR_HTTP_PORT="${ESHU_COMPONENT_EXTENSION_COLLECTOR_HTTP_PORT:-28084}"
export ESHU_COMPONENT_EXTENSION_COLLECTOR_METRICS_PORT="${ESHU_COMPONENT_EXTENSION_COLLECTOR_METRICS_PORT:-29470}"
# Minimal service set: Postgres + migrations + the install init + coordinator +
# the OCI worker. depends_on pulls in the rest of the chain.
proof_services="postgres db-migrate component-extension-install workflow-coordinator component-extension-collector"

# 1. Local OCI registry for immutable digest resolution. Owned by this harness;
#    the host port comes from CE_OCI_REGISTRY so it never collides with another
#    registry or a host service.
if ! docker inspect "${registry_name}" >/dev/null 2>&1; then
	log "starting local registry ${registry_name} on ${registry}"
	docker run -d --restart=no -p "${registry_host_port}:5000" --name "${registry_name}" registry:2 >/dev/null \
		|| die "could not start registry on host port ${registry_host_port}; set CE_OCI_REGISTRY=localhost:<free-port>"
	sleep 2
elif [[ "$(docker inspect -f '{{.State.Running}}' "${registry_name}")" != "true" ]]; then
	docker start "${registry_name}" >/dev/null
	sleep 2
fi

# 2. Build + push the artifact image; capture the immutable digest.
log "building + pushing Scorecard artifact image"
docker build -f examples/collector-extensions/scorecard/Dockerfile.oci \
	-t "${registry}/${repo}:proof" . >/dev/null
push_digest="$(docker push "${registry}/${repo}:proof" 2>&1 | rg -o 'sha256:[0-9a-f]{64}' | tail -1)"
[[ -n "${push_digest}" ]] || die "failed to resolve pushed image digest"
oci_image="${registry}/${repo}@${push_digest}"
export ESHU_SCORECARD_OCI_IMAGE="${oci_image}"
log "artifact digest-pinned ref: ${oci_image}"

# 3. Build the eshu base image from the current checkout. The OCI worker image
#    (oci.worker.Dockerfile) layers on eshu:local, so a STALE base would ship an
#    old eshu-collector-component-extension binary that predates the OCI adapter
#    and reject `adapter: oci` at startup. Rebuild by default; set
#    CE_OCI_SKIP_BASE_BUILD=1 only when eshu:local is known current.
if [[ "${CE_OCI_SKIP_BASE_BUILD:-0}" != "1" ]]; then
	log "building eshu:local base image from current checkout"
	docker build -t eshu:local -f Dockerfile . >/dev/null
elif ! docker image inspect eshu:local >/dev/null 2>&1; then
	docker build -t eshu:local -f Dockerfile . >/dev/null
fi
log "bringing up OCI component-extension stack (project ${project})"
# shellcheck disable=SC2086
compose up -d --build ${proof_services}

# 4. Wait for the Scorecard work item to reach terminal success.
log "waiting for Scorecard component work item to complete"
deadline=$(( $(date +%s) + 240 ))
terminal=false
while [[ "$(date +%s)" -lt "${deadline}" ]]; do
	if ! docker inspect "${postgres}" >/dev/null 2>&1; then
		sleep 3; continue
	fi
	rows="$(docker exec "${postgres}" psql -U eshu -d eshu -tAF'|' -c \
		"SELECT status, count(*) FROM workflow_work_items WHERE collector_kind='scorecard' GROUP BY status;" 2>/dev/null || true)"
	if printf '%s' "${rows}" | rg -q '^(failed|dead_letter)\|'; then
		docker exec "${postgres}" psql -U eshu -d eshu -c \
			"SELECT work_item_id,status,last_error FROM workflow_work_items WHERE collector_kind='scorecard';" || true
		die "scorecard work item entered failed/dead_letter state"
	fi
	if printf '%s' "${rows}" | rg -q '^completed\|[1-9]'; then
		terminal=true; break
	fi
	sleep 5
done
[[ "${terminal}" == true ]] || { compose logs --tail=60 component-extension-collector || true; die "timed out waiting for terminal success"; }
log "scorecard work item reached terminal success"

# 5a. Shared inventory/workflow/facts capture + base verify (reused script).
CE_PROOF_PROJECT="${project}" \
	"${repo_root}/scripts/run-remote-e2e-component-extension.sh" --artifacts "${artifacts_dir}"

# 5b. OCI provenance: adapter, digest-pinned artifact, and run identity. Bounded,
#     port-only telemetry handle and a localhost registry ref so no host secret
#     or raw IP reaches the artifact.
eshu_commit="$(git -C "${repo_root}" rev-parse HEAD 2>/dev/null || echo unknown)"
backend="$(docker exec "${collector}" sh -c 'printf %s "${ESHU_GRAPH_BACKEND:-nornicdb}"' 2>/dev/null || echo nornicdb)"
metrics_port="${ESHU_COMPONENT_EXTENSION_COLLECTOR_METRICS_PORT:-19470}"
cat >"${artifacts_dir}/provenance-oci.json" <<JSON
{
  "adapter": "oci",
  "component_id": "${component_id}",
  "oci_image": "${oci_image}",
  "eshu_commit": "${eshu_commit}",
  "core_version": "dev",
  "sdk_protocol": "collector-sdk/v1alpha1",
  "backend": "${backend}",
  "queue_terminal_state": "completed",
  "metrics_handle": "http://localhost:${metrics_port}/metrics"
}
JSON

# 6. OCI verifier (re-runs the shared invariants + OCI-specific checks).
"${repo_root}/scripts/verify-remote-e2e-component-extension-oci.sh" --artifacts "${artifacts_dir}"

log "OCI proof complete; artifacts in ${artifacts_dir}"
if [[ "${keep_up}" == false ]]; then
	log "tearing down stack (pass --keep-up to inspect); registry ${registry_name} left running"
	# Project-scoped teardown (no profile filter) so every service — including the
	# default-profile dependencies pulled in by depends_on — is removed.
	docker compose -p "${project}" down -v --remove-orphans >/dev/null 2>&1 || true
else
	log "stack left up: docker compose -p ${project} down -v --remove-orphans   to tear down"
fi
