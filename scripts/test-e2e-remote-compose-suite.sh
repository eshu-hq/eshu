#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
suite="${repo_root}/scripts/e2e_remote_compose_suite.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fake_bin="${tmp_root}/bin"
state_dir="${tmp_root}/state"
mkdir -p "${fake_bin}" "${state_dir}"

cat >"${fake_bin}/docker" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

case "$*" in
  "stats --no-stream --format "*)
    if [[ "${ESHU_FAKE_DOCKER_STATS_EMPTY:-0}" == "1" ]]; then
      exit 0
    fi
    if [[ "${ESHU_FAKE_DOCKER_STATS_NO_MEM:-0}" == "1" ]]; then
      printf '{"name":"eshu","cpu":"12.5%%"}\n'
      exit 0
    fi
    printf '{"name":"eshu","cpu":"12.5%%","mem":"256MiB / 1GiB","net":"0B / 0B","block":"0B / 0B"}\n'
    ;;
  compose*)
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
      logs)
        if [[ "${ESHU_FAKE_LOGS_EMPTY:-0}" == "1" ]]; then
          exit 0
        fi
        printf 'eshu api ready\nworkflow coordinator complete\n'
        ;;
      *)
        printf 'unexpected docker compose subcommand: %s\n' "${subcommand}" >&2
        exit 2
        ;;
    esac
    ;;
  *)
    printf 'unexpected docker args: %s\n' "$*" >&2
    exit 2
    ;;
esac
SH
chmod +x "${fake_bin}/docker"

cat >"${fake_bin}/curl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

if [[ "${ESHU_FAKE_PPROF_DOWN:-0}" == "1" ]]; then
  exit 7
fi
url="${@: -1}"
case "${url}" in
  */debug/pprof/)
    printf 'pprof index\n'
    ;;
  *)
    printf 'unexpected curl url: %s\n' "${url}" >&2
    exit 2
    ;;
esac
SH
chmod +x "${fake_bin}/curl"

runtime_verifier="${tmp_root}/runtime-verifier.sh"
cat >"${runtime_verifier}" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

if [[ "${ESHU_FAKE_RUNTIME_VERIFIER_FAIL:-0}" == "1" ]]; then
  printf 'runtime verifier failed\n' >&2
  exit 1
fi
printf 'remote runtime verified\n'
SH
chmod +x "${runtime_verifier}"

write_summary() {
  local path="$1"
  jq -n '{
    schema_version: 1,
    status: "pass",
    corpus: {
      mode: "representative",
      repository_count: 24,
      coverage: {
        ecosystems: {
          npm: {status: "pass", count: 3},
          gomod: {status: "pass", count: 3},
          pypi: {status: "pass", count: 2},
          maven: {status: "pass", count: 2},
          composer: {status: "pass", count: 2},
          rubygems: {status: "pass", count: 1},
          cargo: {status: "pass", count: 1},
          nuget: {status: "pass", count: 1}
        },
        evidence_families: {
          terraform_iac: {status: "pass", count: 3},
          kubernetes_iac: {status: "pass", count: 2},
          image_sbom: {status: "pass", count: 2},
          deployment: {status: "pass", count: 2},
          vulnerability: {status: "pass", count: 4},
          observability: {status: "pass", count: 1},
          incident: {status: "pass", count: 1},
          work_item: {status: "pass", count: 1}
        }
      }
    },
    runtimes: {
      schema_bootstrap: {status: "pass"},
      api: {status: "pass"},
      mcp_server: {status: "pass"},
      ingester: {status: "pass"},
      resolution_engine: {status: "pass"},
      workflow_coordinator: {status: "pass"},
      hosted_collectors: {status: "pass"},
      scanner_worker: {status: "pass"}
    },
    collectors: {
      git: {status: "pass", facts: 10},
      terraform_state: {status: "pass", facts: 5},
      aws_cloud: {status: "pass", facts: 7},
      oci_registry: {status: "pass", facts: 4},
      package_registry: {status: "pass", facts: 6},
      sbom_attestation: {status: "pass", facts: 2},
      provider_security_alerts: {status: "pass", facts: 4},
      vulnerability_intelligence: {status: "pass", facts: 8},
      scanner_worker: {status: "pass", facts: 3},
      confluence: {status: "pass", facts: 1},
      pagerduty: {status: "pass", facts: 1},
      jira: {status: "pass", facts: 1},
      grafana: {status: "pass", facts: 1},
      prometheus_mimir: {status: "pass", facts: 1},
      loki: {status: "pass", facts: 1},
      tempo: {status: "pass", facts: 1}
    },
    reducers: {
      repository_dependencies: {status: "pass", count: 10},
      terraform_iac_relationships: {status: "pass", count: 5},
      aws_cloud_relationships: {status: "pass", count: 5},
      oci_image_identity: {status: "pass", count: 3},
      sbom_attachment: {status: "pass", count: 3},
      vulnerability_matching: {status: "pass", count: 4},
      provider_alert_reconciliation: {status: "pass", count: 4},
      supply_chain_impact: {status: "pass", count: 4},
      deployment_correlation: {status: "pass", count: 2},
      observability_correlation: {status: "pass", count: 1},
      incident_work_item_correlation: {status: "pass", count: 1}
    },
    readback: {
      api: {status: "pass", checked: 12, failed: 0, truncated: 0},
      mcp: {status: "pass", checked: 12, failed: 0, truncated: 0},
      cli: {status: "pass", checked: 6, failed: 0, truncated: 0}
    },
    queue: {
      pending: 0,
      in_flight: 0,
      retrying: 0,
      failed: 0,
      dead_letter: 0
    },
    privacy: {status: "pass"},
    preserved: {
      duplicate_claims: 0,
      duplicate_facts: 0,
      duplicate_findings: 0,
      new_dead_letters: 0
    },
    follow_up_issues: []
  }' >"${path}"
}

write_volume_proof() {
  local path="$1"
  local kind="$2"
  if [[ "${kind}" == "clean" ]]; then
    jq -n '{
      schema_version: 1,
      proof_id: "clean-volume-proof-v1",
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
      proof_id: "preserved-volume-proof-v1",
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

run_suite() {
  local label="$1"
  local kind="$2"
  local summary="$3"
  local manifest="$4"
  local volume_proof="$5"
  shift 5
  PATH="${fake_bin}:${PATH}" \
    ESHU_REMOTE_E2E_COMPOSE_FILES="docker-compose.remote-e2e.yaml" \
    "${suite}" \
      --run-kind "${kind}" \
      --manifest "${manifest}" \
      --summary "${summary}" \
      --volume-proof "${volume_proof}" \
      --runtime-verifier "${runtime_verifier}" \
      --pprof-base-url "http://127.0.0.1:16060" \
      --run-id "remote-suite-${label}" \
      --commit "1234567890abcdef" \
      --image-tag-candidate "v0.0.3-test" \
      --backend-kind "nornicdb" \
      "$@" >"${tmp_root}/${label}.out" 2>"${tmp_root}/${label}.err"
}

expect_pass() {
  local label="$1"
  local kind="$2"
  local summary="$3"
  local manifest="$4"
  local volume_proof="$5"
  shift 5
  if ! run_suite "${label}" "${kind}" "${summary}" "${manifest}" "${volume_proof}" "$@"; then
    printf 'expected %s to pass\n' "${label}" >&2
    sed -n '1,160p' "${tmp_root}/${label}.err" >&2
    exit 1
  fi
}

expect_fail_with() {
  local label="$1"
  local pattern="$2"
  local kind="$3"
  local summary="$4"
  local manifest="$5"
  local volume_proof="$6"
  shift 6
  if run_suite "${label}" "${kind}" "${summary}" "${manifest}" "${volume_proof}" "$@"; then
    printf 'expected %s to fail\n' "${label}" >&2
    exit 1
  fi
  if ! rg --quiet -- "${pattern}" "${tmp_root}/${label}.err"; then
    printf 'expected %s failure to include %s\n' "${label}" "${pattern}" >&2
    sed -n '1,160p' "${tmp_root}/${label}.err" >&2
    exit 1
  fi
}

summary="${state_dir}/summary.json"
clean_volume="${state_dir}/clean-volume-proof.json"
preserved_volume="${state_dir}/preserved-volume-proof.json"
write_summary "${summary}"
write_volume_proof "${clean_volume}" clean
write_volume_proof "${preserved_volume}" preserved

clean_manifest="${state_dir}/clean-manifest.json"
expect_pass clean clean "${summary}" "${clean_manifest}" "${clean_volume}"
jq -e '
  .status == "pass" and
  .run.kind == "clean" and
  .remote_compose.runtime_state_verified == true and
  .volume_proof.run_kind == "clean" and
  .observability.pprof_status == "reachable" and
  .observability.logs_status == "captured" and
  .observability.resource_snapshot_status == "captured"
' "${clean_manifest}" >/dev/null \
  || { printf 'clean manifest did not record required suite evidence\n' >&2; exit 1; }

preserved_manifest="${state_dir}/preserved-manifest.json"
expect_pass preserved preserved "${summary}" "${preserved_manifest}" "${preserved_volume}" \
  --previous-manifest "${clean_manifest}"
jq -e '
  .status == "pass" and
  .run.kind == "preserved" and
  .preserved_restart.previous_clean_manifest == "accepted" and
  .preserved_restart.duplicate_claims == 0 and
  .preserved_restart.duplicate_facts == 0 and
  .preserved_restart.duplicate_findings == 0 and
  .preserved_restart.new_dead_letters == 0
' "${preserved_manifest}" >/dev/null \
  || { printf 'preserved manifest did not record restart contract\n' >&2; exit 1; }

expect_fail_with preserved_missing_previous 'preserved run requires --previous-manifest' \
  preserved "${summary}" "${state_dir}/preserved-missing-previous.json" "${preserved_volume}"

duplicate_summary="${state_dir}/duplicate-summary.json"
jq '.preserved.duplicate_facts = 1' "${summary}" >"${duplicate_summary}"
expect_fail_with preserved_duplicate_facts 'preserved run has duplicate facts' \
  preserved "${duplicate_summary}" "${state_dir}/preserved-duplicate.json" "${preserved_volume}" \
  --previous-manifest "${clean_manifest}"

private_summary="${state_dir}/private-summary.json"
jq '.repository = "private-org/private-service"' "${summary}" >"${private_summary}"
expect_fail_with private_summary 'summary looks like private data' \
  clean "${private_summary}" "${state_dir}/private-summary-manifest.json" "${clean_volume}"

ESHU_FAKE_PPROF_DOWN=1 \
  expect_fail_with pprof_down 'pprof endpoint was not reachable' \
    clean "${summary}" "${state_dir}/pprof-down.json" "${clean_volume}"

ESHU_FAKE_LOGS_EMPTY=1 \
  expect_fail_with empty_logs 'Compose logs were not captured' \
    clean "${summary}" "${state_dir}/empty-logs.json" "${clean_volume}"

ESHU_FAKE_DOCKER_STATS_NO_MEM=1 \
  expect_fail_with invalid_stats 'docker stats CPU/memory snapshot was not captured' \
    clean "${summary}" "${state_dir}/invalid-stats.json" "${clean_volume}"

ESHU_FAKE_RUNTIME_VERIFIER_FAIL=1 \
  expect_fail_with verifier_failed 'remote runtime verifier failed' \
    clean "${summary}" "${state_dir}/verifier-failed.json" "${clean_volume}"

printf 'e2e remote Compose suite tests passed\n'
