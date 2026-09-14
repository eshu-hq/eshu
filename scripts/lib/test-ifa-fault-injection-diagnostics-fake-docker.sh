#!/usr/bin/env bash
set -euo pipefail
default_repository="timothyswt/nornicdb-cpu-bge"
default_index_digest="sha256:a47ae7eadc80229d3109ade7a57dfc1f1504b7586798859e2b2ac6fc38897440"
default_child_digest="sha256:4256d970a1aad702b85fbd4dafa9299bb274d82090ae90eb59b88d48e9291adc"
default_image="${default_repository}:v1.3.2@${default_index_digest}"
rendered_image="${IFA_TEST_RENDERED_IMAGE:-${NORNICDB_IMAGE:-${default_image}}}"
rendered_platform="${IFA_TEST_RENDERED_PLATFORM:-${NORNICDB_PLATFORM:-linux/amd64}}"
runtime_image_id="${IFA_TEST_RUNTIME_IMAGE_ID:-sha256:9999999999999999999999999999999999999999999999999999999999999999}"
if [[ "${IFA_TEST_MISSING_RUNTIME_IMAGE_ID:-0}" -eq 1 ]]; then
	runtime_image_id=""
fi
container_config_image="${IFA_TEST_CONTAINER_CONFIG_IMAGE:-${rendered_image}}"
runtime_repository_digest="${IFA_TEST_RUNTIME_REPO_DIGEST:-${default_repository}@${default_child_digest}}"
runtime_os="${IFA_TEST_RUNTIME_OS:-linux}"
runtime_architecture="${IFA_TEST_RUNTIME_ARCHITECTURE:-amd64}"
if [[ "$1" == compose && "$*" == *" config --format json" ]]; then
	printf '{"services":{"nornicdb":{"image":"%s","platform":"%s","environment":{"NORNICDB_ADMIN_TOKEN":"compose-secret"},"volumes":[{"source":"/private/compose/path","target":"/data"}]},"postgres":{"environment":{"POSTGRES_PASSWORD":"compose-secret"}}}}\n' \
		"${rendered_image}" "${rendered_platform}"
elif [[ "$1" == compose && "$*" == *" ps -q nornicdb" ]]; then
	printf 'container-1\n'
elif [[ "$1" == compose && "$*" == *" logs --no-color" ]]; then
	printf 'synthetic compose log\n'
elif [[ "$1" == compose && "$*" == *" images nornicdb" ]]; then
	printf 'nornicdb fixture-image sha256:image\n'
elif [[ "$1" == compose && "$*" == *" exec -T postgres psql"* ]]; then
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
	printf '[{"Image":"%s","RestartCount":1,"State":{"Status":"running","Running":true,"StartedAt":"start","FinishedAt":"stop","ExitCode":0},"Mounts":[{"Type":"volume","Name":"data-volume","Source":"/private/host/path","Destination":"/data","RW":true}],"Config":{"Image":"%s","Env":["NORNICDB_DATA_DIR=/data","NORNICDB_ASYNC_WRITES_ENABLED=false","NORNICDB_ADMIN_TOKEN=secret"]}}]\n' \
		"${runtime_image_id}" "${container_config_image}"
else
	printf 'unexpected docker invocation: %s\n' "$*" >&2
	exit 2
fi
