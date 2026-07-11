#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify_remote_e2e_target_story.sh"
# shellcheck source=scripts/lib/remote_e2e_target_story_alignment.sh
source "${repo_root}/scripts/lib/remote_e2e_target_story_alignment.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fake_bin="${tmp_root}/bin"
state_dir="${tmp_root}/state"
mkdir -p "${fake_bin}" "${state_dir}"

cp "${repo_root}/scripts/lib/remote_e2e_target_story_fake_curl.sh" "${fake_bin}/curl"
chmod +x "${fake_bin}/curl"

write_manifest() {
	# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
	# the entire heredoc body to a pipe before forking the reader, and macOS's
	# 512-byte pipe buffer deadlocks on any body over that size (#5074).
	cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-canonical-ids-target-story.json" >"${state_dir}/target-story.json"
}

reset_state() {
	rm -f "${state_dir}/curl-targets" "${state_dir}/mcp-tools"
	write_manifest
	export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
	cat >"${state_dir}/repo-story.json" <<'JSON'
{"data":{"repository":{"id":"repository:r_8f14e45f","name":"api"}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
	cat >"${state_dir}/image-count.json" <<'JSON'
{"data":{"total_identities":1},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
	cat >"${state_dir}/sbom-count.json" <<'JSON'
{"data":{"total_attachments":1},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
	cat >"${state_dir}/service-catalog.json" <<'JSON'
{"data":{"count":1,"correlations":[{"correlation_id":"corr-1","repository_id":"repository:r_8f14e45f","workload_id":"workload:api"}],"truncated":false,"evidence_summary":{"local_descriptors":{"state":"present","count":1},"external_catalog_confirmation":{"state":"present","count":1,"reason":"catalog_match"}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
	cat >"${state_dir}/service-story.json" <<'JSON'
{"data":{"code_to_runtime_trace":{"segments":[{"name":"image_package","status":"exact","basis":"container_image_identity_and_sbom_attachment","evidence":[{"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","sbom_attachment_id":"sbom-attachment-1","sbom_attachment_status":"attached_verified"}]}]}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
	# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
	# the entire heredoc body to a pipe before forking the reader, and macOS's
	# 512-byte pipe buffer deadlocks on any body over that size (#5074).
	cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-canonical-ids-mcp-service-catalog.json" >"${state_dir}/mcp-service-catalog.json"
	# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
	# the entire heredoc body to a pipe before forking the reader, and macOS's
	# 512-byte pipe buffer deadlocks on any body over that size (#5074).
	cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-canonical-ids-mcp-service-story.json" >"${state_dir}/mcp-service-story.json"
}

run_verifier() {
	ESHU_REMOTE_E2E_TEST_STATE="${state_dir}" \
		PATH="${fake_bin}:${PATH}" \
		ESHU_REMOTE_E2E_API_BASE_URL="http://127.0.0.1:18080/api/v0" \
		ESHU_REMOTE_E2E_MCP_URL="http://127.0.0.1:18081/mcp/message" \
		ESHU_REMOTE_E2E_API_KEY="test-api-key" \
		"${verifier}" >/tmp/eshu-remote-e2e-target-story-canonical.out 2>/tmp/eshu-remote-e2e-target-story-canonical.err
}

expect_pass() {
	if ! run_verifier; then
		printf 'expected canonical target-story verifier to pass\n' >&2
		sed -n '1,160p' /tmp/eshu-remote-e2e-target-story-canonical.err >&2
		exit 1
	fi
}

expect_fail_with() {
	local pattern="$1"
	if run_verifier; then
		printf 'expected canonical target-story verifier to fail with %s\n' "${pattern}" >&2
		sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-canonical.out >&2
		exit 1
	fi
	if ! rg -q "${pattern}" /tmp/eshu-remote-e2e-target-story-canonical.err; then
		printf 'expected canonical failure output to contain %s\n' "${pattern}" >&2
		sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-canonical.err >&2
		exit 1
	fi
}

reset_state
if ! target_story_alignment_is_opaque_repository_id "repository:r_8f14e45f"; then
	printf 'expected repository:r_8f14e45f to be treated as an opaque canonical repository id\n' >&2
	exit 1
fi
if target_story_alignment_is_opaque_repository_id "repository:r_example_api"; then
	printf 'expected readable fixture repository id to keep static alignment validation\n' >&2
	exit 1
fi
expect_pass
if ! rg -F -q '/api/v0/services/api/story?repo=repository%3Ar_8f14e45f' "${state_dir}/curl-targets"; then
	printf 'expected service-story readback to stay repository-scoped\n' >&2
	sed -n '1,200p' "${state_dir}/curl-targets" >&2
	exit 1
fi
if rg -q 'repository:r_8f14e45f|workload:api|sha256:aaaaaaaa' /tmp/eshu-remote-e2e-target-story-canonical.out; then
	printf 'canonical target-story proof leaked raw target values\n' >&2
	sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-canonical.out >&2
	exit 1
fi

reset_state
jq '.expected_workload_id = "workload:other-api"' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
cat >"${state_dir}/service-catalog.json" <<'JSON'
{"data":{"count":0,"correlations":[],"truncated":false,"evidence_summary":{"local_descriptors":{"state":"missing","count":0},"external_catalog_confirmation":{"state":"missing","count":0,"reason":"target_not_linked"}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
expect_fail_with 'target service_catalog_correlations=0 below required minimum 1'

printf 'verify-remote-e2e-target-story canonical-id tests passed\n'
