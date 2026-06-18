#!/usr/bin/env bash
# Verify static runtime parity across Compose, remote E2E Compose, and Helm.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
while [ "$#" -gt 0 ]; do
    case "$1" in
        --repo-root) repo_root="${2:?missing repo root}"; shift 2 ;;
        -h|--help)
            printf 'Usage: scripts/verify-compose-helm-runtime-parity.sh [--repo-root PATH]\n'
            exit 0
            ;;
        *) printf 'unknown argument: %s\n' "$1" >&2; exit 1 ;;
    esac
done

cd "${repo_root}"
command -v docker >/dev/null 2>&1 || { printf 'docker compose is required\n' >&2; exit 1; }
command -v helm >/dev/null 2>&1 || { printf 'helm is required\n' >&2; exit 1; }

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}" 2>/dev/null || true' EXIT
failures=0

fail() {
    printf '%s\n' "$*" >&2
    failures=$((failures + 1))
}

require_line() {
    local file="$1"
    local value="$2"
    local message="$3"
    if ! rg --fixed-strings --quiet -- "${value}" "${file}"; then
        fail "${message}: ${value}"
    fi
}

require_pattern() {
    local file="$1"
    local pattern="$2"
    local message="$3"
    if ! rg --quiet -- "${pattern}" "${file}"; then
        fail "${message}: ${pattern}"
    fi
}

require_service_monitor_component() {
    local component="$1"
    local found=false
    while IFS= read -r template; do
        if rg --quiet -- "app.kubernetes.io/component: ${component}" "${template}"; then
            found=true
            break
        fi
    done < <(rg -l -- 'kind: ServiceMonitor' deploy/helm/eshu/templates)
    if [[ "${found}" != true ]]; then
        fail "missing collector ServiceMonitor template coverage: app.kubernetes.io/component: ${component}"
    fi
}

default_services="${tmp_dir}/default-services.txt"
remote_services="${tmp_dir}/remote-services.txt"
remote_profile_services="${tmp_dir}/remote-profile-services.txt"
remote_observability_services="${tmp_dir}/remote-observability-services.txt"
default_config="${tmp_dir}/default-compose.yaml"
remote_config="${tmp_dir}/remote-compose.yaml"
helm_rendered="${tmp_dir}/helm-servicemonitor.yaml"

docker compose -f docker-compose.yaml config --services >"${default_services}"
docker compose -f docker-compose.yaml config >"${default_config}"
docker compose --env-file .env.remote-e2e.example -f docker-compose.remote-e2e.yaml config --services >"${remote_services}"
docker compose --env-file .env.remote-e2e.example -f docker-compose.remote-e2e.yaml config >"${remote_config}"
docker compose --env-file .env.remote-e2e.example -f docker-compose.remote-e2e.yaml \
    --profile jira --profile pagerduty --profile confluence config --services >"${remote_profile_services}"
docker compose --env-file .env.remote-e2e.example \
    -f docker-compose.remote-e2e.yaml \
    -f docker-compose.remote-e2e.observability.yaml \
    --profile grafana --profile prometheus-mimir --profile loki --profile tempo \
    config --services >"${remote_observability_services}"
helm template eshu deploy/helm/eshu \
    --set observability.prometheus.enabled=true \
    --set observability.prometheus.serviceMonitor.enabled=true >"${helm_rendered}"

for service in nornicdb postgres db-migrate workspace-setup bootstrap-index eshu mcp-server ingester resolution-engine; do
    require_line "${default_services}" "${service}" "missing default Compose service"
done

for service in eshu mcp-server ingester projector resolution-engine workflow-coordinator webhook-listener \
    collector-terraform-state collector-oci-registry collector-package-registry \
    collector-sbom-attestation collector-security-alerts collector-vulnerability-intelligence \
    collector-aws-cloud scanner-worker; do
    require_line "${remote_services}" "${service}" "missing remote Compose service"
done

for service in collector-confluence collector-jira collector-pagerduty; do
    require_line "${remote_profile_services}" "${service}" "missing profile-expanded remote Compose service"
done

for service in collector-grafana collector-prometheus-mimir collector-loki collector-tempo; do
    require_line "${remote_observability_services}" "${service}" "missing observability remote Compose service"
done

for token in ESHU_GRAPH_BACKEND ESHU_POSTGRES_DSN NEO4J_URI; do
    require_pattern "${default_config}" "${token}:" "default Compose missing critical env"
    require_pattern "${remote_config}" "${token}:" "remote Compose missing critical env"
done

for path in 'http://localhost:8080/health' 'http://localhost:8080/healthz'; do
    require_pattern "${default_config}" "${path}" "default Compose missing accepted health probe"
done
require_pattern "${remote_config}" 'http://localhost:8080/healthz' "remote Compose missing /healthz probes"
require_pattern "${default_config}" '9464' "default Compose missing metrics port wiring"
require_pattern "${remote_config}" '9464' "remote Compose missing metrics port wiring"

for workload in 'Deployment/eshu-api' 'Deployment/eshu-mcp-server' 'StatefulSet/eshu' 'Deployment/eshu-resolution-engine' 'Job/eshu-schema-bootstrap'; do
    kind="${workload%%/*}"
    name="${workload##*/}"
    if ! awk -v kind="${kind}" -v name="${name}" '
        $1 == "kind:" { current_kind = $2 }
        $1 == "name:" && current_kind == kind && $2 == name { found = 1 }
        END { exit found ? 0 : 1 }
    ' "${helm_rendered}"; then
        fail "missing Helm workload: ${workload}"
    fi
done

for component in api mcp-server ingester resolution-engine; do
    require_pattern "${helm_rendered}" "app.kubernetes.io/component: ${component}" "missing core ServiceMonitor coverage"
done

for component in confluence-collector oci-registry-collector terraform-state-collector aws-cloud-collector gcp-cloud-collector \
    package-registry-collector sbom-attestation-collector security-alert-collector cicd-run-collector \
    pagerduty-collector jira-collector grafana-collector prometheus-mimir-collector loki-collector \
    tempo-collector scanner-worker vulnerability-intelligence-collector component-extension-collector; do
    require_service_monitor_component "${component}"
    require_pattern "deploy/helm/eshu/templates" "app.kubernetes.io/component: ${component}" "missing collector component template"
done

if [ "${failures}" -ne 0 ]; then
    printf 'runtime parity verification failed with %s issue(s)\n' "${failures}" >&2
    exit 1
fi

printf 'runtime parity verification passed\n'
printf 'default Compose services: pass\n'
printf 'remote Compose services: pass\n'
printf 'profile-expanded remote collectors: pass\n'
printf 'observability remote collectors: pass\n'
printf 'critical env wiring: pass\n'
printf 'health and metrics probes: pass\n'
printf 'core ServiceMonitor coverage: pass\n'
printf 'collector ServiceMonitor template coverage: pass\n'
