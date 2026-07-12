#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify_remote_e2e_runtime_state.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fake_bin="${tmp_root}/bin"
state_dir="${tmp_root}/state"
mkdir -p "${fake_bin}" "${state_dir}/containers" "${state_dir}/service_ids"

# Delivered from a sibling fixture file, not a heredoc: Homebrew bash >= 5.1
# writes an entire heredoc body to a pipe before forking the reader, and
# macOS's 512-byte pipe buffer deadlocks on this ~1413B body (#5074). The
# body is fully static (was a quoted <<'SH', no shell expansion), so
# the file is byte-identical to the original heredoc body.
cp "${repo_root}/scripts/lib/test-verify-remote-e2e-tfstate-warnings-fake-docker.sh" "${fake_bin}/docker"
chmod +x "${fake_bin}/docker"

# Delivered from a sibling fixture file, not a heredoc: Homebrew bash >= 5.1
# writes an entire heredoc body to a pipe before forking the reader, and
# macOS's 512-byte pipe buffer deadlocks on this ~1168B body (#5074). The
# body is fully static (was a quoted <<'SH', no shell expansion), so
# the file is byte-identical to the original heredoc body.
cp "${repo_root}/scripts/lib/test-verify-remote-e2e-tfstate-warnings-fake-curl.sh" "${fake_bin}/curl"
chmod +x "${fake_bin}/curl"

write_service_state() {
  local service="$1"
  local container_id="${service}-container"
  printf '%s\n' "${container_id}" >"${state_dir}/service_ids/${service}"
  printf 'running healthy\n' >"${state_dir}/containers/${container_id}"
}

write_common_state() {
  rm -f "${state_dir}/curl-targets"
  local service
  for service in \
    eshu mcp-server ingester projector resolution-engine workflow-coordinator \
    collector-terraform-state collector-oci-registry collector-package-registry \
    collector-sbom-attestation collector-security-alerts \
    collector-vulnerability-intelligence collector-aws-cloud scanner-worker
  do
    write_service_state "${service}"
  done
  cat >"${state_dir}/index-status.json" <<'JSON'
{"status":"healthy","queue":{"outstanding":0,"in_flight":0,"pending":0,"retrying":0,"failed":0,"dead_letter":0},"coordinator":{"run_status_counts":[{"name":"complete","count":4}],"completeness_counts":[]}}
JSON
  cat >"${state_dir}/package-count.json" <<'JSON'
{"total_packages":3}
JSON
  cat >"${state_dir}/advisory-evidence.json" <<'JSON'
{"count":1}
JSON
  cat >"${state_dir}/impact-count.json" <<'JSON'
{"total_findings":2,"affected_findings":2}
JSON
  cat >"${state_dir}/security-alert-count.json" <<'JSON'
{"total_reconciliations":1}
JSON
  cat >"${state_dir}/sbom-count.json" <<'JSON'
{"total_attachments":1}
JSON
  cat >"${state_dir}/container-image-count.json" <<'JSON'
{"total_identities":1}
JSON
}

run_verifier() {
  ESHU_REMOTE_E2E_TEST_STATE="${state_dir}" \
    PATH="${fake_bin}:${PATH}" \
    ESHU_REMOTE_E2E_COMPOSE_FILES="docker-compose.remote-e2e.yaml" \
    "${verifier}" >/tmp/eshu-remote-e2e-tfstate.out 2>/tmp/eshu-remote-e2e-tfstate.err
}

expect_fail_with() {
  local pattern="$1"
  if run_verifier; then
    printf 'expected remote E2E runtime verifier to fail with %s\n' "${pattern}" >&2
    sed -n '1,220p' /tmp/eshu-remote-e2e-tfstate.out >&2
    exit 1
  fi
  if ! rg -q "${pattern}" /tmp/eshu-remote-e2e-tfstate.err; then
    printf 'expected failure output to contain %s\n' "${pattern}" >&2
    sed -n '1,220p' /tmp/eshu-remote-e2e-tfstate.err >&2
    exit 1
  fi
}

expect_pass() {
  if ! run_verifier; then
    printf 'expected remote E2E runtime verifier to pass\n' >&2
    sed -n '1,220p' /tmp/eshu-remote-e2e-tfstate.err >&2
    exit 1
  fi
}

write_common_state
cat >"${state_dir}/status-index.json" <<'JSON'
{}
JSON
expect_fail_with 'Terraform-state warning summary missing from status readback'

write_common_state
# Delivered from a sibling fixture file, not a heredoc: Homebrew bash >= 5.1
# writes an entire heredoc body to a pipe before forking the reader, and
# macOS's 512-byte pipe buffer deadlocks on this ~587B body (#5074). The
# body is fully static (was a quoted <<'JSON', no shell expansion), so
# the file is byte-identical to the original heredoc body.
cat "${repo_root}/scripts/lib/test-verify-remote-e2e-tfstate-warnings-status-index-exceeded.json" >"${state_dir}/status-index.json"
expect_fail_with 'Terraform-state state_missing warnings exceeded'

write_common_state
cat >"${state_dir}/status-index.json" <<'JSON'
{
  "terraform_state": {
    "warning_summary": [
      {"warning_kind":"state_missing","reason":"s3_not_found","scope_class":"s3","count":1}
    ],
    "recent_warnings": []
  }
}
JSON
export ESHU_REMOTE_E2E_TFSTATE_STATE_MISSING_MAX=1
expect_fail_with 'Terraform-state state_missing warning detail missing from status readback'
unset ESHU_REMOTE_E2E_TFSTATE_STATE_MISSING_MAX

write_common_state
# Delivered from a sibling fixture file, not a heredoc: Homebrew bash >= 5.1
# writes an entire heredoc body to a pipe before forking the reader, and
# macOS's 512-byte pipe buffer deadlocks on this ~809B body (#5074). The
# body is fully static (was a quoted <<'JSON', no shell expansion), so
# the file is byte-identical to the original heredoc body.
cat "${repo_root}/scripts/lib/test-verify-remote-e2e-tfstate-warnings-status-index-pass.json" >"${state_dir}/status-index.json"
export ESHU_REMOTE_E2E_TFSTATE_STATE_MISSING_MAX=2
expect_pass
if ! rg -q 'Terraform-state proof summary: configured_targets=2 attempted_reads=2 successful_snapshots=1 missing_states=1' /tmp/eshu-remote-e2e-tfstate.out; then
  printf 'expected Terraform-state proof summary in verifier output\n' >&2
  sed -n '1,240p' /tmp/eshu-remote-e2e-tfstate.out >&2
  exit 1
fi
if ! rg -q 'Terraform-state warning summary: warning_kind=state_missing reason=s3_not_found scope_class=s3 count=1' /tmp/eshu-remote-e2e-tfstate.out; then
  printf 'expected Terraform-state warning summary in verifier output\n' >&2
  sed -n '1,240p' /tmp/eshu-remote-e2e-tfstate.out >&2
  exit 1
fi
if ! rg -q 'Terraform-state warning detail: warning_kind=state_missing reason=s3_not_found source=graph source_handle=state_snapshot:s3:hash-missing safe_locator_hash=hash-missing' /tmp/eshu-remote-e2e-tfstate.out; then
  printf 'expected actionable Terraform-state warning detail in verifier output\n' >&2
  sed -n '1,240p' /tmp/eshu-remote-e2e-tfstate.out >&2
  exit 1
fi
if rg -q 'tfstate-prod|services/deleted|123456789012|/secure/local' /tmp/eshu-remote-e2e-tfstate.out; then
  printf 'Terraform-state verifier output leaked raw state locator context\n' >&2
  sed -n '1,240p' /tmp/eshu-remote-e2e-tfstate.out >&2
  exit 1
fi
unset ESHU_REMOTE_E2E_TFSTATE_STATE_MISSING_MAX

printf 'verify-remote-e2e-tfstate-warnings tests passed\n'
