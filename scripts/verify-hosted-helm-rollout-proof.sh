#!/usr/bin/env bash
# Build a public-safe hosted Helm install, upgrade, and rollback proof artifact.

set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
chart="${repo_root}/deploy/helm/eshu"
release="eshu"
namespace="eshu"
mode="install"
out_dir="${repo_root}/hosted-helm-rollout-proof"
api_base_url=""
mcp_base_url=""
first_query_path="/api/v0/index-status"
api_token_env=""
mcp_token_env=""
upgrade_state=""
rollback_state=""
live_cluster=false
values_files=()

usage() {
    # Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
    # the entire heredoc body to a pipe before forking the reader, and
    # macOS's 512-byte pipe buffer deadlocks on any body over that size
    # (#5074).
    cat "${repo_root}/scripts/lib/verify-hosted-helm-rollout-proof-usage.txt"
}

die() {
    printf '%s\n' "$*" >&2
    exit 1
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --mode) mode="${2:?missing mode}"; shift 2 ;;
        --out-dir) out_dir="${2:?missing out dir}"; shift 2 ;;
        --chart) chart="${2:?missing chart path}"; shift 2 ;;
        --release) release="${2:?missing release}"; shift 2 ;;
        --namespace) namespace="${2:?missing namespace}"; shift 2 ;;
        -f|--values) values_files+=("${2:?missing values file}"); shift 2 ;;
        --api-base-url) api_base_url="${2:?missing API base URL}"; shift 2 ;;
        --mcp-base-url) mcp_base_url="${2:?missing MCP base URL}"; shift 2 ;;
        --first-query-path) first_query_path="${2:?missing first query path}"; shift 2 ;;
        --api-token-env) api_token_env="${2:?missing API token env}"; shift 2 ;;
        --mcp-token-env) mcp_token_env="${2:?missing MCP token env}"; shift 2 ;;
        --upgrade-state) upgrade_state="${2:?missing upgrade state file}"; shift 2 ;;
        --rollback-state) rollback_state="${2:?missing rollback state file}"; shift 2 ;;
        --live-cluster) live_cluster=true; shift ;;
        -h|--help) usage; exit 0 ;;
        *) die "unknown argument: $1" ;;
    esac
done

case "${mode}" in
    install|upgrade|rollback|all) ;;
    *) die "--mode must be install, upgrade, rollback, or all" ;;
esac

command -v helm >/dev/null 2>&1 || die "helm is required"
command -v jq >/dev/null 2>&1 || die "jq is required"
[ -d "${chart}" ] || die "chart directory does not exist: ${chart}"

set +u
values_file_count="${#values_files[@]}"
set -u

set +u
for file in "${values_files[@]}"; do
    set -u
    [ -f "${file}" ] || die "values file does not exist: ${file}"
done
set -u

mkdir -p "${out_dir}"
scratch="$(mktemp -d)"
cleanup() {
    rm -rf "${scratch}" 2>/dev/null || true
}
trap cleanup EXIT

helm_value_args=()
set +u
for file in "${values_files[@]}"; do
    set -u
    helm_value_args+=("-f" "${file}")
done
set -u

chart_field() {
    local field="$1"
    awk -v key="${field}:" '
        $1 == key {
            value = $2
            gsub(/^"/, "", value)
            gsub(/"$/, "", value)
            print value
            exit
        }
    ' "${chart}/Chart.yaml"
}

image_value() {
    local field="$1"
    awk -v key="${field}:" '
        $1 == "image:" { in_image = 1; next }
        in_image && $0 !~ /^[[:space:]]/ { in_image = 0 }
        in_image && $1 == key {
            value = $2
            gsub(/^"/, "", value)
            gsub(/"$/, "", value)
            print value
            exit
        }
    ' "${chart}/values.yaml"
}

digest_files() {
    local digest_input="${scratch}/digest-input.txt"
    : >"${digest_input}"
    if [ "${values_file_count}" -eq 0 ]; then
        shasum -a 256 "${chart}/values.yaml" >>"${digest_input}"
    else
        set +u
        shasum -a 256 "${values_files[@]}" >>"${digest_input}"
        set -u
    fi
    shasum -a 256 "${digest_input}" | awk '{print $1}'
}

json_string_array() {
    local file="$1"
    if [ -s "${file}" ]; then
        jq -R -s 'split("\n") | map(select(length > 0))' "${file}"
    else
        printf '[]\n'
    fi
}

normalize_base_url() {
    local url="$1"
    printf '%s\n' "${url%/}"
}

curl_json_or_text() {
    local token_env="$1"
    local url="$2"
    local output="$3"
    local curl_args=(-fsS --max-time 15)
    local config_file=""
    if [ -n "${token_env}" ]; then
        local token="${!token_env:-}"
        [ -n "${token}" ] || die "token environment variable is empty: ${token_env}"
        config_file="${scratch}/curl-${token_env}.conf"
        : >"${config_file}"
        chmod 600 "${config_file}"
        printf 'header = "Authorization: Bearer %s"\n' "${token}" >"${config_file}"
        curl_args+=(--config "${config_file}")
    fi
    curl "${curl_args[@]}" "${url}" >"${output}"
}

readback_health() {
    local base_url="$1"
    local token_env="$2"
    local label="$3"
    if [ -z "${base_url}" ]; then
        printf 'not_requested\n'
        return 0
    fi
    local normalized health_out ready_out
    normalized="$(normalize_base_url "${base_url}")"
    health_out="${scratch}/${label}-health.txt"
    ready_out="${scratch}/${label}-ready.txt"
    if curl_json_or_text "${token_env}" "${normalized}/healthz" "${health_out}" \
        && curl_json_or_text "${token_env}" "${normalized}/readyz" "${ready_out}"; then
        printf 'pass\n'
    else
        printf 'fail\n'
    fi
}

capture_queue_state() {
    local base_url="$1"
    local token_env="$2"
    local output="$3"
    if [ -z "${base_url}" ]; then
        jq -n '{status:"not_requested"}' >"${output}"
        return 0
    fi
    local normalized raw
    normalized="$(normalize_base_url "${base_url}")"
    raw="${scratch}/admin-status.raw.json"
    if curl_json_or_text "${token_env}" "${normalized}/admin/status?format=json" "${raw}" \
        && jq -e '.queue' "${raw}" >/dev/null; then
        jq '{
            status: "captured",
            outstanding: (.queue.outstanding // null),
            pending: (.queue.pending // null),
            in_flight: (.queue.in_flight // null),
            retrying: (.queue.retrying // null),
            failed: (.queue.failed // null),
            dead_letter: (.queue.dead_letter // null),
            generation: (.generation // null)
        }' "${raw}" >"${output}"
    else
        jq -n '{status:"failed"}' >"${output}"
    fi
}

capture_first_query() {
    local base_url="$1"
    local token_env="$2"
    local output="$3"
    if [ -z "${base_url}" ]; then
        jq -n '{status:"not_requested"}' >"${output}"
        return 0
    fi
    local normalized raw
    normalized="$(normalize_base_url "${base_url}")"
    raw="${scratch}/first-query.raw.json"
    if curl_json_or_text "${token_env}" "${normalized}${first_query_path}" "${raw}" \
        && jq -e 'type == "object"' "${raw}" >/dev/null; then
        jq --arg path "${first_query_path}" '{
            status: "captured",
            path: $path,
            response_status: (.status // .state // null),
            truth: (.truth // null),
            freshness: (.freshness // .truth.freshness // null),
            error_code: (.error.code // null)
        }' "${raw}" >"${output}"
    else
        jq -n --arg path "${first_query_path}" '{status:"failed", path:$path}' >"${output}"
    fi
}

validate_state_file() {
    local file="$1"
    local label="$2"
    shift 2
    [ -n "${file}" ] || die "${label} proof requires a state declaration file"
    [ -f "${file}" ] || die "${label} state declaration does not exist: ${file}"
    jq -e . "${file}" >/dev/null || die "${label} state declaration is not valid JSON"
    local missing=()
    local field
    for field in "$@"; do
        if ! jq -e --arg field "${field}" 'has($field) and (.[$field] | tostring | length > 0)' "${file}" >/dev/null; then
            missing+=("${field}")
        fi
    done
    if [ "${#missing[@]}" -gt 0 ]; then
        die "${label} proof requires declarations for: ${missing[*]}"
    fi
}

rendered="${scratch}/rendered.yaml"
lint_out="${scratch}/helm-lint.txt"
dry_run_out="${scratch}/helm-dry-run.txt"

set +u
helm lint "${chart}" "${helm_value_args[@]}" >"${lint_out}" 2>&1
helm template "${release}" "${chart}" --namespace "${namespace}" "${helm_value_args[@]}" >"${rendered}"
helm upgrade --install "${release}" "${chart}" --namespace "${namespace}" --dry-run --debug "${helm_value_args[@]}" >"${dry_run_out}" 2>&1
set -u

workloads_file="${scratch}/workloads.txt"
awk '
    $1 == "kind:" { kind = $2; next }
    $1 == "name:" && kind != "" {
        print kind "/" $2
        kind = ""
    }
' "${rendered}" | sort -u >"${workloads_file}"

images_file="${scratch}/images.txt"
rg '^[[:space:]]*image:[[:space:]]*' "${rendered}" \
    | sed -E 's/^[[:space:]]*image:[[:space:]]*"?([^"]+)"?.*/\1/' \
    | sort -u >"${images_file}" || true

required_file="${scratch}/required.txt"
chart_name="$(chart_field name)"
helm_fullname="${release}-${chart_name}"
if [[ "${release}" == *"${chart_name}"* ]]; then
    helm_fullname="${release}"
fi
printf '%s\n' \
    "Deployment/${helm_fullname}-api" \
    "Deployment/${helm_fullname}-mcp-server" \
    "StatefulSet/${helm_fullname}" \
    "Deployment/${helm_fullname}-resolution-engine" \
    "Job/${helm_fullname}-schema-bootstrap" >"${required_file}"

required_present=true
while IFS= read -r required; do
    if ! rg --fixed-strings --quiet -- "${required}" "${workloads_file}"; then
        required_present=false
    fi
done <"${required_file}"

schema_rendered=false
schema_hook=false
if rg --quiet '^Job/.+schema-bootstrap$|^Job/eshu-schema-bootstrap$' "${workloads_file}"; then
    schema_rendered=true
fi
if rg --quiet '"helm.sh/hook":[[:space:]]*pre-install,pre-upgrade' "${rendered}"; then
    schema_hook=true
fi

core_rollout_status="not_requested"
schema_bootstrap_outcome="rendered"
if [ "${live_cluster}" = true ]; then
    command -v kubectl >/dev/null 2>&1 || die "kubectl is required with --live-cluster"
    core_rollout_status="pass"
    for resource in \
        "deployment/${helm_fullname}-api" \
        "deployment/${helm_fullname}-mcp-server" \
        "statefulset/${helm_fullname}" \
        "deployment/${helm_fullname}-resolution-engine"; do
        if ! kubectl -n "${namespace}" rollout status "${resource}" --timeout=120s >"${scratch}/rollout-${resource//\//-}.txt" 2>&1; then
            core_rollout_status="fail"
        fi
    done
    bootstrap_raw="${scratch}/schema-bootstrap-job.json"
    if kubectl -n "${namespace}" get "job/${helm_fullname}-schema-bootstrap" -o json >"${bootstrap_raw}" 2>&1 \
        && jq -e '(.status.failed // 0) == 0 and ((.status.succeeded // 0) >= 1 or any(.status.conditions[]?; .type == "Complete" and .status == "True"))' "${bootstrap_raw}" >/dev/null; then
        schema_bootstrap_outcome="complete"
    else
        schema_bootstrap_outcome="failed"
    fi
fi

api_health="$(readback_health "${api_base_url}" "${api_token_env}" api)"
mcp_health="$(readback_health "${mcp_base_url}" "${mcp_token_env}" mcp)"
queue_file="${scratch}/queue.json"
first_query_file="${scratch}/first-query.json"
capture_queue_state "${api_base_url}" "${api_token_env}" "${queue_file}"
capture_first_query "${api_base_url}" "${api_token_env}" "${first_query_file}"

upgrade_status="not_requested"
upgrade_json='{}'
if [ "${mode}" = "upgrade" ] || [ "${mode}" = "all" ] || [ -n "${upgrade_state}" ]; then
    validate_state_file "${upgrade_state}" "upgrade" durable_state queue_state graph_rebuild preserved_volumes
    upgrade_status="pass"
    upgrade_json="$(jq -c '{
        durable_state: .durable_state,
        queue_state: .queue_state,
        graph_rebuild: .graph_rebuild,
        preserved_volumes: .preserved_volumes
    }' "${upgrade_state}")"
fi

rollback_status="not_requested"
rollback_json='{}'
if [ "${mode}" = "rollback" ] || [ "${mode}" = "all" ] || [ -n "${rollback_state}" ]; then
    validate_state_file "${rollback_state}" "rollback" helm_rollback postgres_restore graph_rebuild decision_point
    rollback_status="pass"
    rollback_json="$(jq -c '{
        helm_rollback: .helm_rollback,
        postgres_restore: .postgres_restore,
        graph_rebuild: .graph_rebuild,
        decision_point: .decision_point
    }' "${rollback_state}")"
fi

chart_version="$(chart_field version)"
app_version="$(chart_field appVersion)"
image_repository="$(image_value repository)"
image_tag="$(image_value tag)"
values_digest="$(digest_files)"
workloads_json="$(json_string_array "${workloads_file}")"
images_json="$(json_string_array "${images_file}")"
queue_json="$(jq -c . "${queue_file}")"
first_query_json="$(jq -c . "${first_query_file}")"

artifact="${out_dir}/hosted-helm-rollout-proof.json"
jq -n \
    --arg generated_at "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
    --arg mode "${mode}" \
    --arg release "${release}" \
    --arg namespace "${namespace}" \
    --arg chart_version "${chart_version}" \
    --arg app_version "${app_version}" \
    --arg image_repository "${image_repository}" \
    --arg image_tag "${image_tag}" \
    --arg values_digest "${values_digest}" \
    --arg api_health "${api_health}" \
    --arg mcp_health "${mcp_health}" \
    --arg core_rollout_status "${core_rollout_status}" \
    --arg schema_bootstrap_outcome "${schema_bootstrap_outcome}" \
    --arg upgrade_status "${upgrade_status}" \
    --arg rollback_status "${rollback_status}" \
    --argjson rendered_workloads "${workloads_json}" \
    --argjson image_refs "${images_json}" \
    --argjson required_present "${required_present}" \
    --argjson schema_rendered "${schema_rendered}" \
    --argjson schema_hook "${schema_hook}" \
    --argjson queue_state "${queue_json}" \
    --argjson first_query "${first_query_json}" \
    --argjson upgrade "${upgrade_json}" \
    --argjson rollback "${rollback_json}" \
    '{
        generated_at: $generated_at,
        mode: $mode,
        release: $release,
        namespace: $namespace,
        chart: {version: $chart_version, app_version: $app_version},
        image: {repository: $image_repository, tag: $image_tag, rendered_refs: $image_refs},
        install: {
            helm_lint: "pass",
            helm_dry_run: "pass",
            core_rollout_status: $core_rollout_status,
            values_digest: $values_digest,
            rendered_workloads: $rendered_workloads,
            rendered_workload_count: ($rendered_workloads | length),
            required_workloads_present: $required_present,
            schema_bootstrap: {
                rendered: $schema_rendered,
                helm_hook: $schema_hook,
                outcome: $schema_bootstrap_outcome
            }
        },
        readback: {
            api_health: $api_health,
            mcp_health: $mcp_health,
            queue_state: $queue_state,
            first_query: $first_query
        },
        upgrade: ({
            status: $upgrade_status,
            values_digest: $values_digest
        } + $upgrade),
        rollback: ({
            status: $rollback_status
        } + $rollback)
    }' >"${artifact}"

if ! jq -e '.install.required_workloads_present == true and .install.schema_bootstrap.rendered == true and .install.schema_bootstrap.helm_hook == true and .install.core_rollout_status != "fail" and .install.schema_bootstrap.outcome != "failed"' "${artifact}" >/dev/null; then
    die "install proof missing required workloads or schema-bootstrap hook"
fi

summary="${out_dir}/hosted-helm-rollout-proof.md"
{
    printf '# Hosted Helm Rollout Proof\n\n'
    printf -- '- Release: `%s`\n' "${release}"
    printf -- '- Namespace: `%s`\n' "${namespace}"
    printf -- '- Chart version: `%s`\n' "${chart_version}"
    printf -- '- App version: `%s`\n' "${app_version}"
    printf -- '- Image: `%s:%s`\n' "${image_repository}" "${image_tag}"
    printf -- '- Values digest: `%s`\n' "${values_digest}"
    printf -- '- Helm lint: `pass`\n'
    printf -- '- Helm dry-run: `pass`\n'
    printf -- '- Core rollout status: `%s`\n' "${core_rollout_status}"
    printf -- '- Required workloads present: `%s`\n' "${required_present}"
    printf -- '- Schema bootstrap outcome: `%s`\n' "${schema_bootstrap_outcome}"
    printf -- '- API health readback: `%s`\n' "${api_health}"
    printf -- '- MCP health readback: `%s`\n' "${mcp_health}"
    printf -- '- Upgrade proof: `%s`\n' "${upgrade_status}"
    printf -- '- Rollback proof: `%s`\n' "${rollback_status}"
} >"${summary}"

printf 'wrote %s\n' "${artifact}"
