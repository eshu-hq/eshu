#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify_remote_e2e_runtime_state.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fake_bin="${tmp_root}/bin"
state_dir="${tmp_root}/state"
mkdir -p "${fake_bin}" "${state_dir}/containers" "${state_dir}/service_ids"

cat >"${fake_bin}/docker" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

state_dir="${ESHU_REMOTE_E2E_TEST_STATE:?set ESHU_REMOTE_E2E_TEST_STATE}"

if [[ "${1:-}" == "inspect" ]]; then
  shift
  while [[ "${1:-}" == -* ]]; do
    shift
    if [[ "${1:-}" == "{{"* ]]; then
      shift
    fi
  done
  cat "${state_dir}/containers/${1:?container id required}"
  exit 0
fi

if [[ "${1:-}" != "compose" ]]; then
  echo "unexpected docker command: $*" >&2
  exit 2
fi

shift
while (($# > 0)); do
  case "${1}" in
    --env-file|-f|-p|--project-name) shift 2 ;;
    *) break ;;
  esac
done

subcommand="${1:-}"
shift || true
case "${subcommand}" in
  config)
    if [[ "${1:-}" == "--services" ]]; then
      printf '%s\n' \
        eshu mcp-server ingester projector resolution-engine workflow-coordinator \
        collector-terraform-state collector-oci-registry collector-package-registry \
        collector-sbom-attestation collector-security-alerts \
        collector-vulnerability-intelligence collector-aws-cloud scanner-worker
      exit 0
    fi
    echo "unexpected compose config args: $*" >&2
    exit 2
    ;;
  ps)
    service="${@: -1}"
    if [[ -f "${state_dir}/service_ids/${service}" ]]; then
      cat "${state_dir}/service_ids/${service}"
    fi
    ;;
  port)
    printf '0.0.0.0:18080\n'
    ;;
  exec)
    printf 'test-api-key\n'
    ;;
  *)
    echo "unexpected compose subcommand: ${subcommand}" >&2
    exit 2
    ;;
esac
SH
chmod +x "${fake_bin}/docker"

cat >"${fake_bin}/curl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

state_dir="${ESHU_REMOTE_E2E_TEST_STATE:?set ESHU_REMOTE_E2E_TEST_STATE}"
printf '%s\n' "$*" >>"${state_dir}/curl-targets"
if [[ "$*" == *"test-api-key"* ]]; then
  echo "curl arguments leaked API key" >&2
  exit 2
fi
if [[ "$*" != *"--max-time"* ]]; then
  echo "curl call is missing max-time" >&2
  exit 2
fi

case "$*" in
  *"/api/v0/index-status"*) cat "${state_dir}/index-status.json" ;;
  *"/api/v0/status/index"*) cat "${state_dir}/status-index.json" ;;
  *"/api/v0/package-registry/packages/count"*) cat "${state_dir}/package-count.json" ;;
  *"/api/v0/supply-chain/advisories/evidence"*) cat "${state_dir}/advisory-evidence.json" ;;
  *"/api/v0/supply-chain/impact/findings/count"*) cat "${state_dir}/impact-count.json" ;;
  *"/api/v0/supply-chain/security-alerts/reconciliations/count"*) cat "${state_dir}/security-alert-count.json" ;;
  *"/api/v0/supply-chain/sbom-attestations/attachments/count"*) cat "${state_dir}/sbom-count.json" ;;
  *"/api/v0/supply-chain/container-images/identities/count"*) cat "${state_dir}/container-image-count.json" ;;
  *)
    echo "unexpected curl target: $*" >&2
    exit 2
    ;;
esac
SH
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
cat >"${state_dir}/status-index.json" <<'JSON'
{
  "terraform_state": {
    "warning_summary": [
      {"warning_kind":"state_missing","reason":"s3_not_found","scope_class":"s3","count":2},
      {"warning_kind":"state_too_large","reason":"size_limit","scope_class":"local","count":1}
    ],
    "recent_warnings": [
      {"safe_locator_hash":"hash-a","warning_kind":"state_missing","reason":"s3_not_found","source":"graph","source_handle":"state_snapshot:s3:hash-a"},
      {"safe_locator_hash":"hash-b","warning_kind":"state_missing","reason":"s3_not_found","source":"graph","source_handle":"state_snapshot:s3:hash-b"}
    ]
  }
}
JSON
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
cat >"${state_dir}/status-index.json" <<'JSON'
{
  "terraform_state": {
    "last_serials": [
      {
        "safe_locator_hash": "hash-ok",
        "backend_kind": "s3",
        "serial": 42,
        "generation_id": "terraform_state:state_snapshot:s3:hash-ok:lineage-ok:serial:42"
      }
    ],
    "warning_summary": [
      {"warning_kind":"state_missing","reason":"s3_not_found","scope_class":"s3","count":1}
    ],
    "recent_warnings": [
      {
        "safe_locator_hash":"hash-missing",
        "warning_kind":"state_missing",
        "reason":"s3_not_found",
        "source":"graph",
        "source_handle":"state_snapshot:s3:hash-missing",
        "raw_bucket":"tfstate-prod",
        "raw_key":"services/deleted/terraform.tfstate",
        "account_id":"123456789012",
        "path":"/secure/local/terraform.tfstate"
      }
    ]
  }
}
JSON
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
