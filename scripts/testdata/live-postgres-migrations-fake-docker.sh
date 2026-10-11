#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${FAKE_CALLS}"
case "$1" in
  inspect)
    [[ -f "${FAKE_CONTAINER}" ]] || exit 1
    if [[ "$*" == *'.State.Running'* ]]; then printf 'true\n'; fi
    if [[ "$*" == *'{{.Id}}'* ]]; then printf '%064d\n' 0; fi
    if [[ "$*" == *'"eshu.fixture"'* ]]; then printf '%s\n' "${FAKE_DOCKER_OWNER:-live-postgres-migrations}"; fi
    if [[ "$*" == *'"eshu.fixture-id"'* ]]; then printf 'testfixture\n'; fi
    if [[ "$*" == *'"eshu.fixture-token"'* ]]; then
      printf '%s\n' "${FAKE_DOCKER_TOKEN_OVERRIDE:-$(<"${FAKE_CONTAINER_TOKEN}")}"
    fi
    ;;
  run)
    [[ "${FAKE_DOCKER_RUN_MODE:-pass}" != no_create_failure ]] || exit 7
    [[ ! -f "${FAKE_CONTAINER}" ]] || exit 1
    : >"${FAKE_CONTAINER}"
    for argument in "$@"; do
      if [[ "${argument}" == eshu.fixture-token=* ]]; then
        printf '%s\n' "${argument#eshu.fixture-token=}" >"${FAKE_CONTAINER_TOKEN}"
      fi
    done
    [[ "${FAKE_DOCKER_RUN_MODE:-pass}" != created_failure ]] || exit 7
    printf '%064d\n' 0
    ;;
  port) printf '127.0.0.1:15432\n' ;;
  exec)
    if [[ "$*" == *'pg_control_system()'* ]]; then printf '%s\n' "${FAKE_DOCKER_IDENTITY:-123456789|180006}"; fi
    ;;
  rm) rm -f "${FAKE_CONTAINER}" "${FAKE_CONTAINER_TOKEN}" ;;
  *) exit 9 ;;
esac
