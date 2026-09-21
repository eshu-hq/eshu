#!/usr/bin/env bash
# Immutable image provenance and timeout-input regressions for failure diagnostics.

test_ifa_fault_prepare_provenance_case() {
	local case_dir="$1" fake_bin="${1}/bin"
	mkdir -p "${fake_bin}"
	printf '{"edges":[],"nodes":[]}\n' >"${case_dir}/graph-baseline.dump"
	printf 'cell_baseline\n' >"${case_dir}/current-cell"
	cp "${diagnostics_fake_docker_lib}" "${fake_bin}/docker"
	chmod +x "${fake_bin}/docker"
}

test_ifa_fault_backend_accepts_index_repository_digest() (
	local case_dir fake_bin
	case_dir="$(mktemp -d -t ifa-fault-index-digest.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	fake_bin="${case_dir}/bin"
	test_ifa_fault_prepare_provenance_case "${case_dir}"
	source "${diagnostics_lib}"
	PATH="${fake_bin}:${PATH}" \
		IFA_TEST_RUNTIME_REPO_DIGEST=ghcr.io/eshu-hq/nornicdb-amd64-cpu@sha256:eb69530fa2951d74d89ed9df947aeea10beb0c5e2f2ede4c0080e09fe78aa555 \
		ifa_fault_capture_failure_diagnostics \
		"${case_dir}" "${case_dir}/logs" test-project compose.yaml 1 test-dsn
	[[ -s "${case_dir}/diagnostics-complete" ]] \
		|| fail "the configured index repository digest was not accepted"
)

test_ifa_fault_backend_digest_mismatch_retains_evidence() (
	local case_dir fake_bin rc
	case_dir="$(mktemp -d -t ifa-fault-digest-mismatch.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	fake_bin="${case_dir}/bin"
	test_ifa_fault_prepare_provenance_case "${case_dir}"
	source "${diagnostics_lib}"
	set +e
	PATH="${fake_bin}:${PATH}" \
		IFA_TEST_RUNTIME_REPO_DIGEST=ghcr.io/eshu-hq/nornicdb-amd64-cpu@sha256:2222222222222222222222222222222222222222222222222222222222222222 \
		ifa_fault_capture_failure_diagnostics \
		"${case_dir}" "${case_dir}/logs" test-project compose.yaml 1 test-dsn \
		2>/dev/null
	rc=$?
	set -e
	[[ "${rc}" -ne 0 ]] || fail "backend digest mismatch did not fail closed"
	jq -e '
		.expected_index_digest == "sha256:eb69530fa2951d74d89ed9df947aeea10beb0c5e2f2ede4c0080e09fe78aa555"
		and .runtime_repo_digests == ["ghcr.io/eshu-hq/nornicdb-amd64-cpu@sha256:2222222222222222222222222222222222222222222222222222222222222222"]
		and .provenance_match == false
	' "${case_dir}/backend-provenance.json" >/dev/null \
		|| fail "digest mismatch did not retain both provenance sides"
	[[ ! -e "${case_dir}/diagnostics-complete" ]] \
		|| fail "digest mismatch published diagnostics-complete"
)

test_ifa_fault_backend_requires_official_proof_image() (
	local artifact case_dir fake_bin rc override_digest
	case_dir="$(mktemp -d -t ifa-fault-image-override.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	fake_bin="${case_dir}/bin"
	test_ifa_fault_prepare_provenance_case "${case_dir}"
	source "${diagnostics_lib}"
	set +e
	PATH="${fake_bin}:${PATH}" NORNICDB_IMAGE=registry.example/nornicdb:test \
		ifa_fault_capture_failure_diagnostics \
		"${case_dir}" "${case_dir}/logs" test-project compose.yaml 1 test-dsn \
		2>/dev/null
	rc=$?
	set -e
	[[ "${rc}" -ne 0 ]] || fail "tag-only backend image override succeeded"
	[[ ! -e "${case_dir}/diagnostics-complete" ]] \
		|| fail "tag-only backend image override published diagnostics-complete"

	override_digest="sha256:3333333333333333333333333333333333333333333333333333333333333333"
	rm -f "${case_dir}/diagnostics-complete"
	for artifact in backend-image.txt.error backend-container.json.error \
		backend-runtime-image.json.error nornicdb-environment.txt.error; do
		printf 'stale private-registry.internal evidence\n' >"${case_dir}/${artifact}"
	done
	set +e
	PATH="${fake_bin}:${PATH}" \
		NORNICDB_IMAGE="private-registry.internal/nornicdb:test@${override_digest}" \
		IFA_TEST_RUNTIME_REPO_DIGEST="private-registry.internal/nornicdb@${override_digest}" \
		IFA_TEST_COMPOSE_LOGS_PRIVATE=1 \
		ifa_fault_capture_failure_diagnostics \
		"${case_dir}" "${case_dir}/logs" test-project compose.yaml 1 test-dsn \
		2>/dev/null
	rc=$?
	set -e
	[[ "${rc}" -ne 0 ]] || fail "unmapped digest-pinned backend image override succeeded"
	[[ ! -e "${case_dir}/diagnostics-complete" ]] \
		|| fail "unmapped backend image override published diagnostics-complete"
	for artifact in "${case_dir}"/backend-*; do
		[[ -e "${artifact}" ]] || continue
		if rg --fixed-strings --quiet -- 'private-registry.internal' "${artifact}"; then
			fail "backend image override leaked a private registry in ${artifact##*/}"
		fi
	done
	[[ ! -e "${case_dir}/nornicdb-environment.txt.error" ]] \
		|| fail "unmapped backend override retained stale environment diagnostics"
	if [[ -e "${case_dir}/logs/compose-services.log" ]] \
		&& rg --quiet 'private-registry\.internal|/private/compose/path' \
			"${case_dir}/logs/compose-services.log"; then
		fail "unmapped backend override retained untrusted Compose logs"
	fi
)

test_ifa_fault_compose_config_stderr_is_not_retained() (
	local case_dir fake_bin rc
	case_dir="$(mktemp -d -t ifa-fault-compose-stderr.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	fake_bin="${case_dir}/bin"
	test_ifa_fault_prepare_provenance_case "${case_dir}"
	source "${diagnostics_lib}"
	set +e
	PATH="${fake_bin}:${PATH}" IFA_TEST_COMPOSE_CONFIG_FAIL=1 \
		IFA_TEST_COMPOSE_EXEC_FAIL=1 \
		ifa_fault_capture_failure_diagnostics \
		"${case_dir}" "${case_dir}/logs" test-project compose.yaml 1 test-dsn \
		2>/dev/null
	rc=$?
	set -e
	[[ "${rc}" -ne 0 ]] || fail "failed Compose rendering published complete diagnostics"
	if [[ -e "${case_dir}/backend-compose-config.json.error" ]] \
		&& rg --quiet 'private-registry\.internal|/private/compose/path' \
			"${case_dir}/backend-compose-config.json.error"; then
		fail "Compose rendering stderr retained a private registry or host path"
	fi
	for artifact in "${case_dir}/work-items.csv.error" "${case_dir}/gcp-facts.jsonl.error"; do
		if [[ -e "${artifact}" ]] \
			&& rg --quiet 'private-registry\.internal|/private/compose/path' "${artifact}"; then
			fail "untrusted pre-selection Compose exec stderr leaked through ${artifact##*/}"
		fi
	done
)

test_ifa_fault_backend_rejects_incomplete_or_wrong_runtime_identity() (
	local case_dir fake_bin rc test_case
	for test_case in empty-digests image-inspect-failure missing-image-id \
		wrong-repository config-image-mismatch wrong-platform; do
		case_dir="$(mktemp -d -t "ifa-fault-${test_case}.XXXXXX")"
		trap 'rm -rf "${case_dir}"' EXIT
		fake_bin="${case_dir}/bin"
		test_ifa_fault_prepare_provenance_case "${case_dir}"
		source "${diagnostics_lib}"
		set +e
		case "${test_case}" in
			empty-digests)
				PATH="${fake_bin}:${PATH}" IFA_TEST_EMPTY_REPO_DIGESTS=1 \
					ifa_fault_capture_failure_diagnostics "${case_dir}" "${case_dir}/logs" test-project compose.yaml 1 test-dsn 2>/dev/null
				;;
			image-inspect-failure)
				PATH="${fake_bin}:${PATH}" IFA_TEST_IMAGE_INSPECT_FAIL=1 \
					ifa_fault_capture_failure_diagnostics "${case_dir}" "${case_dir}/logs" test-project compose.yaml 1 test-dsn 2>/dev/null
				;;
			missing-image-id)
				PATH="${fake_bin}:${PATH}" IFA_TEST_MISSING_RUNTIME_IMAGE_ID=1 \
					ifa_fault_capture_failure_diagnostics "${case_dir}" "${case_dir}/logs" test-project compose.yaml 1 test-dsn 2>/dev/null
				;;
			wrong-repository)
				PATH="${fake_bin}:${PATH}" IFA_TEST_RUNTIME_REPO_DIGEST=registry.example/wrong@sha256:4416241599d4abe3e608c73af44e4487e7231cacd6691339f1d941bd2547021e \
					ifa_fault_capture_failure_diagnostics "${case_dir}" "${case_dir}/logs" test-project compose.yaml 1 test-dsn 2>/dev/null
				;;
			config-image-mismatch)
				PATH="${fake_bin}:${PATH}" IFA_TEST_CONTAINER_CONFIG_IMAGE=registry.example/wrong@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
					ifa_fault_capture_failure_diagnostics "${case_dir}" "${case_dir}/logs" test-project compose.yaml 1 test-dsn 2>/dev/null
				;;
			wrong-platform)
				PATH="${fake_bin}:${PATH}" IFA_TEST_RUNTIME_ARCHITECTURE=arm64 \
					ifa_fault_capture_failure_diagnostics "${case_dir}" "${case_dir}/logs" test-project compose.yaml 1 test-dsn 2>/dev/null
				;;
		esac
		rc=$?
		set -e
		[[ "${rc}" -ne 0 ]] || fail "${test_case} backend identity did not fail closed"
		if [[ "${test_case}" == wrong-platform ]]; then
			jq -e '
				.rendered_platform == "linux/amd64"
				and .runtime_platform == "linux/arm64"
				and .expected_platform_digest == "sha256:75ab7efc167b254a4d2e191b4dc4279a196c593539a72b9a756f88afa84b3f46"
				and .runtime_repo_digests == ["ghcr.io/eshu-hq/nornicdb-amd64-cpu@sha256:75ab7efc167b254a4d2e191b4dc4279a196c593539a72b9a756f88afa84b3f46"]
				and .provenance_match == false
			' "${case_dir}/backend-provenance.json" >/dev/null \
				|| fail "wrong-platform case did not isolate the platform mismatch"
		fi
		[[ ! -e "${case_dir}/diagnostics-complete" ]] \
			|| fail "${test_case} backend identity published diagnostics-complete"
		rm -rf "${case_dir}"
		trap - EXIT
	done
)

test_ifa_fault_cleanup_names_only_produced_artifacts() {
	if rg --fixed-strings --quiet -- \
		'backend-expected-platform-image.json' "${diagnostics_lib}"; then
		fail "failure cleanup names an artifact the diagnostics never produce"
	fi
}

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
	test_ifa_fault_cleanup_names_only_produced_artifacts
	test_ifa_fault_backend_accepts_index_repository_digest
	test_ifa_fault_backend_digest_mismatch_retains_evidence
	test_ifa_fault_backend_requires_official_proof_image
	test_ifa_fault_compose_config_stderr_is_not_retained
	test_ifa_fault_backend_rejects_incomplete_or_wrong_runtime_identity
	test_ifa_fault_invalid_timeout_fails_before_collection
}
