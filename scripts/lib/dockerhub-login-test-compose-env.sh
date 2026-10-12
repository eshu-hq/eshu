#!/usr/bin/env bash
#
# dockerhub-login-test-compose-env.sh - seeded RED/GREEN cases for compose
# extends:/include:, job and workflow env defaults, and docker in every command
# position, for scripts/verify-dockerhub-login.sh (#7886). Sourced, never run,
# from scripts/test-verify-dockerhub-login.sh at the point where these cases
# used to sit. It uses the test's helpers (new_case, add_job, run_verifier,
# expect_red, expect_green) and its failure counter.
#
# shellcheck disable=SC2154  # failures, repo_root, verifier come from the test

# --- G1a: compose extends: and include: are followed ---------------------------
no_login='verify-agent-hygiene.yml:fixture-compose: no Docker Hub docker/login-action@v3 step'
compose_case() { # <name> <compose body>: scratch repo whose job runs the fixture compose file
  local dir; dir="$(new_case "$1")"
  printf '%s' "$2" >"${dir}/docker-compose.fixture.yaml"
  printf '#!/usr/bin/env bash\ndocker compose -f docker-compose.fixture.yaml up -d\n' >"${dir}/scripts/fixture-compose.sh"
  add_job "${dir}" verify-agent-hygiene fixture-compose 'bash scripts/fixture-compose.sh'
  CASE_DIR="${dir}"
}
base_hub=$'services:\n  base:\n    image: postgres:18\n'
base_ghcr=$'services:\n  base:\n    image: ghcr.io/eshu-hq/eshu:main\n'
extends_base=$'services:\n  db:\n    extends:\n      file: docker-compose.fixture-base.yaml\n      service: base\n'

# Malformed service shapes must remain unresolved so Hub login is required.
compose_case compose-services-false $'services: false\n'
expect_red services-false-counts-as-hub "${CASE_DIR}" "${no_login}"

compose_case compose-services-empty-list $'services: []\n'
expect_red services-empty-list-counts-as-hub "${CASE_DIR}" "${no_login}"

compose_case compose-service-false $'services:\n  api: false\n'
expect_red service-false-counts-as-hub "${CASE_DIR}" "${no_login}"

compose_case compose-service-empty-map $'services:\n  api: {}\n'
expect_red service-empty-map-counts-as-hub "${CASE_DIR}" "${no_login}"

compose_case compose-extends-hub-base-file "${extends_base}"
printf '%s' "${base_hub}" >"${CASE_DIR}/docker-compose.fixture-base.yaml"
expect_red extends-follows-a-hub-image-in-the-base-file "${CASE_DIR}" "${no_login}"

compose_case compose-extends-hub-build "${extends_base}"
printf 'services:\n  base:\n    build:\n      context: .\n      dockerfile: Dockerfile.fixture-hub\n' >"${CASE_DIR}/docker-compose.fixture-base.yaml"
printf 'FROM alpine:3.21\n' >"${CASE_DIR}/Dockerfile.fixture-hub"
expect_red extends-follows-a-hub-dockerfile-in-the-base-build "${CASE_DIR}" "${no_login}"

compose_case compose-extends-missing-base $'services:\n  db:\n    extends:\n      file: docker-compose.missing.yaml\n      service: base\n'
expect_red extends-of-an-unresolvable-file-counts-as-hub "${CASE_DIR}" "${no_login}"

compose_case compose-extends-cycle $'services:\n  a:\n    extends:\n      file: docker-compose.fixture-base.yaml\n      service: b\n'
printf 'services:\n  b:\n    extends:\n      file: docker-compose.fixture.yaml\n      service: a\n' >"${CASE_DIR}/docker-compose.fixture-base.yaml"
expect_red extends-cycle-counts-as-hub "${CASE_DIR}" "${no_login}"

compose_case compose-include-hub-file $'include:\n  - docker-compose.fixture-base.yaml\nservices:\n  api:\n    image: ghcr.io/eshu-hq/eshu:main\n'
printf '%s' "${base_hub}" >"${CASE_DIR}/docker-compose.fixture-base.yaml"
expect_red include-follows-a-hub-image-in-the-included-file "${CASE_DIR}" "${no_login}"

compose_case compose-include-path-map $'include:\n  - path: docker-compose.fixture-base.yaml\nservices:\n  api:\n    image: ghcr.io/eshu-hq/eshu:main\n'
printf '%s' "${base_hub}" >"${CASE_DIR}/docker-compose.fixture-base.yaml"
expect_red include-path-map-is-followed "${CASE_DIR}" "${no_login}"

compose_case compose-extends-ghcr-base "${extends_base}"
printf '%s' "${base_ghcr}" >"${CASE_DIR}/docker-compose.fixture-base.yaml"
expect_green extends-of-a-ghcr-base-needs-no-login "${CASE_DIR}"

compose_case compose-include-ghcr-file $'include:\n  - docker-compose.fixture-base.yaml\nservices:\n  api:\n    image: ghcr.io/eshu-hq/eshu:main\n'
printf '%s' "${base_ghcr}" >"${CASE_DIR}/docker-compose.fixture-base.yaml"
expect_green include-of-a-ghcr-file-needs-no-login "${CASE_DIR}"

compose_case compose-extends-overrides-image $'services:\n  db:\n    extends:\n      file: docker-compose.fixture-base.yaml\n      service: base\n    image: ghcr.io/eshu-hq/eshu:main\n'
printf '%s' "${base_hub}" >"${CASE_DIR}/docker-compose.fixture-base.yaml"
expect_green extending-service-image-overrides-a-hub-base "${CASE_DIR}"

# --- G1b: job and workflow env feed ${VAR:-default} and ${{ env.X }} ------------
default_script='#!/usr/bin/env bash\nimage="${FIXTURE_IMAGE:-ghcr.io/eshu-hq/eshu:main}"\ndocker run --rm "${image}" true\n'
env_case() { # <name> <script body printf format>: job fixture-run runs scripts/fixture-run.sh
  local dir; dir="$(new_case "$1")"
  printf "$2" >"${dir}/scripts/fixture-run.sh"
  add_job "${dir}" verify-agent-hygiene fixture-run 'bash scripts/fixture-run.sh'
  CASE_DIR="${dir}"
}
run_no_login='verify-agent-hygiene.yml:fixture-run: no Docker Hub docker/login-action@v3 step'

env_case job-env-overrides-default "${default_script}"
yq e -i '.jobs.fixture-run.env.FIXTURE_IMAGE = "postgres:18"' "${CASE_DIR}/.github/workflows/verify-agent-hygiene.yml"
expect_red job-env-value-beats-the-ghcr-default "${CASE_DIR}" "${run_no_login}"

env_case workflow-env-overrides-default "${default_script}"
yq e -i '.env.FIXTURE_IMAGE = "postgres:18"' "${CASE_DIR}/.github/workflows/verify-agent-hygiene.yml"
expect_red workflow-env-value-beats-the-ghcr-default "${CASE_DIR}" "${run_no_login}"

env_case job-env-unresolvable-expression "${default_script}"
yq e -i '.jobs.fixture-run.env.FIXTURE_IMAGE = "${{ inputs.image }}"' "${CASE_DIR}/.github/workflows/verify-agent-hygiene.yml"
expect_red unresolvable-expression-in-job-env-counts-as-hub "${CASE_DIR}" "${run_no_login}"

env_case job-env-ghcr-over-hub-default '#!/usr/bin/env bash\nimage="${FIXTURE_IMAGE:-postgres:18}"\ndocker run --rm "${image}" true\n'
yq e -i '.jobs.fixture-run.env.FIXTURE_IMAGE = "ghcr.io/eshu-hq/eshu:main"' "${CASE_DIR}/.github/workflows/verify-agent-hygiene.yml"
expect_green job-env-ghcr-value-beats-the-hub-default "${CASE_DIR}"

d="$(new_case env-expression-ghcr-in-run)"
add_job "${d}" verify-agent-hygiene fixture-run 'docker run --rm ${{ env.FIXTURE_IMAGE }} true'
yq e -i '.env.FIXTURE_IMAGE = "ghcr.io/eshu-hq/eshu:main"' "${d}/.github/workflows/verify-agent-hygiene.yml"
expect_green env-expression-of-a-ghcr-image-needs-no-login "${d}"

d="$(new_case env-expression-hub-in-run)"
add_job "${d}" verify-agent-hygiene fixture-run 'docker run --rm ${{ env.FIXTURE_IMAGE }} true'
yq e -i '.env.FIXTURE_IMAGE = "postgres:18"' "${d}/.github/workflows/verify-agent-hygiene.yml"
expect_red env-expression-of-a-hub-image-needs-login "${d}" "${run_no_login}"

compose_case compose-image-job-env-default $'services:\n  db:\n    image: ${FIXTURE_IMAGE:-ghcr.io/eshu-hq/eshu:main}\n'
yq e -i '.jobs.fixture-compose.env.FIXTURE_IMAGE = "postgres:18"' "${CASE_DIR}/.github/workflows/verify-agent-hygiene.yml"
expect_red compose-image-default-is-beaten-by-job-env "${CASE_DIR}" "${no_login}"

# --- G1c: docker is recognised in every command position ------------------------
fix_no_login='verify-agent-hygiene.yml:fixture-docker: no Docker Hub docker/login-action@v3 step'
run_red() { # <name> <run text>
  local dir; dir="$(new_case "$1")"
  add_job "${dir}" verify-agent-hygiene fixture-docker "$2"
  expect_red "$1" "${dir}" "${fix_no_login}"
}
run_green() { # <name> <run text>
  local dir; dir="$(new_case "$1")"
  add_job "${dir}" verify-agent-hygiene fixture-docker "$2"
  expect_green "$1" "${dir}"
}
run_red docker-after-if 'if docker pull postgres:18; then echo ok; fi'
run_red docker-after-while-not 'while ! docker pull postgres:18; do sleep 1; done'
run_red docker-after-until 'until docker pull postgres:18; do sleep 1; done'
run_red docker-after-then $'if true\nthen docker pull postgres:18\nfi'
run_red docker-in-bash-c "bash -c 'docker pull postgres:18'"
run_red docker-in-sh-c 'sh -c "docker pull postgres:18"'
run_red docker-after-line-continuation $'docker \\\n  pull postgres:18'
run_red docker-global-config-flag 'docker --config /tmp/dockercfg pull postgres:18'
run_red docker-global-context-flag 'docker --context ci -D pull postgres:18'
run_red docker-global-host-flag 'docker -H tcp://127.0.0.1:2375 pull postgres:18'
run_red docker-after-sudo 'sudo docker pull postgres:18'
run_red docker-after-xargs 'echo postgres:18 | xargs docker pull'
run_red docker-after-command 'command docker pull postgres:18'
run_red docker-after-env 'env DOCKER_BUILDKIT=1 docker pull postgres:18'
run_red docker-variable-invocation $'DOCKER=docker\n$DOCKER pull postgres:18'
run_red docker-unknown-wrapper-argument 'retry 3 docker pull postgres:18'
run_red docker-unresolved-variable-command '"${DOCKER_BIN}" pull postgres:18'

d="$(new_case compose-variable-invocation)"
printf 'services:\n  db:\n    image: postgres:18\n' >"${d}/docker-compose.fixture.yaml"
add_job "${d}" verify-agent-hygiene fixture-docker $'DC="docker compose"\n$DC -f docker-compose.fixture.yaml up -d'
expect_red compose-invoked-through-a-variable "${d}" "${fix_no_login}"

d="$(new_case compose-array-invocation)"
printf 'services:\n  db:\n    image: postgres:18\n' >"${d}/docker-compose.fixture.yaml"
add_job "${d}" verify-agent-hygiene fixture-docker $'COMPOSE_CMD=(docker compose)\n"${COMPOSE_CMD[@]}" -f docker-compose.fixture.yaml up -d'
expect_red compose-invoked-through-an-array "${d}" "${fix_no_login}"

run_green docker-ghcr-after-if 'if docker pull ghcr.io/eshu-hq/eshu:main; then echo ok; fi'
run_green docker-ghcr-in-bash-c "bash -c 'docker pull ghcr.io/eshu-hq/eshu:main'"
run_green docker-ghcr-with-context-flag 'docker --context ci pull ghcr.io/eshu-hq/eshu:main'
run_green docker-named-by-echo 'echo docker pull postgres:18'
run_green docker-existence-check 'command -v docker >/dev/null 2>&1 || echo missing'
run_green docker-inspection-only $'docker ps -q\ndocker compose ps -q\ndocker logs --tail 5 x || true'
run_green docker-in-heredoc-data $'cat <<\'EOF\' >note.txt\ndocker pull postgres:18\nEOF'
