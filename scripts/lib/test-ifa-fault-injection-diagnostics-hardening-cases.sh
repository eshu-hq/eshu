#!/usr/bin/env bash
# Revision provenance and timeout-input regressions for failure diagnostics.

test_ifa_fault_prepare_revision_case() {
	local case_dir="$1" fake_bin="${1}/bin"
	mkdir -p "${fake_bin}"
	printf '{"edges":[],"nodes":[]}\n' >"${case_dir}/graph-baseline.dump"
	printf 'cell_baseline\n' >"${case_dir}/current-cell"
	cp "${diagnostics_fake_docker_lib}" "${fake_bin}/docker"
	chmod +x "${fake_bin}/docker"
}

test_ifa_fault_backend_revision_mismatch_retains_both_sides() (
	local case_dir fake_bin rc
	case_dir="$(mktemp -d -t ifa-fault-revision-mismatch.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	fake_bin="${case_dir}/bin"
	test_ifa_fault_prepare_revision_case "${case_dir}"
	source "${diagnostics_lib}"
	set +e
	PATH="${fake_bin}:${PATH}" \
		IFA_TEST_ACTUAL_BACKEND_REVISION=2222222222222222222222222222222222222222 \
		ifa_fault_capture_failure_diagnostics \
		"${case_dir}" "${case_dir}/logs" test-project compose.yaml 1 test-dsn \
		2>/dev/null
	rc=$?
	set -e
	[[ "${rc}" -ne 0 ]] || fail "backend revision mismatch did not fail closed"
	[[ "$(cat "${case_dir}/backend-expected-revision.txt")" == 1111111111111111111111111111111111111111 ]] \
		|| fail "revision mismatch did not retain the Compose-expected revision"
	[[ "$(cat "${case_dir}/backend-actual-revision.txt")" == 2222222222222222222222222222222222222222 ]] \
		|| fail "revision mismatch did not retain the running container revision"
	[[ ! -e "${case_dir}/diagnostics-complete" ]] \
		|| fail "revision mismatch published diagnostics-complete"
)

test_ifa_fault_backend_image_override_requires_explicit_revision() (
	local case_dir fake_bin rc
	case_dir="$(mktemp -d -t ifa-fault-revision-override.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	fake_bin="${case_dir}/bin"
	test_ifa_fault_prepare_revision_case "${case_dir}"
	source "${diagnostics_lib}"
	PATH="${fake_bin}:${PATH}" \
		NORNICDB_IMAGE=registry.example/nornicdb:test \
		IFA_FAULT_EXPECTED_NORNICDB_REVISION=3333333333333333333333333333333333333333 \
		IFA_TEST_ACTUAL_BACKEND_REVISION=3333333333333333333333333333333333333333 \
		ifa_fault_capture_failure_diagnostics \
		"${case_dir}" "${case_dir}/logs" test-project compose.yaml 1 test-dsn
	[[ "$(cat "${case_dir}/backend-expected-revision.txt")" == 3333333333333333333333333333333333333333 ]] \
		|| fail "backend image override ignored its explicit expected revision"

	rm -f "${case_dir}/diagnostics-complete" "${case_dir}/backend-expected-revision.txt"
	set +e
	PATH="${fake_bin}:${PATH}" NORNICDB_IMAGE=registry.example/nornicdb:test \
		ifa_fault_capture_failure_diagnostics \
		"${case_dir}" "${case_dir}/logs" test-project compose.yaml 1 test-dsn \
		2>/dev/null
	rc=$?
	set -e
	[[ "${rc}" -ne 0 ]] || fail "backend image override without expected revision succeeded"
	[[ ! -e "${case_dir}/backend-expected-revision.txt" ]] \
		|| fail "missing override revision retained a stale expected artifact"
	[[ -s "${case_dir}/backend-actual-revision.txt" ]] \
		|| fail "missing override revision prevented actual revision capture"
	[[ ! -e "${case_dir}/diagnostics-complete" ]] \
		|| fail "missing override revision published diagnostics-complete"
)

test_ifa_fault_invalid_timeout_fails_before_collection() (
	local case_dir timeout_value rc
	for timeout_value in 0 nope 31; do
		case_dir="$(mktemp -d -t ifa-fault-invalid-timeout.XXXXXX)"
		trap 'rm -rf "${case_dir}"' EXIT
		source "${diagnostics_lib}"
		set +e
		IFA_FAULT_DIAGNOSTIC_TIMEOUT="${timeout_value}" \
			ifa_fault_capture_failure_diagnostics \
			"${case_dir}" "${case_dir}/logs" test-project compose.yaml 1 test-dsn \
			2>/dev/null
		rc=$?
		set -e
		[[ "${rc}" -ne 0 ]] || fail "invalid diagnostic timeout ${timeout_value} succeeded"
		[[ ! -e "${case_dir}/diagnostics-manifest.tsv" && ! -e "${case_dir}/logs" ]] \
			|| fail "invalid diagnostic timeout ${timeout_value} began collection"
		rm -rf "${case_dir}"
		trap - EXIT
	done
)

run_ifa_fault_injection_diagnostics_hardening_cases() {
	test_ifa_fault_backend_revision_mismatch_retains_both_sides
	test_ifa_fault_backend_image_override_requires_explicit_revision
	test_ifa_fault_invalid_timeout_fails_before_collection
}
