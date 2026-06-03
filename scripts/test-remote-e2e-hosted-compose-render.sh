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

compose_pprof_config() {
	local output="$1"
	shift
	docker compose --env-file "${REPO_ROOT}/.env.remote-e2e.example" \
		-f "${REPO_ROOT}/docker-compose.remote-e2e.yaml" \
		-f "${REPO_ROOT}/docker-compose.remote-e2e.pprof.yaml" \
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
		'^  collector-jira:$' \
		'^  collector-pagerduty:$' \
		'ESHU_JIRA_COLLECTOR_INSTANCE_ID: remote-e2e-jira' \
		'ESHU_PAGERDUTY_COLLECTOR_INSTANCE_ID: remote-e2e-pagerduty' \
		'127.0.0.1:19675:6060' \
		'127.0.0.1:19674:6060' \
		'ESHU_REMOTE_E2E_JIRA_ENABLED=false' \
		'ESHU_REMOTE_E2E_PAGERDUTY_ENABLED=false'; do
		if ! rg -q "${want}" \
			"${REPO_ROOT}/docker-compose.remote-e2e.yaml" \
			"${REPO_ROOT}/docker-compose.remote-e2e.pprof.yaml" \
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
assert_service_absent "${default_services}" collector-jira
assert_service_absent "${default_services}" collector-pagerduty

profiled_services="${TMP_DIR}/profiled-services.txt"
compose_services "${profiled_services}" --profile jira --profile pagerduty
assert_service_present "${profiled_services}" collector-jira
assert_service_present "${profiled_services}" collector-pagerduty

rendered="${TMP_DIR}/profiled-config.yaml"
docker compose --env-file "${REPO_ROOT}/.env.remote-e2e.example" \
	-f "${REPO_ROOT}/docker-compose.remote-e2e.yaml" \
	--profile jira \
	--profile pagerduty \
	config >"${rendered}"

for want in \
	'ESHU_JIRA_COLLECTOR_INSTANCE_ID: remote-e2e-jira' \
	'ESHU_PAGERDUTY_COLLECTOR_INSTANCE_ID: remote-e2e-pagerduty' \
	'JIRA_API_TOKEN:' \
	'PAGERDUTY_API_TOKEN:'; do
	if ! rg -q "${want}" "${rendered}"; then
		die "profiled render missing ${want}"
	fi
done

pprof_rendered="${TMP_DIR}/profiled-pprof-config.yaml"
compose_pprof_config "${pprof_rendered}" --profile jira --profile pagerduty
for want in \
	'collector-jira:' \
	'collector-pagerduty:' \
	'ESHU_PPROF_ADDR: 0.0.0.0:6060' \
	'host_ip: 127.0.0.1' \
	'target: 6060' \
	'published: "19675"' \
	'published: "19674"'; do
	if ! rg -q "${want}" "${pprof_rendered}"; then
		die "profiled pprof render missing ${want}"
	fi
done

printf 'remote E2E hosted Compose render tests passed\n'
