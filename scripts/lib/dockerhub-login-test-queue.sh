#!/usr/bin/env bash
#
# dockerhub-login-test-queue.sh - seeded RED/GREEN cases for the queue-path rule
# of scripts/verify-dockerhub-login.sh (#7886). Sourced, never run, from
# scripts/test-verify-dockerhub-login.sh after its own cases. It uses the test's
# helpers (new_case, add_job, run_verifier, expect_red, expect_green) and its
# failure counter.
#
# A job of a workflow whose on: has merge_group or pull_request must not reach
# Docker Hub: the RED batch plants one Docker Hub reach per class as a separate
# job in ONE scratch queue workflow (fixture-queue.yml) and checks that the
# verifier names every job, and the GREEN batch plants the ghcr.io digest form of
# the same classes and checks that the whole tree passes. The login rule for jobs
# outside the queue path is covered by the cases in the main test script.
#
# shellcheck disable=SC2154  # failures, repo_root, verifier come from the test

# Two ghcr.io digest refs for the host checks, read from the ledger so the test
# copies no digest (specs/ci-images.v1.json is the only approved list).
ghcr_digest="$(jq -r '.images[] | select(.id == "alpine") | .destination + "@" + .digest' "${repo_root}/specs/ci-images.v1.json")"
ghcr_buildkit="$(jq -r '.images[] | select(.id == "buildkit") | .destination + "@" + .digest' "${repo_root}/specs/ci-images.v1.json")"
queue_wf='.github/workflows/fixture-queue.yml'

queue_job_red() { printf 'fixture-queue.yml:%s: queue-path job (its workflow runs on merge_group or pull_request) must not reach Docker Hub' "$1"; }

# expect_queue_jobs_red <case name> <dir> <job>...: one verifier run, one ok line per job.
expect_queue_jobs_red() {
  local name="$1" dir="$2" out job
  shift 2
  if out="$(run_verifier "${dir}")"; then
    printf 'not ok - %s: verifier passed a tree with planted queue-path violations\n' "${name}"; failures=$((failures + 1))
    return 0
  fi
  for job in "$@"; do
    if [[ "${out}" == *"$(queue_job_red "${job}")"* ]]; then
      printf 'ok - %s: %s\n' "${name}" "${job}"
    else
      printf 'not ok - %s: %s was not named in:\n%s\n' "${name}" "${job}" "${out}"; failures=$((failures + 1))
    fi
  done
}

# queue_tree <name>: a scratch tree with an empty queue-path workflow
# (merge_group and pull_request) and the helper scripts the shapes call.
queue_tree() {
  local dir; dir="$(new_case "$1")"
  yq e -n '.name = "fixture-queue" | .on.merge_group.types = ["checks_requested"] | .on.pull_request = null | .jobs = {}' >"${dir}/${queue_wf}"
  printf 'FROM alpine:3.21\n' >"${dir}/Dockerfile.fixture-hub"
  printf 'FROM %s\n' "${ghcr_digest}" >"${dir}/Dockerfile.fixture-ghcr"
  printf 'services:\n  db:\n    image: postgres:18\n' >"${dir}/docker-compose.fixture-hub.yaml"
  printf 'services:\n  db:\n    image: %s\n' "${ghcr_digest}" >"${dir}/docker-compose.fixture-ghcr.yaml"
  printf '#!/usr/bin/env bash\ndocker compose -f docker-compose.fixture-hub.yaml up -d\n' >"${dir}/scripts/fixture-q-compose-hub.sh"
  printf '#!/usr/bin/env bash\ndocker compose -f docker-compose.fixture-ghcr.yaml up -d\n' >"${dir}/scripts/fixture-q-compose-ghcr.sh"
  printf '#!/usr/bin/env bash\nsource "$(dirname "$0")/lib/fixture-q-run-lib.sh"\n' >"${dir}/scripts/fixture-q-run-hub.sh"
  printf '#!/usr/bin/env bash\ndocker run --rm alpine:3.21 true\n' >"${dir}/scripts/lib/fixture-q-run-lib.sh"
  printf '#!/usr/bin/env bash\nsource "$(dirname "$0")/lib/fixture-q-run-lib-ghcr.sh"\n' >"${dir}/scripts/fixture-q-run-ghcr.sh"
  printf '#!/usr/bin/env bash\ndocker run --rm %s true\n' "${ghcr_digest}" >"${dir}/scripts/lib/fixture-q-run-lib-ghcr.sh"
  QUEUE_DIR="${dir}"
}

# qjob <dir> <job> <json>: sets one job of the queue workflow from a JSON map.
qjob() { yq e -i ".jobs[\"$2\"] = $3" "$1/${queue_wf}"; }
steps_run() { printf '{"runs-on": "ubuntu-latest", "steps": [{"uses": "actions/checkout@v5"}, {"run": "%s"}]}' "$1"; }

# --- RED: every Docker Hub reach class fails on the queue path --------------------
queue_tree queue-red-reach-classes
d="${QUEUE_DIR}"
qjob "${d}" q-service-image '{"runs-on": "ubuntu-latest", "services": {"db": {"image": "postgres:18"}}, "steps": [{"run": "echo ok"}]}'
qjob "${d}" q-service-image-with-hub-credentials '{"runs-on": "ubuntu-latest", "services": {"db": {"image": "postgres:18", "credentials": {"username": "${{ secrets.DOCKERHUB_USERNAME }}", "password": "${{ secrets.DOCKERHUB_TOKEN }}"}}}, "steps": [{"run": "echo ok"}]}'
qjob "${d}" q-job-container '{"runs-on": "ubuntu-latest", "container": "postgres:18", "steps": [{"run": "echo ok"}]}'
qjob "${d}" q-compose-image "$(steps_run 'bash scripts/fixture-q-compose-hub.sh')"
qjob "${d}" q-dockerfile-from "$(steps_run 'docker build -f Dockerfile.fixture-hub -t fixture .')"
qjob "${d}" q-build-push-from '{"runs-on": "ubuntu-latest", "steps": [{"uses": "docker/build-push-action@v6", "with": {"context": ".", "file": "Dockerfile.fixture-hub"}}]}'
qjob "${d}" q-docker-run-in-transitive-script "$(steps_run 'bash scripts/fixture-q-run-hub.sh')"
qjob "${d}" q-docker-run-direct "$(steps_run 'docker run --rm alpine:3.21 true')"
qjob "${d}" q-setup-buildx-default '{"runs-on": "ubuntu-latest", "steps": [{"uses": "docker/setup-buildx-action@v3"}]}'
qjob "${d}" q-setup-buildx-hub-builder-image '{"runs-on": "ubuntu-latest", "steps": [{"uses": "docker/setup-buildx-action@v3", "with": {"driver-opts": "image=moby/buildkit:master"}}]}'
qjob "${d}" q-setup-qemu-default '{"runs-on": "ubuntu-latest", "steps": [{"uses": "docker/setup-qemu-action@v3"}]}'
qjob "${d}" q-container-action '{"runs-on": "ubuntu-latest", "steps": [{"uses": "docker://alpine:3.21"}]}'
# A step whose whole run is an expression may run any script the workflow names.
qjob "${d}" q-expression-run '{"runs-on": "ubuntu-latest", "steps": [{"run": "${{ matrix.command }}"}]}'
# A job-level if: does not take a job off the queue path.
qjob "${d}" q-job-if-does-not-escape '{"runs-on": "ubuntu-latest", "if": "github.event_name == '"'"'push'"'"'", "services": {"db": {"image": "postgres:18"}}, "steps": [{"run": "echo ok"}]}'
qjob "${d}" q-env-expression-hub "$(steps_run 'docker run --rm ${{ env.FIXTURE_IMAGE }} true')"
yq e -i '.env.FIXTURE_IMAGE = "postgres:18"' "${d}/${queue_wf}"
expect_queue_jobs_red queue-path-reach-classes-fail "${d}" q-service-image q-service-image-with-hub-credentials \
  q-job-container q-compose-image q-dockerfile-from q-build-push-from q-docker-run-in-transitive-script q-docker-run-direct \
  q-setup-buildx-default q-setup-buildx-hub-builder-image q-setup-qemu-default q-container-action q-expression-run \
  q-job-if-does-not-escape q-env-expression-hub

# --- GREEN: Docker Hub login wiring on a queue-path job that reaches nothing ------
# A login, a login flag or service credentials are not a reach and not a finding
# of this rule; the dead-login rule is a separate contract.
queue_tree queue-green-login-wiring-without-reach
d="${QUEUE_DIR}"
qjob "${d}" g-login-without-reach '{"runs-on": "ubuntu-latest", "steps": [{"uses": "docker/login-action@v3", "if": "env.DOCKERHUB_LOGIN_ENABLED == '"'"'true'"'"'", "with": {"username": "${{ secrets.DOCKERHUB_USERNAME }}", "password": "${{ secrets.DOCKERHUB_TOKEN }}"}}, {"run": "echo ok"}]}'
qjob "${d}" g-login-env-without-reach '{"runs-on": "ubuntu-latest", "env": {"DOCKERHUB_LOGIN_ENABLED": "${{ secrets.DOCKERHUB_USERNAME != '"'"''"'"' && secrets.DOCKERHUB_TOKEN != '"'"''"'"' }}"}, "steps": [{"run": "docker pull '"${ghcr_digest}"'"}]}'
qjob "${d}" g-credentials-on-a-ghcr-service '{"runs-on": "ubuntu-latest", "services": {"db": {"image": "'"${ghcr_digest}"'", "credentials": {"username": "${{ secrets.DOCKERHUB_USERNAME }}", "password": "${{ secrets.DOCKERHUB_TOKEN }}"}}}, "steps": [{"run": "echo ok"}]}'
expect_green queue-path-login-wiring-without-a-reach-is-not-a-finding "${d}"
queue_tree queue-red-login-does-not-waive-a-reach
d="${QUEUE_DIR}"
qjob "${d}" q-reach-with-login '{"runs-on": "ubuntu-latest", "env": {"DOCKERHUB_LOGIN_ENABLED": "${{ secrets.DOCKERHUB_USERNAME != '"'"''"'"' && secrets.DOCKERHUB_TOKEN != '"'"''"'"' }}"}, "steps": [{"uses": "docker/login-action@v3", "if": "env.DOCKERHUB_LOGIN_ENABLED == '"'"'true'"'"'", "with": {"username": "${{ secrets.DOCKERHUB_USERNAME }}", "password": "${{ secrets.DOCKERHUB_TOKEN }}"}}, {"run": "docker pull postgres:18"}]}'
expect_queue_jobs_red a-gated-login-does-not-waive-a-queue-path-reach "${d}" q-reach-with-login

# --- GREEN: the ghcr.io digest form of each class passes -------------------------
queue_tree queue-green-ghcr-classes
d="${QUEUE_DIR}"
qjob "${d}" g-service-image '{"runs-on": "ubuntu-latest", "permissions": {"contents": "read", "packages": "read"}, "services": {"db": {"image": "'"${ghcr_digest}"'", "credentials": {"username": "${{ github.actor }}", "password": "${{ secrets.GITHUB_TOKEN }}"}}}, "steps": [{"run": "echo ok"}]}'
qjob "${d}" g-compose-image "$(steps_run 'bash scripts/fixture-q-compose-ghcr.sh')"
qjob "${d}" g-dockerfile-from "$(steps_run 'docker build -f Dockerfile.fixture-ghcr -t fixture .')"
qjob "${d}" g-build-push-from '{"runs-on": "ubuntu-latest", "steps": [{"uses": "docker/build-push-action@v6", "with": {"context": ".", "file": "Dockerfile.fixture-ghcr"}}]}'
qjob "${d}" g-docker-run-in-transitive-script "$(steps_run 'bash scripts/fixture-q-run-ghcr.sh')"
qjob "${d}" g-setup-buildx-ghcr-builder '{"runs-on": "ubuntu-latest", "steps": [{"uses": "docker/setup-buildx-action@v3", "with": {"driver-opts": "image='"${ghcr_buildkit}"'"}}]}'
qjob "${d}" g-setup-qemu-ghcr-image '{"runs-on": "ubuntu-latest", "steps": [{"uses": "docker/setup-qemu-action@v3", "with": {"image": "ghcr.io/eshu-hq/binfmt:latest"}}]}'
qjob "${d}" g-ghcr-login-then-pull '{"runs-on": "ubuntu-latest", "permissions": {"contents": "read", "packages": "read"}, "steps": [{"uses": "docker/login-action@v3", "with": {"registry": "ghcr.io", "username": "${{ github.actor }}", "password": "${{ secrets.GITHUB_TOKEN }}"}}, {"run": "docker pull '"${ghcr_digest}"'"}]}'
qjob "${d}" g-expression-run '{"runs-on": "ubuntu-latest", "steps": [{"run": "${{ matrix.command }}"}]}'
qjob "${d}" g-script-named-for-expression "$(steps_run 'bash scripts/fixture-q-run-ghcr.sh')"
qjob "${d}" g-no-docker "$(steps_run 'echo hello')"
expect_green queue-path-ghcr-digest-form-of-every-class-passes "${d}"

# --- trigger forms: string, list and map on:, and a reusable callee ---------------
trigger_case() { # <name> <on yq assignment>: a Hub service job in fixture-queue.yml
  queue_tree "$1"
  yq e -i 'del(.on)' "${QUEUE_DIR}/${queue_wf}"
  yq e -i "$2" "${QUEUE_DIR}/${queue_wf}"
  qjob "${QUEUE_DIR}" q-trigger-form '{"runs-on": "ubuntu-latest", "services": {"db": {"image": "postgres:18"}}, "steps": [{"run": "echo ok"}]}'
}
trigger_case queue-on-string '.on = "pull_request"'
expect_queue_jobs_red on-as-a-string-is-the-queue-path "${QUEUE_DIR}" q-trigger-form
trigger_case queue-on-list '.on = ["push", "merge_group"]'
expect_queue_jobs_red on-as-a-list-is-the-queue-path "${QUEUE_DIR}" q-trigger-form
trigger_case queue-on-map-filtered '.on.pull_request.paths = ["scripts/**"] | .on.push.branches = ["main"]'
expect_queue_jobs_red on-pull-request-with-filters-is-the-queue-path "${QUEUE_DIR}" q-trigger-form

# Outside the queue path (tag push, schedule, dispatch): the Docker Hub login rule
# applies, not the queue-path rule.
trigger_case non-queue-no-login '.on.push.tags = ["v*"] | .on.schedule = [{"cron": "0 3 * * 1"}] | .on.workflow_dispatch = null'
if out="$(run_verifier "${QUEUE_DIR}")"; then
  printf 'not ok - non-queue-hub-service-without-credentials: verifier passed\n'; failures=$((failures + 1))
elif [[ "${out}" == *'fixture-queue.yml:q-trigger-form: service db'* && "${out}" != *'queue-path job'* ]]; then
  echo 'ok - non-queue-hub-service-without-credentials needs credentials, not ghcr'
else
  printf 'not ok - non-queue-hub-service-without-credentials: unexpected output:\n%s\n' "${out}"; failures=$((failures + 1))
fi
trigger_case non-queue-with-login '.on.push.tags = ["v*"] | .on.workflow_dispatch = null'
qjob "${QUEUE_DIR}" q-trigger-form '{"runs-on": "ubuntu-latest", "services": {"db": {"image": "postgres:18", "credentials": {"username": "${{ secrets.DOCKERHUB_USERNAME }}", "password": "${{ secrets.DOCKERHUB_TOKEN }}"}}}, "steps": [{"run": "echo ok"}]}'
expect_green dispatch-and-tag-only-hub-job-with-credentials-passes "${QUEUE_DIR}"

# A reusable workflow inherits the caller's triggers.
queue_tree queue-reusable-callee
d="${QUEUE_DIR}"
yq e -n '.name = "callee" | .on.workflow_call = null | .jobs.inner = {"runs-on": "ubuntu-latest", "services": {"db": {"image": "postgres:18", "credentials": {"username": "${{ secrets.DOCKERHUB_USERNAME }}", "password": "${{ secrets.DOCKERHUB_TOKEN }}"}}}, "steps": [{"run": "echo ok"}]}' >"${d}/.github/workflows/fixture-callee.yml"
qjob "${d}" call '{"uses": "./.github/workflows/fixture-callee.yml"}'
if out="$(run_verifier "${d}")"; then
  printf 'not ok - reusable-callee-of-a-queue-workflow: verifier passed\n'; failures=$((failures + 1))
elif [[ "${out}" == *'fixture-callee.yml:inner: queue-path job'* ]]; then
  echo 'ok - reusable-callee-of-a-queue-workflow is on the queue path'
else
  printf 'not ok - reusable-callee-of-a-queue-workflow: unexpected output:\n%s\n' "${out}"; failures=$((failures + 1))
fi
d="$(new_case non-queue-reusable-callee)"
yq e -n '.name = "callee" | .on.workflow_call = null | .jobs.inner = {"runs-on": "ubuntu-latest", "services": {"db": {"image": "postgres:18", "credentials": {"username": "${{ secrets.DOCKERHUB_USERNAME }}", "password": "${{ secrets.DOCKERHUB_TOKEN }}"}}}, "steps": [{"run": "echo ok"}]}' >"${d}/.github/workflows/fixture-callee.yml"
yq e -i '.jobs.call = {"uses": "./.github/workflows/fixture-callee.yml"}' "${d}/.github/workflows/verify-agent-hygiene.yml"
expect_green reusable-callee-of-a-non-queue-workflow-keeps-the-login-rule "${d}"

# --- the committed contention-gate workflow is off the queue path (no ---------
# --- merge_group/pull_request trigger since the merge-queue cleanup), so a -----
# --- planted Docker Hub image must fail under the login rule instead: the ------
# --- verifier never reaches the login check for a queue-path job, so this ------
# --- string also proves the job is not queue-path (still fail-closed). ---------
d="$(new_case real-reducer-contention-workflow)"
cp "${repo_root}/.github/workflows/reducer-contention-gate.yml" "${d}/.github/workflows/reducer-contention-gate.yml"
yq e -i '.jobs.contention-gate.services.postgres.image = "postgres:18-alpine"' "${d}/.github/workflows/reducer-contention-gate.yml"
expect_red committed-queue-workflow-with-a-planted-hub-image "${d}" 'reducer-contention-gate.yml:contention-gate: service postgres (postgres:18-alpine) pulls from Docker Hub at job init without credentials'

d="$(new_case explain-lists-queue-path)"
if out="$(ESHU_DOCKERHUB_LOGIN_ROOT="${d}" ESHU_DOCKERHUB_LOGIN_EXPLAIN=1 bash "${verifier}" 2>&1)" && [[ "${out}" == *'queue-path: '*' workflow file(s) are on the queue path'* ]]; then
  echo 'ok - explain output reports the queue-path workflow count'
else
  printf 'not ok - explain output reports the queue-path workflow count:\n%s\n' "${out:-}"; failures=$((failures + 1))
fi
