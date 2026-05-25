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

run_phase_runtime() {
    [ -n "${api_base_url}" ] || die "runtime phase requires --api-base-url"
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

    jq --argjson readback "${readback_json}" \
       --arg pprof "${pprof_status}" \
       --arg pprof_url "${pprof_url}" \
       --arg docker_stats_file "${docker_stats_file}" \
       --arg runtime_log "${readback_dir}/verify_remote_e2e_runtime_state.log" \
       --argjson endpoints_checked "${ep_count}" \
       --argjson endpoints_failed "${ep_failed}" \
       --argjson runtime_state_ok "${runtime_ok}" \
       '.runtime = {
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
    mkdir -p "${k8s_dir}"
    local pods_ok=1 top_ok=1 helm_ok=1
    if ! kubectl -n "${k8s_namespace}" get pods -o json >"${k8s_dir}/pods.json" 2>"${k8s_dir}/pods.err"; then
        pods_ok=0
        record_failure k8s "kubectl get pods failed in namespace ${k8s_namespace}; see ${k8s_dir}/pods.err"
    fi
    if ! kubectl -n "${k8s_namespace}" top pods --no-headers >"${k8s_dir}/top-pods.txt" 2>"${k8s_dir}/top-pods.err"; then
        top_ok=0
        # 'kubectl top' depends on metrics-server; the gate doc requires
        # resource snapshots so missing metrics counts as a phase failure.
        record_failure k8s "kubectl top pods failed in namespace ${k8s_namespace}; see ${k8s_dir}/top-pods.err"
    fi
    if command -v helm >/dev/null 2>&1; then
        if ! helm get values "${helm_release}" -n "${k8s_namespace}" >"${k8s_dir}/helm-values.yaml" 2>"${k8s_dir}/helm-values.err"; then
            helm_ok=0
            record_failure k8s "helm get values ${helm_release} failed in namespace ${k8s_namespace}; see ${k8s_dir}/helm-values.err"
        fi
    else
        helm_ok=0
        printf 'helm binary not on PATH; helm get values skipped\n' >"${k8s_dir}/helm-values.err"
        record_failure k8s "helm binary missing on PATH; cannot capture rendered values for ${helm_release}"
    fi
    local pod_count="0"
    if [ -s "${k8s_dir}/pods.json" ]; then
        pod_count="$(jq '.items | length' "${k8s_dir}/pods.json" 2>/dev/null || echo 0)"
    fi
    jq --arg ns "${k8s_namespace}" \
       --arg release "${helm_release}" \
       --arg pods_file "${k8s_dir}/pods.json" \
       --arg top_file "${k8s_dir}/top-pods.txt" \
       --arg values_file "${k8s_dir}/helm-values.yaml" \
       --argjson pod_count "${pod_count}" \
       --argjson pods_ok "${pods_ok}" \
       --argjson top_ok "${top_ok}" \
       --argjson helm_ok "${helm_ok}" \
       '.k8s = {
            namespace: $ns,
            helm_release: $release,
            pod_count: $pod_count,
            pods_file: $pods_file,
            top_pods_file: $top_file,
            helm_values_file: $values_file,
            pods_ok: ($pods_ok == 1),
            top_ok: ($top_ok == 1),
            helm_values_ok: ($helm_ok == 1)
       }' "${out_json}.tmp" >"${out_json}.tmp.new"
    mv "${out_json}.tmp.new" "${out_json}.tmp"
}
