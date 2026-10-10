#!/usr/bin/env bash
# IFA fault diagnostic upload contract and mutation cases.
# The parent mirror defines fail(), repo_root, script, and diagnostics_lib.
# shellcheck disable=SC2034,SC2154

# Check the upload step's own condition, not a free-floating mention of the
# failure guard elsewhere in the workflow. The queue selector may wrap the
# guard, but it must remain a top-level AND term so diagnostics upload cannot
# start after a successful fault shard.
ifa_fault_upload_is_failure_only() {
	local workflow_text="$1" upload_step condition guard suffix
	[[ "$(printf '%s\n' "${workflow_text}" | rg -c '^      - name: Upload fault-injection diagnostics$')" == 1 ]] || return 1
	upload_step="$(printf '%s\n' "${workflow_text}" | awk '
		/^      - name: Upload fault-injection diagnostics$/ { inside = 1; next }
		inside && /^      - name: / { exit }
		inside { print }
	')"
	condition="$(printf '%s\n' "${upload_step}" | rg '^        if: ')" || return 1
	[[ "${condition}" != *$'\n'* ]] || return 1
	condition="${condition#        if: }"
	guard="failure() && steps.fault_matrix.outcome == 'failure'"
	if [[ "${condition}" == "${guard}" || "${condition}" == "\${{ ${guard} }}" ]]; then
		return 0
	fi
	suffix=") && (${guard}) }}"
	[[ "${condition}" == '${{ ('* && "${condition}" == *"${suffix}" ]]
}

# Mutation cases pin the dangerous direction: a queue wrapper cannot turn the
# failure guard into an OR, drop it, or leave it on a different step.
test_ifa_fault_upload_condition_mutations() {
	local upload_name='      - name: Upload fault-injection diagnostics'
	local guard="failure() && steps.fault_matrix.outcome == 'failure'"
	local good_wrapped bad_or bad_missing bad_other_step
	good_wrapped="${upload_name}"$'\n'"        if: \${{ (github.event_name != 'merge_group' || selected) && (${guard}) }}"
	bad_or="${upload_name}"$'\n'"        if: \${{ selected || (${guard}) }}"
	bad_missing="${upload_name}"$'\n'"        if: \${{ selected }}"
	bad_other_step='      - name: Some other step'$'\n'"        if: \${{ ${guard} }}"$'\n'"${upload_name}"$'\n'"        uses: actions/upload-artifact@v4"
	ifa_fault_upload_is_failure_only "${good_wrapped}" || fail "wrapped fault upload failure guard was rejected"
	ifa_fault_upload_is_failure_only "${upload_name}"$'\n'"        if: ${guard}" \
		|| fail "unwrapped fault upload failure guard was rejected"
	if ifa_fault_upload_is_failure_only "${bad_or}"; then
		fail "fault upload OR condition can run after a successful shard"
	fi
	if ifa_fault_upload_is_failure_only "${bad_missing}"; then
		fail "fault upload with no failure guard was accepted"
	fi
	if ifa_fault_upload_is_failure_only "${bad_other_step}"; then
		fail "fault upload borrowed another step's failure guard"
	fi
}

test_ifa_fault_failure_artifact_contract() {
	local workflow="${repo_root}/.github/workflows/ifa-determinism-gate.yml"
	local needle
	for needle in \
		'run: bash scripts/verify-ifa-fault-injection.sh --keep --shard "${IFA_FAULT_SHARD}/4"' \
		'uses: actions/upload-artifact@v4' \
		'name: ifa-fault-injection-shard-${{ matrix.shard }}-attempt-${{ github.run_attempt }}-failure' \
		'/tmp/ifa-fault-injection.*/graph-*.dump' \
		'/tmp/ifa-fault-injection.*/graph-manifest.tsv' \
		'/tmp/ifa-fault-injection.*/work-items.csv' \
		'/tmp/ifa-fault-injection.*/gcp-facts.jsonl' \
		'/tmp/ifa-fault-injection.*/fault-restart-backend.json' \
		'/tmp/ifa-fault-injection.*/restart-watch-result' \
		'/tmp/ifa-fault-injection.*/fault-restart-backend.json.restart-sentinel.trigger.json' \
		'/tmp/ifa-fault-injection.*/backend-image.txt' \
		'/tmp/ifa-fault-injection.*/backend-compose-config.json' \
		'/tmp/ifa-fault-injection.*/backend-container.json' \
		'/tmp/ifa-fault-injection.*/backend-runtime-image.json' \
		'/tmp/ifa-fault-injection.*/backend-provenance.json' \
		'/tmp/ifa-fault-injection.*/nornicdb-environment.txt' \
		'/tmp/ifa-fault-injection.*/diagnostics-manifest.tsv' \
		'/tmp/ifa-fault-injection.*/diagnostics-complete' \
		'/tmp/ifa-fault-injection.*/current-cell' \
		'/tmp/ifa-fault-injection.*/logs/*.log' \
		'if-no-files-found: error' \
		'retention-days: 7'; do
		rg --fixed-strings --quiet -- "${needle}" "${workflow}" \
			|| fail "fault workflow does not preserve diagnostic artifact: ${needle}"
	done
	ifa_fault_upload_is_failure_only "$(cat "${workflow}")" \
		|| fail "fault diagnostic upload can run without a failed fault matrix"
	# #6162 follow-up: a literal per-cell name here (instead of the glob above)
	# would silently drop every OTHER cell's graph-<cell>.dump (expirelease,
	# killworker, etc.) from the uploaded artifact.
	if rg --fixed-strings --quiet -- '/tmp/ifa-fault-injection.*/graph-baseline.dump' "${workflow}"; then
		fail "fault workflow pins a literal per-cell dump name again instead of the graph-*.dump glob"
	fi
	rg --fixed-strings --quiet -- 'FAULT_COMPOSE_PROJECT: eshu-ifa-fault-injection-${{ github.run_id }}-${{ github.run_attempt }}-${{ matrix.shard }}' "${workflow}" \
		|| fail "fault job does not pin a stable per-shard Compose project"
	rg --fixed-strings --quiet -- 'docker compose -p "${FAULT_COMPOSE_PROJECT}" -f docker-compose.yaml down -v' "${workflow}" \
		|| fail "workflow teardown does not target the retained fault stack"
	rg --fixed-strings --quiet -- 'name: Verify fault-injection diagnostic completeness' "${workflow}" \
		|| fail "workflow does not fail closed when diagnostic collection is incomplete"
	local verify_line upload_line teardown_line
	verify_line="$(rg -n --fixed-strings -- 'name: Verify fault-injection diagnostic completeness' "${workflow}" | cut -d: -f1)"
	upload_line="$(rg -n --fixed-strings -- 'name: Upload fault-injection diagnostics' "${workflow}" | cut -d: -f1)"
	teardown_line="$(rg -n --fixed-strings -- 'name: Tear down Docker services' "${workflow}" | tail -1 | cut -d: -f1)"
	[[ "${verify_line}" -lt "${upload_line}" && "${upload_line}" -lt "${teardown_line}" ]] \
		|| fail "diagnostic verify/upload/teardown ordering is unsafe"

	rg --fixed-strings --quiet -- 'ifa_fault_capture_failure_preserving_status "${status}"' "${script}" \
		|| fail "cleanup does not use the executable exit-status-preservation seam"
	rg --fixed-strings --quiet -- '"${use_compose:-0}" "${ESHU_POSTGRES_DSN:-}" "${bin_dir:-}"' "${script}" \
		|| fail "cleanup does not pass the built graph-dump binary to failure diagnostics"
	rg --fixed-strings --quiet -- 'docker compose -p "${compose_project}" -f "${compose_file}" logs --no-color' "${diagnostics_lib}" \
		|| fail "failure diagnostics do not retain complete Compose service logs"
	rg --fixed-strings --quiet -- 'COPY (SELECT work_item_id, stage, domain, scope_id, generation_id, status, attempt_count, failure_class, failure_message' "${diagnostics_lib}" \
		|| fail "failure diagnostics do not retain durable work-item state"
	local envelope_field
	for envelope_field in fact_id scope_id generation_id fact_kind stable_fact_key schema_version \
		collector_kind fencing_token source_confidence source_system source_fact_key source_uri \
		source_record_id observed_at ingested_at is_tombstone payload; do
		rg --fixed-strings --quiet -- "'${envelope_field}', ${envelope_field}" "${diagnostics_lib}" \
			|| fail "failure diagnostics omit fact envelope field ${envelope_field}"
	done
	rg --fixed-strings --quiet -- "fact_kind IN ('gcp_cloud_resource', 'gcp_cloud_relationship')" "${diagnostics_lib}" \
		|| fail "failure diagnostics do not retain the durable GCP fact inputs"
	rg --fixed-strings --quiet -- 'backend_source_revision: "unavailable"' "${diagnostics_lib}" \
		|| fail "failure diagnostics do not record unavailable backend source revision honestly"
	rg --fixed-strings --quiet -- 'fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1' "${diagnostics_lib}" \
		|| fail "failure diagnostics do not require the proven immutable backend index"
	rg --fixed-strings --quiet -- 'config --format json' "${diagnostics_lib}" \
		|| fail "diagnostics do not derive backend provenance from rendered Compose config"
	rg --fixed-strings --quiet -- 'NORNICDB_(NO_AUTH|DATA_DIR|HTTP_PORT|BOLT_PORT|ASYNC_WRITES_ENABLED|' "${diagnostics_lib}" \
		|| fail "NornicDB environment capture is not an explicit allowlist"
	rg --fixed-strings --quiet -- 'command -v gtimeout' "${diagnostics_lib}" \
		|| fail "bounded diagnostics do not support macOS coreutils"
	rg --fixed-strings --quiet -- "perl -e 'alarm shift; exec @ARGV or exit 127'" "${diagnostics_lib}" \
		|| fail "bounded diagnostics do not support stock macOS"
	if rg --fixed-strings --quiet -- "rg '^NORNICDB_'" "${diagnostics_lib}"; then
		fail "NornicDB environment capture reverted to an open prefix match"
	fi
	rg --fixed-strings --quiet -- "'go/internal/storage/cypher/fault/executor/*.go'" "${workflow}" \
		|| fail "workflow does not trigger on every fault-executor module"
	rg --fixed-strings --quiet -- '"go/internal/storage/cypher/fault/executor/*.go"' "${repo_root}/specs/ci-gates.v1.yaml" \
		|| fail "CI registry does not trigger on every fault-executor module"
}
