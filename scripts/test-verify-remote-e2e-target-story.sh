#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify_remote_e2e_target_story.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fake_bin="${tmp_root}/bin"
state_dir="${tmp_root}/state"
mkdir -p "${fake_bin}" "${state_dir}"

cat >"${fake_bin}/curl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

state_dir="${ESHU_REMOTE_E2E_TEST_STATE:?set ESHU_REMOTE_E2E_TEST_STATE}"
printf '%s\n' "$*" >>"${state_dir}/curl-targets"
if [[ "$*" == *"test-api-key"* ]]; then
  echo "curl arguments leaked API key" >&2
  exit 2
fi
curl_config=""
args=("$@")
for ((i = 0; i < ${#args[@]}; i++)); do
  if [[ "${args[$i]}" == "-K" ]]; then
    curl_config="${args[$((i + 1))]:-}"
  fi
done
if [[ -z "${curl_config}" ]] || ! rg -q 'Accept: application/eshu.envelope\+json' "${curl_config}"; then
  echo "curl call is missing Eshu envelope Accept header" >&2
  exit 2
fi
if [[ "$*" != *"--max-time"* ]]; then
  echo "curl call is missing max-time" >&2
  exit 2
fi
case "$*" in
  *"/api/v0/repositories/repo%3A%2F%2Fexample%2Fapi/story"*)
    cat "${state_dir}/repo-story.json"
    ;;
  *"/api/v0/supply-chain/impact/findings/count?repository_id=repo%3A%2F%2Fexample%2Fapi&profile=comprehensive"*)
    cat "${state_dir}/impact-count.json"
    ;;
  *"/api/v0/supply-chain/security-alerts/reconciliations?repository_id=repo%3A%2F%2Fexample%2Fapi&limit=1"*)
    cat "${state_dir}/security-alert-count.json"
    ;;
  *"/api/v0/supply-chain/container-images/identities/count?digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&repository_id=oci-registry%3A%2F%2Fregistry.example%2Fteam%2Fapi"*)
    cat "${state_dir}/image-count.json"
    ;;
  *"/api/v0/supply-chain/sbom-attestations/attachments/count?subject_digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"*)
    cat "${state_dir}/sbom-count.json"
    ;;
  *"/api/v0/service-catalog/correlations?repository_id=repo%3A%2F%2Fexample%2Fapi&limit=1&service_id=service%3Aapi"*)
    cat "${state_dir}/service-catalog.json"
    ;;
  *"/api/v0/ci-cd/run-correlations/count?repository_id=repo%3A%2F%2Fexample%2Fapi&artifact_digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"*)
    cat "${state_dir}/cicd-count.json"
    ;;
  *)
    echo "unexpected curl target: $*" >&2
    exit 2
    ;;
esac
SH
chmod +x "${fake_bin}/curl"

write_manifest() {
  cat >"${state_dir}/target-story.json" <<'JSON'
{
  "target_repository_id": "repo://example/api",
  "expected_security_alert_repository": "example/api",
  "expected_service_id": "service:api",
  "expected_oci_repository_id": "oci-registry://registry.example/team/api",
  "expected_image_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "expected_sbom_subject_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "minimums": {
    "impact_findings": 1,
    "security_alert_reconciliations": 1,
    "container_image_identities": 1,
    "sbom_attachments": 1,
    "service_catalog_correlations": 1,
    "ci_cd_run_correlations": 1
  }
}
JSON
}

reset_state() {
  rm -f "${state_dir}/curl-targets"
  write_manifest
  cat >"${state_dir}/repo-story.json" <<'JSON'
{"data":{"repository":{"id":"repo://example/api","name":"api"}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/impact-count.json" <<'JSON'
{"data":{"total_findings":5,"affected_findings":5},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/security-alert-count.json" <<'JSON'
{
  "data": {
    "count": 1,
    "reconciliations": [
      {
        "reconciliation_id": "rec-1",
        "provider_alert": {
          "provider_alert_id": "github_dependabot:security-alert:github:example/api:42",
          "repository_id": "repository:r_example_api"
        }
      }
    ]
  },
  "truth": {"level": "exact", "freshness": {"state": "fresh"}},
  "error": null
}
JSON
  cat >"${state_dir}/image-count.json" <<'JSON'
{"data":{"total_identities":1},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/sbom-count.json" <<'JSON'
{"data":{"total_attachments":1},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/service-catalog.json" <<'JSON'
{"data":{"count":1,"correlations":[{"correlation_id":"corr-1","service_id":"service:api"}],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/cicd-count.json" <<'JSON'
{"data":{"total_correlations":1},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
}

run_verifier() {
  ESHU_REMOTE_E2E_TEST_STATE="${state_dir}" \
    PATH="${fake_bin}:${PATH}" \
    ESHU_REMOTE_E2E_API_BASE_URL="http://127.0.0.1:18080/api/v0" \
    ESHU_REMOTE_E2E_API_KEY="test-api-key" \
    "${verifier}" >/tmp/eshu-remote-e2e-target-story.out 2>/tmp/eshu-remote-e2e-target-story.err
}

expect_pass() {
  if ! run_verifier; then
    printf 'expected target-story verifier to pass\n' >&2
    sed -n '1,160p' /tmp/eshu-remote-e2e-target-story.err >&2
    exit 1
  fi
}

expect_fail_with() {
  local pattern="$1"
  if run_verifier; then
    printf 'expected target-story verifier to fail with %s\n' "${pattern}" >&2
    sed -n '1,200p' /tmp/eshu-remote-e2e-target-story.out >&2
    exit 1
  fi
  if ! rg -q "${pattern}" /tmp/eshu-remote-e2e-target-story.err; then
    printf 'expected failure output to contain %s\n' "${pattern}" >&2
    sed -n '1,200p' /tmp/eshu-remote-e2e-target-story.err >&2
    exit 1
  fi
}

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
expect_pass
if rg -q 'repo://example/api|oci-registry://registry.example/team/api' /tmp/eshu-remote-e2e-target-story.out; then
  printf 'target-story proof leaked private target values\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story.out >&2
  exit 1
fi

reset_state
jq '.expected_security_alert_repository = "repository:r_example_api"' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
expect_pass

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
cat >"${state_dir}/image-count.json" <<'JSON'
{"data":{"total_identities":0},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
expect_fail_with 'target container_image_identities=0 below required minimum 1'

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
cat >"${state_dir}/service-catalog.json" <<'JSON'
{"data":{"count":0,"correlations":[],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
expect_fail_with 'target service_catalog_correlations=0 below required minimum 1'

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
cat >"${state_dir}/security-alert-count.json" <<'JSON'
{
  "data": {
    "count": 1,
    "reconciliations": [
      {
        "reconciliation_id": "rec-1",
        "provider_alert": {
          "repository_id": "security-alert:github:example/other"
        }
      }
    ]
  },
  "truth": {"level": "exact", "freshness": {"state": "fresh"}},
  "error": null
}
JSON
expect_fail_with 'target security_alert_reconciliations=0 below required minimum 1'

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
jq 'del(.expected_image_digest)' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target container_image_identities requires expected_image_digest or expected_image_ref'

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
jq 'del(.expected_security_alert_repository)' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target security_alert_reconciliations requires expected_security_alert_repository'

reset_state
rm -f "${state_dir}/target-story.json"
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
expect_fail_with 'target story file not found'

reset_state
unset ESHU_REMOTE_E2E_TARGET_STORY_FILE
expect_pass
if ! rg -q 'remote E2E target story proof skipped: no target story configured' /tmp/eshu-remote-e2e-target-story.out; then
  printf 'expected no-target skip message\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story.out >&2
  exit 1
fi

printf 'verify-remote-e2e-target-story tests passed\n'
