#!/usr/bin/env bash
#
# dockerhub-login-test-calls.sh - seeded RED/GREEN cases for the two classes of
# compose call in scripts/verify-dockerhub-login.sh (#7886). Sourced, never run,
# from scripts/test-verify-dockerhub-login.sh after lib/dockerhub-login-test-ledger.sh.
# It uses ledger_tree, lj, lput, expect_queue_jobs_red and the test's helpers.
#
# The contract has two classes and no overlay modelling:
#   W  the command word is the literal scripts/ci/compose.sh (behind bash, sh,
#      exec, timeout, nice, sudo or env, with at most one variable prefix), the
#      step is in CI image mode, and the file is in the tree. The wrapper renders
#      an overlay that covers every image or exits 1, and it refuses to run
#      without CI image mode in CI, so the gate settles the call and does not
#      resolve its -f operands. Every W fixture below names a Docker Hub compose
#      file (fixture-ledger.yaml) to prove that the gate does not read it.
#   R  every other compose call is judged by the images its files name, exactly
#      as without CI image mode. "${ESHU_CI_COMPOSE_OVERLAY}" as an -f operand
#      settles nothing. Every R fixture names the same Hub compose file.
#
# shellcheck disable=SC2154  # failures, repo_root, verifier come from the test

hub_file='docker-compose.fixture-ledger.yaml'
wrapper_cmd='bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'

# wrapper_tree <name>: a ledger tree with the real wrapper in it.
wrapper_tree() {
  ledger_tree "$1"
  mkdir -p "${QUEUE_DIR}/scripts/ci"
  cp "${repo_root}/scripts/ci/compose.sh" "${QUEUE_DIR}/scripts/ci/compose.sh"
}

# wjob <dir> <job> <ci 0|1> <line>...: a job whose script runs the given lines.
wjob() {
  local dir="$1" job="$2" ci="$3"
  shift 3
  printf '%s\n' "$@" | lput "${dir}" "fixture-c-${job}.sh"
  lj "${dir}" "${job}" "bash scripts/fixture-c-${job}.sh" "${ci}"
}

# --- GREEN: a call through the wrapper is settled, whatever its operands ------------
wrapper_tree queue-green-wrapper-calls
d="${QUEUE_DIR}"
lj "${d}" w-run-text "${wrapper_cmd}" 1
wjob "${d}" w-script 1 "${wrapper_cmd}"
wjob "${d}" w-repo-root-prefix 1 'repo_root="$(pwd)"' 'bash "${repo_root}/scripts/ci/compose.sh" -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" w-unquoted-variable-prefix 1 'bash $repo_root/scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" w-dot-slash 1 './scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" w-timeout 1 'timeout 300 bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" w-nice 1 'nice -n 5 bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" w-sudo-preserving-the-environment 1 'sudo -E -u root bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" w-env 1 'env FOO=1 bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" w-exec 1 'exec bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" w-sh 1 'sh scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" w-in-a-subshell 1 '( bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d )'
wjob "${d}" w-in-a-function 1 'stack_up() { bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d; }' 'stack_up'
wjob "${d}" w-in-a-pipeline 1 'bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d | cat'
wjob "${d}" w-in-the-background 1 'bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d &' 'wait'
wjob "${d}" w-unresolvable-operand 1 'bash scripts/ci/compose.sh -f "${compose_file}" -- up -d'
wjob "${d}" w-two-stacks 1 'bash scripts/ci/compose.sh -p a -f docker-compose.fixture-ledger.yaml -- up -d' \
  'bash scripts/ci/compose.sh -p b -f docker-compose.fixture-other.yaml -- up -d'
wjob "${d}" w-under-xargs 1 'echo x | xargs bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" w-as-the-argument-of-a-runner-function 1 'run_bounded() { "$@"; }' 'run_bounded bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" w-in-a-literal-bash-c-string 1 "bash -o pipefail -c 'bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- config'"
wjob "${d}" w-in-a-command-array 1 'WC=(bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml --)' '"${WC[@]}" up -d'
wjob "${d}" w-variable-timeout-duration 1 'timeout "${wait_seconds}" bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" w-env-keeps-the-mode 1 'env GITHUB_ACTIONS=true ESHU_CI_IMAGES=1 bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" w-prefix-keeps-the-mode 1 'ESHU_CI_IMAGES=1 bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" w-pulling-nothing-beside 1 "${wrapper_cmd}" 'docker compose -f docker-compose.fixture-ledger.yaml down -v'
expect_green queue-path-wrapper-calls-are-settled "${d}"

# --- RED: every other call is judged by its files ------------------------------------
wrapper_tree queue-red-raw-compose-calls
d="${QUEUE_DIR}"
raw='docker compose -f docker-compose.fixture-ledger.yaml'
lj "${d}" q-raw-run-text "${raw} up -d" 1
wjob "${d}" q-raw-in-a-script 1 "${raw} up -d"
wjob "${d}" q-raw-root-build 1 'docker compose -f docker-compose.fixture-root-build.yaml up -d --build'
wjob "${d}" q-raw-compose-run 1 "${raw} run --rm db true"
wjob "${d}" q-raw-docker-compose-binary 1 'docker-compose -f docker-compose.fixture-ledger.yaml up -d'
wjob "${d}" q-raw-timeout 1 "timeout 300 ${raw} up -d"
wjob "${d}" q-raw-retry-function 1 "retry 3 ${raw} up -d"
wjob "${d}" q-raw-xargs 1 "echo up | xargs ${raw}"
wjob "${d}" q-raw-bash-lc-string 1 "cmd=\"${raw} up -d\"" 'bash -lc "${cmd}"'
wjob "${d}" q-raw-eval 1 "eval \"${raw} up -d\""
wjob "${d}" q-raw-command-array 1 'DC=(docker compose -f docker-compose.fixture-ledger.yaml)' '"${DC[@]}" up -d'
wjob "${d}" q-raw-variable-command 1 'DOCKER=docker' '$DOCKER compose -f docker-compose.fixture-ledger.yaml up -d'
wjob "${d}" q-raw-array-of-flags 1 'args=(-f docker-compose.fixture-ledger.yaml)' 'docker compose "${args[@]}" up -d'
wjob "${d}" q-raw-flag-values-before-subcommand 1 'docker compose -p logs --profile ps -f docker-compose.fixture-ledger.yaml up -d'
wjob "${d}" q-raw-scale 1 "${raw} scale db=2"
wjob "${d}" q-raw-extra-f-after-prepare 1 'source "$(dirname "$0")/lib/ci-image-compose.sh"' \
  'eshu_ci_compose_prepare -f docker-compose.fixture-ledger.yaml' \
  'docker compose -f docker-compose.fixture-ledger.yaml -f docker-compose.fixture-other.yaml up -d'
# The former overlay-operand settle is gone: a correct prepare and the operand do not
# make a direct call safe. This is the contract change.
wjob "${d}" q-raw-overlay-operand-after-prepare 1 'source "$(dirname "$0")/lib/ci-image-compose.sh"' \
  'eshu_ci_compose_prepare -f docker-compose.fixture-ledger.yaml' \
  'docker compose -f docker-compose.fixture-ledger.yaml -f "${ESHU_CI_COMPOSE_OVERLAY}" up -d'
wjob "${d}" q-raw-overlay-array-after-prepare 1 'source "$(dirname "$0")/lib/ci-image-compose.sh"' \
  'compose_args=(-f docker-compose.fixture-ledger.yaml)' 'eshu_ci_compose_prepare "${compose_args[@]}"' \
  'if [[ -n "${ESHU_CI_COMPOSE_OVERLAY}" ]]; then compose_args+=(-f "${ESHU_CI_COMPOSE_OVERLAY}"); fi' \
  'docker compose "${compose_args[@]}" up -d'
wjob "${d}" q-raw-prepare-failure-swallowed 1 'source "$(dirname "$0")/lib/ci-image-compose.sh"' \
  'compose_args=(-f docker-compose.fixture-ledger.yaml)' 'eshu_ci_compose_prepare "${compose_args[@]}" || true' \
  'if [[ -n "${ESHU_CI_COMPOSE_OVERLAY}" ]]; then compose_args+=(-f "${ESHU_CI_COMPOSE_OVERLAY}"); fi' \
  'docker compose "${compose_args[@]}" up -d'
# A call that only looks like the wrapper (another directory, a longer name, a second
# prefix) is a raw call. A wrapper path held entirely in a variable is not seen at all:
# a static limit (docs/internal/ci-images.md).
wjob "${d}" q-wrapper-other-directory 1 'bash tools/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" q-wrapper-longer-name 1 'bash scripts/ci/compose.sh.old -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" q-wrapper-with-two-prefixes 1 'bash a/b/scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" q-wrapper-beside-a-raw-call 1 "${wrapper_cmd}" "${raw} up -d"
# The wrapper match is wider than the command word: it is also settled as the argument
# of a runner function, in a literal bash -c string and in a command array. These are
# the neighbours that must stay raw: a raw call in the same string, a runner function
# that runs a raw compose itself, and a look-alike path handed to a runner.
wjob "${d}" q-bash-c-string-with-a-raw-call-inside 1 \
  "bash -c 'bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- config; docker compose -f docker-compose.fixture-ledger.yaml up -d'"
wjob "${d}" q-runner-function-with-a-raw-compose-inside 1 \
  'run_bounded() { docker compose -f docker-compose.fixture-ledger.yaml up -d; "$@"; }' \
  'run_bounded bash scripts/ci/compose.sh -f docker-compose.fixture-other.yaml -- config'
wjob "${d}" q-runner-argument-with-a-lookalike-path 1 \
  'run_bounded bash tools/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" q-command-array-with-a-raw-call-beside 1 \
  'WC=(bash scripts/ci/compose.sh -f docker-compose.fixture-other.yaml --)' '"${WC[@]}" config' \
  'docker compose -f docker-compose.fixture-ledger.yaml up -d'
# An env or a prefix that removes GITHUB_ACTIONS or CI image mode runs the wrapper raw:
# the run-time guard tests GITHUB_ACTIONS, so it never fires. Those calls are raw.
wjob "${d}" q-env-ignore-environment-before-the-wrapper 1 'env -i bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" q-env-unset-the-mode-before-the-wrapper 1 'env -u GITHUB_ACTIONS -u ESHU_CI_IMAGES bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" q-env-long-unset-before-the-wrapper 1 'env --unset=GITHUB_ACTIONS bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" q-prefix-turns-the-mode-off-before-the-wrapper 1 'GITHUB_ACTIONS=false ESHU_CI_IMAGES=0 bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" q-prefix-turns-ci-images-off-before-the-wrapper 1 'ESHU_CI_IMAGES=0 bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
# A docker compose call that carries the wrapper path as its own arguments is the raw
# call it is: run pulls the service image before the container command starts. A
# second raw name that is not Docker Hub must not hide it.
printf 'services:\n  db:\n    image: %s\n' "${ledger_pin_alpine}" >"${d}/docker-compose.fixture-ghcr.yaml"
wjob "${d}" q-compose-run-with-the-wrapper-as-its-command 1 \
  'docker compose -f docker-compose.fixture-ghcr.yaml config' \
  'docker compose -f docker-compose.fixture-ledger.yaml run --rm db bash scripts/ci/compose.sh'
wjob "${d}" q-docker-compose-binary-with-the-wrapper-as-its-command 1 \
  'docker compose -f docker-compose.fixture-ghcr.yaml config' \
  'docker-compose -f docker-compose.fixture-ledger.yaml run --rm db bash scripts/ci/compose.sh'
# sudo resets the environment by default (env_reset): GITHUB_ACTIONS and CI image mode
# are gone, so the wrapper runs raw unless sudo is told to keep them.
wjob "${d}" q-sudo-resets-the-environment-before-the-wrapper 1 'sudo -u root bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
# env - and exec -c clear the environment like env -i; doas and bwrap start another
# one. A variable at command position may hold docker compose: its words are not read.
wjob "${d}" q-env-bare-dash-before-the-wrapper 1 'env - bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" q-exec-clears-the-environment-before-the-wrapper 1 'exec -c bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" q-doas-before-the-wrapper 1 'doas bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" q-bwrap-clearenv-before-the-wrapper 1 'bwrap --clearenv --bind / / bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
wjob "${d}" q-variable-docker-with-the-wrapper-as-its-command 1 \
  'DOCKER=docker' 'docker compose -f docker-compose.fixture-ghcr.yaml config' \
  '$DOCKER compose -f docker-compose.fixture-ledger.yaml run --rm db bash scripts/ci/compose.sh'
wjob "${d}" q-braced-variable-docker-with-the-wrapper-as-its-command 1 \
  'DOCKER_BIN=docker' 'docker compose -f docker-compose.fixture-ghcr.yaml config' \
  '"${DOCKER_BIN}" compose -f docker-compose.fixture-ledger.yaml run --rm db bash scripts/ci/compose.sh'
# A raw call whose subcommand pulls or builds is judged by its files.
wjob "${d}" q-raw-pull 1 "${raw} pull"
wjob "${d}" q-raw-create 1 "${raw} create"
wjob "${d}" q-raw-build 1 "${raw} build"
wjob "${d}" q-wrapper-without-ci-mode 0 "${wrapper_cmd}"
expect_queue_jobs_red queue-path-raw-compose-calls-fail "${d}" \
  q-raw-run-text q-raw-in-a-script q-raw-root-build q-raw-compose-run q-raw-docker-compose-binary q-raw-timeout \
  q-raw-retry-function q-raw-xargs q-raw-bash-lc-string q-raw-eval q-raw-command-array q-raw-variable-command \
  q-raw-array-of-flags q-raw-flag-values-before-subcommand q-raw-scale q-raw-extra-f-after-prepare \
  q-raw-overlay-operand-after-prepare q-raw-overlay-array-after-prepare q-raw-prepare-failure-swallowed \
  q-wrapper-other-directory q-wrapper-longer-name q-wrapper-with-two-prefixes q-wrapper-beside-a-raw-call q-wrapper-without-ci-mode \
  q-bash-c-string-with-a-raw-call-inside q-runner-function-with-a-raw-compose-inside \
  q-runner-argument-with-a-lookalike-path q-command-array-with-a-raw-call-beside \
  q-env-ignore-environment-before-the-wrapper q-env-unset-the-mode-before-the-wrapper \
  q-env-long-unset-before-the-wrapper q-prefix-turns-the-mode-off-before-the-wrapper \
  q-prefix-turns-ci-images-off-before-the-wrapper q-compose-run-with-the-wrapper-as-its-command \
  q-docker-compose-binary-with-the-wrapper-as-its-command q-sudo-resets-the-environment-before-the-wrapper \
  q-env-bare-dash-before-the-wrapper q-exec-clears-the-environment-before-the-wrapper q-doas-before-the-wrapper \
  q-bwrap-clearenv-before-the-wrapper q-variable-docker-with-the-wrapper-as-its-command \
  q-braced-variable-docker-with-the-wrapper-as-its-command q-raw-pull q-raw-create q-raw-build

# A direct call with a non-pulling subcommand reaches no image, whatever file it names:
# the golden gate keeps its polls and teardown direct (main's gate read them the same).
wrapper_tree queue-green-raw-nonpulling-calls
d="${QUEUE_DIR}"
for sub in 'exec -T db true' 'ps -q' 'logs --tail=20' 'down -v' 'config --quiet' 'stop' 'restart db'; do
  wjob "${d}" "g-raw-${sub%% *}" 1 "docker compose -f docker-compose.fixture-ledger.yaml ${sub}"
done
wjob "${d}" g-raw-nonpulling-beside-a-wrapper-up 1 "${wrapper_cmd}" \
  'docker compose -f docker-compose.fixture-ledger.yaml exec -T db true'
expect_green queue-path-direct-nonpulling-calls-pull-nothing "${d}"

# A tree without the wrapper makes every wrapper call a raw call.
ledger_tree queue-red-wrapper-missing
d="${QUEUE_DIR}"
lj "${d}" q-wrapper-file-missing "${wrapper_cmd}" 1
expect_queue_jobs_red queue-path-wrapper-call-without-the-wrapper-file "${d}" q-wrapper-file-missing
