#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154,SC2329
# Hermetic regressions for the Neo4j identity profile of the fault-injection
# failure diagnostics (owner order 2026-11-05: Ifá gates run on Neo4j).
# The NornicDB profile keeps its own cases in the sibling case files.

neo4j_pinned_digest="sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f"
neo4j_container_env='"NEO4J_AUTH=neo4j/compose-secret","NEO4J_PLUGINS=[]","NEO4J_server_memory_heap_max__size=512m"'

test_ifa_fault_neo4j_prepare_case() {
	local case_dir="$1" fake_bin="${1}/bin"
	mkdir -p "${fake_bin}"
	printf '{"edges":[],"nodes":[]}\n' >"${case_dir}/graph-baseline.dump"
	printf 'cell_baseline\n' >"${case_dir}/current-cell"
	cp "${diagnostics_fake_docker_lib}" "${fake_bin}/docker"
	chmod +x "${fake_bin}/docker"
}

# Runs the capture against the fake docker as the Neo4j service. Extra
# IFA_TEST_* settings arrive as arguments (VAR=value); the return status is the
# capture's own.
test_ifa_fault_neo4j_capture() {
	local case_dir="$1"
	shift
	PATH="${case_dir}/bin:${PATH}" env \
		ESHU_GRAPH_BACKEND="${TEST_NEO4J_BACKEND:-neo4j}" \
		IFA_TEST_SERVICE=neo4j \
		IFA_TEST_RENDERED_PLATFORM= \
		IFA_TEST_RENDERED_IMAGE="${TEST_NEO4J_IMAGE:-neo4j:2026-community@${neo4j_pinned_digest}}" \
		IFA_TEST_RUNTIME_REPO_DIGEST="${TEST_NEO4J_REPO_DIGEST:-neo4j@${neo4j_pinned_digest}}" \
		IFA_TEST_CONTAINER_ENV_JSON="${neo4j_container_env}" \
		"$@" \
		bash -c 'source "$1"; shift; ifa_fault_capture_failure_diagnostics "$@"' _ \
		"${diagnostics_lib}" "${case_dir}" "${case_dir}/logs" test-project compose.yaml 1 test-dsn
}

test_ifa_fault_neo4j_profile_captures_safe_backend_state() (
	local case_dir
	case_dir="$(mktemp -d -t ifa-fault-neo4j-success.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	test_ifa_fault_neo4j_prepare_case "${case_dir}"
	test_ifa_fault_neo4j_capture "${case_dir}" >/dev/null 2>&1 \
		|| fail "pinned Neo4j image was not accepted by the Neo4j profile"
	[[ "$(cat "${case_dir}/diagnostics-complete")" == complete ]] \
		|| fail "Neo4j profile did not publish the completeness marker"
	jq -e --arg digest "${neo4j_pinned_digest}" '
		.rendered_image == ("neo4j:2026-community@" + $digest)
		and .rendered_platform == "linux/amd64"
		and .runtime_platform == "linux/amd64"
		and .accepted_runtime_repo_digest == ("neo4j@" + $digest)
		and .provenance_match == true
	' "${case_dir}/backend-provenance.json" >/dev/null \
		|| fail "Neo4j provenance did not bind the rendered pinned image to its runtime image"
	rg --fixed-strings --quiet -- 'NEO4J_server_memory_heap_max__size=512m' "${case_dir}/backend-environment.txt" \
		|| fail "Neo4j environment omitted an allowlisted memory setting"
	if rg --fixed-strings --quiet -- 'NEO4J_AUTH' "${case_dir}/backend-environment.txt" \
		|| rg --fixed-strings --quiet -- 'compose-secret' "${case_dir}/backend-environment.txt"; then
		fail "Neo4j environment captured the credential-bearing NEO4J_AUTH"
	fi
)

test_ifa_fault_neo4j_profile_accepts_the_ci_mirror_image() (
	local case_dir
	case_dir="$(mktemp -d -t ifa-fault-neo4j-mirror.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	test_ifa_fault_neo4j_prepare_case "${case_dir}"
	TEST_NEO4J_IMAGE="ghcr.io/eshu-hq/ci-neo4j-community@${neo4j_pinned_digest}" \
		TEST_NEO4J_REPO_DIGEST="ghcr.io/eshu-hq/ci-neo4j-community@${neo4j_pinned_digest}" \
		test_ifa_fault_neo4j_capture "${case_dir}" >/dev/null 2>&1 \
		|| fail "the CI mirror image CI substitutes through NEO4J_IMAGE was rejected"
	[[ -s "${case_dir}/diagnostics-complete" ]] || fail "CI mirror image did not publish the completeness marker"
)

test_ifa_fault_neo4j_profile_rejects_unpinned_or_foreign_images() (
	local case_dir image rc
	for image in \
		"neo4j:latest" \
		"neo4j:2026-community@sha256:1111111111111111111111111111111111111111111111111111111111111111" \
		"ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1"; do
		case_dir="$(mktemp -d -t ifa-fault-neo4j-reject.XXXXXX)"
		test_ifa_fault_neo4j_prepare_case "${case_dir}"
		set +e
		TEST_NEO4J_IMAGE="${image}" test_ifa_fault_neo4j_capture "${case_dir}" >/dev/null 2>&1
		rc=$?
		set -e
		[[ "${rc}" -ne 0 ]] || fail "Neo4j profile accepted image ${image}"
		[[ ! -e "${case_dir}/diagnostics-complete" ]] \
			|| fail "Neo4j profile published the completeness marker for image ${image}"
		[[ ! -e "${case_dir}/backend-provenance.json" ]] \
			|| fail "Neo4j profile wrote identity artifacts for the rejected image ${image}"
		rm -rf "${case_dir}"
	done
)

test_ifa_fault_neo4j_profile_rejects_a_runtime_digest_mismatch() (
	local case_dir rc
	case_dir="$(mktemp -d -t ifa-fault-neo4j-mismatch.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	test_ifa_fault_neo4j_prepare_case "${case_dir}"
	set +e
	TEST_NEO4J_REPO_DIGEST="neo4j@sha256:2222222222222222222222222222222222222222222222222222222222222222" \
		test_ifa_fault_neo4j_capture "${case_dir}" >/dev/null 2>&1
	rc=$?
	set -e
	[[ "${rc}" -ne 0 ]] || fail "Neo4j runtime digest mismatch did not fail closed"
	jq -e '.provenance_match == false' "${case_dir}/backend-provenance.json" >/dev/null \
		|| fail "Neo4j digest mismatch did not retain the provenance evidence"
	[[ ! -e "${case_dir}/diagnostics-complete" ]] || fail "Neo4j digest mismatch published the completeness marker"
)

test_ifa_fault_neo4j_profile_rejects_an_unsupported_backend() (
	local case_dir rc
	case_dir="$(mktemp -d -t ifa-fault-neo4j-badbackend.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	test_ifa_fault_neo4j_prepare_case "${case_dir}"
	set +e
	TEST_NEO4J_BACKEND=memgraph test_ifa_fault_neo4j_capture "${case_dir}" >/dev/null 2>&1
	rc=$?
	set -e
	[[ "${rc}" -ne 0 ]] || fail "an unsupported ESHU_GRAPH_BACKEND did not fail closed"
	[[ ! -e "${case_dir}/diagnostics-complete" ]] \
		|| fail "an unsupported ESHU_GRAPH_BACKEND published the completeness marker"
)

run_ifa_fault_injection_diagnostics_neo4j_cases() {
	test_ifa_fault_neo4j_profile_captures_safe_backend_state
	test_ifa_fault_neo4j_profile_accepts_the_ci_mirror_image
	test_ifa_fault_neo4j_profile_rejects_unpinned_or_foreign_images
	test_ifa_fault_neo4j_profile_rejects_a_runtime_digest_mismatch
	test_ifa_fault_neo4j_profile_rejects_an_unsupported_backend
}
