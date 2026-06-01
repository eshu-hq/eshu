# shellcheck shell=bash
# Runtime evidence helpers for security_intelligence_release_gate.sh.

validate_runtime_run_kind() {
    case "${runtime_run_kind}" in
        clean|preserved) ;;
        "") die "runtime phase requires --runtime-run-kind clean or preserved" ;;
        *) die "runtime phase requires --runtime-run-kind clean or preserved, got ${runtime_run_kind}" ;;
    esac
}

runtime_previous_evidence_json() {
    if [ "${runtime_run_kind}" != "preserved" ]; then
        printf '{}'
        return 0
    fi
    [ -n "${previous_runtime_evidence}" ] || die "preserved runtime proof requires --previous-runtime-evidence"
    [ -f "${previous_runtime_evidence}" ] || die "previous runtime evidence file not found"
    if ! jq -e '.runtime.status == "pass" and .runtime.run_kind == "clean"' \
        "${previous_runtime_evidence}" >/dev/null; then
        die "previous runtime evidence must be a passing clean runtime proof"
    fi
    jq -c '{
        status: (.runtime.status // "unknown"),
        run_kind: (.runtime.run_kind // "unknown"),
        git_commit: (.state.git_commit // null),
        image_tag_candidate: (.state.image_tag_candidate // null),
        queue_terminal_ok: (.runtime.queue_terminal_ok // null),
        docker_stats_status: (.runtime.docker_stats_status // null),
        pprof_status: (.runtime.pprof_status // null)
    }' "${previous_runtime_evidence}"
}

runtime_volume_proof_json() {
    if [ -z "${runtime_volume_proof}" ]; then
        record_failure runtime "runtime phase requires --runtime-volume-proof"
        printf '{"status":"missing"}'
        return 0
    fi
    [ -f "${runtime_volume_proof}" ] || die "runtime-volume-proof file not found"
    jq -e . "${runtime_volume_proof}" >/dev/null 2>&1 || die "runtime-volume-proof must be valid JSON"
    if ! jq -e '
        [.. | objects | keys[] as $key |
            select([
                "volume","volumes","volume_id","volume_ids","volume_name","volume_names","volume_path",
                "mount","mounts","mountpoint","mount_path","host_path","driver","path","paths",
                "file","files","host","hostname","ip","token","payload","raw","body"
            ] | index($key))]
        | length == 0
    ' "${runtime_volume_proof}" >/dev/null; then
        die "runtime-volume-proof looks like private data; only aggregate volume status is accepted"
    fi
    if jq -r '.. | strings' "${runtime_volume_proof}" | rg --quiet \
        'ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|https?://|arn:(aws|aws-us-gov|aws-cn):|/(Users|home|private|var|tmp|Volumes|workspace|workspaces|repos|personal-repos)/|(^|[^0-9])[0-9]{12}([^0-9]|$)|([0-9]{1,3}\.){3}[0-9]{1,3}|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}|(^|[[:space:]"'\''])[-A-Za-z0-9_.]+/[-A-Za-z0-9_.]+($|[[:space:]"'\'',])'; then
        die "runtime-volume-proof looks like private data; only aggregate volume status is accepted"
    fi
    local contract
    contract='
        def proof_id_ok: (.proof_id | type == "string" and test("^[A-Za-z0-9._-]+$"));
        def store_ok($name): (.backing_stores[$name] // {} | .status == "pass");
        def stores_ok: store_ok("nornicdb_data") and store_ok("postgres_data") and store_ok("eshu_data");
        . as $root |
        ($root.schema_version == 1) and ($root | proof_id_ok) and ($root.run_kind == $kind) and ($root | stores_ok) and
        if $kind == "clean" then
            ($root.clean_volume_state == "reset_before_run") and
            all(["nornicdb_data","postgres_data","eshu_data"][]; . as $name |
                ($root.backing_stores[$name].before == "absent") and
                ($root.backing_stores[$name].after == "present"))
        else
            ($root.restart_without_prune == true) and
            ($root.previous_run_kind == "clean") and
            all(["nornicdb_data","postgres_data","eshu_data"][]; . as $name |
                ($root.backing_stores[$name].same_as_clean == true))
        end
    '
    if ! jq -e --arg kind "${runtime_run_kind}" "${contract}" "${runtime_volume_proof}" >/dev/null; then
        die "runtime-volume-proof does not satisfy clean/preserved Compose volume requirements"
    fi
    jq -c '{
        status: "pass",
        proof_id: .proof_id,
        run_kind: .run_kind,
        clean_volume_state: (.clean_volume_state // null),
        previous_run_kind: (.previous_run_kind // null),
        restart_without_prune: (.restart_without_prune // null),
        backing_stores: {
            nornicdb_data: (.backing_stores.nornicdb_data // {}),
            postgres_data: (.backing_stores.postgres_data // {}),
            eshu_data: (.backing_stores.eshu_data // {})
        }
    }' "${runtime_volume_proof}"
}

runtime_index_status_json() {
    local file="$1"
    if [ ! -s "${file}" ] || ! jq -e . "${file}" >/dev/null 2>&1; then
        printf '{"status":"missing","queue":{},"queue_terminal_ok":false}'
        return 0
    fi
    jq -c '{
        status: (.status // "unknown"),
        health_state: (if (.health | type) == "object" then (.health.state // "") else (.health // "") end),
        queue: {
            outstanding: (.queue.outstanding // null),
            pending: (.queue.pending // null),
            in_flight: (.queue.in_flight // null),
            retrying: (.queue.retrying // null),
            failed: (.queue.failed // null),
            dead_letter: (.queue.dead_letter // null)
        },
        queue_terminal_ok: (
            [(.queue.outstanding // 0), (.queue.pending // 0), (.queue.in_flight // 0),
             (.queue.retrying // 0), (.queue.failed // 0), (.queue.dead_letter // 0)] | add == 0
        )
    }' "${file}"
}

runtime_docker_stats_status() {
    local file="$1"
    if [ ! -s "${file}" ]; then
        printf 'missing'
        return 0
    fi
    if jq -e -s 'length > 0 and all(.[]; ((.cpu // "") | length > 0) and ((.mem // "") | length > 0))' \
        "${file}" >/dev/null 2>&1; then
        printf 'captured'
    else
        printf 'invalid'
    fi
}
