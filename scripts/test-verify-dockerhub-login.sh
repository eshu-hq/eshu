#!/usr/bin/env bash
#
# test-verify-dockerhub-login.sh - hermetic tests for
# scripts/verify-dockerhub-login.sh (#7886).
#
# The committed tree must pass (GREEN). Each RED case plants one violation in a
# scratch copy of the repo (workflows, scripts, compose files, Dockerfile), so
# the committed files are never touched, and expects the verifier to name it.
# GREEN cases prove the derivation does not over-reach (ghcr-only images, a
# docker/login-action + metadata-only job, a path or comment that merely names docker).
# The G7/H1/H3/H4 batch (variable runtimes, Hub host spellings, composite actions,
# triggers, CI timeout) lives in lib/dockerhub-login-test-shapes.sh, sourced after
# these cases, and the script-reference batch (env prefixes, path-qualified shells,
# wrapper flags, several calls on a line) in lib/dockerhub-login-test-script-refs.sh.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-dockerhub-login.sh"
for tool in yq rg; do
  command -v "${tool}" >/dev/null 2>&1 || { echo "test-verify-dockerhub-login: ${tool} is required" >&2; exit 1; }
done
tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT
failures=0

# One scratch base copy; each case copies it. The verifier reads the workflows,
# the scripts they invoke, the compose files and the Dockerfile. The base keeps
# only the workflows the cases mutate (the committed tree is checked whole by
# the first case), which keeps each verifier run to a few seconds.
base="${tmp_root}/base"
mkdir -p "${base}/.github/workflows"
for wf in e2e-tests live-backend-tests reducer-contention-gate docker-publish verify-agent-hygiene verify-replay-tier; do
  cp "${repo_root}/.github/workflows/${wf}.yml" "${base}/.github/workflows/${wf}.yml"
done
cp -R "${repo_root}/scripts" "${base}/scripts"
cp "${repo_root}"/Dockerfile "${repo_root}"/docker-compose*.y*ml "${base}/"

new_case() { # <name>: prints the scratch repo dir
  local dir="${tmp_root}/$1"
  cp -R "${base}" "${dir}"
  printf '%s' "${dir}"
}

run_verifier() { ESHU_DOCKERHUB_LOGIN_ROOT="$1" bash "${verifier}" 2>&1; }

expect_red() { # <name> <dir> <expected output fragment>
  local out
  if out="$(run_verifier "$2")"; then
    printf 'not ok - %s: verifier passed a planted violation\n' "$1"; failures=$((failures + 1))
  elif [[ "${out}" == *"$3"* ]]; then
    printf 'ok - %s\n' "$1"
  else
    printf 'not ok - %s: expected "%s" in:\n%s\n' "$1" "$3" "${out}"; failures=$((failures + 1))
  fi
}

expect_green() { # <name> <dir>
  local out
  if out="$(run_verifier "$2")"; then
    printf 'ok - %s\n' "$1"
  else
    printf 'not ok - %s: verifier failed a clean tree:\n%s\n' "$1" "${out}"; failures=$((failures + 1))
  fi
}

# add_job <dir> <workflow> <job> <run-text>: a checkout + one run step job, no login.
add_job() {
  RUN="$4" yq e -i ".jobs[\"$3\"] = {\"runs-on\": \"ubuntu-latest\", \"steps\": [{\"uses\": \"actions/checkout@v5\"}, {\"name\": \"Fixture\", \"run\": strenv(RUN)}]}" "$1/.github/workflows/$2.yml"
}

login_ref='.uses == "docker/login-action@v3"'

# --- GREEN: the committed tree, and the scratch copy of it ----------------------
if bash "${verifier}" >/dev/null 2>&1; then
  echo "ok - committed workflows pass"
else
  echo "not ok - committed workflows pass"; failures=$((failures + 1))
fi
expect_green scratch-copy-passes "${base}"

# --- RED: scope derived from scripts and workflows, no hand-kept list ----------
d="$(new_case script-driven-no-login)"
printf '#!/usr/bin/env bash\nsource "$(dirname "$0")/lib/fixture-docker-lib.sh"\n' >"${d}/scripts/fixture-docker.sh"
printf '#!/usr/bin/env bash\n# cycle back: source scripts/fixture-docker.sh\nsource scripts/fixture-docker.sh\ndocker run --rm alpine:3.21 true\n' >"${d}/scripts/lib/fixture-docker-lib.sh"
add_job "${d}" verify-agent-hygiene fixture-docker 'bash scripts/fixture-docker.sh'
expect_red script-driven-job-without-login "${d}" 'verify-agent-hygiene.yml:fixture-docker: no Docker Hub docker/login-action@v3 step'

d="$(new_case direct-docker-no-login)"
add_job "${d}" verify-agent-hygiene fixture-docker 'docker pull postgres:18'
expect_red direct-docker-job-without-login "${d}" 'verify-agent-hygiene.yml:fixture-docker: no Docker Hub docker/login-action@v3 step'

d="$(new_case login-after-first-docker)"
# Move the login after the script-driven step that pulls (steps[5] runs the
# script, steps[1] is the login in the committed workflow).
yq e -i '.jobs.live-backend.steps as $s | .jobs.live-backend.steps = ($s[0:1] + $s[2:6] + [$s[1]] + $s[6:])' "${d}/.github/workflows/live-backend-tests.yml"
expect_red login-placed-after-first-docker-step "${d}" 'live-backend-tests.yml:live-backend: login steps[5] comes after the first Docker-reaching steps[4]'

d="$(new_case service-without-credentials)"
yq e -i 'del(.jobs.contention-gate.services.postgres.credentials)' "${d}/.github/workflows/reducer-contention-gate.yml"
expect_red service-without-credentials "${d}" 'reducer-contention-gate.yml:contention-gate: service postgres'

d="$(new_case ghcr-login-only)"
yq e -i "(.jobs.test.steps[] | select(${login_ref}) | .with.registry) = \"ghcr.io\"" "${d}/.github/workflows/e2e-tests.yml"
expect_red login-to-ghcr-only "${d}" 'e2e-tests.yml:test: no Docker Hub docker/login-action@v3 step'

d="$(new_case wrong-secret)"
yq e -i "(.jobs.live-backend.steps[] | select(${login_ref}) | .with.password) = \"x\"" "${d}/.github/workflows/live-backend-tests.yml"
expect_red wrong-secret-names "${d}" 'login steps[1] must use the DOCKERHUB_USERNAME and DOCKERHUB_TOKEN secrets'

d="$(new_case ungated-login)"
yq e -i "del(.jobs.test.steps[] | select(${login_ref}) | .if)" "${d}/.github/workflows/e2e-tests.yml"
expect_red ungated-login-step "${d}" 'must run only when DOCKERHUB_LOGIN_ENABLED is true'

d="$(new_case missing-login)"
yq e -i "del(.jobs.test.steps[] | select(${login_ref}))" "${d}/.github/workflows/e2e-tests.yml"
expect_red missing-login-step "${d}" 'e2e-tests.yml:test: no Docker Hub docker/login-action@v3 step'

d="$(new_case login-without-job-env)"
yq e -i 'del(.jobs.test.env.DOCKERHUB_LOGIN_ENABLED)' "${d}/.github/workflows/e2e-tests.yml"
expect_red login-without-job-env "${d}" 'job env must define DOCKERHUB_LOGIN_ENABLED'

d="$(new_case build-push-without-login)"
yq e -i "del(.jobs.build-and-push-image.steps[] | select(${login_ref} and .with.registry == null))" "${d}/.github/workflows/docker-publish.yml"
expect_red build-push-job-without-hub-login "${d}" 'docker-publish.yml:build-and-push-image: no Docker Hub docker/login-action@v3 step'

d="$(new_case container-action-hub-image)"
yq e -i '.jobs.fixture-action = {"runs-on": "ubuntu-latest", "steps": [{"uses": "docker://alpine:3.21"}]}' "${d}/.github/workflows/verify-agent-hygiene.yml"
expect_red container-action-pulled-from-hub "${d}" 'verify-agent-hygiene.yml:fixture-action: no Docker Hub docker/login-action@v3 step'

d="$(new_case script-docker-run-hub-default)"
printf '#!/usr/bin/env bash\nimage="${FIXTURE_IMAGE:-neo4j@sha256:6c162e2432f861f2c4e3da77a6ba478e7f10e2160b870541f85294532bc6ff5f}"\ndocker run --rm -d --name "fixture" -p 127.0.0.1::7687 -e "A=1" \\\n\t"${image}" >/dev/null\n' >"${d}/scripts/fixture-run.sh"
add_job "${d}" verify-agent-hygiene fixture-run 'bash scripts/fixture-run.sh'
expect_red script-docker-run-of-a-hub-default-image "${d}" 'verify-agent-hygiene.yml:fixture-run: no Docker Hub docker/login-action@v3 step'

d="$(new_case direct-buildx-build)"
add_job "${d}" verify-agent-hygiene fixture-build 'docker buildx build --platform linux/amd64 --no-cache .'
expect_red buildx-build-of-hub-based-dockerfile "${d}" 'verify-agent-hygiene.yml:fixture-build: no Docker Hub docker/login-action@v3 step'

d="$(new_case compose-hub-image)"
printf '#!/usr/bin/env bash\ndocker compose -f docker-compose.fixture.yaml up -d\n' >"${d}/scripts/fixture-compose.sh"
printf 'services:\n  db:\n    image: postgres:18\n' >"${d}/docker-compose.fixture.yaml"
add_job "${d}" verify-agent-hygiene fixture-compose 'bash scripts/fixture-compose.sh'
expect_red compose-file-with-hub-image "${d}" 'verify-agent-hygiene.yml:fixture-compose: no Docker Hub docker/login-action@v3 step'

d="$(new_case compose-unnamed-file)"
printf '#!/usr/bin/env bash\ndocker compose -f "${COMPOSE_FILE}" up -d\n' >"${d}/scripts/fixture-compose.sh"
add_job "${d}" verify-agent-hygiene fixture-compose 'bash scripts/fixture-compose.sh'
expect_red compose-file-not-derivable-counts-as-hub "${d}" 'verify-agent-hygiene.yml:fixture-compose: no Docker Hub docker/login-action@v3 step'

# --- GREEN: the derivation must not over-reach ---------------------------------
d="$(new_case ghcr-only-compose)"
printf '#!/usr/bin/env bash\ndocker compose -f docker-compose.fixture.yaml up -d\n' >"${d}/scripts/fixture-compose.sh"
printf 'services:\n  api:\n    image: ghcr.io/eshu-hq/eshu:main\n' >"${d}/docker-compose.fixture.yaml"
add_job "${d}" verify-agent-hygiene fixture-compose 'bash scripts/fixture-compose.sh'
expect_green compose-only-ghcr-images-need-no-login "${d}"

d="$(new_case ghcr-container-action)"
yq e -i '.jobs.fixture-action = {"runs-on": "ubuntu-latest", "steps": [{"uses": "docker://ghcr.io/eshu-hq/eshu:main"}]}' "${d}/.github/workflows/verify-agent-hygiene.yml"
expect_green ghcr-container-action-needs-no-login "${d}"

d="$(new_case ghcr-docker-run-direct)"
add_job "${d}" verify-agent-hygiene fixture-ghcr 'docker run --rm -e A=1 ghcr.io/eshu-hq/eshu:main true'
expect_green direct-docker-run-of-a-ghcr-image-needs-no-login "${d}"

d="$(new_case ghcr-docker-run-script-var)"
printf '#!/usr/bin/env bash\nFIXTURE_IMAGE="ghcr.io/eshu-hq/nornicdb:pinned@sha256:74a8ed7b"\ndocker run -d --name fixture \\\n\t-p "${HTTP_PORT}:7474" \\\n\t"${FIXTURE_IMAGE}" >/dev/null\n' >"${d}/scripts/fixture-run.sh"
add_job "${d}" verify-agent-hygiene fixture-ghcr 'bash scripts/fixture-run.sh'
expect_green script-docker-run-of-a-ghcr-variable-needs-no-login "${d}"

d="$(new_case ghcr-imagetools-step-env)"
printf '#!/usr/bin/env bash\nimage="${IMAGE:?IMAGE is required}"\ndocker buildx imagetools create -t "${image}:main" "${image}@${DIGEST}"\n' >"${d}/scripts/fixture-promote.sh"
add_job "${d}" verify-agent-hygiene fixture-promote 'bash scripts/fixture-promote.sh'
yq e -i '.env.REGISTRY = "ghcr.io"' "${d}/.github/workflows/verify-agent-hygiene.yml"
yq e -i '.jobs.fixture-promote.steps[1].env.IMAGE = "${{ env.REGISTRY }}/${{ github.repository }}"' "${d}/.github/workflows/verify-agent-hygiene.yml"
expect_green imagetools-of-a-ghcr-image-from-step-env-needs-no-login "${d}"

d="$(new_case ghcr-only-dockerfile-build)"
printf 'FROM ghcr.io/eshu-hq/base:1 AS base\nFROM base AS final\n' >"${d}/Dockerfile.fixture"
add_job "${d}" verify-agent-hygiene fixture-build 'docker build -f Dockerfile.fixture -t fixture .'
expect_green build-of-a-ghcr-only-dockerfile-needs-no-login "${d}"

d="$(new_case ghcr-compose-build)"
printf 'FROM ghcr.io/eshu-hq/base:1\n' >"${d}/Dockerfile.fixture"
printf 'services:\n  api:\n    build:\n      context: .\n      dockerfile: Dockerfile.fixture\n' >"${d}/docker-compose.fixture.yaml"
printf '#!/usr/bin/env bash\ndocker compose -f docker-compose.fixture.yaml up -d --build\n' >"${d}/scripts/fixture-compose.sh"
add_job "${d}" verify-agent-hygiene fixture-compose 'bash scripts/fixture-compose.sh'
expect_green compose-build-of-a-literal-ghcr-dockerfile-needs-no-login "${d}"

d="$(new_case unresolved-rule-text)"
if out="$(ESHU_DOCKERHUB_LOGIN_ROOT="${d}" ESHU_DOCKERHUB_LOGIN_EXPLAIN=1 bash "${verifier}" 2>&1)" && [[ "${out}" == *"An image host that cannot be resolved statically, for example an environment-variable reference without a default, counts as Docker Hub, because that is the only fail-closed rule."* ]]; then
  echo "ok - explain output states the unresolvable-host rule"
else
  printf 'not ok - explain output states the unresolvable-host rule:\n%s\n' "${out:-}"; failures=$((failures + 1))
fi
if bash "${verifier}" --help | rg -q 'counts as Docker Hub, because that is the only fail-closed rule'; then
  echo "ok - help output states the unresolvable-host rule"
else
  echo "not ok - help output states the unresolvable-host rule"; failures=$((failures + 1))
fi

d="$(new_case replay-tier-is-ghcr)"
expect_green committed-ghcr-nornicdb-replay-tier-needs-no-login "${d}"

d="$(new_case ghcr-only-service)"
yq e -i '.jobs.fixture-svc = {"runs-on": "ubuntu-latest", "services": {"api": {"image": "ghcr.io/eshu-hq/eshu:main"}}, "steps": [{"run": "echo ok"}]}' "${d}/.github/workflows/verify-agent-hygiene.yml"
expect_green ghcr-only-service-needs-no-credentials "${d}"

d="$(new_case login-action-only)"
yq e -i '.jobs.fixture-setup = {"runs-on": "ubuntu-latest", "steps": [{"uses": "docker/login-action@v3", "with": {"registry": "ghcr.io", "username": "u", "password": "p"}}, {"uses": "docker/metadata-action@v5"}]}' "${d}/.github/workflows/verify-agent-hygiene.yml"
expect_green login-and-metadata-steps-are-not-docker-uses "${d}"

d="$(new_case names-docker-only)"
printf '#!/usr/bin/env bash\n# docker compose up is documented in docker-compose.yaml\necho "run docker compose -f docker-compose.yaml up"\nrg -q docker-compose.yaml README.md || true\n' >"${d}/scripts/fixture-mentions.sh"
add_job "${d}" verify-agent-hygiene fixture-mentions 'bash scripts/fixture-mentions.sh'
expect_green comment-string-and-compose-path-are-not-docker-uses "${d}"

d="$(new_case alternate-job-env)"
add_job "${d}" verify-agent-hygiene fixture-docker 'docker pull postgres:18'
yq e -i '.jobs.fixture-docker.steps = [{"uses": "docker/login-action@v3", "if": "env.DOCKERHUB_LOGIN_ENABLED == '"'"'true'"'"'", "with": {"username": "${{ secrets.DOCKERHUB_USERNAME }}", "password": "${{ secrets.DOCKERHUB_TOKEN }}"}}] + .jobs.fixture-docker.steps' "${d}/.github/workflows/verify-agent-hygiene.yml"
yq e -i ".jobs.fixture-docker.env.DOCKERHUB_LOGIN_ENABLED = \"\${{ secrets.DOCKERHUB_USERNAME != '' && secrets.DOCKERHUB_TOKEN != '' }}\"" "${d}/.github/workflows/verify-agent-hygiene.yml"
expect_green compliant-new-job-passes "${d}"

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

# --- G2: setup-buildx / setup-qemu are Docker Hub pulls; the login precedes them --
login_json='{"uses": "docker/login-action@v3", "if": "env.DOCKERHUB_LOGIN_ENABLED == '"'"'true'"'"'", "with": {"username": "${{ secrets.DOCKERHUB_USERNAME }}", "password": "${{ secrets.DOCKERHUB_TOKEN }}"}}'
enabled_json="\${{ secrets.DOCKERHUB_USERNAME != '' && secrets.DOCKERHUB_TOKEN != '' }}"
buildx_job() { # <dir> <steps json>
  add_job "$1" verify-agent-hygiene fixture-docker 'echo placeholder'
  yq e -i ".jobs.fixture-docker.steps = $2" "$1/.github/workflows/verify-agent-hygiene.yml"
  yq e -i ".jobs.fixture-docker.env.DOCKERHUB_LOGIN_ENABLED = \"${enabled_json}\"" "$1/.github/workflows/verify-agent-hygiene.yml"
}
buildx_step='{"uses": "docker/setup-buildx-action@v3"}'
pull_step='{"run": "docker pull postgres:18"}'

d="$(new_case login-after-setup-buildx)"
buildx_job "${d}" "[${buildx_step}, ${login_json}, ${pull_step}]"
expect_red login-between-setup-buildx-and-the-build "${d}" 'login steps[1] comes after the first Docker-reaching steps[0] (docker/setup-buildx-action bootstraps'

d="$(new_case login-before-setup-buildx)"
buildx_job "${d}" "[${login_json}, ${buildx_step}, ${pull_step}]"
expect_green login-before-setup-buildx-passes "${d}"

# A setup-buildx step is itself a Docker Hub pull (the moby/buildkit bootstrap):
# a job with only that step needs the login first.
d="$(new_case setup-buildx-only)"
buildx_job "${d}" "[${buildx_step}]"
expect_red setup-buildx-alone-needs-the-hub-login "${d}" 'verify-agent-hygiene.yml:fixture-docker: no Docker Hub docker/login-action@v3 step'

d="$(new_case setup-buildx-with-ghcr-login)"
yq e -i '.jobs.fixture-setup = {"runs-on": "ubuntu-latest", "steps": [{"uses": "docker/setup-buildx-action@v3"}, {"uses": "docker/login-action@v3", "with": {"registry": "ghcr.io", "username": "u", "password": "p"}}]}' "${d}/.github/workflows/verify-agent-hygiene.yml"
expect_red setup-buildx-and-a-ghcr-login-need-the-hub-login "${d}" 'verify-agent-hygiene.yml:fixture-setup: no Docker Hub docker/login-action@v3 step'

d="$(new_case login-then-setup-buildx-only)"
buildx_job "${d}" "[${login_json}, ${buildx_step}]"
expect_green login-then-setup-buildx-passes "${d}"

d="$(new_case setup-buildx-docker-driver)"
buildx_job "${d}" '[{"uses": "docker/setup-buildx-action@v3", "with": {"driver": "docker"}}]'
expect_green docker-driver-pulls-no-builder-image "${d}"

d="$(new_case setup-buildx-ghcr-builder-image)"
buildx_job "${d}" '[{"uses": "docker/setup-buildx-action@v3", "with": {"driver-opts": "image=ghcr.io/eshu-hq/buildkit:1"}}]'
expect_green ghcr-builder-image-needs-no-login "${d}"

d="$(new_case setup-buildx-hub-builder-image)"
buildx_job "${d}" '[{"uses": "docker/setup-buildx-action@v3", "with": {"driver-opts": "image=moby/buildkit:master"}}]'
expect_red hub-builder-image-needs-login "${d}" 'verify-agent-hygiene.yml:fixture-docker: no Docker Hub docker/login-action@v3 step'

d="$(new_case setup-qemu-default-image)"
buildx_job "${d}" '[{"uses": "docker/setup-qemu-action@v3"}]'
expect_red setup-qemu-default-image-is-a-hub-pull "${d}" 'verify-agent-hygiene.yml:fixture-docker: no Docker Hub docker/login-action@v3 step'

d="$(new_case setup-qemu-ghcr-image)"
buildx_job "${d}" '[{"uses": "docker/setup-qemu-action@v3", "with": {"image": "ghcr.io/eshu-hq/binfmt:latest"}}]'
expect_green setup-qemu-ghcr-image-needs-no-login "${d}"

# --- G3: a login step must not continue on error --------------------------------
d="$(new_case login-continue-on-error)"
yq e -i "(.jobs.test.steps[] | select(${login_ref}) | .continue-on-error) = true" "${d}/.github/workflows/e2e-tests.yml"
expect_red login-with-continue-on-error "${d}" 'e2e-tests.yml:test: login steps[1] sets continue-on-error (true)'

d="$(new_case login-continue-on-error-expression)"
yq e -i "(.jobs.test.steps[] | select(${login_ref}) | .continue-on-error) = \"\${{ github.event_name == 'push' }}\"" "${d}/.github/workflows/e2e-tests.yml"
expect_red login-with-continue-on-error-expression "${d}" 'sets continue-on-error'

d="$(new_case login-continue-on-error-false)"
yq e -i "(.jobs.test.steps[] | select(${login_ref}) | .continue-on-error) = false" "${d}/.github/workflows/e2e-tests.yml"
expect_green login-with-continue-on-error-false-passes "${d}"

# --- G5: the gate's triggers cover the files whose names it derives from --------
# The registry row and the static-contract-gates path filter must both list the
# nested Dockerfile and compose globs (the lockstep gate keeps them equal).
for glob in '**/Dockerfile*' '**/docker-compose*.y*ml'; do
  if yq e '.gates[] | select(.id == "dockerhub-login") | .triggers[]' "${repo_root}/specs/ci-gates.v1.yaml" | rg -qFx -- "${glob}"; then
    echo "ok - registry row triggers include ${glob}"
  else
    echo "not ok - registry row triggers include ${glob}"; failures=$((failures + 1))
  fi
  if yq e '.jobs.changes.steps[] | select(.id == "filter") | .with.filters' "${repo_root}/.github/workflows/static-contract-gates.yml" |
    yq e '.dockerhublogin[]' - | rg -qFx -- "${glob}"; then
    echo "ok - path filter dockerhublogin includes ${glob}"
  else
    echo "not ok - path filter dockerhublogin includes ${glob}"; failures=$((failures + 1))
  fi
done

# --- G7, H1, H3, H4: variable runtimes, Hub hosts, composite actions, triggers ---
# shellcheck source=lib/dockerhub-login-test-shapes.sh
source "${repo_root}/scripts/lib/dockerhub-login-test-shapes.sh"
# shellcheck source=lib/dockerhub-login-test-script-refs.sh
source "${repo_root}/scripts/lib/dockerhub-login-test-script-refs.sh"

if [[ "${failures}" -gt 0 ]]; then
  echo "test-verify-dockerhub-login: ${failures} case(s) failed" >&2
  exit 1
fi
echo "test-verify-dockerhub-login tests passed"
