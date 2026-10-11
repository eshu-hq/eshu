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
wjob "${d}" w-sudo 1 'sudo -u root bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
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
wjob "${d}" q-wrapper-without-ci-mode 0 "${wrapper_cmd}"
expect_queue_jobs_red queue-path-raw-compose-calls-fail "${d}" \
  q-raw-run-text q-raw-in-a-script q-raw-root-build q-raw-compose-run q-raw-docker-compose-binary q-raw-timeout \
  q-raw-retry-function q-raw-xargs q-raw-bash-lc-string q-raw-eval q-raw-command-array q-raw-variable-command \
  q-raw-array-of-flags q-raw-flag-values-before-subcommand q-raw-scale q-raw-extra-f-after-prepare \
  q-raw-overlay-operand-after-prepare q-raw-overlay-array-after-prepare q-raw-prepare-failure-swallowed \
  q-wrapper-other-directory q-wrapper-longer-name q-wrapper-with-two-prefixes q-wrapper-beside-a-raw-call q-wrapper-without-ci-mode \
  q-bash-c-string-with-a-raw-call-inside q-runner-function-with-a-raw-compose-inside \
  q-runner-argument-with-a-lookalike-path q-command-array-with-a-raw-call-beside

# A tree without the wrapper makes every wrapper call a raw call.
ledger_tree queue-red-wrapper-missing
d="${QUEUE_DIR}"
lj "${d}" q-wrapper-file-missing "${wrapper_cmd}" 1
expect_queue_jobs_red queue-path-wrapper-call-without-the-wrapper-file "${d}" q-wrapper-file-missing
