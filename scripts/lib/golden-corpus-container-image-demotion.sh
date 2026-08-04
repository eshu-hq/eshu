#!/usr/bin/env bash
# Deterministic container image identity demotion proof for B-7. The caller
# provides repo_root, log_dir, ESHU_POSTGRES_DSN, log(), and die().

golden_corpus_exact_test_passed() {
	local json_log="$1" test_name="$2" counts run_count pass_count skip_count
	counts="$(jq -r --arg test_name "${test_name}" -s '
    [
      ([.[] | select(.Test == $test_name and .Action == "run")] | length),
      ([.[] | select(.Test == $test_name and .Action == "pass")] | length),
      ([.[] | select(.Test == $test_name and .Action == "skip")] | length)
    ] | @tsv
  ' "${json_log}")" || return 1
	IFS=$'\t' read -r run_count pass_count skip_count <<<"${counts}"
	[[ "${run_count}" -eq 1 && "${pass_count}" -eq 1 && "${skip_count}" -eq 0 ]]
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

	log "B-7 container image identity canonical-to-demoted lifecycle proof"
	proof_start="$(date +%s)"
	if ! (
		cd "${repo_root}/go"
		ESHU_POSTGRES_TEST_DSN="${ESHU_POSTGRES_DSN}" go test ./internal/reducer -run "^${test_name}$" -count=1 -timeout=60s -json
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
