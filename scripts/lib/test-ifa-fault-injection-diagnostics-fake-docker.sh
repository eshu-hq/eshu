#!/usr/bin/env bash
set -euo pipefail
expected_revision="1111111111111111111111111111111111111111"
actual_revision="${IFA_TEST_ACTUAL_BACKEND_REVISION:-${expected_revision}}"
if [[ "$1" == compose && "$*" == *" config --format json" ]]; then
	printf '{"services":{"nornicdb":{"build":{"labels":{"org.opencontainers.image.revision":"%s"}}}}}\n' \
		"${expected_revision}"
elif [[ "$1" == compose && "$*" == *" ps -q nornicdb" ]]; then
	printf 'container-1\n'
elif [[ "$1" == compose && "$*" == *" logs --no-color" ]]; then
	printf 'synthetic compose log\n'
elif [[ "$1" == compose && "$*" == *" images nornicdb" ]]; then
	printf 'nornicdb fixture-image sha256:image\n'
elif [[ "$1" == compose && "$*" == *" exec -T postgres psql"* ]]; then
	printf 'fixture database row\n'
elif [[ "$1" == inspect && "$2" == --format ]]; then
	printf '{"org.opencontainers.image.revision":"%s"}\n' "${actual_revision}"
elif [[ "$1" == inspect ]]; then
	printf '%s\n' '[{"Image":"sha256:image","RestartCount":1,"State":{"Status":"running","Running":true,"StartedAt":"start","FinishedAt":"stop","ExitCode":0},"Mounts":[{"Type":"volume","Name":"data-volume","Source":"/private/host/path","Destination":"/data","RW":true}],"Config":{"Env":["NORNICDB_DATA_DIR=/data","NORNICDB_ASYNC_WRITES_ENABLED=false","NORNICDB_ADMIN_TOKEN=secret"]}}]'
else
	printf 'unexpected docker invocation: %s\n' "$*" >&2
	exit 2
fi
