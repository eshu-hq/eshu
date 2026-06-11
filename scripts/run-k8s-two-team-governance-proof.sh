#!/usr/bin/env bash
set -euo pipefail

# Live Kubernetes/Helm driver for the two-team hosted governance cross-scope
# denial proof (#1910). It is the cluster sibling of
# scripts/run-two-team-governance-proof.sh (Compose). It:
#
#   - installs the Eshu Helm chart into a UNIQUE namespace on the current
#     kubectl context (designed for a single-node local cluster such as
#     OrbStack) with bundled NornicDB, a minimal in-namespace Postgres, restricted
#     NetworkPolicy egress, and the scoped-token Secret mounted into API + MCP,
#   - seeds two repositories with a one-shot bootstrap-index Job (filesystem
#     fixtures baked into a seed image FROM the chart image),
#   - asserts through the LIVE in-cluster API and MCP (via kubectl port-forward)
#     that team-A reads only its own repo and not team-B's (and symmetrically),
#     the out-of-grant single-repo selector fails closed (403), unauthenticated
#     reads are rejected (401), and the admin token sees every repo,
#   - records that the rendered NetworkPolicies are actually applied in-cluster,
#   - shapes the results into normalized proof artifacts (counts + HTTP states
#     only, never bodies or token material) and runs the verifier, and
#   - uninstalls the release and deletes the namespace on exit (success OR
#     failure) so it never leaves cluster cruft.
#
# Usage (from repo root):
#   scripts/run-k8s-two-team-governance-proof.sh [--artifacts <dir>] [--keep-up]
#       [--image-repo REPO] [--image-tag TAG] [--namespace NS] [--release NAME]
#       [--no-build]
#
# Environment overrides mirror the flags (K8S_GOV_*). The chart image must
# already exist on the cluster's Docker daemon (OrbStack shares the daemon);
# without --no-build the driver builds eshu:local and a seed image from it.

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || (cd "$(dirname "$0")/.." && pwd))"

image_repo="${K8S_GOV_IMAGE_REPO:-eshu}"
image_tag="${K8S_GOV_IMAGE_TAG:-local}"
seed_image="${K8S_GOV_SEED_IMAGE:-eshu-gov-seed:local}"
release="${K8S_GOV_RELEASE:-eshu}"
ns_suffix="$(LC_ALL=C tr -dc 'a-z0-9' </dev/urandom 2>/dev/null | head -c 6 || echo "$$")"
namespace="${K8S_GOV_NAMESPACE:-eshu-gov-proof-${ns_suffix}}"
artifacts_dir=""
keep_up=false
do_build=true
pg_password="gov-proof-pg-$(date +%s)"
admin_token="gov-proof-admin-$(date +%s)"
team_a_token="gov-proof-team-a-$(date +%s)"
team_b_token="gov-proof-team-b-$(date +%s)"
api_local_port="${K8S_GOV_API_PORT:-38080}"
mcp_local_port="${K8S_GOV_MCP_PORT:-38081}"

die() {
	printf 'run-k8s-two-team-governance-proof: %s\n' "$*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--artifacts) artifacts_dir="${2:-}"; shift 2 ;;
		--keep-up) keep_up=true; shift ;;
		--image-repo) image_repo="${2:-}"; shift 2 ;;
		--image-tag) image_tag="${2:-}"; shift 2 ;;
		--namespace) namespace="${2:-}"; shift 2 ;;
		--release) release="${2:-}"; shift 2 ;;
		--no-build) do_build=false; shift ;;
		-h|--help) sed -n '3,40p' "$0"; exit 0 ;;
		*) die "unknown option: $1" ;;
	esac
done

command -v kubectl >/dev/null 2>&1 || die "kubectl is required"
command -v helm >/dev/null 2>&1 || die "helm is required"
command -v docker >/dev/null 2>&1 || die "docker is required"
command -v curl >/dev/null 2>&1 || die "curl is required"
command -v rg >/dev/null 2>&1 || die "rg is required"
if command -v shasum >/dev/null 2>&1; then
	sha256_cmd="shasum -a 256"
elif command -v sha256sum >/dev/null 2>&1; then
	sha256_cmd="sha256sum"
else
	die "shasum or sha256sum is required"
fi

chart="${repo_root}/deploy/helm/eshu"
values_file="${chart}/ci/governance-two-team-k8s.values.yaml"
[[ -d "${chart}" ]] || die "chart not found: ${chart}"
[[ -f "${values_file}" ]] || die "values file not found: ${values_file}"

# Cluster manifest emitters (Postgres + seed Job) live in a sibling file to keep
# this driver under the repo file-size cap.
manifests_lib="$(dirname "$0")/k8s-two-team-governance-manifests.sh"
[[ -f "${manifests_lib}" ]] || die "manifests helper not found: ${manifests_lib}"
# shellcheck source=scripts/k8s-two-team-governance-manifests.sh
. "${manifests_lib}"

if [[ -z "${artifacts_dir}" ]]; then
	artifacts_dir="$(mktemp -d "${TMPDIR:-/tmp}/k8s-gov-artifacts.XXXXXX")"
fi
mkdir -p "${artifacts_dir}"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/k8s-gov-work.XXXXXX")"

context="$(kubectl config current-context 2>/dev/null || echo unknown)"
pf_api_pid=""
pf_mcp_pid=""

cleanup() {
	[[ -n "${pf_api_pid}" ]] && kill "${pf_api_pid}" >/dev/null 2>&1 || true
	[[ -n "${pf_mcp_pid}" ]] && kill "${pf_mcp_pid}" >/dev/null 2>&1 || true
	if [[ "${keep_up}" != true ]]; then
		printf '==> cleanup: helm uninstall + delete namespace %s\n' "${namespace}" >&2
		helm uninstall "${release}" -n "${namespace}" >/dev/null 2>&1 || true
		kubectl delete namespace "${namespace}" --wait=false >/dev/null 2>&1 || true
	fi
	rm -rf "${work_dir}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

sha256_hex() {
	printf '%s' "$1" | ${sha256_cmd} | rg -o '^[0-9a-f]{64}' | head -1
}

kc() { kubectl -n "${namespace}" "$@"; }

# ---------------------------------------------------------------------------
# Phase 0: build the chart image and a seed image (fixtures baked in) so the
# single-node cluster can pull locally (imagePullPolicy IfNotPresent / Never).
# ---------------------------------------------------------------------------
if [[ "${do_build}" == true ]]; then
	printf '==> building chart image %s:%s\n' "${image_repo}" "${image_tag}"
	docker build -t "${image_repo}:${image_tag}" -f "${repo_root}/Dockerfile" "${repo_root}" >/dev/null
fi

# Two generic fixture repositories (no tenant/provider/private data).
seed_ctx="${work_dir}/seed"
mkdir -p "${seed_ctx}/fixtures/repo-team-alpha" "${seed_ctx}/fixtures/repo-team-beta"
cat >"${seed_ctx}/fixtures/repo-team-alpha/main.go" <<'GO'
package main

import "fmt"

func main() { fmt.Println("alpha service") }
GO
cat >"${seed_ctx}/fixtures/repo-team-beta/main.go" <<'GO'
package main

import "fmt"

func main() { fmt.Println("beta service") }
GO
cat >"${seed_ctx}/Dockerfile" <<DOCKER
FROM ${image_repo}:${image_tag}
COPY fixtures /fixtures
DOCKER
printf '==> building seed image %s (filesystem fixtures)\n' "${seed_image}"
docker build -t "${seed_image}" "${seed_ctx}" >/dev/null

# ---------------------------------------------------------------------------
# Phase 1: namespace + minimal Postgres + apiAuth + DSN secrets.
# ---------------------------------------------------------------------------
printf '==> creating namespace %s on context %s\n' "${namespace}" "${context}"
kubectl create namespace "${namespace}" >/dev/null

pg_dsn="postgresql://eshu:${pg_password}@postgres:5432/eshu?sslmode=disable"
kc create secret generic eshu-content-store --from-literal=dsn="${pg_dsn}" >/dev/null
kc create secret generic eshu-api-auth --from-literal=api-key="${admin_token}" >/dev/null

printf '==> deploying in-namespace Postgres\n'
postgres_manifest | kc apply -f - >/dev/null

printf '==> waiting for Postgres Ready\n'
kc rollout status deployment/postgres --timeout=180s

# ---------------------------------------------------------------------------
# Phase 2: helm install (NO scoped-token registry yet) so the admin token can
# enumerate both seeded repositories. The scoped-tokens Secret is optional in the
# values file, so the API/MCP start cleanly before the registry exists.
# ---------------------------------------------------------------------------
helm_common=(
	"${chart}"
	--namespace "${namespace}"
	-f "${values_file}"
	--set "image.repository=${image_repo}"
	--set "image.tag=${image_tag}"
	--set "image.pullPolicy=IfNotPresent"
	--set "contentStore.secretName=eshu-content-store"
	--set "apiAuth.secretName=eshu-api-auth"
	--set "apiAuth.key=api-key"
)

# Install WITHOUT --wait: the bundled NornicDB needs a few seconds to accept
# Bolt connections, and the schema-bootstrap Job + API/MCP may briefly fail
# (connection-refused / CrashLoopBackOff) until it does. The Job's raised
# backoffLimit and the deployments' restart loops ride through that warmup; the
# explicit waits below assert the steady state instead of failing on the race.
printf '==> helm install %s into %s\n' "${release}" "${namespace}"
helm install "${release}" "${helm_common[@]}" >/dev/null \
	|| { kc get pods >&2 || true; die "helm install failed"; }

printf '==> waiting for graph backend (NornicDB) Ready\n'
kc rollout status deployment/eshu-nornicdb --timeout=240s
printf '==> waiting for schema bootstrap Job to complete (rides NornicDB warmup)\n'
if ! kc wait --for=condition=complete job/eshu-schema-bootstrap --timeout=300s; then
	kc logs -l app.kubernetes.io/component=schema-bootstrap --tail=40 >&2 || true
	die "schema-bootstrap job did not complete"
fi
printf '==> waiting for API + MCP Ready\n'
kc rollout status deployment/eshu-api --timeout=300s
kc rollout status deployment/eshu-mcp-server --timeout=300s

# ---------------------------------------------------------------------------
# Phase 3: seed two repositories with a one-shot bootstrap-index Job.
# ---------------------------------------------------------------------------
printf '==> seeding two repositories via bootstrap-index Job\n'
seed_job_manifest | kc apply -f - >/dev/null
kc wait --for=condition=complete job/gov-seed --timeout=300s \
	|| { kc logs job/gov-seed --tail=80 >&2 || true; die "seed job did not complete"; }

# ---------------------------------------------------------------------------
# Phase 4: port-forward the in-cluster API + MCP services and read back.
# ---------------------------------------------------------------------------
api_base="http://127.0.0.1:${api_local_port}"
mcp_base="http://127.0.0.1:${mcp_local_port}"

# start_pf launches a kubectl port-forward in the CURRENT shell (not a command
# substitution, which would background the process inside a subshell that then
# exits and kills it) and stores the PID in the named variable given as $5. It
# polls until the local port answers, retrying through the brief window before
# kubectl establishes the tunnel.
start_pf() {
	local svc="$1" local_port="$2" remote_port="$3" base="$4" pid_var="$5"
	kc port-forward "svc/${svc}" "${local_port}:${remote_port}" >/dev/null 2>&1 &
	local pid=$!
	printf -v "${pid_var}" '%s' "${pid}"
	local tries=30 code
	while [[ "${tries}" -gt 0 ]]; do
		kill -0 "${pid}" 2>/dev/null || die "port-forward for ${svc} died"
		code="$(curl -s -o /dev/null -w '%{http_code}' "${base}/" 2>/dev/null || echo 000)"
		[[ "${code}" != "000" ]] && return 0
		tries=$((tries - 1)); sleep 1
	done
	return 0
}

# The chart fullname (and thus the API Service name) is the release name when it
# already contains the chart name "eshu", else "<release>-eshu". The API Service
# is the bare fullname; the MCP Service appends "-mcp-server".
if [[ "${release}" == *eshu* ]]; then
	fullname="${release}"
else
	fullname="${release}-eshu"
fi
api_svc="${fullname}"
mcp_svc="${fullname}-mcp-server"

printf '==> port-forwarding API (svc/%s) + MCP (svc/%s) services\n' "${api_svc}" "${mcp_svc}"
start_pf "${api_svc}" "${api_local_port}" 8080 "${api_base}" pf_api_pid
start_pf "${mcp_svc}" "${mcp_local_port}" 8080 "${mcp_base}" pf_mcp_pid

wait_for() {
	local url="$1" want="$2" tries=40 code
	while [[ "${tries}" -gt 0 ]]; do
		code="$(curl -s -o /dev/null -w '%{http_code}' "${url}" || echo 000)"
		[[ "${code}" == "${want}" ]] && return 0
		tries=$((tries - 1)); sleep 3
	done
	return 1
}
wait_for "${api_base}/health" "200" || die "API never healthy via port-forward"
wait_for "${mcp_base}/healthz" "200" || die "MCP never healthy via port-forward"

api_repo_ids() {
	curl -s -H "Authorization: Bearer $1" "${api_base}/api/v0/repositories" \
		| rg -o 'repository:[A-Za-z0-9_]+' | sort -u
}
mcp_repo_ids() {
	curl -s -H "Authorization: Bearer $1" -H 'Content-Type: application/json' \
		-X POST "${mcp_base}/mcp/message" \
		--data '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_indexed_repositories","arguments":{}}}' \
		| rg -o 'repository:[A-Za-z0-9_]+' | sort -u
}
http_status() { curl -s -o /dev/null -w '%{http_code}' "$@"; }
contains_id() { rg -q -F -x "$1"; }

printf '==> admin (all-scopes) enumeration\n'
admin_ids="$(api_repo_ids "${admin_token}")"
admin_count="$(printf '%s\n' "${admin_ids}" | rg -c '^.+$' || echo 0)"
[[ "${admin_count}" -ge 2 ]] || { kc logs deploy/eshu-api --tail=60 >&2 || true; die "admin enumerated ${admin_count} repos; need >=2"; }
repo_a="$(printf '%s\n' "${admin_ids}" | sed -n '1p')"
repo_b="$(printf '%s\n' "${admin_ids}" | sed -n '2p')"
[[ -n "${repo_a}" && -n "${repo_b}" && "${repo_a}" != "${repo_b}" ]] || die "could not pick two distinct repos"
printf '    team-A repo: %s\n    team-B repo: %s\n' "${repo_a}" "${repo_b}"
cat >"${artifacts_dir}/admin.json" <<JSON
{
  "tenant": "admin",
  "repository_count": ${admin_count},
  "team_a_repo": "${repo_a}",
  "team_b_repo": "${repo_b}"
}
JSON

# ---------------------------------------------------------------------------
# Phase 5: write the scoped-token registry Secret and restart API + MCP so the
# registry is mounted and enforced.
# ---------------------------------------------------------------------------
tokens_file="${work_dir}/scoped-tokens.json"
cat >"${tokens_file}" <<JSON
{
  "version": 1,
  "tokens": [
    {"token_sha256": "$(sha256_hex "${admin_token}")", "tenant_id": "tenant-admin", "workspace_id": "ws-admin", "all_scopes": true},
    {"token_sha256": "$(sha256_hex "${team_a_token}")", "tenant_id": "tenant-a", "workspace_id": "ws-a", "allowed_repository_ids": ["${repo_a}"]},
    {"token_sha256": "$(sha256_hex "${team_b_token}")", "tenant_id": "tenant-b", "workspace_id": "ws-b", "allowed_repository_ids": ["${repo_b}"]}
  ]
}
JSON

printf '==> creating scoped-token registry Secret and enabling enforcement via helm upgrade\n'
kc create secret generic eshu-scoped-tokens --from-file=scoped-tokens.json="${tokens_file}" >/dev/null
# helm upgrade sets ESHU_SCOPED_TOKENS_FILE on API + MCP. The optional Secret
# volume is already mounted, so this flips both surfaces into scoped-token
# enforcement and rolls the pods. The API/MCP now fail closed on a malformed
# registry and enforce per-tenant grants on every scoped-read route.
helm upgrade "${release}" "${chart}" \
	--namespace "${namespace}" \
	--reuse-values \
	--set "api.env.ESHU_SCOPED_TOKENS_FILE=/run/secrets/eshu-scoped-tokens/scoped-tokens.json" \
	--set "mcpServer.env.ESHU_SCOPED_TOKENS_FILE=/run/secrets/eshu-scoped-tokens/scoped-tokens.json" \
	>/dev/null || die "helm upgrade (enable scoped tokens) failed"
kc rollout status deployment/eshu-api --timeout=240s
kc rollout status deployment/eshu-mcp-server --timeout=240s

# The helm upgrade rolled API + MCP, so the old port-forwards point at gone pods.
# Reconnect on FRESH local ports: reusing the just-killed ports can fail to bind
# while they sit in TIME_WAIT, which would leave no working tunnel.
kill "${pf_api_pid}" "${pf_mcp_pid}" >/dev/null 2>&1 || true
api_local_port=$((api_local_port + 100))
mcp_local_port=$((mcp_local_port + 100))
api_base="http://127.0.0.1:${api_local_port}"
mcp_base="http://127.0.0.1:${mcp_local_port}"
start_pf "${api_svc}" "${api_local_port}" 8080 "${api_base}" pf_api_pid
start_pf "${mcp_svc}" "${mcp_local_port}" 8080 "${mcp_base}" pf_mcp_pid
wait_for "${api_base}/health" "200" || die "API never healthy after registry mount"
wait_for "${mcp_base}/healthz" "200" || die "MCP never healthy after registry mount"

capture_team() {
	local file="$1" token="$2" own="$3" other="$4"
	local api_ids mcp_ids api_count mcp_count
	api_ids="$(api_repo_ids "${token}")"
	mcp_ids="$(mcp_repo_ids "${token}")"
	api_count="$(printf '%s\n' "${api_ids}" | rg -c '^.+$' || echo 0)"
	mcp_count="$(printf '%s\n' "${mcp_ids}" | rg -c '^.+$' || echo 0)"
	local api_own=false api_other=false mcp_own=false mcp_other=false
	printf '%s\n' "${api_ids}" | contains_id "${own}"   && api_own=true   || true
	printf '%s\n' "${api_ids}" | contains_id "${other}" && api_other=true || true
	printf '%s\n' "${mcp_ids}" | contains_id "${own}"   && mcp_own=true   || true
	printf '%s\n' "${mcp_ids}" | contains_id "${other}" && mcp_other=true || true
	local api_sel mcp_sel
	api_sel="$(http_status -H "Authorization: Bearer ${token}" "${api_base}/api/v0/repositories/${other}/context")"
	mcp_sel="$(http_status -H "Authorization: Bearer ${token}" "${mcp_base}/api/v0/repositories/${other}/context")"
	cat >"${file}" <<JSON
{
  "own_repo": "${own}",
  "other_repo": "${other}",
  "api_repository_count": ${api_count},
  "api_own_repo_present": "${api_own}",
  "api_other_repo_present": "${api_other}",
  "api_other_repo_selector_status": ${api_sel},
  "mcp_repository_count": ${mcp_count},
  "mcp_own_repo_present": "${mcp_own}",
  "mcp_other_repo_present": "${mcp_other}",
  "mcp_other_repo_selector_status": ${mcp_sel}
}
JSON
}

printf '==> capturing team-A scoped reads\n'
capture_team "${artifacts_dir}/team-a.json" "${team_a_token}" "${repo_a}" "${repo_b}"
printf '==> capturing team-B scoped reads\n'
capture_team "${artifacts_dir}/team-b.json" "${team_b_token}" "${repo_b}" "${repo_a}"

printf '==> capturing unauthenticated rejection states\n'
cat >"${artifacts_dir}/unauth.json" <<JSON
{
  "api_status": $(http_status "${api_base}/api/v0/repositories"),
  "mcp_status": $(http_status "${mcp_base}/api/v0/repositories")
}
JSON

# ---------------------------------------------------------------------------
# Phase 6: in-cluster NetworkPolicy applied-state proof (counts only).
# ---------------------------------------------------------------------------
np_total="$(kc get networkpolicy -o name 2>/dev/null | rg -c '^.+$' || echo 0)"
np_api="$(kc get networkpolicy eshu-api -o name >/dev/null 2>&1 && echo true || echo false)"
np_mcp="$(kc get networkpolicy eshu-mcp-server -o name >/dev/null 2>&1 && echo true || echo false)"
cat >"${artifacts_dir}/network-policy.json" <<JSON
{
  "applied_count": ${np_total},
  "api_policy_applied": "${np_api}",
  "mcp_policy_applied": "${np_mcp}",
  "egress_mode": "restricted"
}
JSON

# ---------------------------------------------------------------------------
# Provenance (low-cardinality, port-only handle; no token/host/IP).
# ---------------------------------------------------------------------------
eshu_commit="$(git -C "${repo_root}" rev-parse --short HEAD 2>/dev/null || echo unknown)"
# Capture the LIVE cluster's version. The node kubelet version is the
# authoritative server version (kubectl version's first gitVersion is the client,
# which can skew from the cluster). Strip the vendor suffix to a clean canary.
k8s_version="$(kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.kubeletVersion}' 2>/dev/null | rg -o '^v[0-9][^[:space:]]*' | head -1 || echo unknown)"
[[ -n "${k8s_version}" ]] || k8s_version="unknown"
cat >"${artifacts_dir}/provenance.json" <<JSON
{
  "eshu_commit": "${eshu_commit}",
  "backend": "nornicdb",
  "platform": "kubernetes",
  "kubernetes_version": "${k8s_version}",
  "registry_token_count": 3,
  "metrics_handle": ":9464/metrics",
  "counts_and_states_only": true
}
JSON

printf 'captured live K8s proof artifacts to %s\n' "${artifacts_dir}"
"${repo_root}/scripts/verify-k8s-two-team-governance-proof.sh" --artifacts "${artifacts_dir}"
