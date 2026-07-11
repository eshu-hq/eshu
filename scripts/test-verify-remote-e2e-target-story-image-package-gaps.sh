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
cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-image-package-gaps-target-story.json" >"${state_dir}/target-story.json"

cat >"${state_dir}/repo-story.json" <<'JSON'
{"data":{"repository":{"id":"repo://example/api","name":"api"}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON

cat >"${state_dir}/impact-count.json" <<'JSON'
{"data":{"total_findings":5,"affected_findings":5},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON

# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
# the entire heredoc body to a pipe before forking the reader, and macOS's
# 512-byte pipe buffer deadlocks on any body over that size (#5074).
cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-image-package-gaps-service-story.json" >"${state_dir}/service-story.json"

# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
# the entire heredoc body to a pipe before forking the reader, and macOS's
# 512-byte pipe buffer deadlocks on any body over that size (#5074).
cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-image-package-gaps-mcp-service-story.json" >"${state_dir}/mcp-service-story.json"

ESHU_REMOTE_E2E_TEST_STATE="${state_dir}" \
  PATH="${fake_bin}:${PATH}" \
  ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json" \
  ESHU_REMOTE_E2E_API_BASE_URL="http://127.0.0.1:18080/api/v0" \
  ESHU_REMOTE_E2E_MCP_URL="http://127.0.0.1:18081/mcp/message" \
  ESHU_REMOTE_E2E_API_KEY="test-api-key" \
  "${verifier}" >/tmp/eshu-remote-e2e-target-story-image-package.out 2>/tmp/eshu-remote-e2e-target-story-image-package.err

if ! rg -q 'image_package_missing_evidence=oci_registry_target_outside_scope' /tmp/eshu-remote-e2e-target-story-image-package.out; then
  printf 'expected target-story proof to report API image-package missing evidence\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-image-package.out >&2
  exit 1
fi
if ! rg -q 'image_package_collector_scope=outside_configured_targets' /tmp/eshu-remote-e2e-target-story-image-package.out; then
  printf 'expected target-story proof to report API image-package collector scope\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-image-package.out >&2
  exit 1
fi
if ! rg -q 'mcp_image_package_missing_evidence=oci_registry_target_outside_scope' /tmp/eshu-remote-e2e-target-story-image-package.out; then
  printf 'expected target-story proof to report MCP image-package missing evidence\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-image-package.out >&2
  exit 1
fi
if ! rg -q 'mcp_image_package_collector_scope=outside_configured_targets' /tmp/eshu-remote-e2e-target-story-image-package.out; then
  printf 'expected target-story proof to report MCP image-package collector scope\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-image-package.out >&2
  exit 1
fi
if rg -q 'repo://example/api|oci-registry://registry.example/team/api|test-api-key' /tmp/eshu-remote-e2e-target-story-image-package.out; then
  printf 'target-story image-package proof leaked private target values\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-image-package.out >&2
  exit 1
fi

printf 'verify-remote-e2e-target-story image-package gap tests passed\n'
