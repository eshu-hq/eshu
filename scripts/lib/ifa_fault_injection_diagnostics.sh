#!/usr/bin/env bash
# shellcheck disable=SC2154
# Failure diagnostics for scripts/verify-ifa-fault-injection.sh.
#
# These artifacts contain exact graph statements and fixture facts. They are
# only safe for this committed synthetic CI corpus; do not reuse this collector
# against live, production, customer, or otherwise private datasets.

ifa_fault_sha256_file() {
	local path="$1"
	if command -v shasum >/dev/null 2>&1; then
		ifa_fault_run_bounded shasum -a 256 "${path}" | awk '{print $1}'
	elif command -v sha256sum >/dev/null 2>&1; then
		ifa_fault_run_bounded sha256sum "${path}" | awk '{print $1}'
	else
		return 1
	fi
}

ifa_fault_write_graph_manifest() {
	local work_root="$1" manifest="${1}/graph-manifest.tsv"
	local graph_dump digest bytes nodes edges gcp_edges count=0
	printf 'dump\tsha256\tbytes\tnodes\tedges\tgcp_edges\n' >"${manifest}"
	while IFS= read -r graph_dump; do
		[[ -f "${graph_dump}" ]] || continue
		digest="$(ifa_fault_sha256_file "${graph_dump}")" || return 1
		bytes="$(ifa_fault_run_bounded wc -c <"${graph_dump}")" || return 1
		bytes="${bytes//[[:space:]]/}"
		nodes="$(ifa_fault_run_bounded jq -r '(.nodes // []) | length' "${graph_dump}")" || return 1
		edges="$(ifa_fault_run_bounded jq -r '(.edges // []) | length' "${graph_dump}")" || return 1
		gcp_edges="$(ifa_fault_run_bounded jq -r '[.edges[]? | select(.type | startswith("GCP_"))] | length' "${graph_dump}")" || return 1
		printf '%s\t%s\t%s\t%s\t%s\t%s\n' \
			"${graph_dump##*/}" "${digest}" "${bytes}" "${nodes}" "${edges}" "${gcp_edges}" >>"${manifest}"
		count=$((count + 1))
	done < <(printf '%s\n' "${work_root}"/graph-*.dump)
	[[ "${count}" -gt 0 ]]
}

ifa_fault_record_diagnostic_status() {
	local manifest="$1" artifact="$2" status="$3" detail="$4"
	printf '%s\t%s\t%s\n' "${artifact}" "${status}" "${detail}" >>"${manifest}"
}

# ifa_fault_run_bounded executes one external command under a hard deadline.
# GNU coreutils uses timeout on Linux and commonly gtimeout on macOS; Perl is
# the stock-macOS fallback. The alarm survives exec and terminates the command.
ifa_fault_validate_diagnostic_timeout() {
	local timeout_budget="${IFA_FAULT_DIAGNOSTIC_TIMEOUT:-5}"
	if [[ ! "${timeout_budget}" =~ ^[1-9][0-9]*$ ]] \
		|| [[ "${#timeout_budget}" -gt 2 ]] \
		|| ((10#${timeout_budget} > 30)); then
		printf 'ifa fault diagnostics: IFA_FAULT_DIAGNOSTIC_TIMEOUT must be an integer from 1 through 30 seconds\n' >&2
		return 2
	fi
}

ifa_fault_run_bounded() {
	local timeout_budget="${IFA_FAULT_DIAGNOSTIC_TIMEOUT:-5}"
	ifa_fault_validate_diagnostic_timeout || return $?
	if command -v timeout >/dev/null 2>&1; then
		timeout -s KILL "${timeout_budget}" "$@"
	elif command -v gtimeout >/dev/null 2>&1; then
		gtimeout -s KILL "${timeout_budget}" "$@"
	elif command -v perl >/dev/null 2>&1; then
		perl -e 'alarm shift; exec @ARGV or exit 127' "${timeout_budget}" "$@"
	else
		return 127
	fi
}

# The default gate derives its expected revision from rendered Compose. An
# explicit NORNICDB_IMAGE must pair with IFA_FAULT_EXPECTED_NORNICDB_REVISION so
# failure evidence never silently compares an alternate image to the default.
ifa_fault_expected_backend_revision() {
	local compose_project="$1" compose_file="$2" rendered_config expected_revision
	if [[ -n "${NORNICDB_IMAGE:-}" ]]; then
		expected_revision="${IFA_FAULT_EXPECTED_NORNICDB_REVISION:-}"
		if [[ ! "${expected_revision}" =~ ^[0-9a-f]{40}$ ]]; then
			printf 'ifa fault diagnostics: NORNICDB_IMAGE requires a 40-character lowercase IFA_FAULT_EXPECTED_NORNICDB_REVISION\n' >&2
			return 1
		fi
		printf '%s\n' "${expected_revision}"
		return 0
	fi
	rendered_config="$(ifa_fault_run_bounded \
		docker compose -p "${compose_project}" -f "${compose_file}" config --format json)" \
		|| return 1
	expected_revision="$(ifa_fault_run_bounded jq -er '
		.services.nornicdb.build.labels["org.opencontainers.image.revision"]
		| select(type == "string" and test("^[0-9a-f]{40}$"))
	' <<<"${rendered_config}")" || return 1
	printf '%s\n' "${expected_revision}"
}

# ifa_fault_capture_command runs one external diagnostic under a hard deadline,
# writes clean stdout on success, and records an explicit status either way.
ifa_fault_capture_command() {
	local manifest="$1" artifact="$2" output="$3"
	shift 3
	local error_output="${output}.error" rc=0
	rm -f "${output}" "${error_output}"
	ifa_fault_run_bounded "$@" >"${output}" 2>"${error_output}" || rc=$?
	if [[ "${rc}" -eq 0 && -s "${output}" ]]; then
		rm -f "${error_output}"
		ifa_fault_record_diagnostic_status "${manifest}" "${artifact}" complete "exit=0"
		return 0
	fi
	ifa_fault_record_diagnostic_status "${manifest}" "${artifact}" failed "exit=${rc}"
	return 1
}

ifa_fault_record_existing_artifact() {
	local manifest="$1" artifact="$2" path="$3"
	local bytes
	if [[ -s "${path}" ]]; then
		bytes="$(ifa_fault_run_bounded wc -c <"${path}")" || {
			ifa_fault_record_diagnostic_status "${manifest}" "${artifact}" failed size-unavailable
			return 1
		}
		bytes="${bytes//[[:space:]]/}"
		ifa_fault_record_diagnostic_status "${manifest}" "${artifact}" present "bytes=${bytes}"
		return 0
	else
		ifa_fault_record_diagnostic_status "${manifest}" "${artifact}" absent "bytes=0"
		return 1
	fi
}

ifa_fault_capture_restart_failure_graph() {
	local manifest="$1" work_root="$2" current_cell="$3" ifa_bin_dir="$4"
	local regular_dump="${work_root}/graph-restartbackend.dump"
	local failure_dump="${work_root}/graph-restartbackend-failure.dump"
	[[ "${current_cell}" == cell_restartbackend ]] || return 0
	if [[ -s "${regular_dump}" ]]; then
		ifa_fault_record_diagnostic_status \
			"${manifest}" restart-failure-graph skipped regular-dump-present
		return 0
	fi
	if [[ -z "${ifa_bin_dir}" || ! -x "${ifa_bin_dir}/eshu-ifa" ]]; then
		ifa_fault_record_diagnostic_status \
			"${manifest}" restart-failure-graph failed graph-dump-binary-unavailable
		return 1
	fi
	ifa_fault_capture_command \
		"${manifest}" restart-failure-graph "${failure_dump}" \
		"${ifa_bin_dir}/eshu-ifa" graph-dump
}

ifa_fault_validate_cell_artifacts() {
	local manifest="$1" work_root="$2" current_cell="$3"
	local existing restart_result="" complete=1
	if [[ "${current_cell}" == cell_restartbackend ]]; then
		for existing in graph-baseline.dump fault-restart-backend.json restart-watch-result \
			fault-restart-backend.json.restart-sentinel.trigger.json \
			logs/restart-watcher.log logs/reducer-restartbackend.log logs/projector-restartbackend.log; do
			ifa_fault_record_existing_artifact \
				"${manifest}" "${existing}" "${work_root}/${existing}" || complete=0
		done
		if [[ -s "${work_root}/graph-restartbackend.dump" ]]; then
			ifa_fault_record_existing_artifact \
				"${manifest}" graph-restartbackend.dump "${work_root}/graph-restartbackend.dump" || complete=0
		elif [[ -s "${work_root}/graph-restartbackend-failure.dump" ]]; then
			ifa_fault_record_existing_artifact \
				"${manifest}" graph-restartbackend-failure.dump \
				"${work_root}/graph-restartbackend-failure.dump" || complete=0
		else
			ifa_fault_record_diagnostic_status \
				"${manifest}" restart-graph-dump absent no-regular-or-failure-dump
			complete=0
		fi
		if [[ -s "${work_root}/restart-watch-result" ]] \
			&& IFS= read -r restart_result <"${work_root}/restart-watch-result" \
			&& [[ "${restart_result}" == fired ]]; then
			ifa_fault_record_diagnostic_status "${manifest}" restart-watch-result-value valid fired
		else
			ifa_fault_record_diagnostic_status "${manifest}" restart-watch-result-value invalid not-fired
			complete=0
		fi
		if ifa_fault_run_bounded jq -e \
			'.group_ordinal >= 1 and (.surface == "execute_group" or .surface == "execute_phase_group") and (.statements | type == "array" and length >= 1)' \
			"${work_root}/fault-restart-backend.json.restart-sentinel.trigger.json" >/dev/null 2>&1; then
			ifa_fault_record_diagnostic_status "${manifest}" restart-trigger-json valid parseable
		else
			ifa_fault_record_diagnostic_status "${manifest}" restart-trigger-json invalid unparseable
			complete=0
		fi
	fi
	[[ "${complete}" -eq 1 ]]
}

# ifa_fault_capture_failure_diagnostics snapshots state while the failed cell's
# Compose stack is still available. Every external call is bounded. Collection
# remains best-effort, but the return value and diagnostics-complete marker fail
# closed when any core snapshot is missing.
ifa_fault_capture_failure_diagnostics() {
	local work_root="$1" logs="$2" compose_project="$3" compose_file="$4"
	local use_compose="$5" postgres_dsn="$6" ifa_bin_dir="${7:-}"
	local manifest="${work_root}/diagnostics-manifest.tsv" container_id="" complete=1
	local current_cell="" expected_revision="" actual_revision=""
	local expected_revision_source="rendered-compose-config"
	local work_items_sql gcp_facts_sql
	ifa_fault_validate_diagnostic_timeout || return $?
	mkdir -p "${logs}"
	rm -f "${work_root}/diagnostics-complete"
	printf 'artifact\tstatus\tdetail\n' >"${manifest}"

	if ifa_fault_record_existing_artifact "${manifest}" current-cell "${work_root}/current-cell"; then
		IFS= read -r current_cell <"${work_root}/current-cell" || complete=0
	else
		complete=0
	fi
	ifa_fault_capture_restart_failure_graph \
		"${manifest}" "${work_root}" "${current_cell}" "${ifa_bin_dir}" || complete=0
	if ifa_fault_write_graph_manifest "${work_root}"; then
		ifa_fault_record_diagnostic_status "${manifest}" graph-manifest complete "exit=0"
	else
		ifa_fault_record_diagnostic_status "${manifest}" graph-manifest failed "no-readable-graph-dump"
		complete=0
	fi
	ifa_fault_validate_cell_artifacts "${manifest}" "${work_root}" "${current_cell}" || complete=0

	work_items_sql="COPY (SELECT work_item_id, stage, domain, scope_id, generation_id, status, attempt_count, failure_class, failure_message FROM fact_work_items ORDER BY stage, domain, scope_id, generation_id, work_item_id) TO STDOUT WITH (FORMAT csv, HEADER true);"
	gcp_facts_sql="SELECT jsonb_build_object('fact_id', fact_id, 'scope_id', scope_id, 'generation_id', generation_id, 'fact_kind', fact_kind, 'stable_fact_key', stable_fact_key, 'schema_version', schema_version, 'collector_kind', collector_kind, 'fencing_token', fencing_token, 'source_confidence', source_confidence, 'source_system', source_system, 'source_fact_key', source_fact_key, 'source_uri', source_uri, 'source_record_id', source_record_id, 'observed_at', observed_at, 'ingested_at', ingested_at, 'is_tombstone', is_tombstone, 'payload', payload) FROM fact_records WHERE fact_kind IN ('gcp_cloud_resource', 'gcp_cloud_relationship') ORDER BY scope_id, generation_id, observed_at, fact_id;"
	if [[ "${use_compose}" -eq 1 ]]; then
		ifa_fault_capture_command "${manifest}" work-items "${work_root}/work-items.csv" \
			docker compose -p "${compose_project}" -f "${compose_file}" exec -T postgres \
			psql -U eshu -d eshu -tA -c "${work_items_sql}" || complete=0
		ifa_fault_capture_command "${manifest}" gcp-facts "${work_root}/gcp-facts.jsonl" \
			docker compose -p "${compose_project}" -f "${compose_file}" exec -T postgres \
			psql -U eshu -d eshu -tA -c "${gcp_facts_sql}" || complete=0
	else
		ifa_fault_capture_command "${manifest}" work-items "${work_root}/work-items.csv" \
			psql "${postgres_dsn}" -tA -c "${work_items_sql}" || complete=0
		ifa_fault_capture_command "${manifest}" gcp-facts "${work_root}/gcp-facts.jsonl" \
			psql "${postgres_dsn}" -tA -c "${gcp_facts_sql}" || complete=0
	fi

	if [[ "${use_compose}" -eq 1 ]]; then
		rm -f "${work_root}/backend-expected-revision.txt"
		if [[ -n "${NORNICDB_IMAGE:-}" ]]; then
			expected_revision_source="explicit-image-override"
		fi
		if expected_revision="$(ifa_fault_expected_backend_revision "${compose_project}" "${compose_file}")"; then
			printf '%s\n' "${expected_revision}" >"${work_root}/backend-expected-revision.txt"
			ifa_fault_record_diagnostic_status \
				"${manifest}" backend-expected-revision complete "${expected_revision_source}"
		else
			ifa_fault_record_diagnostic_status \
				"${manifest}" backend-expected-revision failed unavailable-or-invalid
			complete=0
		fi
		ifa_fault_capture_command "${manifest}" compose-logs "${logs}/compose-services.log" \
			docker compose -p "${compose_project}" -f "${compose_file}" logs --no-color || complete=0
		ifa_fault_capture_command "${manifest}" backend-image "${work_root}/backend-image.txt" \
			docker compose -p "${compose_project}" -f "${compose_file}" images nornicdb || complete=0
		container_id="$(ifa_fault_run_bounded \
			docker compose -p "${compose_project}" -f "${compose_file}" ps -q nornicdb 2>/dev/null)" || true
		if [[ -n "${container_id}" ]]; then
			ifa_fault_capture_command "${manifest}" backend-labels "${work_root}/backend-labels.json" \
				docker inspect --format '{{json .Config.Labels}}' "${container_id}" || complete=0
			ifa_fault_capture_command "${manifest}" backend-container "${work_root}/backend-container.json" \
				bash -c 'docker inspect "$1" | jq ".[0] | {image: .Image, restart_count: .RestartCount, state: {status: .State.Status, running: .State.Running, started_at: .State.StartedAt, finished_at: .State.FinishedAt, exit_code: .State.ExitCode}, mounts: [.Mounts[] | {type: .Type, name: .Name, destination: .Destination, rw: .RW}]}"' _ "${container_id}" || complete=0
			ifa_fault_capture_command \
				"${manifest}" backend-actual-revision "${work_root}/backend-actual-revision.txt" \
				jq -er --arg revision_key org.opencontainers.image.revision \
				'.[$revision_key] | select(type == "string" and test("^[0-9a-f]{40}$"))' \
				"${work_root}/backend-labels.json" || complete=0
			if [[ -s "${work_root}/backend-actual-revision.txt" ]]; then
				IFS= read -r actual_revision <"${work_root}/backend-actual-revision.txt" || complete=0
			fi
			if [[ -n "${expected_revision}" && -n "${actual_revision}" \
				&& "${actual_revision}" == "${expected_revision}" ]]; then
				ifa_fault_record_diagnostic_status \
					"${manifest}" backend-revision-match complete expected-equals-actual
			else
				ifa_fault_record_diagnostic_status \
					"${manifest}" backend-revision-match failed expected-does-not-equal-actual
				complete=0
			fi
			ifa_fault_capture_command "${manifest}" nornicdb-environment "${work_root}/nornicdb-environment.txt" \
				bash -c 'docker inspect "$1" | jq -r '\''.[0].Config.Env[] | select(test("^(NORNICDB_(NO_AUTH|DATA_DIR|HTTP_PORT|BOLT_PORT|ASYNC_WRITES_ENABLED|HEIMDALL_ENABLED|QDRANT_GRPC_ENABLED|EMBEDDING_ENABLED|EMBEDDING_PROVIDER|SEARCH_BM25_ENABLED|SEARCH_VECTOR_ENABLED|SEARCH_BM25_WARMING|SEARCH_VECTOR_WARMING|PERSIST_SEARCH_INDEXES)=)"))'\'' | LC_ALL=C sort' _ "${container_id}" || complete=0
		else
			ifa_fault_record_diagnostic_status "${manifest}" backend-container failed container-id-unavailable
			complete=0
		fi
	fi

	if [[ "${complete}" -eq 1 ]]; then
		printf 'complete\n' >"${work_root}/diagnostics-complete"
		return 0
	fi
	printf 'ifa fault diagnostics: one or more core snapshots are incomplete\n' >&2
	return 1
}

# ifa_fault_capture_failure_preserving_status is the EXIT-trap seam. It always
# returns the gate status it received, even if every diagnostic command fails.
ifa_fault_capture_failure_preserving_status() {
	local original_status="$1"
	shift
	ifa_fault_capture_failure_diagnostics "$@" || true
	return "${original_status}"
}
