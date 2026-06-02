# shellcheck shell=bash
# Kubernetes evidence phase for security_intelligence_release_gate.sh.

write_k8s_servicemonitor_summary() {
    local input="$1"
    local output="$2"
    jq '[
        .items | to_entries[] | {
            resource_ref: ("servicemonitor-" + ((.key + 1) | tostring)),
            endpoint_count: ((.value.spec.endpoints // []) | length),
            selector_label_count: ((.value.spec.selector.matchLabels // {}) | length)
        }
    ]' "${input}" >"${output}"
}

write_k8s_networkpolicy_summary() {
    local input="$1"
    local output="$2"
    jq '[
        .items | to_entries[] | {
            resource_ref: ("networkpolicy-" + ((.key + 1) | tostring)),
            policy_types: (.value.spec.policyTypes // []),
            ingress_rule_count: ((.value.spec.ingress // []) | length),
            egress_rule_count: ((.value.spec.egress // []) | length)
        }
    ]' "${input}" >"${output}"
}

write_k8s_pdb_summary() {
    local input="$1"
    local output="$2"
    jq '[
        .items | to_entries[] | {
            resource_ref: ("pdb-" + ((.key + 1) | tostring)),
            current_healthy: (.value.status.currentHealthy // null),
            desired_healthy: (.value.status.desiredHealthy // null),
            disruptions_allowed: (.value.status.disruptionsAllowed // null)
        }
    ]' "${input}" >"${output}"
}

write_k8s_schema_bootstrap_job_summary() {
    local input="$1"
    local output="$2"
    jq '[
        .items | to_entries[] |
        select(
            ((.value.metadata.labels["app.kubernetes.io/component"] // "") == "schema-bootstrap") or
            ((.value.metadata.name // "") | test("schema-bootstrap"))
        ) | {
            resource_ref: ("job-" + ((.key + 1) | tostring)),
            active: (.value.status.active // 0),
            succeeded: (.value.status.succeeded // 0),
            failed: (.value.status.failed // 0),
            complete_condition: any(.value.status.conditions[]?; .type == "Complete" and .status == "True"),
            failed_condition: any(.value.status.conditions[]?; .type == "Failed" and .status == "True")
        }
    ]' "${input}" >"${output}"
}

write_helm_manifest_kind_summary() {
    local input="$1"
    local output="$2"
    awk '/^kind:[[:space:]]*/ { print $2 }' "${input}" \
        | jq -R . \
        | jq -sc 'group_by(.) | map({kind: .[0], count: length})' >"${output}"
}

k8s_summary_count() {
    local file="$1"
    if [ -s "${file}" ]; then
        jq 'length' "${file}" 2>/dev/null || echo 0
    else
        echo 0
    fi
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
    local service_monitor_ok=1 network_policy_ok=1 pdb_ok=1 bootstrap_job_ok=1 helm_manifest_ok=1
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
        sanitize_public_file "${top_err_raw}" "${k8s_dir}/top-pods.err"
        record_failure k8s "kubectl top pods failed; see k8s/top-pods.err"
    fi
    rm -f "${top_raw}" "${top_err_raw}"

    local service_monitor_raw="${k8s_dir}/servicemonitors.raw.json"
    local service_monitor_err_raw="${k8s_dir}/servicemonitors.raw.err"
    if kubectl -n "${k8s_namespace}" get servicemonitors -o json >"${service_monitor_raw}" 2>"${service_monitor_err_raw}" \
        && write_k8s_servicemonitor_summary "${service_monitor_raw}" "${k8s_dir}/servicemonitors-summary.json"; then
        :
    else
        service_monitor_ok=0
        sanitize_public_file "${service_monitor_err_raw}" "${k8s_dir}/servicemonitors.err"
        record_failure k8s "ServiceMonitor evidence could not be captured; see k8s/servicemonitors.err"
    fi
    rm -f "${service_monitor_raw}" "${service_monitor_err_raw}"
    local service_monitor_count
    service_monitor_count="$(k8s_summary_count "${k8s_dir}/servicemonitors-summary.json")"
    if [ "${service_monitor_count}" -eq 0 ]; then
        service_monitor_ok=0
        record_failure k8s "no ServiceMonitor evidence was captured for long-running runtimes"
    fi

    local network_policy_raw="${k8s_dir}/networkpolicies.raw.json"
    local network_policy_err_raw="${k8s_dir}/networkpolicies.raw.err"
    if kubectl -n "${k8s_namespace}" get networkpolicies -o json >"${network_policy_raw}" 2>"${network_policy_err_raw}" \
        && write_k8s_networkpolicy_summary "${network_policy_raw}" "${k8s_dir}/networkpolicies-summary.json"; then
        :
    else
        network_policy_ok=0
        sanitize_public_file "${network_policy_err_raw}" "${k8s_dir}/networkpolicies.err"
        record_failure k8s "NetworkPolicy evidence could not be captured; see k8s/networkpolicies.err"
    fi
    rm -f "${network_policy_raw}" "${network_policy_err_raw}"
    local network_policy_count
    network_policy_count="$(k8s_summary_count "${k8s_dir}/networkpolicies-summary.json")"
    if [ "${network_policy_count}" -eq 0 ]; then
        network_policy_ok=0
        record_failure k8s "no NetworkPolicy evidence was captured"
    fi

    local pdb_raw="${k8s_dir}/pdbs.raw.json"
    local pdb_err_raw="${k8s_dir}/pdbs.raw.err"
    if kubectl -n "${k8s_namespace}" get poddisruptionbudgets -o json >"${pdb_raw}" 2>"${pdb_err_raw}" \
        && write_k8s_pdb_summary "${pdb_raw}" "${k8s_dir}/pdbs-summary.json"; then
        :
    else
        pdb_ok=0
        sanitize_public_file "${pdb_err_raw}" "${k8s_dir}/pdbs.err"
        record_failure k8s "PodDisruptionBudget evidence could not be captured; see k8s/pdbs.err"
    fi
    rm -f "${pdb_raw}" "${pdb_err_raw}"
    local pdb_count
    pdb_count="$(k8s_summary_count "${k8s_dir}/pdbs-summary.json")"
    if [ "${pdb_count}" -eq 0 ]; then
        pdb_ok=0
        record_failure k8s "no PodDisruptionBudget evidence was captured"
    fi

    local jobs_raw="${k8s_dir}/jobs.raw.json"
    local jobs_err_raw="${k8s_dir}/jobs.raw.err"
    if kubectl -n "${k8s_namespace}" get jobs -o json >"${jobs_raw}" 2>"${jobs_err_raw}" \
        && write_k8s_schema_bootstrap_job_summary "${jobs_raw}" "${k8s_dir}/schema-bootstrap-jobs-summary.json"; then
        :
    else
        bootstrap_job_ok=0
        sanitize_public_file "${jobs_err_raw}" "${k8s_dir}/jobs.err"
        record_failure k8s "schema-bootstrap Job evidence could not be captured; see k8s/jobs.err"
    fi
    rm -f "${jobs_raw}" "${jobs_err_raw}"
    local schema_bootstrap_job_count schema_bootstrap_failed
    schema_bootstrap_job_count="$(k8s_summary_count "${k8s_dir}/schema-bootstrap-jobs-summary.json")"
    schema_bootstrap_failed="$(jq '[.[]?.failed // 0] | add // 0' "${k8s_dir}/schema-bootstrap-jobs-summary.json" 2>/dev/null || echo 0)"
    if [ "${schema_bootstrap_job_count}" -eq 0 ]; then
        bootstrap_job_ok=0
        record_failure k8s "no schema-bootstrap Job evidence was captured"
    elif ! jq -e '
        length > 0 and all(.[]; ((.failed // 0) == 0) and ((.active // 0) == 0) and (((.succeeded // 0) >= 1) or (.complete_condition == true)))
    ' "${k8s_dir}/schema-bootstrap-jobs-summary.json" >/dev/null; then
        bootstrap_job_ok=0
        record_failure k8s "schema-bootstrap Job was not complete and failure-free"
    fi

    local helm_raw="${k8s_dir}/helm-values.raw.yaml"
    local helm_manifest_raw="${k8s_dir}/helm-manifest.raw.yaml"
    local helm_err_raw="${k8s_dir}/helm-values.raw.err"
    local helm_manifest_err_raw="${k8s_dir}/helm-manifest.raw.err"
    if command -v helm >/dev/null 2>&1; then
        if helm get values "${helm_release}" -n "${k8s_namespace}" >"${helm_raw}" 2>"${helm_err_raw}"; then
            sanitize_public_file "${helm_raw}" "${k8s_dir}/helm-values.sanitized.yaml"
        else
            helm_ok=0
            sanitize_public_file "${helm_err_raw}" "${k8s_dir}/helm-values.err"
            record_failure k8s "helm get values failed; see k8s/helm-values.err"
        fi
        if helm get manifest "${helm_release}" -n "${k8s_namespace}" >"${helm_manifest_raw}" 2>"${helm_manifest_err_raw}"; then
            sanitize_public_file "${helm_manifest_raw}" "${k8s_dir}/helm-manifest.sanitized.yaml"
            write_helm_manifest_kind_summary "${helm_manifest_raw}" "${k8s_dir}/helm-manifest-kinds.json"
            if ! jq -e 'length > 0' "${k8s_dir}/helm-manifest-kinds.json" >/dev/null; then
                helm_manifest_ok=0
                record_failure k8s "helm manifest kind summary was empty"
            fi
        else
            helm_manifest_ok=0
            sanitize_public_file "${helm_manifest_err_raw}" "${k8s_dir}/helm-manifest.err"
            record_failure k8s "helm get manifest failed; see k8s/helm-manifest.err"
        fi
    else
        helm_ok=0
        helm_manifest_ok=0
        printf 'helm binary not on PATH; helm get values skipped\n' >"${k8s_dir}/helm-values.err"
        printf 'helm binary not on PATH; helm get manifest skipped\n' >"${k8s_dir}/helm-manifest.err"
        record_failure k8s "helm binary missing on PATH; cannot capture rendered values"
    fi
    rm -f "${helm_raw}" "${helm_err_raw}" "${helm_manifest_raw}" "${helm_manifest_err_raw}"

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
    local queue_outstanding="null"
    local queue_pending="null"
    local queue_in_flight="null"
    local queue_terminal_ok=0
    if [ -s "${readback_dir}/admin-status-summary.json" ]; then
        queue_outstanding="$(jq '.queue.outstanding' "${readback_dir}/admin-status-summary.json")"
        queue_pending="$(jq '.queue.pending' "${readback_dir}/admin-status-summary.json")"
        queue_in_flight="$(jq '.queue.in_flight' "${readback_dir}/admin-status-summary.json")"
        queue_retrying="$(jq '.queue.retrying' "${readback_dir}/admin-status-summary.json")"
        queue_dead_letter="$(jq '.queue.dead_letter' "${readback_dir}/admin-status-summary.json")"
        queue_failed="$(jq '.queue.failed' "${readback_dir}/admin-status-summary.json")"
    fi
    if [ -s "${readback_dir}/admin-status-summary.json" ] && [ -s "${readback_dir}/index-status-summary.json" ]; then
        local admin_queue_total index_queue_total
        admin_queue_total="$(jq '[.queue.outstanding, .queue.pending, .queue.in_flight, .queue.retrying, .queue.failed, .queue.dead_letter, .queue.overdue_claims] | map(. // 0) | add' "${readback_dir}/admin-status-summary.json")"
        index_queue_total="$(jq '[.queue.outstanding, .queue.pending, .queue.in_flight, .queue.retrying, .queue.failed, .queue.dead_letter] | map(. // 0) | add' "${readback_dir}/index-status-summary.json")"
        if [ "${admin_queue_total}" -eq 0 ] && [ "${index_queue_total}" -eq 0 ]; then
            queue_terminal_ok=1
        else
            record_failure k8s "queue readback was not terminal; active, retrying, failed, or dead-letter work remains"
        fi
    fi

    local pprof_status="missing"
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
    else
        pprof_ok=0
        record_failure k8s "k8s phase requires --pprof-base-url for pprof proof"
    fi

    local phase_status="pass"
    if [ "${pods_ok}" -eq 0 ] || [ "${top_ok}" -eq 0 ] || [ "${helm_ok}" -eq 0 ] \
        || [ "${logs_ok}" -eq 0 ] || [ "${readback_ok}" -eq 0 ] || [ "${pprof_ok}" -eq 0 ] \
        || [ "${queue_terminal_ok}" -eq 0 ] || [ "${service_monitor_ok}" -eq 0 ] \
        || [ "${network_policy_ok}" -eq 0 ] || [ "${pdb_ok}" -eq 0 ] \
        || [ "${bootstrap_job_ok}" -eq 0 ] || [ "${helm_manifest_ok}" -eq 0 ]; then
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
       --arg service_monitor_file "$(evidence_ref "${k8s_dir}/servicemonitors-summary.json")" \
       --arg network_policy_file "$(evidence_ref "${k8s_dir}/networkpolicies-summary.json")" \
       --arg pdb_file "$(evidence_ref "${k8s_dir}/pdbs-summary.json")" \
       --arg schema_bootstrap_job_file "$(evidence_ref "${k8s_dir}/schema-bootstrap-jobs-summary.json")" \
       --arg helm_manifest_file "$(evidence_ref "${k8s_dir}/helm-manifest.sanitized.yaml")" \
       --arg helm_manifest_kinds_file "$(evidence_ref "${k8s_dir}/helm-manifest-kinds.json")" \
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
       --argjson queue_outstanding "${queue_outstanding}" \
       --argjson queue_pending "${queue_pending}" \
       --argjson queue_in_flight "${queue_in_flight}" \
       --argjson queue_retrying "${queue_retrying}" \
       --argjson queue_dead_letter "${queue_dead_letter}" \
       --argjson queue_failed "${queue_failed}" \
       --argjson queue_terminal_ok "${queue_terminal_ok}" \
       --argjson service_monitor_ok "${service_monitor_ok}" \
       --argjson service_monitor_count "${service_monitor_count}" \
       --argjson network_policy_ok "${network_policy_ok}" \
       --argjson network_policy_count "${network_policy_count}" \
       --argjson pdb_ok "${pdb_ok}" \
       --argjson pdb_count "${pdb_count}" \
       --argjson schema_bootstrap_job_ok "${bootstrap_job_ok}" \
       --argjson schema_bootstrap_job_count "${schema_bootstrap_job_count}" \
       --argjson schema_bootstrap_failed "${schema_bootstrap_failed}" \
       --argjson helm_manifest_ok "${helm_manifest_ok}" \
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
            queue_outstanding: $queue_outstanding,
            queue_pending: $queue_pending,
            queue_in_flight: $queue_in_flight,
            queue_retrying: $queue_retrying,
            queue_dead_letter: $queue_dead_letter,
            queue_failed: $queue_failed,
            queue_terminal_ok: ($queue_terminal_ok == 1),
            pprof_status: $pprof_status,
            pprof_diagnostic: $pprof_diagnostic,
            pods_ok: ($pods_ok == 1),
            resource_snapshot_ok: ($top_ok == 1),
            top_ok: ($top_ok == 1),
            helm_values_ok: ($helm_ok == 1),
            logs_ok: ($logs_ok == 1),
            queue_readback_ok: ($readback_ok == 1),
            service_monitor_ok: ($service_monitor_ok == 1),
            service_monitor_count: $service_monitor_count,
            service_monitor_file: $service_monitor_file,
            network_policy_ok: ($network_policy_ok == 1),
            network_policy_count: $network_policy_count,
            network_policy_file: $network_policy_file,
            pdb_ok: ($pdb_ok == 1),
            pdb_count: $pdb_count,
            pdb_file: $pdb_file,
            schema_bootstrap_job_ok: ($schema_bootstrap_job_ok == 1),
            schema_bootstrap_job_count: $schema_bootstrap_job_count,
            schema_bootstrap_failed: $schema_bootstrap_failed,
            schema_bootstrap_job_file: $schema_bootstrap_job_file,
            helm_manifest_ok: ($helm_manifest_ok == 1),
            helm_manifest_file: $helm_manifest_file,
            helm_manifest_kinds_file: $helm_manifest_kinds_file
       }' "${out_json}.tmp" >"${out_json}.tmp.new"
    mv "${out_json}.tmp.new" "${out_json}.tmp"
}
