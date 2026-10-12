#!/usr/bin/env bash
#
# dockerhub-login-test-legacy.sh - the non-queue Docker Hub login fixture for
# scripts/test-verify-dockerhub-login.sh (#7886). Sourced, never run.
#
# Every queue-path workflow in the committed tree pulls from ghcr.io only, so no
# committed workflow carries the Docker Hub login wiring any more. The login rule
# still governs jobs outside the queue path (a workflow that runs on a tag push
# or workflow_dispatch only), so the login cases run on this scratch workflow
# instead. It holds one compliant job per shape the cases mutate:
#   test                 login at steps[1], then a Hub pull
#   live-backend         login at steps[1], the Hub pull at steps[5]
#   contention-gate      a Hub service image with DOCKERHUB credentials
#   build-and-push-image Hub login, a ghcr login, then a build of a Hub Dockerfile
#
# shellcheck disable=SC2154  # failures and helpers come from the test

legacy_login_json='{"uses": "docker/login-action@v3", "if": "env.DOCKERHUB_LOGIN_ENABLED == '"'"'true'"'"'", "with": {"username": "${{ secrets.DOCKERHUB_USERNAME }}", "password": "${{ secrets.DOCKERHUB_TOKEN }}"}}'
legacy_enabled_expr="\${{ secrets.DOCKERHUB_USERNAME != '' && secrets.DOCKERHUB_TOKEN != '' }}"

# legacy_workflow <dir>: writes .github/workflows/legacy-hub.yml and the Hub
# Dockerfile its build job names.
legacy_workflow() {
  local f="$1/.github/workflows/legacy-hub.yml" env_json
  env_json="{\"DOCKERHUB_LOGIN_ENABLED\": \"${legacy_enabled_expr}\"}"
  yq e -n '.name = "legacy-hub" | .on.push.tags = ["v*"] | .on.workflow_dispatch = null' >"${f}"
  yq e -i ".jobs.test = {\"runs-on\": \"ubuntu-latest\", \"env\": ${env_json}, \"steps\": [{\"uses\": \"actions/checkout@v5\"}, ${legacy_login_json}, {\"run\": \"docker pull postgres:18\"}]}" "${f}"
  yq e -i ".jobs.live-backend = {\"runs-on\": \"ubuntu-latest\", \"env\": ${env_json}, \"steps\": [{\"uses\": \"actions/checkout@v5\"}, ${legacy_login_json}, {\"run\": \"echo two\"}, {\"run\": \"echo three\"}, {\"run\": \"echo four\"}, {\"run\": \"docker pull postgres:18\"}]}" "${f}"
  yq e -i '.jobs.contention-gate = {"runs-on": "ubuntu-latest", "services": {"postgres": {"image": "postgres:18-alpine", "credentials": {"username": "${{ secrets.DOCKERHUB_USERNAME }}", "password": "${{ secrets.DOCKERHUB_TOKEN }}"}}}, "steps": [{"run": "echo ok"}]}' "${f}"
  yq e -i ".jobs.build-and-push-image = {\"runs-on\": \"ubuntu-latest\", \"env\": ${env_json}, \"steps\": [{\"uses\": \"actions/checkout@v5\"}, ${legacy_login_json}, {\"uses\": \"docker/login-action@v3\", \"with\": {\"registry\": \"ghcr.io\", \"username\": \"u\", \"password\": \"p\"}}, {\"uses\": \"docker/build-push-action@v6\", \"with\": {\"context\": \".\", \"file\": \"Dockerfile.fixture-hub\"}}]}" "${f}"
  printf 'FROM alpine:3.21\n' >"$1/Dockerfile.fixture-hub"
}
