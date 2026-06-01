# shellcheck shell=bash
# Phase implementations for security_intelligence_release_gate.sh.
#
# These phases require an active remote Compose stack or Kubernetes cluster and
# are split out of the main script to keep both files under the repo file-size
# rule. The functions read globals declared by the main script: api_base_url,
# api_key, pprof_base_url, k8s_namespace, helm_release, repo_root, out_dir,
# out_json. They call die() and record_failure() from the main script.

curl_readback() {
    local path="$1"
    if [ -n "${api_key}" ]; then
        curl -fsS -m 15 -H "Authorization: Bearer ${api_key}" "${api_base_url}${path}"
    else
        curl -fsS -m 15 "${api_base_url}${path}"
    fi
}

# Normalize api_base_url so a value that already ends with "/api/v0" (the
# shape verify_remote_e2e_runtime_state.sh expects from
# ESHU_REMOTE_E2E_API_BASE_URL) does not double-prefix our hard-coded
# /api/v0/... endpoint paths.
normalize_api_base_url() {
    local raw="$1"
    raw="${raw%/}"
    raw="${raw%/api/v0}"
    printf '%s' "${raw}"
}

evidence_ref() {
    local path="$1"
    printf '%s' "${path#${out_dir}/}"
}

sanitize_public_file() {
    local input="$1"
    local output="$2"
    sed -E \
        -e 's#https?://[^[:space:]"<>]+#[redacted-url]#g' \
        -e 's#(ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-)[A-Za-z0-9_./+=:-]*#[redacted-token]#g' \
        -e 's#([0-9]{1,3}\.){3}[0-9]{1,3}#[redacted-ip]#g' \
        -e 's#([[:alnum:]_-]+\.)+(internal|local|example\.com|invalid|com|net|org|io|dev|cloud)(:[0-9]+)?#[redacted-host]#g' \
        -e 's#/(Users|home|private|var|tmp|Volumes|workspace|workspaces|repos|personal-repos)/[^[:space:]",}]*(/[^[:space:]",}]*)*#[redacted-path]#g' \
        -e 's#((repository|repo|repo_id|package|package_name|provider_url|url|host|hostname|ip|path|file|token)=)["'\'']?[^[:space:]"'\'',}]+#\1[redacted]#g' \
        -e 's#("(repo|repository|repo_id|package|package_name|provider_url|url|host|hostname|ip|path|file|token)"[[:space:]]*:[[:space:]]*)"[^"]*"#\1"[redacted]"#g' \
        "${input}" >"${output}"
}

write_k8s_pod_summary() {
    local input="$1"
    local output="$2"
    jq '[
        .items | to_entries[] | {
            pod_ref: ("pod-" + ((.key + 1) | tostring)),
            component: (.value.metadata.labels["app.kubernetes.io/component"] // "unknown"),
            phase: (.value.status.phase // "unknown"),
            container_count: ((.value.spec.containers // []) | length),
            ready_containers: ([.value.status.containerStatuses[]? | select(.ready == true)] | length),
            restart_count: ([.value.status.containerStatuses[]?.restartCount // 0] | add // 0),
            resources: [
                .value.spec.containers[]? | {
                    container: (.name // "unknown"),
                    requests: (.resources.requests // {}),
                    limits: (.resources.limits // {})
                }
            ]
        }
    ]' "${input}" >"${output}"
}

write_k8s_resource_snapshot() {
    local input="$1"
    local output="$2"
    awk 'NF >= 3 { printf "pod-%d cpu=%s memory=%s\n", NR, $2, $3 }' "${input}" >"${output}"
}

k8s_curl_readback() {
    local base="$1"
    local path="$2"
    if [ -n "${api_key}" ]; then
        curl -fsS -m 15 -H "Accept: application/json" -H "Authorization: Bearer ${api_key}" "${base}${path}"
    else
        curl -fsS -m 15 -H "Accept: application/json" "${base}${path}"
    fi
}

summarize_admin_status() {
    local input="$1"
    local output="$2"
    jq '{
        queue: {
            outstanding: (.queue.outstanding // null),
            pending: (.queue.pending // null),
            in_flight: (.queue.in_flight // null),
            retrying: (.queue.retrying // null),
            failed: (.queue.failed // null),
            dead_letter: (.queue.dead_letter // null),
            overdue_claims: (.queue.overdue_claims // null)
        },
        retry_policies_count: ((.retry_policies // []) | length),
        vulnerability_source_terminal_statuses: (
            (.vulnerability_sources // [])
            | map(.terminal_status // "unknown")
            | sort
            | group_by(.)
            | map({status: .[0], count: length})
        )
    }' "${input}" >"${output}" \
        && jq -e '.queue.retrying != null and .queue.dead_letter != null and .queue.failed != null' "${output}" >/dev/null
}

summarize_index_status() {
    local input="$1"
    local output="$2"
    jq '{
        status: (.status // "unknown"),
        health_state: (if (.health | type) == "object" then (.health.state // "") else (.health // "") end),
        queue: {
            outstanding: (.queue.outstanding // null),
            pending: (.queue.pending // null),
            in_flight: (.queue.in_flight // null),
            retrying: (.queue.retrying // null),
            failed: (.queue.failed // null),
            dead_letter: (.queue.dead_letter // null)
        }
    }' "${input}" >"${output}" \
        && jq -e '.queue.retrying != null and .queue.dead_letter != null and (.status | length > 0)' "${output}" >/dev/null
}

run_phase_runtime() {
    [ -n "${api_base_url}" ] || die "runtime phase requires --api-base-url"
    api_base_url="$(normalize_api_base_url "${api_base_url}")"
    local runtime_script="${repo_root}/scripts/verify_remote_e2e_runtime_state.sh"
    local readback_dir="${out_dir}/runtime-readback"
    mkdir -p "${readback_dir}"

    local runtime_ok=1
    if [ -x "${runtime_script}" ]; then
        if ! "${runtime_script}" >"${readback_dir}/verify_remote_e2e_runtime_state.log" 2>&1; then
            runtime_ok=0
            record_failure runtime "verify_remote_e2e_runtime_state.sh failed"
        fi
    else
        runtime_ok=0
        printf 'verify_remote_e2e_runtime_state.sh missing or not executable at %s\n' "${runtime_script}" \
            >"${readback_dir}/verify_remote_e2e_runtime_state.log"
        record_failure runtime "verify_remote_e2e_runtime_state.sh missing or not executable at ${runtime_script}"
    fi

    local endpoints=(
        "/api/v0/index-status"
        "/api/v0/supply-chain/advisories/evidence?limit=1"
        "/api/v0/supply-chain/impact/findings?limit=1"
        "/api/v0/supply-chain/security-alerts/reconciliations?limit=1"
        "/api/v0/supply-chain/sbom-attestations/attachments?limit=1"
        "/api/v0/supply-chain/container-images/identities?limit=1"
    )
    local readback_json="{}"
    local ep_count=0
    local ep_failed=0
    local ep safe_name body_file status
    for ep in "${endpoints[@]}"; do
        safe_name="$(printf '%s' "${ep}" | tr '/?=&' '____')"
        body_file="${readback_dir}/${safe_name}.json"
        if curl_readback "${ep}" >"${body_file}" 2>"${body_file}.err"; then
            status="ok"
        else
            status="error"
            ep_failed=$((ep_failed + 1))
        fi
        readback_json="$(jq --arg path "${ep}" --arg status "${status}" --arg body "${body_file}" \
            '. + {($path): {status: $status, body: $body}}' <<<"${readback_json}")"
        ep_count=$((ep_count + 1))
    done
    if [ "${ep_failed}" -gt 0 ]; then
        record_failure runtime "${ep_failed} of ${ep_count} supply-chain endpoint readbacks failed"
    fi

    local docker_stats_file="${readback_dir}/docker-stats.json"
    if command -v docker >/dev/null 2>&1; then
        docker stats --no-stream --format \
            '{"name":"{{.Name}}","cpu":"{{.CPUPerc}}","mem":"{{.MemUsage}}","net":"{{.NetIO}}","block":"{{.BlockIO}}"}' \
            >"${docker_stats_file}" 2>/dev/null || true
    fi

    # Pprof rides a separate opt-in listener (ESHU_PPROF_ADDR) and in remote
    # Compose it lives on a different host port than the API. Probe only when
    # an operator provides the URL explicitly; record "unchecked" otherwise.
    local pprof_status="unchecked"
    local pprof_url=""
    if [ -n "${pprof_base_url}" ]; then
        pprof_url="${pprof_base_url%/}/debug/pprof/"
        if command -v curl >/dev/null 2>&1 \
            && curl -fsS -m 5 "${pprof_url}" >/dev/null 2>&1; then
            pprof_status="reachable"
        else
            pprof_status="not_reachable"
        fi
    fi

    local phase_status="pass"
    if [ "${runtime_ok}" -eq 0 ] || [ "${ep_failed}" -gt 0 ]; then
        phase_status="fail"
    fi

    jq --argjson readback "${readback_json}" \
       --arg api_base_url "${api_base_url}" \
       --arg pprof "${pprof_status}" \
       --arg pprof_url "${pprof_url}" \
       --arg docker_stats_file "${docker_stats_file}" \
       --arg runtime_log "${readback_dir}/verify_remote_e2e_runtime_state.log" \
       --arg phase_status "${phase_status}" \
       --argjson endpoints_checked "${ep_count}" \
       --argjson endpoints_failed "${ep_failed}" \
       --argjson runtime_state_ok "${runtime_ok}" \
       '.runtime = {
            status: $phase_status,
            api_base_url: $api_base_url,
            runtime_state_ok: ($runtime_state_ok == 1),
            runtime_state_log: $runtime_log,
            endpoints_checked: $endpoints_checked,
            endpoints_failed: $endpoints_failed,
            readback: $readback,
            pprof_status: $pprof,
            pprof_url: $pprof_url,
            docker_stats_file: $docker_stats_file
       }' "${out_json}.tmp" >"${out_json}.tmp.new"
    mv "${out_json}.tmp.new" "${out_json}.tmp"
}

run_phase_k8s() {
    [ -n "${k8s_namespace}" ] || die "k8s phase requires --k8s-namespace"
    command -v kubectl >/dev/null 2>&1 || die "kubectl is required for k8s phase"
    local k8s_dir="${out_dir}/k8s"
    local logs_dir="${k8s_dir}/logs"
    local readback_dir="${k8s_dir}/readback"
    mkdir -p "${k8s_dir}"
    mkdir -p "${logs_dir}" "${readback_dir}"
    local pods_ok=1 top_ok=1 helm_ok=1 logs_ok=1 readback_ok=1 pprof_ok=1
    local pods_raw="${k8s_dir}/pods.raw.json"
    local pods_err_raw="${k8s_dir}/pods.raw.err"
    if kubectl -n "${k8s_namespace}" get pods -o json >"${pods_raw}" 2>"${pods_err_raw}"; then
        if ! write_k8s_pod_summary "${pods_raw}" "${k8s_dir}/pods-summary.json" 2>"${pods_err_raw}"; then
            pods_ok=0
            sanitize_public_file "${pods_err_raw}" "${k8s_dir}/pods.err"
            record_failure k8s "kubectl pod snapshot could not be summarized; see k8s/pods.err"
        fi
    else
        pods_ok=0
        sanitize_public_file "${pods_err_raw}" "${k8s_dir}/pods.err"
        record_failure k8s "kubectl get pods failed; see k8s/pods.err"
    fi
    rm -f "${pods_err_raw}"

    local top_raw="${k8s_dir}/top-pods.raw.txt"
    local top_err_raw="${k8s_dir}/top-pods.raw.err"
    if kubectl -n "${k8s_namespace}" top pods --no-headers >"${top_raw}" 2>"${top_err_raw}"; then
        write_k8s_resource_snapshot "${top_raw}" "${k8s_dir}/resource-snapshot.txt"
        if [ ! -s "${k8s_dir}/resource-snapshot.txt" ]; then
            top_ok=0
            record_failure k8s "kubectl top pods produced no CPU or memory rows; see k8s/resource-snapshot.txt"
        fi
    else
        top_ok=0
        # 'kubectl top' depends on metrics-server; the gate doc requires
        # resource snapshots so missing metrics counts as a phase failure.
        sanitize_public_file "${top_err_raw}" "${k8s_dir}/top-pods.err"
        record_failure k8s "kubectl top pods failed; see k8s/top-pods.err"
    fi
    rm -f "${top_raw}" "${top_err_raw}"

    local helm_raw="${k8s_dir}/helm-values.raw.yaml"
    local helm_err_raw="${k8s_dir}/helm-values.raw.err"
    if command -v helm >/dev/null 2>&1; then
        if helm get values "${helm_release}" -n "${k8s_namespace}" >"${helm_raw}" 2>"${helm_err_raw}"; then
            sanitize_public_file "${helm_raw}" "${k8s_dir}/helm-values.sanitized.yaml"
        else
            helm_ok=0
            sanitize_public_file "${helm_err_raw}" "${k8s_dir}/helm-values.err"
            record_failure k8s "helm get values failed; see k8s/helm-values.err"
        fi
    else
        helm_ok=0
        printf 'helm binary not on PATH; helm get values skipped\n' >"${k8s_dir}/helm-values.err"
        record_failure k8s "helm binary missing on PATH; cannot capture rendered values"
    fi
    rm -f "${helm_raw}" "${helm_err_raw}"

    local pod_count="0"
    if [ -s "${k8s_dir}/pods-summary.json" ]; then
        pod_count="$(jq 'length' "${k8s_dir}/pods-summary.json" 2>/dev/null || echo 0)"
    fi
    local logs_captured=0
    if [ "${pods_ok}" -eq 1 ]; then
        local target_count=0
        while IFS=$'\t' read -r pod pod_ref; do
            [ -n "${pod}" ] || continue
            target_count=$((target_count + 1))
            local log_raw="${logs_dir}/${pod_ref}.raw.log"
            local log_err_raw="${logs_dir}/${pod_ref}.raw.err"
            if kubectl -n "${k8s_namespace}" logs "${pod}" --all-containers --tail=200 >"${log_raw}" 2>"${log_err_raw}"; then
                sanitize_public_file "${log_raw}" "${logs_dir}/${pod_ref}.log"
                logs_captured=$((logs_captured + 1))
            else
                logs_ok=0
                sanitize_public_file "${log_err_raw}" "${logs_dir}/${pod_ref}.err"
                record_failure k8s "kubectl logs failed for an Eshu pod; see k8s/logs/${pod_ref}.err"
            fi
            rm -f "${log_raw}" "${log_err_raw}"
        done < <(jq -r --arg release "${helm_release}" '
            .items | to_entries[] |
            select(
                ((.value.metadata.labels["app.kubernetes.io/name"] // "") == "eshu") or
                ((.value.metadata.labels["app.kubernetes.io/instance"] // "") == $release) or
                ((.value.metadata.name // "") | test("(^|-)eshu($|-)"))
            ) |
            [.value.metadata.name, ("pod-" + ((.key + 1) | tostring))] | @tsv
        ' "${pods_raw}")
        if [ "${target_count}" -eq 0 ]; then
            logs_ok=0
            record_failure k8s "no Eshu pods were found for sanitized log capture"
        fi
    else
        logs_ok=0
    fi
    rm -f "${pods_raw}"

    local admin_raw="${readback_dir}/admin-status.raw.json"
    local admin_err_raw="${readback_dir}/admin-status.raw.err"
    local index_raw="${readback_dir}/index-status.raw.json"
    local index_err_raw="${readback_dir}/index-status.raw.err"
    if [ -z "${api_base_url}" ]; then
        readback_ok=0
        record_failure k8s "k8s phase requires --api-base-url for admin/status readback"
    elif ! command -v curl >/dev/null 2>&1; then
        readback_ok=0
        record_failure k8s "curl is required for k8s admin/status readback"
    else
        local k8s_api_base_url
        k8s_api_base_url="$(normalize_api_base_url "${api_base_url}")"
        if k8s_curl_readback "${k8s_api_base_url}" "/admin/status?format=json" >"${admin_raw}" 2>"${admin_err_raw}" \
            && summarize_admin_status "${admin_raw}" "${readback_dir}/admin-status-summary.json"; then
            :
        else
            readback_ok=0
            sanitize_public_file "${admin_err_raw}" "${readback_dir}/admin-status.err"
            record_failure k8s "admin/status readback failed or did not include queue retry/dead-letter fields"
        fi
        if k8s_curl_readback "${k8s_api_base_url}" "/api/v0/index-status" >"${index_raw}" 2>"${index_err_raw}" \
            && summarize_index_status "${index_raw}" "${readback_dir}/index-status-summary.json"; then
            :
        else
            readback_ok=0
            sanitize_public_file "${index_err_raw}" "${readback_dir}/index-status.err"
            record_failure k8s "index-status readback failed or did not include queue retry/dead-letter fields"
        fi
    fi
    rm -f "${admin_raw}" "${admin_err_raw}" "${index_raw}" "${index_err_raw}"

    local queue_retrying="null"
    local queue_dead_letter="null"
    local queue_failed="null"
    if [ -s "${readback_dir}/admin-status-summary.json" ]; then
        queue_retrying="$(jq '.queue.retrying' "${readback_dir}/admin-status-summary.json")"
        queue_dead_letter="$(jq '.queue.dead_letter' "${readback_dir}/admin-status-summary.json")"
        queue_failed="$(jq '.queue.failed' "${readback_dir}/admin-status-summary.json")"
    fi

    local pprof_status="unchecked"
    local pprof_diagnostic="no --pprof-base-url provided"
    if [ -n "${pprof_base_url}" ]; then
        local pprof_probe_url="${pprof_base_url%/}/debug/pprof/"
        if ! command -v curl >/dev/null 2>&1; then
            pprof_status="not_reachable"
            pprof_diagnostic="curl unavailable for pprof probe"
            pprof_ok=0
            record_failure k8s "pprof endpoint was provided but curl is unavailable"
        elif curl -fsS -m 5 "${pprof_probe_url}" >/dev/null 2>&1; then
            pprof_status="reachable"
            pprof_diagnostic="pprof endpoint responded"
        else
            pprof_status="not_reachable"
            pprof_diagnostic="pprof endpoint did not respond"
            pprof_ok=0
            record_failure k8s "pprof endpoint was provided but not reachable"
        fi
    fi

    local phase_status="pass"
    if [ "${pods_ok}" -eq 0 ] || [ "${top_ok}" -eq 0 ] || [ "${helm_ok}" -eq 0 ] \
        || [ "${logs_ok}" -eq 0 ] || [ "${readback_ok}" -eq 0 ] || [ "${pprof_ok}" -eq 0 ]; then
        phase_status="fail"
    fi
    jq --arg ns "${k8s_namespace}" \
       --arg release "${helm_release}" \
       --arg pods_file "$(evidence_ref "${k8s_dir}/pods-summary.json")" \
       --arg resource_file "$(evidence_ref "${k8s_dir}/resource-snapshot.txt")" \
       --arg values_file "$(evidence_ref "${k8s_dir}/helm-values.sanitized.yaml")" \
       --arg logs_dir "$(evidence_ref "${logs_dir}")" \
       --arg admin_file "$(evidence_ref "${readback_dir}/admin-status-summary.json")" \
       --arg index_file "$(evidence_ref "${readback_dir}/index-status-summary.json")" \
       --arg pprof_status "${pprof_status}" \
       --arg pprof_diagnostic "${pprof_diagnostic}" \
       --arg phase_status "${phase_status}" \
       --argjson pod_count "${pod_count}" \
       --argjson pods_ok "${pods_ok}" \
       --argjson top_ok "${top_ok}" \
       --argjson helm_ok "${helm_ok}" \
       --argjson logs_ok "${logs_ok}" \
       --argjson logs_captured "${logs_captured}" \
       --argjson readback_ok "${readback_ok}" \
       --argjson queue_retrying "${queue_retrying}" \
       --argjson queue_dead_letter "${queue_dead_letter}" \
       --argjson queue_failed "${queue_failed}" \
       '.k8s = {
            status: $phase_status,
            namespace: $ns,
            helm_release: $release,
            pod_count: $pod_count,
            pods_file: $pods_file,
            resource_snapshot_file: $resource_file,
            helm_values_file: $values_file,
            logs_dir: $logs_dir,
            logs_captured: $logs_captured,
            admin_status_file: $admin_file,
            index_status_file: $index_file,
            queue_retrying: $queue_retrying,
            queue_dead_letter: $queue_dead_letter,
            queue_failed: $queue_failed,
            pprof_status: $pprof_status,
            pprof_diagnostic: $pprof_diagnostic,
            pods_ok: ($pods_ok == 1),
            resource_snapshot_ok: ($top_ok == 1),
            top_ok: ($top_ok == 1),
            helm_values_ok: ($helm_ok == 1),
            logs_ok: ($logs_ok == 1),
            queue_readback_ok: ($readback_ok == 1)
       }' "${out_json}.tmp" >"${out_json}.tmp.new"
    mv "${out_json}.tmp.new" "${out_json}.tmp"
}
