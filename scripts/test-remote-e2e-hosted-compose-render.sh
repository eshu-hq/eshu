#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

die() {
	printf 'remote-e2e-hosted-compose-render-test: %s\n' "$*" >&2
	exit 1
}

docker_compose_available() {
	command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1
}

compose_services() {
	local output="$1"
	shift
	docker compose --env-file "${REPO_ROOT}/.env.remote-e2e.example" \
		-f "${REPO_ROOT}/docker-compose.remote-e2e.yaml" \
		"$@" config --services >"${output}"
}

compose_observability_services() {
	local output="$1"
	shift
	docker compose --env-file "${REPO_ROOT}/.env.remote-e2e.example" \
		-f "${REPO_ROOT}/docker-compose.remote-e2e.yaml" \
		-f "${REPO_ROOT}/docker-compose.remote-e2e.observability.yaml" \
		"$@" config --services >"${output}"
}

compose_pprof_config() {
	local output="$1"
	shift
	docker compose --env-file "${REPO_ROOT}/.env.remote-e2e.example" \
		-f "${REPO_ROOT}/docker-compose.remote-e2e.yaml" \
		-f "${REPO_ROOT}/docker-compose.remote-e2e.pprof.yaml" \
		"$@" config >"${output}"
}

compose_observability_pprof_config() {
	local output="$1"
	shift
	docker compose --env-file "${REPO_ROOT}/.env.remote-e2e.example" \
		-f "${REPO_ROOT}/docker-compose.remote-e2e.yaml" \
		-f "${REPO_ROOT}/docker-compose.remote-e2e.observability.yaml" \
		-f "${REPO_ROOT}/docker-compose.remote-e2e.pprof.yaml" \
		-f "${REPO_ROOT}/docker-compose.remote-e2e.observability.pprof.yaml" \
		"$@" config >"${output}"
}

assert_service_absent() {
	local services="$1" service="$2"
	if rg -q "^${service}$" "${services}"; then
		die "${service} rendered without its explicit remote E2E profile"
	fi
}

assert_service_present() {
	local services="$1" service="$2"
	if ! rg -q "^${service}$" "${services}"; then
		die "${service} did not render with its explicit remote E2E profile"
	fi
}

assert_static_contract() {
	for want in \
		'^  collector-security-alerts-preflight:$' \
		'preflight-provider-access' \
		'collector-security-alerts-preflight:' \
		'^  collector-confluence-preflight:$' \
		'remote-e2e-confluence-preflight.sh' \
		'collector-confluence-preflight:' \
		'^  collector-jira:$' \
		'^  collector-pagerduty:$' \
		'^  collector-grafana:$' \
		'^  collector-prometheus-mimir:$' \
		'^  collector-loki:$' \
		'^  collector-tempo:$' \
		'^  collector-confluence:$' \
		'ESHU_JIRA_COLLECTOR_INSTANCE_ID: remote-e2e-jira' \
		'ESHU_JIRA_JQL:' \
		'ESHU_PAGERDUTY_COLLECTOR_INSTANCE_ID: remote-e2e-pagerduty' \
		'ESHU_GRAFANA_COLLECTOR_INSTANCE_ID: remote-e2e-grafana' \
		'ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID: remote-e2e-prometheus-mimir' \
		'ESHU_LOKI_COLLECTOR_INSTANCE_ID: remote-e2e-loki' \
		'ESHU_TEMPO_COLLECTOR_INSTANCE_ID: remote-e2e-tempo' \
		'ESHU_CONFLUENCE_BASE_URL:' \
		'ESHU_CONFLUENCE_SPACE_ID:' \
		'ESHU_CONFLUENCE_SPACE_IDS:' \
		'ESHU_CONFLUENCE_ROOT_PAGE_ID:' \
		'"jql_env": "ESHU_JIRA_JQL"' \
		'127.0.0.1:19675:6060' \
		'127.0.0.1:19674:6060' \
		'127.0.0.1:19676:6060' \
		'127.0.0.1:19677:6060' \
		'127.0.0.1:19678:6060' \
		'127.0.0.1:19679:6060' \
		'127.0.0.1:19668:6060' \
		'ESHU_REMOTE_E2E_JIRA_ENABLED=false' \
		'ESHU_REMOTE_E2E_PAGERDUTY_ENABLED=false' \
		'ESHU_REMOTE_E2E_GRAFANA_ENABLED=false' \
		'ESHU_REMOTE_E2E_PROMETHEUS_MIMIR_ENABLED=false' \
		'ESHU_REMOTE_E2E_LOKI_ENABLED=false' \
		'ESHU_REMOTE_E2E_TEMPO_ENABLED=false'; do
		if ! rg -q "${want}" \
			"${REPO_ROOT}/docker-compose.remote-e2e.yaml" \
			"${REPO_ROOT}/docker-compose.remote-e2e.observability.yaml" \
			"${REPO_ROOT}/docker-compose.remote-e2e.pprof.yaml" \
			"${REPO_ROOT}/docker-compose.remote-e2e.observability.pprof.yaml" \
			"${REPO_ROOT}/.env.remote-e2e.example"; then
			die "checked-in remote E2E hosted Compose contract missing ${want}"
		fi
	done
}

assert_static_contract

if ! docker_compose_available; then
	printf 'docker compose not available; static remote E2E hosted Compose render contract passed\n'
	exit 0
fi

default_services="${TMP_DIR}/default-services.txt"
compose_services "${default_services}"
assert_service_present "${default_services}" collector-security-alerts-preflight
assert_service_absent "${default_services}" collector-jira
assert_service_absent "${default_services}" collector-pagerduty
assert_service_absent "${default_services}" collector-grafana
assert_service_absent "${default_services}" collector-prometheus-mimir
assert_service_absent "${default_services}" collector-loki
assert_service_absent "${default_services}" collector-tempo
assert_service_absent "${default_services}" collector-confluence
assert_service_absent "${default_services}" collector-confluence-preflight

profiled_services="${TMP_DIR}/profiled-services.txt"
compose_services "${profiled_services}" --profile jira --profile pagerduty --profile confluence
assert_service_present "${profiled_services}" collector-jira
assert_service_present "${profiled_services}" collector-pagerduty
assert_service_absent "${profiled_services}" collector-grafana
assert_service_absent "${profiled_services}" collector-prometheus-mimir
assert_service_absent "${profiled_services}" collector-loki
assert_service_absent "${profiled_services}" collector-tempo
assert_service_present "${profiled_services}" collector-confluence
assert_service_present "${profiled_services}" collector-confluence-preflight

observability_profiled_services="${TMP_DIR}/observability-profiled-services.txt"
compose_observability_services "${observability_profiled_services}" --profile grafana --profile prometheus-mimir --profile loki --profile tempo
assert_service_present "${observability_profiled_services}" collector-grafana
assert_service_present "${observability_profiled_services}" collector-prometheus-mimir
assert_service_present "${observability_profiled_services}" collector-loki
assert_service_present "${observability_profiled_services}" collector-tempo

rendered="${TMP_DIR}/profiled-config.yaml"
docker compose --env-file "${REPO_ROOT}/.env.remote-e2e.example" \
	-f "${REPO_ROOT}/docker-compose.remote-e2e.yaml" \
	-f "${REPO_ROOT}/docker-compose.remote-e2e.observability.yaml" \
	--profile jira \
	--profile pagerduty \
	--profile grafana \
	--profile prometheus-mimir \
	--profile loki \
	--profile tempo \
	--profile confluence \
	config >"${rendered}"

for want in \
	'ESHU_JIRA_COLLECTOR_INSTANCE_ID: remote-e2e-jira' \
	'ESHU_PAGERDUTY_COLLECTOR_INSTANCE_ID: remote-e2e-pagerduty' \
	'ESHU_GRAFANA_COLLECTOR_INSTANCE_ID: remote-e2e-grafana' \
	'ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID: remote-e2e-prometheus-mimir' \
	'ESHU_LOKI_COLLECTOR_INSTANCE_ID: remote-e2e-loki' \
	'ESHU_TEMPO_COLLECTOR_INSTANCE_ID: remote-e2e-tempo' \
	'ESHU_JIRA_JQL:' \
	'ESHU_CONFLUENCE_BASE_URL:' \
	'ESHU_CONFLUENCE_SPACE_ID:' \
	'ESHU_CONFLUENCE_SPACE_IDS:' \
	'ESHU_CONFLUENCE_ROOT_PAGE_ID:' \
	'remote-e2e-confluence-preflight.sh' \
	'JIRA_API_TOKEN:' \
	'PAGERDUTY_API_TOKEN:' \
	'GRAFANA_TOKEN:' \
	'PROMETHEUS_MIMIR_TOKEN:' \
	'LOKI_TOKEN:' \
	'TEMPO_TOKEN:'; do
	if ! rg -q "${want}" "${rendered}"; then
		die "profiled render missing ${want}"
	fi
done

if ! rg -q -U 'collector-confluence:\n    profiles:\n(?:.*\n)*    depends_on:\n      collector-confluence-preflight:\n        condition: service_completed_successfully' "${rendered}"; then
	die "profiled Confluence render does not gate collector startup on collector-confluence-preflight"
fi

pprof_rendered="${TMP_DIR}/profiled-pprof-config.yaml"
compose_observability_pprof_config "${pprof_rendered}" --profile jira --profile pagerduty --profile grafana --profile prometheus-mimir --profile loki --profile tempo --profile confluence
for want in \
	'collector-jira:' \
	'collector-pagerduty:' \
	'collector-grafana:' \
	'collector-prometheus-mimir:' \
	'collector-loki:' \
	'collector-tempo:' \
	'collector-confluence:' \
	'ESHU_PPROF_ADDR: 0.0.0.0:6060' \
	'host_ip: 127.0.0.1' \
	'target: 6060' \
	'published: "19675"' \
	'published: "19674"' \
	'published: "19676"' \
	'published: "19677"' \
	'published: "19678"' \
	'published: "19679"' \
	'published: "19668"'; do
	if ! rg -q "${want}" "${pprof_rendered}"; then
		die "profiled pprof render missing ${want}"
	fi
done

base_pprof_rendered="${TMP_DIR}/base-pprof-config.yaml"
compose_pprof_config "${base_pprof_rendered}" --profile jira --profile pagerduty --profile confluence
for service in collector-grafana collector-prometheus-mimir collector-loki collector-tempo; do
	if rg -q "^  ${service}:" "${base_pprof_rendered}"; then
		die "base pprof render unexpectedly created ${service} without the observability overlay"
	fi
done

printf 'remote E2E hosted Compose render tests passed\n'
