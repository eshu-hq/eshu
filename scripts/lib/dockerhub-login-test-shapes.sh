#!/usr/bin/env bash
#
# dockerhub-login-test-shapes.sh - seeded RED/GREEN cases for
# scripts/test-verify-dockerhub-login.sh (#7886). Sourced, never run, from that
# test after its own cases. It uses the test's helpers (new_case, add_job,
# run_verifier, expect_red, expect_green) and failure counter.
#
# Each batch plants several shapes as separate jobs in ONE scratch tree and runs
# the verifier once, then checks that every job is named (RED) or that the whole
# tree passes (GREEN). A job's pull is the same in every shape, so a miss names
# the exact shape that slipped through.
#
# shellcheck disable=SC2154  # failures, repo_root, verifier come from the test

job_no_login() { printf 'verify-agent-hygiene.yml:%s: no Docker Hub docker/login-action@v3 step' "$1"; }

# expect_jobs_red <case name> <dir> <job>...: one verifier run, one ok line per job.
expect_jobs_red() {
  local name="$1" dir="$2" out job
  shift 2
  if out="$(run_verifier "${dir}")"; then
    printf 'not ok - %s: verifier passed a tree with planted violations\n' "${name}"; failures=$((failures + 1))
    return 0
  fi
  for job in "$@"; do
    if [[ "${out}" == *"$(job_no_login "${job}")"* ]]; then
      printf 'ok - %s: %s\n' "${name}" "${job}"
    else
      printf 'not ok - %s: %s was not named in:\n%s\n' "${name}" "${job}" "${out}"; failures=$((failures + 1))
    fi
  done
}

# --- G7: a variable that holds docker is still docker ----------------------------
d="$(new_case g7-variable-runtime-shapes)"
add_job "${d}" verify-agent-hygiene g7-default-expansion $'runtime="${ESHU_OCI_RUNTIME:-docker}"\n"$runtime" pull alpine:3.21'
add_job "${d}" verify-agent-hygiene g7-command-substitution $'rt=$(command -v docker)\n"$rt" pull alpine:3.21'
add_job "${d}" verify-agent-hygiene g7-which-substitution $'rt=$(which docker)\n"$rt" pull alpine:3.21'
add_job "${d}" verify-agent-hygiene g7-assigned-behind-then-else $'if command -v podman >/dev/null; then rt=podman; else rt=docker; fi\n"$rt" pull alpine:3.21'
add_job "${d}" verify-agent-hygiene g7-assigned-behind-do $'for i in 1; do rt=docker; done\n"$rt" pull alpine:3.21'
add_job "${d}" verify-agent-hygiene g7-sticky-docker-value $'rt=docker\nif false; then rt=podman; fi\n"$rt" pull alpine:3.21'
add_job "${d}" verify-agent-hygiene g7-command-word-default '"${DOCKER_BIN:-docker}" pull alpine:3.21'
add_job "${d}" verify-agent-hygiene g7-command-word-which '$(which docker) pull alpine:3.21'
add_job "${d}" verify-agent-hygiene g7-command-word-command-v '"$(command -v docker)" pull alpine:3.21'
add_job "${d}" verify-agent-hygiene g7-array-default $'DC=("${DOCKER_BIN:-docker}" compose)\n"${DC[@]}" -f docker-compose.fixture.yaml up -d'
printf 'services:\n  db:\n    image: postgres:18\n' >"${d}/docker-compose.fixture.yaml"
expect_jobs_red g7-runtime-variable-shapes-need-the-hub-login "${d}" g7-default-expansion g7-command-substitution \
  g7-which-substitution g7-assigned-behind-then-else g7-assigned-behind-do g7-sticky-docker-value \
  g7-command-word-default g7-command-word-which g7-command-word-command-v g7-array-default

# A runtime variable set in a sourced lib, the OCI-runtime idiom in a script, and a
# variable command next to a docker word are unresolvable: fail closed (V meets D).
d="$(new_case g7-script-shapes)"
printf '#!/usr/bin/env bash\nsource "$(dirname "$0")/lib/fixture-runtime.sh"\n"$RUNTIME" pull alpine:3.21\n' >"${d}/scripts/fixture-sourced-runtime.sh"
printf '#!/usr/bin/env bash\nRUNTIME=docker\n' >"${d}/scripts/lib/fixture-runtime.sh"
printf '#!/usr/bin/env bash\nruntime="${ESHU_OCI_RUNTIME:-docker}"\n"$runtime" pull alpine:3.21\n' >"${d}/scripts/fixture-oci-idiom.sh"
printf '#!/usr/bin/env bash\ndocker ps -q\n"$TOOL" pull alpine:3.21\n' >"${d}/scripts/fixture-unresolved-var.sh"
add_job "${d}" verify-agent-hygiene g7-sourced-runtime 'bash scripts/fixture-sourced-runtime.sh'
add_job "${d}" verify-agent-hygiene g7-oci-runtime-idiom 'bash scripts/fixture-oci-idiom.sh'
add_job "${d}" verify-agent-hygiene g7-unresolved-variable-command 'bash scripts/fixture-unresolved-var.sh'
expect_jobs_red g7-script-runtime-shapes-need-the-hub-login "${d}" g7-sourced-runtime g7-oci-runtime-idiom g7-unresolved-variable-command

# GREEN controls: no docker word anywhere, a literal tool variable beside docker ps,
# a script-path variable, and an aliased pull of a ghcr image.
d="$(new_case g7-controls)"
printf '#!/usr/bin/env bash\nTOOL=go\n"$TOOL" test ./...\n"$OTHER" run\n' >"${d}/scripts/fixture-no-docker-word.sh"
printf '#!/usr/bin/env bash\nGO=go\n"$GO" test ./...\ndocker ps -q\n' >"${d}/scripts/fixture-literal-var.sh"
printf '#!/usr/bin/env bash\nverifier="${V:-scripts/verify-x.sh}"\n"${verifier}" --check\ndocker ps -q\n' >"${d}/scripts/fixture-script-var.sh"
add_job "${d}" verify-agent-hygiene g7-variable-command-no-docker-word 'bash scripts/fixture-no-docker-word.sh'
add_job "${d}" verify-agent-hygiene g7-literal-variable-beside-docker-ps 'bash scripts/fixture-literal-var.sh'
add_job "${d}" verify-agent-hygiene g7-script-path-variable-beside-docker-ps 'bash scripts/fixture-script-var.sh'
add_job "${d}" verify-agent-hygiene g7-alias-pulls-ghcr $'runtime="${X:-docker}"\n"$runtime" pull ghcr.io/eshu-hq/eshu:main'
add_job "${d}" verify-agent-hygiene g7-env-array-before-command $'args=(A=1)\nenv "${args[@]}" echo ok\ndocker ps -q'
expect_green variable-commands-without-a-docker-value-need-no-login "${d}"

# --- H1: every Docker Hub host spelling counts -----------------------------------
d="$(new_case h1-hub-hosts)"
add_job "${d}" verify-agent-hygiene h1-registry-hub-docker-com 'docker pull registry.hub.docker.com/library/alpine:3.21'
add_job "${d}" verify-agent-hygiene h1-index-docker-io 'docker pull index.docker.io/library/alpine:3.21'
add_job "${d}" verify-agent-hygiene h1-docker-io-library 'docker pull docker.io/library/alpine:3.21'
add_job "${d}" verify-agent-hygiene h1-registry-1 'docker pull registry-1.docker.io/library/alpine:3.21'
expect_jobs_red h1-hub-host-spellings-need-the-hub-login "${d}" h1-registry-hub-docker-com h1-index-docker-io h1-docker-io-library h1-registry-1

# --- H3: local composite actions are scanned, with the scripts they run ---------
composite_case() { # <name> <steps json>: job fixture-action uses ./.github/actions/fixture
  local dir; dir="$(new_case "$1")"
  mkdir -p "${dir}/.github/actions/fixture"
  yq e -n ".name = \"fixture\" | .description = \"fixture\" | .runs.using = \"composite\" | .runs.steps = $2" >"${dir}/.github/actions/fixture/action.yml"
  yq e -i '.jobs.fixture-action = {"runs-on": "ubuntu-latest", "steps": [{"uses": "actions/checkout@v5"}, {"uses": "./.github/actions/fixture"}]}' "${dir}/.github/workflows/verify-agent-hygiene.yml"
  CASE_DIR="${dir}"
}
composite_case composite-action-pulls-hub '[{"run": "docker pull postgres:18", "shell": "bash"}]'
expect_red composite-action-run-step-pulls-from-hub "${CASE_DIR}" "$(job_no_login fixture-action)"

composite_case composite-action-runs-script '[{"run": "bash scripts/fixture-pull.sh", "shell": "bash"}]'
printf '#!/usr/bin/env bash\ndocker pull postgres:18\n' >"${CASE_DIR}/scripts/fixture-pull.sh"
expect_red composite-action-script-pulls-from-hub "${CASE_DIR}" "$(job_no_login fixture-action)"

composite_case composite-action-no-docker '[{"run": "echo hello", "shell": "bash"}]'
expect_green composite-action-without-docker-needs-no-login "${CASE_DIR}"

# --- H3: the gate's triggers cover composite actions (.github/**) and .sh and
# .bash scripts (**/*.*sh). The registry rejects a glob that matches no tracked
# file, so .github/actions/** and **/*.bash cannot be listed on their own.
for glob in '.github/**' '**/*.*sh'; do
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

# --- H4: the CI cell runs this suite, so it needs a timeout above the 15 minute
# default (the 142-check suite measured 548 to 1088 s wall under load across
# BSD awk, gawk and mawk; see the comment above the cell in the workflow).
if rg -q 'append_gate "\$\{\{ steps\.filter\.outputs\.dockerhublogin \}\}".* 30$' "${repo_root}/.github/workflows/static-contract-gates.yml"; then
  echo "ok - dockerhublogin cell sets a 30 minute timeout"
else
  echo "not ok - dockerhublogin cell sets a 30 minute timeout"; failures=$((failures + 1))
fi
