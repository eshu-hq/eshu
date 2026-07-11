#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify_remote_e2e_target_story.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fake_bin="${tmp_root}/bin"
state_dir="${tmp_root}/state"
mkdir -p "${fake_bin}" "${state_dir}"

cp "${repo_root}/scripts/lib/remote_e2e_target_story_fake_curl.sh" "${fake_bin}/curl"
chmod +x "${fake_bin}/curl"

# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
# the entire heredoc body to a pipe before forking the reader, and macOS's
# 512-byte pipe buffer deadlocks on any body over that size (#5074).
cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-runtime-missing-evidence-target-story.json" >"${state_dir}/target-story.json"

cat >"${state_dir}/repo-story.json" <<'JSON'
{"data":{"repository":{"id":"repo://example/api","name":"api"}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON

cat >"${state_dir}/image-count.json" <<'JSON'
{"data":{"total_identities":0,"by_outcome":{},"by_identity_strength":{},"source_bridge":{"source_repository_id":"repo://example/api","missing_evidence":["deployment_image_reference_missing","image_registry_observation_missing","source_to_image_correlation_missing"]},"scope":{"source_repository_id":"repo://example/api"}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON

# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
# the entire heredoc body to a pipe before forking the reader, and macOS's
# 512-byte pipe buffer deadlocks on any body over that size (#5074).
cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-runtime-missing-evidence-mcp-image-count.json" >"${state_dir}/mcp-image-count.json"

cat >"${state_dir}/sbom-count.json" <<'JSON'
{"data":{"total_attachments":0,"by_attachment_status":{},"by_artifact_kind":{},"missing_evidence":["repository_to_image_evidence_missing"],"scope":{"repository_id":"repo://example/api"}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON

# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
# the entire heredoc body to a pipe before forking the reader, and macOS's
# 512-byte pipe buffer deadlocks on any body over that size (#5074).
cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-runtime-missing-evidence-mcp-sbom-count.json" >"${state_dir}/mcp-sbom-count.json"

ESHU_REMOTE_E2E_TEST_STATE="${state_dir}" \
  PATH="${fake_bin}:${PATH}" \
  ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json" \
  ESHU_REMOTE_E2E_API_BASE_URL="http://127.0.0.1:18080/api/v0" \
  ESHU_REMOTE_E2E_MCP_URL="http://127.0.0.1:18081/mcp/message" \
  ESHU_REMOTE_E2E_API_KEY="test-api-key" \
  "${verifier}" >/tmp/eshu-remote-e2e-target-story-runtime-missing.out 2>/tmp/eshu-remote-e2e-target-story-runtime-missing.err

if ! rg -q 'container_image_missing_evidence=.*source_to_image_correlation_missing' /tmp/eshu-remote-e2e-target-story-runtime-missing.out; then
  printf 'expected API container image missing evidence in proof output\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-runtime-missing.out >&2
  exit 1
fi
if ! rg -q 'mcp_container_image_missing_evidence=.*source_to_image_correlation_missing' /tmp/eshu-remote-e2e-target-story-runtime-missing.out; then
  printf 'expected MCP container image missing evidence in proof output\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-runtime-missing.out >&2
  exit 1
fi
if ! rg -q 'sbom_missing_evidence=repository_to_image_evidence_missing' /tmp/eshu-remote-e2e-target-story-runtime-missing.out; then
  printf 'expected API SBOM missing evidence in proof output\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-runtime-missing.out >&2
  exit 1
fi
if ! rg -q 'mcp_sbom_missing_evidence=repository_to_image_evidence_missing' /tmp/eshu-remote-e2e-target-story-runtime-missing.out; then
  printf 'expected MCP SBOM missing evidence in proof output\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-runtime-missing.out >&2
  exit 1
fi
if rg -q 'repo://example/api|test-api-key' /tmp/eshu-remote-e2e-target-story-runtime-missing.out; then
  printf 'runtime missing-evidence proof leaked private target values\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-runtime-missing.out >&2
  exit 1
fi
rg -x -q 'count_container_image_identities' "${state_dir}/mcp-tools"
rg -x -q 'count_sbom_attestation_attachments' "${state_dir}/mcp-tools"

printf 'verify-remote-e2e-target-story runtime missing-evidence tests passed\n'
