#!/usr/bin/env bash
# Deterministic container image identity lifecycle proofs for B-7. The caller
# provides repo_root, log_dir, ESHU_POSTGRES_DSN, log(), and die().

golden_corpus_exact_test_passed() {
	local verifier_dir
	verifier_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)" || return 1
	python3 "${verifier_dir}/golden_corpus_identity.py" --events "$1" --test "$2"
}

golden_corpus_render_test_output() {
	local json_log="$1"
	jq -jr 'select(.Action == "output") | .Output' "${json_log}" || cat "${json_log}"
}

run_container_image_identity_demotion_proof() {
	local proof_start proof_json proof_stderr test_name
	test_name="TestContainerImageIdentitySupportWriterRetiresPromotedDecisionOnDemotionLive"
	proof_json="${log_dir}/container-image-identity-demotion.json"
	proof_stderr="${log_dir}/container-image-identity-demotion.stderr.log"

	# ./internal/reducer matches one package; ./internal/reducer/... matches the
	# tree. #6061 moves families into subpackages, and this test moved with
	# container_image into internal/reducer/containerimage. Under the narrow
	# form the selector matched nothing, go test exited 0 with "no tests to
	# run", and the exactly-one-pass assertion below is what caught it.
	log "B-7 container image identity canonical-to-demoted lifecycle proof"
	proof_start="$(date +%s)"
	if ! (
		cd "${repo_root}/go"
		ESHU_POSTGRES_TEST_DSN="${ESHU_POSTGRES_DSN}" go test ./internal/reducer/... -run "^${test_name}$" -count=1 -timeout=60s -json
	) >"${proof_json}" 2>"${proof_stderr}"; then
		cat "${proof_stderr}"
		golden_corpus_render_test_output "${proof_json}"
		die "container image identity canonical-to-demoted lifecycle proof failed"
	fi
	cat "${proof_stderr}"
	golden_corpus_render_test_output "${proof_json}"
	golden_corpus_exact_test_passed "${proof_json}" "${test_name}" ||
		die "container image identity demotion proof must report exactly one run, one pass, and zero skips"
	log "container image identity demotion proof completed in $(( $(date +%s) - proof_start ))s"
}

run_container_image_identity_cloud_reopen_ordering_proof() {
	local proof_start proof_json proof_stderr test_name
	test_name="TestContainerImageIdentityCloudReferenceReopenOrderingPostgresLive"
	proof_json="${log_dir}/container-image-identity-cloud-reopen-ordering.json"
	proof_stderr="${log_dir}/container-image-identity-cloud-reopen-ordering.stderr.log"

	log "B-7 container image identity cloud-before-OCI reopen ordering proof"
	proof_start="$(date +%s)"
	if ! (
		cd "${repo_root}/go"
		ESHU_POSTGRES_DSN="${ESHU_POSTGRES_DSN}" go test ./internal/storage/postgres -run "^${test_name}$" -count=1 -timeout=120s -json
	) >"${proof_json}" 2>"${proof_stderr}"; then
		cat "${proof_stderr}"
		golden_corpus_render_test_output "${proof_json}"
		die "container image identity cloud reopen ordering proof failed"
	fi
	cat "${proof_stderr}"
	golden_corpus_render_test_output "${proof_json}"
	golden_corpus_exact_test_passed "${proof_json}" "${test_name}" ||
		die "cloud reopen ordering proof must report exactly one run, one pass, and zero skips"
	log "container image identity cloud reopen ordering proof completed in $(( $(date +%s) - proof_start ))s"
}
