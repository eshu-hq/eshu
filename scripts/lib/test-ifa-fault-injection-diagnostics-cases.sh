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
	local case_dir digest header row dump_name bytes nodes edges gcp_edges
	case_dir="$(mktemp -d -t ifa-fault-manifest.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	printf '{"edges":[{"type":"GCP_TEST"},{"type":"OTHER"}],"nodes":[{},{}]}\n' \
		>"${case_dir}/graph-restartbackend.dump"
	source "${diagnostics_lib}"
	ifa_fault_write_graph_manifest "${case_dir}"
	header="$(sed -n '1p' "${case_dir}/graph-manifest.tsv")"
	[[ "${header}" == $'dump\tartifact_sha256\tbytes\tnodes\tedges\tgcp_edges' ]] \
		|| fail "graph manifest header is ${header}, want artifact_sha256 to name the retained-byte hashing"
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
	local artifact case_dir fake_bin
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
	jq -e '
		.services == {
			nornicdb: {
				image: "ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1",
				platform: "linux/amd64"
			}
		}
	' "${case_dir}/backend-compose-config.json" >/dev/null \
		|| fail "backend Compose artifact is not limited to the safe provenance fields"
	if rg --fixed-strings --quiet -- 'compose-secret' "${case_dir}/backend-compose-config.json" \
		|| rg --fixed-strings --quiet -- '/private/compose/path' "${case_dir}/backend-compose-config.json"; then
		fail "backend Compose artifact retained an interpolated secret or host path"
	fi
	for artifact in \
		"${case_dir}/backend-compose-config.json" \
		"${case_dir}/backend-container.json" \
		"${case_dir}/backend-runtime-image.json" \
		"${case_dir}/backend-provenance.json" \
		"${case_dir}/nornicdb-environment.txt" \
		"${case_dir}/logs/compose-services.log"; do
		if rg --fixed-strings --quiet -- 'compose-secret' "${artifact}" \
			|| rg --fixed-strings --quiet -- '/private/compose/path' "${artifact}"; then
			fail "uploaded diagnostic artifact ${artifact##*/} retained a Compose secret or host path"
		fi
	done
	jq -e '.restart_count == 1 and .mounts[0].name == "data-volume" and (.mounts[0] | has("source") | not)' \
		"${case_dir}/backend-container.json" >/dev/null \
		|| fail "backend container artifact omits required state or leaks the host mount source"
	rg --fixed-strings --quiet -- 'NORNICDB_DATA_DIR=/data' "${case_dir}/nornicdb-environment.txt" \
		|| fail "backend environment omitted an allowlisted durability setting"
	if rg --fixed-strings --quiet -- 'NORNICDB_ADMIN_TOKEN' "${case_dir}/nornicdb-environment.txt"; then
		fail "backend environment captured a non-allowlisted secret"
	fi
	jq -e '
		.rendered_image == "ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1"
		and .expected_index_digest == "sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1"
		and .rendered_platform == "linux/amd64"
		and .runtime_platform == "linux/amd64"
		and .accepted_runtime_repo_digest == "ghcr.io/eshu-hq/nornicdb-amd64-cpu@sha256:c4a2116e3c1547f750426d5c6c7fae618135f2fc1a3f7bf13fd7811a9c189915"
		and .backend_source_revision == "unavailable"
	' "${case_dir}/backend-provenance.json" >/dev/null \
		|| fail "backend provenance did not bind the rendered pinned image to its amd64 artifact"
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

# shellcheck source=scripts/lib/test-ifa-fault-injection-artifact-contract-cases.sh
source "${repo_root}/scripts/lib/test-ifa-fault-injection-artifact-contract-cases.sh"

# test_ifa_fault_injection_go_mod_prewarm (#6162 follow-up): pre-warm the Go
# module cache with the shared download-only retry helper between setup-go and
# the first build/test step, so a dropped proxy stream surfaces here instead
# of as a fault- or race-shaped red (#6075's failure mode; runs 34627429845,
# 34007134864, 33551099795, 34370706054).
ifa_fault_prewarm_step_has_retry() {
	local workflow_text="$1" step
	step="$(printf '%s\n' "${workflow_text}" | awk '
		/^      - name: Pre-warm Go modules$/ { inside = 1; step = ""; next }
		inside && /^      - name: / { inside = 0 }
		inside { step = step $0 "\n" }
		END { printf "%s", step }
	')"
	printf '%s\n' "${step}" | rg --quiet --fixed-strings --line-regexp -- '        run: scripts/ci/go-mod-download-retry.sh'
}

test_ifa_fault_prewarm_step_mutation() {
	local good bad earlier_good_fault_missing
	good=$'      - name: Pre-warm Go modules\n        if: ${{ selected }}\n        run: scripts/ci/go-mod-download-retry.sh\n      - name: Run unit tests'
	bad=$'      - name: Pre-warm Go modules\n        if: ${{ selected }}\n      - name: Later step\n        run: scripts/ci/go-mod-download-retry.sh'
	earlier_good_fault_missing=$'      - name: Pre-warm Go modules\n        run: scripts/ci/go-mod-download-retry.sh\n      - name: Run other job\n      - name: Pre-warm Go modules\n        if: ${{ selected }}\n      - name: Run fault unit tests'
	ifa_fault_prewarm_step_has_retry "${good}" || fail "pre-warm step with queue condition was rejected"
	if ifa_fault_prewarm_step_has_retry "${bad}"; then
		fail "a later step cannot satisfy the pre-warm retry-helper contract"
	fi
	if ifa_fault_prewarm_step_has_retry "${earlier_good_fault_missing}"; then
		fail "an earlier job's pre-warm cannot satisfy the fault job's pre-warm contract"
	fi
}

test_ifa_fault_injection_go_mod_prewarm() {
	local workflow="${repo_root}/.github/workflows/ifa-determinism-gate.yml"
	local setup_line prewarm_line unit_test_line shard_line
	# fault-injection is the LAST Go-building job, so "Set up Go" / "Pre-warm Go
	# modules" last-match here (same idiom as "Tear down Docker services"
	# below); `|| true` stops pipefail turning a no-match rg into a set -e
	# abort before the fail() checks below can report it.
	setup_line="$(rg -n --fixed-strings -- 'name: Set up Go' "${workflow}" | tail -1 | cut -d: -f1)" || true
	prewarm_line="$(rg -n --fixed-strings -- 'name: Pre-warm Go modules' "${workflow}" | tail -1 | cut -d: -f1)" || true
	unit_test_line="$(rg -n --fixed-strings -- 'name: Run tagged fault-classification unit tests' "${workflow}" | cut -d: -f1)" || true
	shard_line="$(rg -n --fixed-strings -- 'name: Run Ifa fault-injection matrix shard' "${workflow}" | cut -d: -f1)" || true
	[[ -n "${setup_line}" && -n "${prewarm_line}" && -n "${unit_test_line}" && -n "${shard_line}" ]] \
		|| fail "fault-injection job is missing one of: Set up Go / Pre-warm Go modules / tagged unit tests / matrix shard steps"
	[[ "${setup_line}" -lt "${prewarm_line}" && "${prewarm_line}" -lt "${unit_test_line}" && "${unit_test_line}" -lt "${shard_line}" ]] \
		|| fail "fault-injection job does not pre-warm Go modules between setup-go and its first go build/test step"
	ifa_fault_prewarm_step_has_retry "$(cat "${workflow}")" \
		|| fail "Pre-warm Go modules step does not run the shared retry helper"
}

run_ifa_fault_injection_diagnostics_cases() {
	test_ifa_fault_upload_condition_mutations
	test_ifa_fault_prewarm_step_mutation
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
	test_ifa_fault_injection_go_mod_prewarm
}
