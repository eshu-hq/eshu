#!/usr/bin/env bash
#
# dockerhub-login-test-calls.sh - seeded RED/GREEN cases for the per-call overlay
# reader of scripts/verify-dockerhub-login.sh (#7886). Sourced, never run, from
# scripts/test-verify-dockerhub-login.sh after lib/dockerhub-login-test-ledger.sh.
# It uses ledger_tree, lj, lput, expect_queue_jobs_red and the test's helpers.
#
# One correct call applies the overlay after eshu_ci_compose_prepare. A second
# call has no overlay and pulls a Docker Hub image at run time. If the reader sees
# only the correct call, it accepts the ledger image for the whole step and the
# second call passes. Every RED job below puts the second call in a shape that a
# reader of "docker compose ..." at the start of a command misses: behind a
# wrapper, in an array or a variable, behind a flag value, in a subcommand that
# pulls although it is not "up", or with an overlay that was reset, held in the
# wrong variable, or prepared in a context that exports nothing.
#
# shellcheck disable=SC2154  # failures, repo_root, verifier come from the test

# call_probe <dir> <job> <line>...: a job in CI image mode that prepares, applies
# the overlay on one pulling call, then runs the given lines.
call_probe() {
  local dir="$1" job="$2"
  shift 2
  {
    printf '#!/usr/bin/env bash\n'
    cat <<'EOF'
source "$(dirname "$0")/lib/ci-image-compose.sh"
eshu_ci_compose_prepare -f docker-compose.fixture-ledger.yaml
docker compose -f docker-compose.fixture-ledger.yaml -f "${ESHU_CI_COMPOSE_OVERLAY}" pull
EOF
    printf '%s\n' "$@"
  } >"${dir}/scripts/fixture-c-${job}.sh"
  lj "${dir}" "${job}" "bash scripts/fixture-c-${job}.sh" 1
}

plain='docker compose -f docker-compose.fixture-ledger.yaml'

# --- RED: a plain call the reader must still see -----------------------------------
ledger_tree queue-red-call-shapes
d="${QUEUE_DIR}"
call_probe "${d}" w-timeout "timeout 300 ${plain} up -d"
call_probe "${d}" w-nice-with-value "nice -n 5 ${plain} up -d"
call_probe "${d}" w-sudo-with-value "sudo -u root ${plain} up -d"
call_probe "${d}" w-stdbuf-with-value "stdbuf -o L ${plain} up -d"
call_probe "${d}" w-retry-function "retry 3 ${plain} up -d"
call_probe "${d}" w-xargs-pipe "echo up | xargs ${plain}"
call_probe "${d}" v-command-array 'DC=(docker compose -f docker-compose.fixture-ledger.yaml)' '"${DC[@]}" up -d'
call_probe "${d}" v-variable-command 'DOCKER=docker' "\$DOCKER compose -f docker-compose.fixture-ledger.yaml up -d"
call_probe "${d}" f-project-named-logs "docker compose -p logs -f docker-compose.fixture-ledger.yaml up -d"
call_probe "${d}" f-profile-named-ps "docker compose --profile ps -f docker-compose.fixture-ledger.yaml up -d"
call_probe "${d}" s-scale "${plain} scale db=2"
call_probe "${d}" s-run "${plain} run --rm db true"
call_probe "${d}" s-build "${plain} build"
call_probe "${d}" s-create "${plain} create"
call_probe "${d}" s-pull "${plain} pull"
# The overlay must be an -f operand, and it must be the one that is in force.
call_probe "${d}" o-alias-prefix 'base="${ESHU_CI_COMPOSE_OVERLAY}"' 'docker compose -f docker-compose.fixture-ledger.yaml -f "${base_file}" up -d'
call_probe "${d}" o-alias-reassigned 'ov="${ESHU_CI_COMPOSE_OVERLAY}"' 'ov=docker-compose.fixture-ledger.yaml' 'docker compose -f docker-compose.fixture-ledger.yaml -f "${ov}" up -d'
call_probe "${d}" o-array-reset 'args=(-f docker-compose.fixture-ledger.yaml -f "${ESHU_CI_COMPOSE_OVERLAY}")' 'args=(-f docker-compose.fixture-ledger.yaml)' 'docker compose "${args[@]}" up -d'
call_probe "${d}" o-env-file "${plain} --env-file \"\${ESHU_CI_COMPOSE_OVERLAY}\" up -d"
# A prepare whose export does not reach the call, or whose failure is swallowed.
# prep_in <dir> <job> <line>: the prepare sits in setup(), and <line> runs setup in a
# context that exports nothing to the shell that appends the overlay and calls compose.
prep_in() {
  lput "$1" "fixture-c-$2.sh" <<EOF
source "\$(dirname "\$0")/lib/ci-image-compose.sh"
setup() { eshu_ci_compose_prepare docker-compose.fixture-ledger.yaml; }
compose_args=(-f docker-compose.fixture-ledger.yaml)
$3
if [[ -n "\${ESHU_CI_COMPOSE_OVERLAY}" ]]; then
  compose_args+=(-f "\${ESHU_CI_COMPOSE_OVERLAY}")
fi
docker compose "\${compose_args[@]}" up -d
EOF
  lj "$1" "$2" "bash scripts/fixture-c-$2.sh" 1
}
prep_in "${d}" p-prepare-in-command-substitution 'x="$(setup)"'
prep_in "${d}" p-prepare-in-background-job 'setup &'
prep_in "${d}" p-prepare-in-pipeline 'setup | cat'
lput "${d}" fixture-c-prep-swallowed.sh <<'EOF'
source "$(dirname "$0")/lib/ci-image-compose.sh"
compose_args=(-f docker-compose.fixture-ledger.yaml)
eshu_ci_compose_prepare "${compose_args[@]}" || true
if [[ -n "${ESHU_CI_COMPOSE_OVERLAY}" ]]; then
  compose_args+=(-f "${ESHU_CI_COMPOSE_OVERLAY}")
fi
docker compose "${compose_args[@]}" up -d
EOF
lj "${d}" p-prepare-failure-swallowed "bash scripts/fixture-c-prep-swallowed.sh" 1
expect_queue_jobs_red queue-path-unseen-compose-calls-fail "${d}" \
  w-timeout w-nice-with-value w-sudo-with-value w-stdbuf-with-value w-retry-function w-xargs-pipe v-command-array \
  v-variable-command f-project-named-logs f-profile-named-ps s-scale s-run s-build s-create s-pull \
  o-alias-prefix o-alias-reassigned o-array-reset o-env-file \
  p-prepare-in-command-substitution p-prepare-in-background-job p-prepare-in-pipeline p-prepare-failure-swallowed

# --- GREEN: calls that pull nothing, and calls that apply the overlay ---------------
ledger_tree queue-green-call-shapes
d="${QUEUE_DIR}"
call_probe "${d}" g-down "${plain} down -v --remove-orphans"
call_probe "${d}" g-config "${plain} config --format json"
call_probe "${d}" g-ps-logs-exec "${plain} ps" "${plain} logs --tail=50 db" "${plain} exec -T db true"
call_probe "${d}" g-version-and-start 'docker compose version' "${plain} start"
call_probe "${d}" g-context-flag 'docker --context ci compose -f docker-compose.fixture-ledger.yaml -f "${ESHU_CI_COMPOSE_OVERLAY}" up -d'
call_probe "${d}" g-docker-compose-binary 'docker-compose -f docker-compose.fixture-ledger.yaml -f "${ESHU_CI_COMPOSE_OVERLAY}" up -d'
call_probe "${d}" g-timeout-with-overlay 'timeout 300 docker compose -f docker-compose.fixture-ledger.yaml -f "${ESHU_CI_COMPOSE_OVERLAY}" up -d'
call_probe "${d}" g-array-with-overlay 'args=(-f docker-compose.fixture-ledger.yaml -f "${ESHU_CI_COMPOSE_OVERLAY}")' 'docker compose "${args[@]}" up -d'
call_probe "${d}" g-command-array-with-overlay 'args=(-f docker-compose.fixture-ledger.yaml -f "${ESHU_CI_COMPOSE_OVERLAY}")' 'DC=(docker compose "${args[@]}")' '"${DC[@]}" up -d'
call_probe "${d}" g-alias-with-overlay 'ov="${ESHU_CI_COMPOSE_OVERLAY}"' 'docker compose -f docker-compose.fixture-ledger.yaml -f "${ov}" up -d'
call_probe "${d}" g-wrapper-script 'bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d'
mkdir -p "${d}/scripts/ci"
cp "${repo_root}/scripts/ci/compose.sh" "${d}/scripts/ci/compose.sh"
expect_green queue-path-pulling-nothing-and-overlay-applied-calls-pass "${d}"
