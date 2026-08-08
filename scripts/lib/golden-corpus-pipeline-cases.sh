#!/usr/bin/env bash
# Pipeline structural cases for scripts/test-verify-golden-corpus-gate.sh.
# This file is sourced after the mirror matcher and fixture paths are loaded.
# Keep these assertions here so the top-level mirror has useful headroom below
# the repository's 500-line limit.

# Drives every pipeline stage end to end.
require "bootstrap stage" "eshu-bootstrap-index"
require "filesystem managed-copy mode" 'export ESHU_REPO_SOURCE_MODE="filesystem"'
require "filesystem managed-copy direct-mode pin" 'export ESHU_FILESYSTEM_DIRECT="false"'
filesystem_direct_exports="$(rg --count '^[[:space:]]*export ESHU_FILESYSTEM_DIRECT=' "${script}" || true)"
[[ "${filesystem_direct_exports:-0}" -eq 1 ]] ||
	fail "golden gate must set ESHU_FILESYSTEM_DIRECT exactly once"

# Pin the extracted helper, exact test name, complete executable line, and its
# failure branch. The JSON event cases below additionally prove that a zero-test
# or skipped-test Go success cannot satisfy B-7.
demotion_lib="${repo_root}/scripts/lib/golden-corpus-container-image-demotion.sh"
[[ -f "${demotion_lib}" ]] || fail "missing demotion proof lib: ${demotion_lib}"
bash -n "${demotion_lib}" || fail "demotion proof lib has a syntax error"
require "demotion proof lib source" "golden-corpus-container-image-demotion.sh"
require_invocation "demotion proof invocation" "run_container_image_identity_demotion_proof"
require_in "exact demotion test name" "${demotion_lib}" \
	'test_name="TestContainerImageIdentitySupportWriterRetiresPromotedDecisionOnDemotionLive"'
demotion_command_pattern='^\t\tESHU_POSTGRES_TEST_DSN="\$\{ESHU_POSTGRES_DSN\}" go test \./internal/reducer -run "\^\$\{test_name\}\$" -count=1 -timeout=60s -json$'
require_matches "container image identity demotion command must be exact and fail closed" \
	"${demotion_lib}" "${demotion_command_pattern}"
demotion_block_pattern='^\tif ! \(\n\t\tcd "\$\{repo_root\}/go"\n\t\tESHU_POSTGRES_TEST_DSN="\$\{ESHU_POSTGRES_DSN\}" go test \./internal/reducer -run "\^\$\{test_name\}\$" -count=1 -timeout=60s -json\n\t\) >"\$\{proof_json\}" 2>"\$\{proof_stderr\}"; then\n\t\tcat "\$\{proof_stderr\}"\n\t\tgolden_corpus_render_test_output "\$\{proof_json\}"\n\t\tdie "container image identity canonical-to-demoted lifecycle proof failed"\n\tfi$'
require_matches "container image identity demotion block must propagate failure" \
	"${demotion_lib}" "${demotion_block_pattern}"
demotion_event_count_pattern='^\tgolden_corpus_exact_test_passed "\$\{proof_json\}" "\$\{test_name\}" \|\|\n\t\tdie "container image identity demotion proof must report exactly one run, one pass, and zero skips"$'
require_matches "demotion proof event-count validation" \
	"${demotion_lib}" "${demotion_event_count_pattern}"

demotion_command_fixture="$(mktemp -t golden-corpus-demotion-command.XXXXXX)"
demotion_command='ESHU_POSTGRES_TEST_DSN="${ESHU_POSTGRES_DSN}" go test ./internal/reducer -run "^${test_name}$" -count=1 -timeout=60s -json'
expect_demotion_command_rejected() {
	local label="$1" command="$2"
	printf '\t\t%s\n' "${command}" >"${demotion_command_fixture}"
	if (require_matches "${label}" "${demotion_command_fixture}" "${demotion_command_pattern}" >/dev/null 2>&1); then
		fail "demotion command mirror accepted ${label}: ${command}"
	fi
}
expect_demotion_command_rejected "a fail-open suffix" "${demotion_command} || true"
expect_demotion_command_rejected "a changed package" "${demotion_command/\.\/internal\/reducer/.\/cmd\/reducer}"
expect_demotion_command_rejected "a missing timeout" "${demotion_command/ -timeout=60s/}"
expect_demotion_command_rejected \
	"a weakened test regex" \
	'ESHU_POSTGRES_TEST_DSN="${ESHU_POSTGRES_DSN}" go test ./internal/reducer -run "${test_name}" -count=1 -timeout=60s -json'
rm -f "${demotion_command_fixture}"

# Exercise the event validator, not just its source. Go deliberately exits zero
# for zero matches and skips, so both must be explicit negative cases.
# shellcheck source=scripts/lib/golden-corpus-container-image-demotion.sh
. "${demotion_lib}"
demotion_event_fixture="$(mktemp -t golden-corpus-demotion-events.XXXXXX)"
demotion_test_name="TestContainerImageIdentitySupportWriterRetiresPromotedDecisionOnDemotionLive"
printf '%s\n' \
	"{\"Action\":\"run\",\"Test\":\"${demotion_test_name}\"}" \
	"{\"Action\":\"pass\",\"Test\":\"${demotion_test_name}\"}" \
	>"${demotion_event_fixture}"
golden_corpus_exact_test_passed "${demotion_event_fixture}" "${demotion_test_name}" ||
	fail "demotion event validator rejected one run and one pass"
printf '%s\n' '{"Action":"pass","Package":"example.invalid/no-tests"}' >"${demotion_event_fixture}"
if golden_corpus_exact_test_passed "${demotion_event_fixture}" "${demotion_test_name}"; then
	fail "demotion event validator accepted a zero-test Go success"
fi
printf '%s\n' \
	"{\"Action\":\"run\",\"Test\":\"${demotion_test_name}\"}" \
	"{\"Action\":\"skip\",\"Test\":\"${demotion_test_name}\"}" \
	>"${demotion_event_fixture}"
if golden_corpus_exact_test_passed "${demotion_event_fixture}" "${demotion_test_name}"; then
	fail "demotion event validator accepted a skipped test"
fi
printf '%s\n' \
	"{\"Action\":\"run\",\"Test\":\"${demotion_test_name}\"}" \
	"{\"Action\":\"run\",\"Test\":\"${demotion_test_name}\"}" \
	"{\"Action\":\"pass\",\"Test\":\"${demotion_test_name}\"}" \
	>"${demotion_event_fixture}"
if golden_corpus_exact_test_passed "${demotion_event_fixture}" "${demotion_test_name}"; then
	fail "demotion event validator accepted duplicate runs"
fi
# Cold Go caches write module-download diagnostics to stderr. If that stream is
# ever merged back into the structured stdout file, parsing must fail loudly.
printf '%s\n' \
	'go: downloading example.invalid/module v1.0.0' \
	"{\"Action\":\"run\",\"Test\":\"${demotion_test_name}\"}" \
	"{\"Action\":\"pass\",\"Test\":\"${demotion_test_name}\"}" \
	>"${demotion_event_fixture}"
if golden_corpus_exact_test_passed "${demotion_event_fixture}" "${demotion_test_name}" 2>/dev/null; then
	fail "demotion event validator accepted non-JSON diagnostics mixed into structured output"
fi
rm -f "${demotion_event_fixture}"
unset demotion_block_pattern demotion_command demotion_command_fixture demotion_command_pattern
unset demotion_event_count_pattern
unset demotion_event_fixture demotion_lib demotion_test_name

cloud_reopen_lib="${repo_root}/scripts/lib/golden-corpus-container-image-demotion.sh"
require_invocation "cloud image reopen ordering proof invocation" \
	"run_container_image_identity_cloud_reopen_ordering_proof"
require_in "exact cloud image reopen ordering test name" "${cloud_reopen_lib}" \
	'test_name="TestContainerImageIdentityCloudReferenceReopenOrderingPostgresLive"'
cloud_reopen_command_pattern='^\t\tESHU_POSTGRES_DSN="\$\{ESHU_POSTGRES_DSN\}" go test \./internal/storage/postgres -run "\^\$\{test_name\}\$" -count=1 -timeout=120s -json$'
require_matches "cloud image reopen ordering command must be exact and fail closed" \
	"${cloud_reopen_lib}" "${cloud_reopen_command_pattern}"
cloud_reopen_block_pattern='^\tif ! \(\n\t\tcd "\$\{repo_root\}/go"\n\t\tESHU_POSTGRES_DSN="\$\{ESHU_POSTGRES_DSN\}" go test \./internal/storage/postgres -run "\^\$\{test_name\}\$" -count=1 -timeout=120s -json\n\t\) >"\$\{proof_json\}" 2>"\$\{proof_stderr\}"; then\n\t\tcat "\$\{proof_stderr\}"\n\t\tgolden_corpus_render_test_output "\$\{proof_json\}"\n\t\tdie "container image identity cloud reopen ordering proof failed"\n\tfi$'
require_matches "cloud image reopen ordering block must propagate failure" \
	"${cloud_reopen_lib}" "${cloud_reopen_block_pattern}"
cloud_reopen_event_count_pattern='^\tgolden_corpus_exact_test_passed "\$\{proof_json\}" "\$\{test_name\}" \|\|\n\t\tdie "cloud reopen ordering proof must report exactly one run, one pass, and zero skips"$'
require_matches "cloud image reopen ordering event-count validation" \
	"${cloud_reopen_lib}" "${cloud_reopen_event_count_pattern}"
unset cloud_reopen_block_pattern cloud_reopen_command_pattern cloud_reopen_event_count_pattern
unset cloud_reopen_lib

replay_lib="${repo_root}/scripts/lib/golden-corpus-cassette-replay.sh"
metrics_source_lib="${repo_root}/scripts/lib/golden-corpus-metrics-source.sh"
service_changed_lib="${repo_root}/scripts/lib/golden-corpus-service-changed-since.sh"
require "cassette replay helper source" "golden-corpus-cassette-replay.sh"
require_in "cassette replay execution" "${replay_lib}" "-mode=cassette"
require_in "semantic replay alias" "${script}" \
	"semantic-extraction-cassette:collector-prometheus-mimir:semanticextraction"
require_invocation "cassette replay invocation" "golden_corpus_start_cassette_replays"
require "service changed-since helper source" "golden-corpus-service-changed-since.sh"
require_invocation "service prior capture" "golden_service_changed_since_capture_prior"
require_invocation "service staged mutation" "golden_service_changed_since_mutate_owner"
require_invocation "service current validation" "golden_service_changed_since_validate_current"
require_invocation "metrics source invocation" "golden_metrics_source_start"
require "mock metrics binary build" "build_bin mock-prometheus-mimir"
require_in "explicit metrics instance id" "${metrics_source_lib}" \
	'ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID="golden-prometheus-range"'
require_in "credential-free metrics tenant" "${metrics_source_lib}" 'tenant_id: "golden-corpus"'
bash "${repo_root}/scripts/lib/test-golden-corpus-cassette-replay.sh" || fail "cassette replay helper tests failed"
bash "${repo_root}/scripts/lib/test-golden-corpus-service-changed-since.sh" || fail "service changed-since helper tests failed"
bash "${repo_root}/scripts/lib/test-golden-corpus-metrics-source.sh" || fail "metrics source helper tests failed"
require "projector drain" "eshu-projector"
require "reducer drain" "eshu-reducer"
# `eshu-api` also names the /readyz failure message, and `eshu-golden-corpus-gate`
# names both gate invocations; each is pinned to the one line that must exist.
# shellcheck disable=SC2016  # the needle is the literal orchestrator source line
require "api for query truth" 'start_bg api api_pid "${bin_dir}/eshu-api"'
require "gate binary built" "build_bin golden-corpus-gate"
require "corpus fixture inventory source" "golden-corpus-fixtures.sh"
require_in "SQL relationship corpus fixture" "${fixture_lib}" $'\tsql_comprehensive'
# These targets use different comment syntaxes, so the matcher derives the
# comment marker from each target rather than assuming shell comments.
require_in "direct comma-separated SQL DROP migration fixture" "${sql_drop_fixture}" \
	'DROP TABLE IF EXISTS public.users, public.orgs;'
require_in "SQL DROP required correlation in B-12 snapshot" "${snapshot}" '"id": "rc-163"'

# Asserts all four B-7 buckets. The snapshot flag appears in both gate
# invocations, so each invocation region is asserted independently.
require "drains phase" "-phase=drains"
require "graph+query+timing phase" "-phase=graph,query,timing"
require_region "snapshot contract (drains phase)" \
	"/-phase=drains/,/-drain-timeout=/" "-snapshot=testdata/golden/e2e-20repo-snapshot.json"
require_region "runtime snapshot contract (graph,query,timing phase)" \
	"/-phase=graph,query,timing,demo-answers/,/-elapsed-seconds=/" '-snapshot="${golden_query_runtime_snapshot}"'
require "timing budget" "-budget-multiplier"
# The blocking-correlation set is single-sourced from the snapshot's required
# correlation IDs through the `all` sentinel (#4596).
require "single-sourced required-correlations" '-required-correlations="all"'
if rg --pcre2 --quiet -- '-required-correlations="rc-[0-9]+,rc-' "${script}"; then
	fail "-required-correlations reverted to a hand-maintained comma-separated id list (#4596 regression)"
fi

# B-11 phase timing and cross-repository dead-code fixture wiring.
require "phase-timing lib source" "golden-corpus-phase-timings.sh"
require_invocation "phase-timing invocation" "emit_phase_timings_and_flags"
require "passes phase flags to gate" "phase_flags"
require "cross-repo dead-code fixture source" "golden-corpus-dead-code-fixtures.sh"
require_invocation "cross-repo dead-code fixture invocation" "seed_cross_repo_dead_code_fixture"

timing_lib="${repo_root}/scripts/lib/golden-corpus-phase-timings.sh"
[[ -f "${timing_lib}" ]] || fail "missing phase-timing lib: ${timing_lib}"
bash -n "${timing_lib}" || fail "phase-timing lib has a syntax error"
dead_code_lib="${repo_root}/scripts/lib/golden-corpus-dead-code-fixtures.sh"
[[ -f "${dead_code_lib}" ]] || fail "missing dead-code fixture lib: ${dead_code_lib}"
bash -n "${dead_code_lib}" || fail "dead-code fixture lib has a syntax error"

pipeline_cases_completed=1
