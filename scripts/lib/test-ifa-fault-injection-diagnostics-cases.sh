#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154,SC2329
# Hermetic regressions for fault-injection failure diagnostics.

test_ifa_fault_diagnostics_sha256() {
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		sha256sum "$1" | awk '{print $1}'
	fi
}

test_ifa_fault_capture_digest_reads_graph_once() (
	local case_dir count_file expected
	case_dir="$(mktemp -d -t ifa-fault-digest.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	mkdir -p "${case_dir}/bin"
	count_file="${case_dir}/calls"
	printf '0\n' >"${count_file}"

	cat >"${case_dir}/bin/eshu-ifa" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
count="$(cat "${IFA_TEST_CALL_COUNT}")"
printf '%s\n' "$((count + 1))" >"${IFA_TEST_CALL_COUNT}"
if [[ "$*" == *" -out "* ]]; then
	printf '{"edges":[],"nodes":[]}\n' >"$3"
else
	printf 'second-live-read\n'
fi
STUB
	chmod +x "${case_dir}/bin/eshu-ifa"

	source "${diagnostics_lib}"
	source "${driver_lib}"
	bin_dir="${case_dir}/bin"
	work_dir="${case_dir}"
	declare -A digests=()
	log() { :; }
	die() { printf '%s\n' "$*" >&2; return 1; }
	export IFA_TEST_CALL_COUNT="${count_file}"

	capture_digest baseline
	[[ "$(cat "${count_file}")" == "1" ]] \
		|| fail "capture_digest must use one graph read, got $(cat "${count_file}")"
	expected="$(test_ifa_fault_diagnostics_sha256 "${case_dir}/graph-baseline.dump")"
	[[ "${digests[baseline]}" == "${expected}" ]] \
		|| fail "capture_digest did not hash the retained canonical dump"
)

test_ifa_fault_graph_manifest_describes_retained_bytes() (
	local case_dir digest row dump_name bytes nodes edges gcp_edges
	case_dir="$(mktemp -d -t ifa-fault-manifest.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	printf '{"edges":[{"type":"GCP_TEST"},{"type":"OTHER"}],"nodes":[{},{}]}\n' \
		>"${case_dir}/graph-restartbackend.dump"
	source "${diagnostics_lib}"
	ifa_fault_write_graph_manifest "${case_dir}"
	row="$(sed -n '2p' "${case_dir}/graph-manifest.tsv")"
	IFS=$'\t' read -r dump_name digest bytes nodes edges gcp_edges <<<"${row}"
	[[ "${dump_name}" == "graph-restartbackend.dump" ]] || fail "graph manifest named ${dump_name}"
	[[ "${digest}" == "$(test_ifa_fault_diagnostics_sha256 "${case_dir}/graph-restartbackend.dump")" ]] \
		|| fail "graph manifest digest does not describe retained bytes"
	[[ "${bytes}" == "$(wc -c <"${case_dir}/graph-restartbackend.dump" | tr -d '[:space:]')" ]] \
		|| fail "graph manifest byte count is wrong"
	[[ "${nodes}:${edges}:${gcp_edges}" == "2:2:1" ]] \
		|| fail "graph manifest counts are ${nodes}:${edges}:${gcp_edges}, want 2:2:1"
)

test_ifa_fault_restart_failure_captures_unreachable_graph_dump() (
	local case_dir fake_bin count_file retained expected_digest manifest_row
	case_dir="$(mktemp -d -t ifa-fault-restart-failure-dump.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	fake_bin="${case_dir}/bin"
	count_file="${case_dir}/graph-dump-calls"
	mkdir -p "${fake_bin}" "${case_dir}/logs"
	printf '0\n' >"${count_file}"
	printf '{"edges":[],"nodes":[]}\n' >"${case_dir}/graph-baseline.dump"
	printf 'cell_restartbackend\n' >"${case_dir}/current-cell"
	printf '{}\n' >"${case_dir}/fault-restart-backend.json"
	printf 'fired\n' >"${case_dir}/restart-watch-result"
	printf '{"group_ordinal":1,"surface":"execute_group","statements":[{"cypher":"RETURN 1"}]}\n' \
		>"${case_dir}/fault-restart-backend.json.restart-sentinel.trigger.json"
	printf 'watcher\n' >"${case_dir}/logs/restart-watcher.log"
	printf 'reducer\n' >"${case_dir}/logs/reducer-restartbackend.log"
	printf 'projector\n' >"${case_dir}/logs/projector-restartbackend.log"
	cat >"${fake_bin}/eshu-ifa" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
count="$(cat "${IFA_TEST_CALL_COUNT}")"
printf '%s\n' "$((count + 1))" >"${IFA_TEST_CALL_COUNT}"
printf '{"edges":[{"type":"GCP_FAILURE"}],"nodes":[{}]}\n'
STUB
	cat >"${fake_bin}/psql" <<'STUB'
#!/usr/bin/env bash
printf 'fixture-row\n'
STUB
	chmod +x "${fake_bin}/eshu-ifa" "${fake_bin}/psql"

	source "${diagnostics_lib}"
	export IFA_TEST_CALL_COUNT="${count_file}"
	PATH="${fake_bin}:${PATH}" ifa_fault_capture_failure_diagnostics \
		"${case_dir}" "${case_dir}/logs" test-project unused-compose.yaml 0 test-dsn "${fake_bin}"

	retained="${case_dir}/graph-restartbackend-failure.dump"
	[[ "$(cat "${count_file}")" == 1 ]] \
		|| fail "restart failure diagnostics must perform exactly one graph read"
	[[ -s "${retained}" ]] \
		|| fail "drain-time restart failure did not retain a canonical graph dump"
	expected_digest="$(test_ifa_fault_diagnostics_sha256 "${retained}")"
	manifest_row="$(rg '^graph-restartbackend-failure[.]dump\t' "${case_dir}/graph-manifest.tsv")"
	[[ "${manifest_row}" == *$'\t'"${expected_digest}"$'\t'* ]] \
		|| fail "failure dump manifest does not hash the exact retained bytes"
	[[ "$(cat "${case_dir}/diagnostics-complete")" == complete ]] \
		|| fail "complete drain-time restart diagnostics did not publish the marker"
)

test_ifa_fault_restart_failure_reuses_regular_dump() (
	local case_dir fake_bin count_file
	case_dir="$(mktemp -d -t ifa-fault-restart-regular-dump.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	fake_bin="${case_dir}/bin"
	count_file="${case_dir}/graph-dump-calls"
	mkdir -p "${fake_bin}"
	printf '0\n' >"${count_file}"
	printf '{"edges":[],"nodes":[]}\n' >"${case_dir}/graph-restartbackend.dump"
	printf 'artifact\tstatus\tdetail\n' >"${case_dir}/manifest.tsv"
	cat >"${fake_bin}/eshu-ifa" <<'STUB'
#!/usr/bin/env bash
printf '1\n' >"${IFA_TEST_CALL_COUNT}"
exit 99
STUB
	chmod +x "${fake_bin}/eshu-ifa"
	source "${diagnostics_lib}"
	export IFA_TEST_CALL_COUNT="${count_file}"
	ifa_fault_capture_restart_failure_graph \
		"${case_dir}/manifest.tsv" "${case_dir}" cell_restartbackend "${fake_bin}"
	[[ "$(cat "${count_file}")" == 0 ]] \
		|| fail "existing regular restart dump triggered an additional live graph read"
	[[ ! -e "${case_dir}/graph-restartbackend-failure.dump" ]] \
		|| fail "existing regular restart dump produced a redundant failure dump"
)

test_ifa_fault_restart_failure_graph_read_is_bounded() (
	local case_dir fake_bin started elapsed rc
	case_dir="$(mktemp -d -t ifa-fault-restart-graph-timeout.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	fake_bin="${case_dir}/bin"
	mkdir -p "${fake_bin}"
	printf 'artifact\tstatus\tdetail\n' >"${case_dir}/manifest.tsv"
	cat >"${fake_bin}/eshu-ifa" <<'STUB'
#!/usr/bin/env bash
sleep 5
STUB
	chmod +x "${fake_bin}/eshu-ifa"
	source "${diagnostics_lib}"
	started="${SECONDS}"
	set +e
	IFA_FAULT_DIAGNOSTIC_TIMEOUT=1 ifa_fault_capture_restart_failure_graph \
		"${case_dir}/manifest.tsv" "${case_dir}" cell_restartbackend "${fake_bin}" \
		2>/dev/null
	rc=$?
	set -e
	elapsed=$((SECONDS - started))
	[[ "${rc}" -ne 0 ]] || fail "timed-out restart failure graph read succeeded"
	[[ "${elapsed}" -lt 5 ]] || fail "restart failure graph read exceeded its hard deadline"
	[[ ! -s "${case_dir}/graph-restartbackend-failure.dump" ]] \
		|| fail "timed-out restart failure graph read retained a partial dump"
)

test_ifa_fault_cleanup_preserves_original_failure_status() (
	local case_dir rc
	case_dir="$(mktemp -d -t ifa-fault-diagnostics.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	source "${diagnostics_lib}"
	ifa_fault_capture_failure_diagnostics() { return 9; }
	set +e
	ifa_fault_capture_failure_preserving_status 37 \
		"${case_dir}" "${case_dir}/logs" test-project docker-compose.yaml 1 test-dsn \
		2>/dev/null
	rc=$?
	set -e
	[[ "${rc}" -eq 37 ]] || fail "cleanup returned ${rc}, want original status 37"
)

test_ifa_fault_diagnostics_are_bounded() (
	local case_dir started elapsed rc
	case_dir="$(mktemp -d -t ifa-fault-timeout.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	source "${diagnostics_lib}"
	started="${SECONDS}"
	set +e
	IFA_FAULT_DIAGNOSTIC_TIMEOUT=1 ifa_fault_capture_command \
		"${case_dir}/manifest.tsv" slow "${case_dir}/slow.txt" bash -c 'sleep 3; printf late' \
		2>/dev/null
	rc=$?
	set -e
	elapsed=$((SECONDS - started))
	[[ "${rc}" -ne 0 ]] || fail "timed-out diagnostic command unexpectedly succeeded"
	[[ "${elapsed}" -lt 3 ]] || fail "diagnostic command was not bounded (elapsed ${elapsed}s)"
)

test_ifa_fault_diagnostics_completeness_marker_is_fail_closed() (
	local case_dir
	case_dir="$(mktemp -d -t ifa-fault-complete.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	source "${diagnostics_lib}"
	set +e
	ifa_fault_capture_failure_diagnostics \
		"${case_dir}" "${case_dir}/logs" test-project missing-compose.yaml 1 test-dsn \
		2>/dev/null
	set -e
	[[ ! -e "${case_dir}/diagnostics-complete" ]] \
		|| fail "incomplete diagnostics published a completeness marker"
	[[ -s "${case_dir}/diagnostics-manifest.tsv" ]] \
		|| fail "incomplete diagnostics did not retain a status manifest"
)

test_ifa_fault_diagnostics_completeness_marker_requires_core_snapshots() (
	local case_dir fake_bin
	case_dir="$(mktemp -d -t ifa-fault-complete-success.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	fake_bin="${case_dir}/bin"
	mkdir -p "${fake_bin}"
	printf '{"edges":[],"nodes":[]}\n' >"${case_dir}/graph-baseline.dump"
	printf 'cell_baseline\n' >"${case_dir}/current-cell"
	cat >"${fake_bin}/psql" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf 'fixture-row\n'
STUB
	chmod +x "${fake_bin}/psql"
	source "${diagnostics_lib}"
	PATH="${fake_bin}:${PATH}" ifa_fault_capture_failure_diagnostics \
		"${case_dir}" "${case_dir}/logs" test-project unused-compose.yaml 0 test-dsn
	[[ "$(cat "${case_dir}/diagnostics-complete")" == complete ]] \
		|| fail "complete core snapshots did not publish the completeness marker"
)

test_ifa_fault_compose_diagnostics_capture_safe_backend_state() (
	local case_dir fake_bin
	case_dir="$(mktemp -d -t ifa-fault-compose-success.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	fake_bin="${case_dir}/bin"
	mkdir -p "${fake_bin}"
	printf '{"edges":[],"nodes":[]}\n' >"${case_dir}/graph-baseline.dump"
	printf 'cell_baseline\n' >"${case_dir}/current-cell"
	# Keep this 1,069-byte body out of a heredoc: Homebrew bash >= 5.1 can
	# deadlock writing bodies over macOS's 512-byte pipe buffer (#5074).
	cp "${diagnostics_fake_docker_lib}" "${fake_bin}/docker"
	chmod +x "${fake_bin}/docker"
	source "${diagnostics_lib}"
	PATH="${fake_bin}:${PATH}" ifa_fault_capture_failure_diagnostics \
		"${case_dir}" "${case_dir}/logs" test-project compose.yaml 1 test-dsn
	jq -e '.restart_count == 1 and .mounts[0].name == "data-volume" and (.mounts[0] | has("source") | not)' \
		"${case_dir}/backend-container.json" >/dev/null \
		|| fail "backend container artifact omits required state or leaks the host mount source"
	rg --fixed-strings --quiet -- 'NORNICDB_DATA_DIR=/data' "${case_dir}/nornicdb-environment.txt" \
		|| fail "backend environment omitted an allowlisted durability setting"
	if rg --fixed-strings --quiet -- 'NORNICDB_ADMIN_TOKEN' "${case_dir}/nornicdb-environment.txt"; then
		fail "backend environment captured a non-allowlisted secret"
	fi
	[[ "$(cat "${case_dir}/backend-expected-revision.txt")" == 1111111111111111111111111111111111111111 ]] \
		|| fail "backend expected revision was not derived from rendered Compose config"
	[[ "$(cat "${case_dir}/backend-actual-revision.txt")" == 1111111111111111111111111111111111111111 ]] \
		|| fail "running backend revision was not retained independently"
)

test_ifa_fault_restart_completeness_requires_each_boundary_artifact() (
	local case_dir manifest missing
	local -a required=(
		graph-baseline.dump
		graph-restartbackend.dump
		fault-restart-backend.json
		restart-watch-result
		fault-restart-backend.json.restart-sentinel.trigger.json
		logs/restart-watcher.log
		logs/reducer-restartbackend.log
		logs/projector-restartbackend.log
	)
	case_dir="$(mktemp -d -t ifa-fault-restart-required.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	mkdir -p "${case_dir}/logs"
	manifest="${case_dir}/manifest.tsv"
	source "${diagnostics_lib}"
	write_required_restart_artifacts() {
		printf '{"edges":[],"nodes":[]}\n' >"${case_dir}/graph-baseline.dump"
		printf '{"edges":[],"nodes":[]}\n' >"${case_dir}/graph-restartbackend.dump"
		printf '{}\n' >"${case_dir}/fault-restart-backend.json"
		printf 'fired\n' >"${case_dir}/restart-watch-result"
		printf '{"group_ordinal":1,"surface":"execute_group","statements":[{"cypher":"RETURN 1"}]}\n' \
			>"${case_dir}/fault-restart-backend.json.restart-sentinel.trigger.json"
		printf 'watcher\n' >"${case_dir}/logs/restart-watcher.log"
		printf 'reducer\n' >"${case_dir}/logs/reducer-restartbackend.log"
		printf 'projector\n' >"${case_dir}/logs/projector-restartbackend.log"
	}
	write_required_restart_artifacts
	for missing in "${required[@]}"; do
		rm -f "${case_dir}/${missing}"
		printf 'artifact\tstatus\tdetail\n' >"${manifest}"
		if ifa_fault_validate_cell_artifacts "${manifest}" "${case_dir}" cell_restartbackend; then
			fail "restart diagnostics accepted missing required artifact ${missing}"
		fi
		write_required_restart_artifacts
	done
)

test_ifa_fault_graph_manifest_timeout_fails_closed() (
	local case_dir fake_bin started elapsed
	case_dir="$(mktemp -d -t ifa-fault-manifest-timeout.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	fake_bin="${case_dir}/bin"
	mkdir -p "${fake_bin}"
	printf '{"edges":[],"nodes":[]}\n' >"${case_dir}/graph-baseline.dump"
	printf 'cell_baseline\n' >"${case_dir}/current-cell"
	cat >"${fake_bin}/shasum" <<'STUB'
#!/usr/bin/env bash
sleep 5
STUB
	cat >"${fake_bin}/psql" <<'STUB'
#!/usr/bin/env bash
printf 'fixture-row\n'
STUB
	chmod +x "${fake_bin}/shasum" "${fake_bin}/psql"
	source "${diagnostics_lib}"
	started="${SECONDS}"
	set +e
	PATH="${fake_bin}:${PATH}" IFA_FAULT_DIAGNOSTIC_TIMEOUT=1 \
		ifa_fault_capture_failure_diagnostics \
		"${case_dir}" "${case_dir}/logs" test-project unused-compose.yaml 0 test-dsn \
		2>/dev/null
	set -e
	elapsed=$((SECONDS - started))
	[[ "${elapsed}" -lt 5 ]] || fail "top-level graph diagnostics ignored the timeout"
	[[ ! -e "${case_dir}/diagnostics-complete" ]] \
		|| fail "timed-out graph manifest published a completeness marker"
)

test_ifa_fault_failure_artifact_contract() {
	local workflow="${repo_root}/.github/workflows/ifa-determinism-gate.yml"
	local needle
	for needle in \
		'run: bash scripts/verify-ifa-fault-injection.sh --keep --shard "${IFA_FAULT_SHARD}/4"' \
		'uses: actions/upload-artifact@v4' \
		'name: ifa-fault-injection-shard-${{ matrix.shard }}-attempt-${{ github.run_attempt }}-failure' \
		'/tmp/ifa-fault-injection.*/graph-baseline.dump' \
		'/tmp/ifa-fault-injection.*/graph-restartbackend.dump' \
		'/tmp/ifa-fault-injection.*/graph-restartbackend-failure.dump' \
		'/tmp/ifa-fault-injection.*/graph-manifest.tsv' \
		'/tmp/ifa-fault-injection.*/work-items.csv' \
		'/tmp/ifa-fault-injection.*/gcp-facts.jsonl' \
		'/tmp/ifa-fault-injection.*/fault-restart-backend.json' \
		'/tmp/ifa-fault-injection.*/restart-watch-result' \
		'/tmp/ifa-fault-injection.*/fault-restart-backend.json.restart-sentinel.trigger.json' \
		'/tmp/ifa-fault-injection.*/backend-image.txt' \
		'/tmp/ifa-fault-injection.*/backend-labels.json' \
		'/tmp/ifa-fault-injection.*/backend-container.json' \
		'/tmp/ifa-fault-injection.*/backend-expected-revision.txt' \
		'/tmp/ifa-fault-injection.*/backend-actual-revision.txt' \
		'/tmp/ifa-fault-injection.*/nornicdb-environment.txt' \
		'/tmp/ifa-fault-injection.*/diagnostics-manifest.tsv' \
		'/tmp/ifa-fault-injection.*/diagnostics-complete' \
		'/tmp/ifa-fault-injection.*/current-cell' \
		'/tmp/ifa-fault-injection.*/logs/*.log' \
		'if: failure()' \
		'if-no-files-found: error' \
		'retention-days: 7'; do
		rg --fixed-strings --quiet -- "${needle}" "${workflow}" \
			|| fail "fault workflow does not preserve diagnostic artifact: ${needle}"
	done
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
	rg --fixed-strings --quiet -- "org.opencontainers.image.revision" "${diagnostics_lib}" \
		|| fail "failure diagnostics do not retain the backend source revision label"
	rg --fixed-strings --quiet -- 'config --format json' "${diagnostics_lib}" \
		|| fail "diagnostics do not derive the backend revision from rendered Compose config"
	rg --fixed-strings --quiet -- 'NORNICDB_(NO_AUTH|DATA_DIR|HTTP_PORT|BOLT_PORT|ASYNC_WRITES_ENABLED|' "${diagnostics_lib}" \
		|| fail "NornicDB environment capture is not an explicit allowlist"
	rg --fixed-strings --quiet -- 'command -v gtimeout' "${diagnostics_lib}" \
		|| fail "bounded diagnostics do not support macOS coreutils"
	rg --fixed-strings --quiet -- "perl -e 'alarm shift; exec @ARGV or exit 127'" "${diagnostics_lib}" \
		|| fail "bounded diagnostics do not support stock macOS"
	if rg --fixed-strings --quiet -- "rg '^NORNICDB_'" "${diagnostics_lib}"; then
		fail "NornicDB environment capture reverted to an open prefix match"
	fi
	rg --fixed-strings --quiet -- "'go/internal/storage/cypher/fault_executor*.go'" "${workflow}" \
		|| fail "workflow does not trigger on every fault-executor module"
	rg --fixed-strings --quiet -- '"go/internal/storage/cypher/fault_executor*.go"' "${repo_root}/specs/ci-gates.v1.yaml" \
		|| fail "CI registry does not trigger on every fault-executor module"
}

run_ifa_fault_injection_diagnostics_cases() {
	test_ifa_fault_capture_digest_reads_graph_once
	test_ifa_fault_graph_manifest_describes_retained_bytes
	test_ifa_fault_restart_failure_captures_unreachable_graph_dump
	test_ifa_fault_restart_failure_reuses_regular_dump
	test_ifa_fault_restart_failure_graph_read_is_bounded
	test_ifa_fault_cleanup_preserves_original_failure_status
	test_ifa_fault_diagnostics_are_bounded
	test_ifa_fault_diagnostics_completeness_marker_is_fail_closed
	test_ifa_fault_diagnostics_completeness_marker_requires_core_snapshots
	test_ifa_fault_compose_diagnostics_capture_safe_backend_state
	test_ifa_fault_restart_completeness_requires_each_boundary_artifact
	test_ifa_fault_graph_manifest_timeout_fails_closed
	test_ifa_fault_failure_artifact_contract
}
