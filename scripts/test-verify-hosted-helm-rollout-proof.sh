#!/usr/bin/env bash
# Focused tests for the hosted Helm install, upgrade, and rollback proof gate.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
gate="${repo_root}/scripts/verify-hosted-helm-rollout-proof.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

install_fake_tools() {
    local dir="$1"
    mkdir -p "${dir}/_bin"

    cat >"${dir}/_bin/helm" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
cmd="$1"
shift
case "${cmd}" in
  lint)
    printf 'lint ok\n'
    ;;
  template)
    cat <<'YAML'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: eshu-api
spec:
  template:
    spec:
      containers:
        - name: api
          image: "ghcr.io/eshu-hq/eshu:v9.9.9"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: eshu-mcp-server
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: eshu
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: eshu-resolution-engine
---
apiVersion: batch/v1
kind: Job
metadata:
  name: eshu-schema-bootstrap
  annotations:
    "helm.sh/hook": pre-install,pre-upgrade
YAML
    ;;
  upgrade)
    printf 'DRY RUN\n'
    ;;
  *)
    printf 'unexpected helm command: %s\n' "${cmd}" >&2
    exit 1
    ;;
esac
SH
    chmod +x "${dir}/_bin/helm"

    cat >"${dir}/_bin/curl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
url="${@: -1}"
case "${url}" in
  */healthz|*/readyz)
    printf 'ok\n'
    ;;
  */admin/status*)
    cat <<'JSON'
{"queue":{"outstanding":0,"pending":0,"in_flight":0,"retrying":0,"failed":0,"dead_letter":0},"generation":42}
JSON
    ;;
  */api/v0/index-status)
    cat <<'JSON'
{"status":"healthy","truth":{"level":"graph"},"freshness":{"state":"current"}}
JSON
    ;;
  *)
    printf 'unexpected curl url: %s\n' "${url}" >&2
    exit 1
    ;;
esac
SH
    chmod +x "${dir}/_bin/curl"

    cat >"${dir}/_bin/kubectl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  "-n eshu rollout status deployment/eshu-api --timeout=120s" | \
  "-n eshu rollout status deployment/eshu-mcp-server --timeout=120s" | \
  "-n eshu rollout status statefulset/eshu --timeout=120s" | \
  "-n eshu rollout status deployment/eshu-resolution-engine --timeout=120s")
    printf 'rolled out\n'
    ;;
  "-n eshu get job/eshu-schema-bootstrap -o json")
    cat <<'JSON'
{"status":{"succeeded":1,"failed":0,"conditions":[{"type":"Complete","status":"True"}]}}
JSON
    ;;
  *)
    printf 'unexpected kubectl args: %s\n' "$*" >&2
    exit 1
    ;;
esac
SH
    chmod +x "${dir}/_bin/kubectl"
}

write_upgrade_state() {
    local file="$1"
    cat >"${file}" <<'JSON'
{
  "durable_state": "postgres-backup-verified",
  "queue_state": "pre-and-post-queue-zero-captured",
  "graph_rebuild": "rebuild-from-postgres-facts-not-required",
  "preserved_volumes": "ingester-workspace-pvc-preserved"
}
JSON
}

write_rollback_state() {
    local file="$1"
    cat >"${file}" <<'JSON'
{
  "helm_rollback": "helm rollback eshu previous-revision",
  "postgres_restore": "restore only if older image cannot read durable state",
  "graph_rebuild": "recreate graph and rerun bootstrap when graph volume is lost",
  "decision_point": "separate chart rollback from data restore"
}
JSON
}

repo="${tmp_root}/repo"
mkdir -p "${repo}"
cp -R "${repo_root}/deploy" "${repo}/deploy"
install_fake_tools "${repo}"

out_dir="${repo}/_proof"
if ! PATH="${repo}/_bin:${PATH}" "${gate}" \
    --out-dir "${out_dir}" \
    --chart "${repo}/deploy/helm/eshu" \
    --release eshu \
    --namespace eshu \
    --api-base-url http://api.example.invalid \
    --mcp-base-url http://mcp.example.invalid \
    --first-query-path /api/v0/index-status \
    --live-cluster \
    >"${repo}/gate.out" 2>"${repo}/gate.err"; then
    sed -n '1,120p' "${repo}/gate.err" >&2
    exit 1
fi

artifact="${out_dir}/hosted-helm-rollout-proof.json"
[ -f "${artifact}" ] || {
    sed -n '1,120p' "${repo}/gate.out" >&2
    sed -n '1,120p' "${repo}/gate.err" >&2
    exit 1
}
jq -e '.chart.version == "0.0.3-pre-release-7"' "${artifact}" >/dev/null \
    || { printf 'chart version was not captured\n' >&2; exit 1; }
jq -e '.chart.app_version == "v0.0.3-pre-release-4"' "${artifact}" >/dev/null \
    || { printf 'app version was not captured\n' >&2; exit 1; }
jq -e '.install.helm_lint == "pass" and .install.helm_dry_run == "pass"' "${artifact}" >/dev/null \
    || { printf 'install lint and dry-run status were not captured\n' >&2; exit 1; }
jq -e '.install.schema_bootstrap.rendered == true and .install.schema_bootstrap.helm_hook == true' "${artifact}" >/dev/null \
    || { printf 'schema bootstrap evidence was not captured\n' >&2; exit 1; }
jq -e '.install.core_rollout_status == "pass" and .install.schema_bootstrap.outcome == "complete"' "${artifact}" >/dev/null \
    || { printf 'live rollout and bootstrap outcome were not captured\n' >&2; exit 1; }
jq -e '.install.required_workloads_present == true and (.install.rendered_workloads | length) >= 5' "${artifact}" >/dev/null \
    || { printf 'rendered workload set was not captured\n' >&2; exit 1; }
jq -e '.readback.api_health == "pass" and .readback.mcp_health == "pass"' "${artifact}" >/dev/null \
    || { printf 'API/MCP health readback was not captured\n' >&2; exit 1; }
jq -e '.readback.queue_state.retrying == 0 and .readback.queue_state.dead_letter == 0' "${artifact}" >/dev/null \
    || { printf 'queue readback was not captured\n' >&2; exit 1; }
jq -e '.readback.first_query.status == "captured"' "${artifact}" >/dev/null \
    || { printf 'first query result was not captured\n' >&2; exit 1; }

bad_upgrade="${repo}/bad-upgrade.json"
cat >"${bad_upgrade}" <<'JSON'
{"durable_state":"postgres-backup-verified","queue_state":"queue-zero"}
JSON
if PATH="${repo}/_bin:${PATH}" "${gate}" --mode upgrade --chart "${repo}/deploy/helm/eshu" --upgrade-state "${bad_upgrade}" >"${repo}/bad-upgrade.out" 2>"${repo}/bad-upgrade.err"; then
    printf 'expected upgrade proof to fail without graph_rebuild declaration\n' >&2
    exit 1
fi
rg --quiet 'upgrade proof requires' "${repo}/bad-upgrade.err" \
    || { printf 'upgrade failure did not explain missing declarations\n' >&2; exit 1; }

upgrade_state="${repo}/upgrade.json"
write_upgrade_state "${upgrade_state}"
PATH="${repo}/_bin:${PATH}" "${gate}" \
    --mode upgrade \
    --chart "${repo}/deploy/helm/eshu" \
    --out-dir "${repo}/_upgrade" \
    --upgrade-state "${upgrade_state}" >/dev/null
jq -e '.upgrade.status == "pass" and .upgrade.values_digest != ""' "${repo}/_upgrade/hosted-helm-rollout-proof.json" >/dev/null \
    || { printf 'upgrade declaration proof was not captured\n' >&2; exit 1; }

bad_rollback="${repo}/bad-rollback.json"
cat >"${bad_rollback}" <<'JSON'
{"helm_rollback":"helm rollback eshu previous-revision"}
JSON
if PATH="${repo}/_bin:${PATH}" "${gate}" --mode rollback --chart "${repo}/deploy/helm/eshu" --rollback-state "${bad_rollback}" >"${repo}/bad-rollback.out" 2>"${repo}/bad-rollback.err"; then
    printf 'expected rollback proof to fail without restore and graph rebuild declarations\n' >&2
    exit 1
fi
rg --quiet 'rollback proof requires' "${repo}/bad-rollback.err" \
    || { printf 'rollback failure did not explain missing declarations\n' >&2; exit 1; }

rollback_state="${repo}/rollback.json"
write_rollback_state "${rollback_state}"
PATH="${repo}/_bin:${PATH}" "${gate}" \
    --mode rollback \
    --chart "${repo}/deploy/helm/eshu" \
    --out-dir "${repo}/_rollback" \
    --rollback-state "${rollback_state}" >/dev/null
jq -e '.rollback.status == "pass" and .rollback.postgres_restore != .rollback.helm_rollback' "${repo}/_rollback/hosted-helm-rollout-proof.json" >/dev/null \
    || { printf 'rollback separation proof was not captured\n' >&2; exit 1; }

printf 'hosted Helm rollout proof tests passed\n'
