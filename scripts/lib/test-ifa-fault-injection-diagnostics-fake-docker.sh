#!/usr/bin/env bash
set -euo pipefail
default_repository="ghcr.io/eshu-hq/nornicdb-amd64-cpu"
default_index_digest="sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1"
default_child_digest="sha256:c4a2116e3c1547f750426d5c6c7fae618135f2fc1a3f7bf13fd7811a9c189915"
default_image="${default_repository}:fix-500-e022384c@${default_index_digest}"
rendered_image="${IFA_TEST_RENDERED_IMAGE:-${NORNICDB_IMAGE:-${default_image}}}"
# IFA_TEST_SERVICE selects the graph service the fake renders (nornicdb by
# default, neo4j for the Neo4j profile). A set-but-empty IFA_TEST_RENDERED_PLATFORM
# renders NO platform key, as real Compose does for the Neo4j service.
service="${IFA_TEST_SERVICE:-nornicdb}"
rendered_platform="${IFA_TEST_RENDERED_PLATFORM-${NORNICDB_PLATFORM:-linux/amd64}}"
platform_json=""
if [[ -n "${rendered_platform}" ]]; then
	platform_json=",\"platform\":\"${rendered_platform}\""
fi
container_env_json="${IFA_TEST_CONTAINER_ENV_JSON:-\"NORNICDB_DATA_DIR=/data\",\"NORNICDB_ASYNC_WRITES_ENABLED=false\",\"NORNICDB_ADMIN_TOKEN=secret\"}"
runtime_image_id="${IFA_TEST_RUNTIME_IMAGE_ID:-sha256:9999999999999999999999999999999999999999999999999999999999999999}"
if [[ "${IFA_TEST_MISSING_RUNTIME_IMAGE_ID:-0}" -eq 1 ]]; then
	runtime_image_id=""
fi
container_config_image="${IFA_TEST_CONTAINER_CONFIG_IMAGE:-${rendered_image}}"
runtime_repository_digest="${IFA_TEST_RUNTIME_REPO_DIGEST:-${default_repository}@${default_child_digest}}"
runtime_os="${IFA_TEST_RUNTIME_OS:-linux}"
runtime_architecture="${IFA_TEST_RUNTIME_ARCHITECTURE:-amd64}"
if [[ "$1" == compose && "$*" == *" config --format json" ]]; then
	if [[ "${IFA_TEST_COMPOSE_CONFIG_FAIL:-0}" -eq 1 ]]; then
		printf 'compose failed for private-registry.internal at /private/compose/path\n' >&2
		exit 1
	fi
	printf '{"services":{"%s":{"image":"%s"%s,"environment":{"NORNICDB_ADMIN_TOKEN":"compose-secret"},"volumes":[{"source":"/private/compose/path","target":"/data"}]},"postgres":{"environment":{"POSTGRES_PASSWORD":"compose-secret"}}}}\n' \
		"${service}" "${rendered_image}" "${platform_json}"
elif [[ "$1" == compose && "$*" == *" ps -q ${service}" ]]; then
	printf 'container-1\n'
elif [[ "$1" == compose && "$*" == *" logs --no-color" ]]; then
	if [[ "${IFA_TEST_COMPOSE_LOGS_PRIVATE:-0}" -eq 1 ]]; then
		printf 'startup used private-registry.internal from /private/compose/path\n'
		exit 0
	fi
	printf 'synthetic compose log\n'
elif [[ "$1" == compose && "$*" == *" images ${service}" ]]; then
	printf '%s fixture-image sha256:image\n' "${service}"
elif [[ "$1" == compose && "$*" == *" exec -T postgres psql"* ]]; then
	if [[ "${IFA_TEST_COMPOSE_EXEC_FAIL:-0}" -eq 1 ]]; then
		printf 'exec failed for private-registry.internal at /private/compose/path\n' >&2
		exit 1
	fi
	printf 'fixture database row\n'
elif [[ "$1" == image && "$2" == inspect ]]; then
	[[ "${IFA_TEST_IMAGE_INSPECT_FAIL:-0}" -eq 0 ]] || exit 1
	if [[ "$*" == *" --platform "* ]]; then
		printf 'unknown flag: --platform\n' >&2
		exit 64
	elif [[ "${IFA_TEST_EMPTY_REPO_DIGESTS:-0}" -eq 1 ]]; then
		printf '[{"Id":"%s","RepoDigests":[],"Os":"%s","Architecture":"%s"}]\n' \
			"${runtime_image_id}" "${runtime_os}" "${runtime_architecture}"
	else
		printf '[{"Id":"%s","RepoDigests":["%s"],"Os":"%s","Architecture":"%s"}]\n' \
			"${runtime_image_id}" "${runtime_repository_digest}" "${runtime_os}" "${runtime_architecture}"
	fi
elif [[ "$1" == inspect ]]; then
	printf '[{"Image":"%s","RestartCount":1,"State":{"Status":"running","Running":true,"StartedAt":"start","FinishedAt":"stop","ExitCode":0},"Mounts":[{"Type":"volume","Name":"data-volume","Source":"/private/host/path","Destination":"/data","RW":true}],"Config":{"Image":"%s","Env":[%s]}}]\n' \
		"${runtime_image_id}" "${container_config_image}" "${container_env_json}"
else
	printf 'unexpected docker invocation: %s\n' "$*" >&2
	exit 2
fi
