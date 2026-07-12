#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
HARNESS="${REPO_ROOT}/scripts/e2e_remote_compose_suite.sh"
MANIFEST_VALIDATOR="${REPO_ROOT}/scripts/verify_e2e_evidence_manifest.sh"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

fake_bin="${TMP_DIR}/bin"
state_dir="${TMP_DIR}/state"
mkdir -p "${fake_bin}" "${state_dir}/logs"

cat >"${fake_bin}/docker" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

state_dir="${ESHU_REMOTE_COMPOSE_SUITE_TEST_STATE:?set ESHU_REMOTE_COMPOSE_SUITE_TEST_STATE}"

if [[ "${1:-}" == "stats" ]]; then
  cat "${state_dir}/docker-stats.jsonl"
  exit 0
fi

if [[ "${1:-}" != "compose" ]]; then
  echo "unexpected docker command: $*" >&2
  exit 2
fi

shift
while (($# > 0)); do
  case "${1}" in
    --env-file|-f|-p|--project-name)
      shift 2
      ;;
    *)
      break
      ;;
  esac
done

subcommand="${1:-}"
shift || true
case "${subcommand}" in
  config)
    [[ "${1:-}" == "--services" ]] || { echo "unexpected config args: $*" >&2; exit 2; }
    cat "${state_dir}/services"
    ;;
  logs)
    while (($# > 0)); do
      case "${1}" in
        --no-color|--timestamps)
          shift
          ;;
        --tail)
          shift 2
          ;;
        *)
          service="${1}"
          shift
          if [[ -f "${state_dir}/logs/${service}.log" ]]; then
            cat "${state_dir}/logs/${service}.log"
          else
            printf '%s started cleanly\n' "${service}"
          fi
          ;;
      esac
    done
    ;;
  exec)
    while (($# > 0)); do
      case "${1}" in
        -T)
          shift
          ;;
        postgres)
          shift
          ;;
        *)
          break
          ;;
      esac
    done
    query="$*"
    case "${query}" in
      *"fact_records"*)
        cat "${state_dir}/fact-counts.tsv"
        ;;
      *"workflow_work_items"*)
        cat "${state_dir}/workflow-counts.tsv"
        ;;
      *"relationship_evidence_facts"*)
        cat "${state_dir}/reducer-relationship-counts.tsv"
        ;;
      *)
        echo "unexpected postgres query: ${query}" >&2
        exit 2
        ;;
    esac
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

state_dir="${ESHU_REMOTE_COMPOSE_SUITE_TEST_STATE:?set ESHU_REMOTE_COMPOSE_SUITE_TEST_STATE}"
printf '%s\n' "$*" >>"${state_dir}/curl-targets"
if [[ "$*" == *"test-api-key"* ]]; then
  echo "curl arguments leaked API key" >&2
  exit 2
fi
if [[ "$*" != *"--max-time"* && "$*" != *"-m"* ]]; then
  echo "curl call is missing timeout" >&2
  exit 2
fi
case "$*" in
  *"/debug/pprof/"*)
    printf 'pprof index\n'
    ;;
  *"/api/v0/index-status"*)
    cat "${state_dir}/index-status.json"
    ;;
  *)
    echo "unexpected curl target: $*" >&2
    exit 2
    ;;
esac
SH
chmod +x "${fake_bin}/curl"

runtime_verifier="${TMP_DIR}/runtime-verifier"
cat >"${runtime_verifier}" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

state_dir="${ESHU_REMOTE_COMPOSE_SUITE_TEST_STATE:?set ESHU_REMOTE_COMPOSE_SUITE_TEST_STATE}"
if [[ -f "${state_dir}/runtime-fail" ]]; then
  echo "runtime verifier failed" >&2
  exit 11
fi
printf 'remote runtime state verified\n'
SH
chmod +x "${runtime_verifier}"

write_volume_proof() {
  local path="$1"
  local kind="$2"
  if [[ "${kind}" == "clean" ]]; then
    jq -n '{
      schema_version: 1,
      proof_id: "remote-compose-suite-test-clean",
      run_kind: "clean",
      clean_volume_state: "reset_before_run",
      backing_stores: {
        nornicdb_data: {status: "pass", before: "absent", after: "present"},
        postgres_data: {status: "pass", before: "absent", after: "present"},
        eshu_data: {status: "pass", before: "absent", after: "present"}
      }
    }' >"${path}"
  else
    jq -n '{
      schema_version: 1,
      proof_id: "remote-compose-suite-test-preserved",
      run_kind: "preserved",
      previous_run_kind: "clean",
      restart_without_prune: true,
      backing_stores: {
        nornicdb_data: {status: "pass", same_as_clean: true},
        postgres_data: {status: "pass", same_as_clean: true},
        eshu_data: {status: "pass", same_as_clean: true}
      }
    }' >"${path}"
  fi
}

write_corpus_coverage() {
  local path="$1"
  jq -n '{
    schema_version: 1, mode: "representative", repository_count: 24,
    ecosystems: {
      npm: {status: "pass", count: 3},
      gomod: {status: "pass", count: 2},
      pypi: {status: "pass", count: 2},
      maven: {status: "pass", count: 2},
      composer: {status: "pass", count: 1},
      rubygems: {status: "pass", count: 1},
      cargo: {status: "pass", count: 1},
      nuget: {status: "pass", count: 1}
    },
    evidence_families: {
      terraform_iac: {status: "pass", count: 2},
      kubernetes_iac: {status: "pass", count: 2},
      image_sbom: {status: "pass", count: 2},
      deployment: {status: "pass", count: 2},
      relationship_evidence: {status: "pass", count: 2},
      vulnerability: {status: "pass", count: 4},
      observability: {status: "pass", count: 3},
      incident: {status: "pass", count: 1},
      work_item: {status: "pass", count: 1}
    }
  }' >"${path}"
}

write_readback_proof() {
  local path="$1"
  jq -n '{
    schema_version: 1,
    proof_id: "remote-compose-suite-readback-test",
    surfaces: {
      api: {status: "pass", checked: 11, failed: 0, truncated: 0, unsupported: 0, missing_evidence: 0, ambiguous: 0},
      mcp: {status: "pass", checked: 11, failed: 0, truncated: 0, unsupported: 0, missing_evidence: 0, ambiguous: 0},
      cli: {status: "pass", checked: 11, failed: 0, truncated: 0, unsupported: 0, missing_evidence: 0, ambiguous: 0}
    },
    queue: {retrying: 0, failed: 0, dead_letter: 0}
  }' >"${path}"
}

reset_state() {
  rm -f "${state_dir}/runtime-fail" "${state_dir}/curl-targets"
  rm -rf "${state_dir}/logs"
  mkdir -p "${state_dir}/logs"
  cat >"${state_dir}/services" <<'SERVICES'
eshu
mcp-server
ingester
projector
resolution-engine
workflow-coordinator
collector-terraform-state
collector-oci-registry
collector-package-registry
collector-sbom-attestation
collector-security-alerts
collector-vulnerability-intelligence
collector-aws-cloud
scanner-worker
collector-confluence
collector-pagerduty
collector-jira
collector-grafana
collector-prometheus-mimir
collector-loki
collector-tempo
SERVICES
  cat >"${state_dir}/index-status.json" <<'JSON'
{
  "status": "healthy",
  "queue": {
    "outstanding": 0,
    "pending": 0,
    "in_flight": 0,
    "retrying": 0,
    "failed": 0,
    "dead_letter": 0
  },
  "coordinator": {
    "run_status_counts": [{"name": "complete", "count": 16}],
    "work_item_status_counts": [{"name": "completed", "count": 16}],
    "completeness_counts": []
  }
}
JSON
  cat >"${state_dir}/docker-stats.jsonl" <<'JSONL'
{"Name":"eshu","CPUPerc":"12.5%","MemUsage":"128MiB / 1GiB"}
{"Name":"resolution-engine","CPUPerc":"22.0%","MemUsage":"256MiB / 2GiB"}
JSONL
  cat >"${state_dir}/fact-counts.tsv" <<'TSV'
git	repository	10
terraform_state	terraform_state.resource	5
aws	aws_resource	7
oci_registry	oci_registry.image_manifest	4
package_registry	package_registry.package_version	6
sbom_document	sbom.component	2
security_alert	security_alert.repository_alert	4
vulnerability_intelligence	vulnerability.affected_package	8
scanner_worker	scanner_worker.vulnerability	3
confluence	documentation_source	1
pagerduty	incident.record	1
jira	work_item.record	1
grafana	observability.observed_dashboard	1
prometheus_mimir	observability.observed_target	1
loki	observability.observed_log_signal	1
tempo	observability.observed_trace_signal	1
reducer	reducer_package_correlation	10
reducer	reducer_aws_cloud_relationship	5
reducer	reducer_container_image_identity	3
reducer	reducer_sbom_attestation_attachment	3
reducer	reducer_security_alert_reconciliation	4
reducer	reducer_supply_chain_impact_finding	4
reducer	reducer_deployment_correlation	2
reducer	reducer_observability_correlation	1
reducer	reducer_incident_work_item_correlation	1
TSV
  cat >"${state_dir}/reducer-relationship-counts.tsv" <<'TSV'
terraform_iac_relationships	5	5
TSV
  cat >"${state_dir}/workflow-counts.tsv" <<'TSV'
git	completed	1
terraform_state	completed	1
aws	completed	1
oci_registry	completed	1
package_registry	completed	1
sbom_attestation	completed	1
security_alert	completed	1
vulnerability_intelligence	completed	1
scanner_worker	completed	1
confluence	completed	1
pagerduty	completed	1
jira	completed	1
grafana	completed	1
prometheus_mimir	completed	1
loki	completed	1
tempo	completed	1
TSV
}

run_harness() {
  local run_kind="$1"
  local manifest="$2"
  local volume_proof="$3"
  local coverage="${TMP_DIR}/corpus-coverage.json"
  local readback_proof="${TMP_DIR}/readback-proof.json"
  shift 3
  write_corpus_coverage "${coverage}"
  write_readback_proof "${readback_proof}"
  ESHU_REMOTE_COMPOSE_SUITE_TEST_STATE="${state_dir}" \
    ESHU_E2E_RUNTIME_STATE_SCRIPT="${runtime_verifier}" \
    ESHU_REMOTE_E2E_UNSUPPORTED_HOSTED_COLLECTORS="${ESHU_REMOTE_E2E_UNSUPPORTED_HOSTED_COLLECTORS:-}" \
    ESHU_REMOTE_E2E_UNSUPPORTED_REDUCERS="${ESHU_REMOTE_E2E_UNSUPPORTED_REDUCERS:-}" \
    PATH="${fake_bin}:${PATH}" \
    "${HARNESS}" \
      --run-kind "${run_kind}" \
      --manifest "${manifest}" \
      --api-base-url "http://127.0.0.1:18080/api/v0" \
      --api-key "test-api-key" \
      --pprof-base-url "http://127.0.0.1:16060" \
      --runtime-volume-proof "${volume_proof}" \
      --out-dir "${TMP_DIR}/evidence-${run_kind}" \
      --corpus-mode representative \
      --repository-count 24 \
      --corpus-coverage "${coverage}" \
      --readback-proof "${readback_proof}" \
      --image-tag-candidate v0.0.3-pre-release-test \
      --compose-files "docker-compose.remote-e2e.yaml:docker-compose.remote-e2e.pprof.yaml" \
      "$@"
}

expect_pass() {
  local label="$1"
  local run_kind="$2"
  local manifest="${TMP_DIR}/${label}.json"
  local volume_proof="${TMP_DIR}/${label}-volume.json"
  shift 2
  write_volume_proof "${volume_proof}" "${run_kind}"
  if ! run_harness "${run_kind}" "${manifest}" "${volume_proof}" "$@" >"${TMP_DIR}/${label}.out" 2>"${TMP_DIR}/${label}.err"; then
    printf 'expected %s to pass\n' "${label}" >&2
    sed -n '1,180p' "${TMP_DIR}/${label}.err" >&2
    exit 1
  fi
  "${MANIFEST_VALIDATOR}" "${manifest}" >/dev/null
  if ! jq -e '.privacy.status == "pass" and .observability.pprof_status == "reachable"' "${manifest}" >/dev/null; then
    printf 'expected %s manifest to include privacy and pprof pass evidence\n' "${label}" >&2
    jq . "${manifest}" >&2
    exit 1
  fi
}

expect_fail_with() {
  local label="$1"
  local run_kind="$2"
  local expected="$3"
  local manifest="${TMP_DIR}/${label}.json"
  local volume_proof="${TMP_DIR}/${label}-volume.json"
  shift 3
  write_volume_proof "${volume_proof}" "${run_kind}"
  if run_harness "${run_kind}" "${manifest}" "${volume_proof}" "$@" >"${TMP_DIR}/${label}.out" 2>"${TMP_DIR}/${label}.err"; then
    printf 'expected %s to fail\n' "${label}" >&2
    jq . "${manifest}" >&2 || true
    exit 1
  fi
  if ! rg --fixed-strings --quiet -- "${expected}" "${TMP_DIR}/${label}.err"; then
    printf 'expected %s failure to contain %s\n' "${label}" "${expected}" >&2
    sed -n '1,220p' "${TMP_DIR}/${label}.err" >&2
    exit 1
  fi
}

reset_state
expect_pass clean clean
if ! jq -e '
  .run.kind == "clean" and
  .collectors.git.status == "pass" and
  .collectors.pagerduty.facts == 1 and
  .reducers.terraform_iac_relationships.source_facts == 5 and
  .reducers.terraform_iac_relationships.reducer_facts == 5 and
  .reducers.vulnerability_matching.reducer_facts == 4 and
  .reducers.vulnerability_matching.readback.api.status == "pass" and
  .reducers.supply_chain_impact.count == 4 and
  .workflow.collector_claims.terraform_state.completed == 1 and
  .queue.retrying == 0 and .queue.dead_letter == 0
' "${TMP_DIR}/clean.json" >/dev/null; then
  printf 'clean manifest did not contain expected aggregate collector/reducer/workflow evidence\n' >&2
  jq . "${TMP_DIR}/clean.json" >&2
  exit 1
fi

reset_state
printf 'panic: boom at /Users/private/repo\n' >"${state_dir}/logs/resolution-engine.log"
expect_fail_with log_failure clean "forbidden log pattern"

reset_state
touch "${state_dir}/runtime-fail"
expect_fail_with runtime_failure clean "runtime state verifier failed"

reset_state
export ESHU_REMOTE_E2E_UNSUPPORTED_HOSTED_COLLECTORS=unknown
expect_fail_with invalid_unsupported_hosted_collector clean "unsupported hosted collector row is invalid: unknown"
unset ESHU_REMOTE_E2E_UNSUPPORTED_HOSTED_COLLECTORS

reset_state
cat >"${state_dir}/fact-counts.tsv" <<'TSV'
git	repository	10
TSV
expect_fail_with missing_collector clean "collector terraform_state has no source facts"

reset_state
awk 'BEGIN {FS=OFS="\t"} $1!="pagerduty" {print}' "${state_dir}/fact-counts.tsv" >"${state_dir}/fact-counts.next"
mv "${state_dir}/fact-counts.next" "${state_dir}/fact-counts.tsv"
expect_fail_with hosted_collector_without_facts clean "collector pagerduty: no source facts observed for enabled collector service"
jq -e '
  .status == "fail" and
  .collectors.pagerduty.status == "fail" and
  .collectors.pagerduty.facts == 0 and
  .collectors.pagerduty.reason == "no source facts observed for enabled collector service"
' "${TMP_DIR}/hosted_collector_without_facts.json" >/dev/null || {
  printf 'missing hosted collector facts did not preserve an explicit failed collector reason\n' >&2
  jq . "${TMP_DIR}/hosted_collector_without_facts.json" >&2
  exit 1
}

reset_state
awk '$0!="collector-pagerduty" {print}' "${state_dir}/services" >"${state_dir}/services.next"
mv "${state_dir}/services.next" "${state_dir}/services"
awk 'BEGIN {FS=OFS="\t"} $1!="pagerduty" {print}' "${state_dir}/fact-counts.tsv" >"${state_dir}/fact-counts.next"
mv "${state_dir}/fact-counts.next" "${state_dir}/fact-counts.tsv"
expect_fail_with disabled_hosted_collector clean "collector pagerduty: collector service disabled in remote Compose profile"
jq -e '
  .status == "partial" and
  .collectors.pagerduty.status == "skipped" and
  .collectors.pagerduty.facts == 0 and
  .collectors.pagerduty.reason == "collector service disabled in remote Compose profile"
' "${TMP_DIR}/disabled_hosted_collector.json" >/dev/null || {
  printf 'disabled hosted collector did not preserve an explicit skipped collector reason\n' >&2
  jq . "${TMP_DIR}/disabled_hosted_collector.json" >&2
  exit 1
}

reset_state
awk '$0!="collector-pagerduty" {print}' "${state_dir}/services" >"${state_dir}/services.next"
mv "${state_dir}/services.next" "${state_dir}/services"
awk 'BEGIN {FS=OFS="\t"} $1!="pagerduty" {print}' "${state_dir}/fact-counts.tsv" >"${state_dir}/fact-counts.next"
mv "${state_dir}/fact-counts.next" "${state_dir}/fact-counts.tsv"
export ESHU_REMOTE_E2E_UNSUPPORTED_HOSTED_COLLECTORS=pagerduty
expect_fail_with unsupported_hosted_collector clean "collector pagerduty: collector explicitly unsupported in remote Compose profile"
unset ESHU_REMOTE_E2E_UNSUPPORTED_HOSTED_COLLECTORS
jq -e '
  .status == "partial" and
  .collectors.pagerduty.status == "unsupported" and
  .collectors.pagerduty.facts == 0 and
  .collectors.pagerduty.reason == "collector explicitly unsupported in remote Compose profile"
' "${TMP_DIR}/unsupported_hosted_collector.json" >/dev/null || {
  printf 'unsupported hosted collector did not preserve an explicit unsupported collector reason\n' >&2
  jq . "${TMP_DIR}/unsupported_hosted_collector.json" >&2
  exit 1
}

reset_state
expect_pass preserved preserved --previous-manifest "${TMP_DIR}/clean.json"
if ! jq -e '.run.kind == "preserved" and .preserved_restart.duplicate_guard_status == "pass"' "${TMP_DIR}/preserved.json" >/dev/null; then
  printf 'preserved manifest did not include duplicate guard pass evidence\n' >&2
  jq . "${TMP_DIR}/preserved.json" >&2
  exit 1
fi

reset_state
awk 'BEGIN {FS=OFS="\t"} $1=="git" {$3=11} {print}' "${state_dir}/fact-counts.tsv" >"${state_dir}/fact-counts.next"
mv "${state_dir}/fact-counts.next" "${state_dir}/fact-counts.tsv"
expect_fail_with preserved_duplicate preserved "preserved restart produced new facts" --previous-manifest "${TMP_DIR}/clean.json"

printf 'e2e remote compose suite tests passed\n'
