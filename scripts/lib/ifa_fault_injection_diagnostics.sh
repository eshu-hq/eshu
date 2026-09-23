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
	printf 'dump\tartifact_sha256\tbytes\tnodes\tedges\tgcp_edges\n' >"${manifest}"
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

# ifa_fault_backend_selection accepts only the exact public immutable image and
# platform this proof lane validates. Alternative registries need their own
# public-safe proof mapping before their failure artifacts may be retained.
ifa_fault_backend_selection() {
	local compose_config="$1"
	ifa_fault_run_bounded jq -er '
		.services.nornicdb
		| select(
			.image == "ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-499-6ac958a9@sha256:fc90a2c3115d5dc0fe9a69ac676e5c77428bcfdcadc3320f2e99f887bea22f26"
			and .platform == "linux/amd64"
		)
		| [.image, .platform]
		| @tsv
	' "${compose_config}"
}

# ifa_fault_write_backend_provenance binds the immutable rendered index and
# platform to the container's configured reference and locally resolved image.
# Docker 28.0 lacks `image inspect --platform`, so the official v1.3.3 amd64
# child is an explicit proof mapping. Unmapped image overrides fail before
# identity artifacts are written, keeping private registry references out.
ifa_fault_write_backend_provenance() {
	local compose_config="$1" container_json="$2" runtime_image_json="$3"
	local output="$4" temporary="${4}.tmp"
	rm -f "${temporary}"
	ifa_fault_run_bounded jq -n \
		--slurpfile compose "${compose_config}" \
		--slurpfile container "${container_json}" \
		--slurpfile runtime "${runtime_image_json}" '
		def repository_from_image:
			split("@")[0]
			| split("/") as $parts
			| ($parts[-1] | sub(":[^:]+$"; "")) as $leaf
			| (($parts[0:-1] + [$leaf]) | join("/"));
		($compose[0].services.nornicdb.image // "") as $rendered_image
		| ($compose[0].services.nornicdb.platform // "") as $rendered_platform
		| (try ($rendered_image | capture("@(?<digest>sha256:[0-9a-f]{64})$").digest) catch "") as $index_digest
		| (if $rendered_image == "" then "" else ($rendered_image | repository_from_image) end) as $repository
		| (
			if $rendered_image == "ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-499-6ac958a9@sha256:fc90a2c3115d5dc0fe9a69ac676e5c77428bcfdcadc3320f2e99f887bea22f26"
				and $rendered_platform == "linux/amd64"
			then "sha256:a1fc8d7256d78a5cbe512f2bfaa2da607e0a453a555d3c0e14bb7bd546c20c9d"
			else $index_digest
			end
		) as $platform_digest
		| ($container[0].image_id // "") as $container_image_id
		| ($container[0].config_image // "") as $container_config_image
		| ($runtime[0].id // "") as $runtime_image_id
		| ($runtime[0].repo_digests // []) as $runtime_repo_digests
		| (($runtime[0].os // "") + "/" + ($runtime[0].architecture // "")) as $runtime_platform
		| [$repository + "@" + $index_digest, $repository + "@" + $platform_digest] as $accepted_digests
		| ([$runtime_repo_digests[] as $digest | select($accepted_digests | index($digest)) | $digest] | unique) as $matches
		| (
			$index_digest != ""
			and ($platform_digest | test("^sha256:[0-9a-f]{64}$"))
			and $rendered_platform == "linux/amd64"
			and $container_config_image == $rendered_image
			and $container_image_id == $runtime_image_id
			and $runtime_platform == $rendered_platform
			and ($matches | length) > 0
		) as $provenance_match
		| {
			rendered_image: $rendered_image,
			expected_repository: $repository,
			expected_index_digest: $index_digest,
			rendered_platform: $rendered_platform,
			expected_platform_digest: $platform_digest,
			container_config_image: $container_config_image,
			runtime_image_id: $runtime_image_id,
			runtime_repo_digests: $runtime_repo_digests,
			runtime_platform: $runtime_platform,
			accepted_runtime_repo_digest: ($matches[0] // null),
			backend_source_revision: "unavailable",
			provenance_match: $provenance_match
		}
	' >"${temporary}" || {
		rm -f "${temporary}"
		return 1
	}
	mv "${temporary}" "${output}"
	ifa_fault_run_bounded jq -e '.provenance_match == true' "${output}" >/dev/null
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
	local current_cell="" runtime_image_id=""
	local backend_selection=""
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
	rm -f "${work_root}/work-items.csv" "${work_root}/work-items.csv.error" \
		"${work_root}/gcp-facts.jsonl" "${work_root}/gcp-facts.jsonl.error"

	if [[ "${use_compose}" -eq 1 ]]; then
		rm -f "${logs}/compose-services.log" \
			"${logs}/compose-services.log.error" \
			"${work_root}/backend-image.txt" \
			"${work_root}/backend-image.txt.error" \
			"${work_root}/backend-compose-config.json" \
			"${work_root}/backend-compose-config.json.error" \
			"${work_root}/backend-container.json" \
			"${work_root}/backend-container.json.error" \
			"${work_root}/backend-runtime-image.json" \
			"${work_root}/backend-runtime-image.json.error" \
			"${work_root}/backend-provenance.json" \
			"${work_root}/nornicdb-environment.txt" \
			"${work_root}/nornicdb-environment.txt.error"
		if ifa_fault_capture_command "${manifest}" backend-compose-config \
			"${work_root}/backend-compose-config.json" \
			bash -o pipefail -c \
			'docker compose -p "$1" -f "$2" config --format json 2>/dev/null | jq -e '\''{services: {nornicdb: {image: .services.nornicdb.image, platform: .services.nornicdb.platform}}} | select(.services.nornicdb.image == "ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-499-6ac958a9@sha256:fc90a2c3115d5dc0fe9a69ac676e5c77428bcfdcadc3320f2e99f887bea22f26" and .services.nornicdb.platform == "linux/amd64")'\''' \
			_ "${compose_project}" "${compose_file}"; then
			backend_selection="$(ifa_fault_backend_selection \
				"${work_root}/backend-compose-config.json")" || true
			if [[ -n "${backend_selection}" ]]; then
				ifa_fault_record_diagnostic_status \
					"${manifest}" backend-selection complete immutable-linux-amd64
			else
				ifa_fault_record_diagnostic_status \
					"${manifest}" backend-selection failed unpinned-or-unsupported-platform
				complete=0
			fi
		else
			complete=0
		fi
	fi

	work_items_sql="COPY (SELECT work_item_id, stage, domain, scope_id, generation_id, status, attempt_count, failure_class, failure_message FROM fact_work_items ORDER BY stage, domain, scope_id, generation_id, work_item_id) TO STDOUT WITH (FORMAT csv, HEADER true);"
	gcp_facts_sql="SELECT jsonb_build_object('fact_id', fact_id, 'scope_id', scope_id, 'generation_id', generation_id, 'fact_kind', fact_kind, 'stable_fact_key', stable_fact_key, 'schema_version', schema_version, 'collector_kind', collector_kind, 'fencing_token', fencing_token, 'source_confidence', source_confidence, 'source_system', source_system, 'source_fact_key', source_fact_key, 'source_uri', source_uri, 'source_record_id', source_record_id, 'observed_at', observed_at, 'ingested_at', ingested_at, 'is_tombstone', is_tombstone, 'payload', payload) FROM fact_records WHERE fact_kind IN ('gcp_cloud_resource', 'gcp_cloud_relationship') ORDER BY scope_id, generation_id, observed_at, fact_id;"
	if [[ "${use_compose}" -eq 1 && -n "${backend_selection}" ]]; then
		ifa_fault_capture_command "${manifest}" work-items "${work_root}/work-items.csv" \
			docker compose -p "${compose_project}" -f "${compose_file}" exec -T postgres \
			psql -U eshu -d eshu -tA -c "${work_items_sql}" || complete=0
		ifa_fault_capture_command "${manifest}" gcp-facts "${work_root}/gcp-facts.jsonl" \
			docker compose -p "${compose_project}" -f "${compose_file}" exec -T postgres \
			psql -U eshu -d eshu -tA -c "${gcp_facts_sql}" || complete=0
	elif [[ "${use_compose}" -eq 0 ]]; then
		ifa_fault_capture_command "${manifest}" work-items "${work_root}/work-items.csv" \
			psql "${postgres_dsn}" -tA -c "${work_items_sql}" || complete=0
		ifa_fault_capture_command "${manifest}" gcp-facts "${work_root}/gcp-facts.jsonl" \
			psql "${postgres_dsn}" -tA -c "${gcp_facts_sql}" || complete=0
	else
		ifa_fault_record_diagnostic_status "${manifest}" work-items skipped untrusted-backend-selection
		ifa_fault_record_diagnostic_status "${manifest}" gcp-facts skipped untrusted-backend-selection
		complete=0
	fi

	if [[ "${use_compose}" -eq 1 ]]; then
		if [[ -n "${backend_selection}" ]]; then
			ifa_fault_capture_command "${manifest}" compose-logs "${logs}/compose-services.log" \
				docker compose -p "${compose_project}" -f "${compose_file}" logs --no-color || complete=0
			ifa_fault_capture_command "${manifest}" backend-image "${work_root}/backend-image.txt" \
				docker compose -p "${compose_project}" -f "${compose_file}" images nornicdb || complete=0
			container_id="$(ifa_fault_run_bounded \
				docker compose -p "${compose_project}" -f "${compose_file}" ps -q nornicdb 2>/dev/null)" || true
		fi
		if [[ -n "${backend_selection}" && -n "${container_id}" ]]; then
			ifa_fault_capture_command "${manifest}" backend-container "${work_root}/backend-container.json" \
				bash -c 'docker inspect "$1" | jq ".[0] | {image_id: .Image, config_image: .Config.Image, restart_count: .RestartCount, state: {status: .State.Status, running: .State.Running, started_at: .State.StartedAt, finished_at: .State.FinishedAt, exit_code: .State.ExitCode}, mounts: [.Mounts[] | {type: .Type, name: .Name, destination: .Destination, rw: .RW}]}"' _ "${container_id}" || complete=0
			runtime_image_id="$(ifa_fault_run_bounded jq -er \
				'.image_id | select(type == "string" and test("^sha256:[0-9a-f]{64}$"))' \
				"${work_root}/backend-container.json" 2>/dev/null)" || true
			if [[ -n "${runtime_image_id}" ]]; then
				ifa_fault_capture_command "${manifest}" backend-runtime-image \
					"${work_root}/backend-runtime-image.json" \
					bash -c 'docker image inspect "$1" | jq ".[0] | {id: .Id, repo_digests: (.RepoDigests // []), os: .Os, architecture: .Architecture}"' \
					_ "${runtime_image_id}" || complete=0
			else
				ifa_fault_record_diagnostic_status \
					"${manifest}" backend-runtime-image failed image-id-unavailable
				complete=0
			fi
			if [[ -s "${work_root}/backend-compose-config.json" \
				&& -s "${work_root}/backend-container.json" \
				&& -s "${work_root}/backend-runtime-image.json" ]] \
				&& ifa_fault_write_backend_provenance \
					"${work_root}/backend-compose-config.json" \
					"${work_root}/backend-container.json" \
					"${work_root}/backend-runtime-image.json" \
					"${work_root}/backend-provenance.json"; then
				ifa_fault_record_diagnostic_status \
					"${manifest}" backend-provenance-match complete expected-equals-runtime
			else
				ifa_fault_record_diagnostic_status \
					"${manifest}" backend-provenance-match failed expected-does-not-equal-runtime
				complete=0
			fi
			ifa_fault_capture_command "${manifest}" nornicdb-environment "${work_root}/nornicdb-environment.txt" \
				bash -c 'docker inspect "$1" | jq -r '\''.[0].Config.Env[] | select(test("^(NORNICDB_(NO_AUTH|DATA_DIR|HTTP_PORT|BOLT_PORT|ASYNC_WRITES_ENABLED|HEIMDALL_ENABLED|QDRANT_GRPC_ENABLED|EMBEDDING_ENABLED|EMBEDDING_PROVIDER|SEARCH_BM25_ENABLED|SEARCH_VECTOR_ENABLED|SEARCH_BM25_WARMING|SEARCH_VECTOR_WARMING|PERSIST_SEARCH_INDEXES)=)"))'\'' | LC_ALL=C sort' _ "${container_id}" || complete=0
		elif [[ -n "${backend_selection}" ]]; then
			ifa_fault_record_diagnostic_status "${manifest}" backend-container failed container-id-unavailable
			complete=0
		else
			ifa_fault_record_diagnostic_status "${manifest}" backend-identity skipped untrusted-backend-selection
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
