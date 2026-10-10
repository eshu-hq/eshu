#!/usr/bin/env bash
if [[ "$1" == ps ]]; then
 [[ "$*" == *'label=com.docker.compose.project=eshu-methodology-scale-ci'* ]] || exit 98
 printf 'postgres-id\nneo4j-id\n'
 exit 0
fi
[[ "$1" == stats ]] || exit 99
[[ "$*" == *'postgres-id neo4j-id'* ]] || exit 97
case "$DOCKER_MODE" in
 valid) printf 'eshu-methodology-scale-ci-postgres-1 CPU=1.00%% memory=10MiB / 1GiB\neshu-methodology-scale-ci-neo4j-1 CPU=1.00%% memory=10MiB / 1GiB\n' ;;
 empty) ;;
 unrelated) printf 'peer-service CPU=1.00%% memory=10MiB / 1GiB\n' ;;
 fail-after-sample)
  if [[ -e "$RUNNER_TEMP/stats-seen" ]]; then touch "$RUNNER_TEMP/stats-failed"; exit 7; fi
  touch "$RUNNER_TEMP/stats-seen"
  printf 'eshu-methodology-scale-ci-postgres-1 CPU=1.00%% memory=10MiB / 1GiB\neshu-methodology-scale-ci-neo4j-1 CPU=1.00%% memory=10MiB / 1GiB\n' ;;
esac
