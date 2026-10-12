#!/usr/bin/env bash
#
# dockerhub-login-test-ledger.sh - seeded RED/GREEN cases for the CI image ledger
# model of scripts/verify-dockerhub-login.sh (#7886). Sourced, never run, from
# scripts/test-verify-dockerhub-login.sh after the queue batch. It uses queue_tree,
# qjob and expect_queue_jobs_red from lib/dockerhub-login-test-queue.sh and the
# test's helpers.
#
# specs/ci-images.v1.json is the only approved-image list, and the gate reads it
# from the tree under test at run time (it holds the required pins that put a step
# in CI image mode). These cases read the same file for every pin they need, so no
# digest is copied here. The Dockerfile ARG and build-arg cases, the operand cache,
# the conditional-literal chains, the runner shapes and the unreadable-ledger case
# live here; the compose call classes (wrapper settled, everything else raw) are in
# lib/dockerhub-login-test-calls.sh.
#
# shellcheck disable=SC2154  # failures, repo_root, verifier come from the test

ledger_file="${repo_root}/specs/ci-images.v1.json"
ledger_helper="${repo_root}/scripts/lib/ci_image_compose.py"
ledger_ids="$(rg -o -N '_REQUIRED = frozenset\(\{[^}]*\}\)' "${ledger_helper}" | rg -o '"[a-z0-9-]+"' | jq -s -c '.')"
# The job env of CI image mode, from the ledger: ESHU_CI_IMAGES plus each required pin.
ledger_ci_env="$(jq -c --argjson ids "${ledger_ids}" \
  '{ESHU_CI_IMAGES: "1"} + ([.images[] | select(.id as $i | $ids | index($i)) | {(.env): (.destination + "@" + .digest)}] | add)' \
  "${ledger_file}")"
ledger_runtime_source="$(jq -r '.images[] | select(.id == "postgres-alpine") | .runtime_sources[0]' "${ledger_file}")"
ledger_pin_alpine="$(jq -r '.images[] | select(.id == "alpine") | .destination + "@" + .digest' "${ledger_file}")"

# ledger_tree <name>: a queue tree with the ledger and the helper, a stub
# eshu_ci_compose_prepare, and the compose files and Dockerfiles the cases use.
ledger_tree() {
  queue_tree "$1"
  local d="${QUEUE_DIR}"
  mkdir -p "${d}/specs"
  cp "${ledger_file}" "${d}/specs/ci-images.v1.json"
  cp "${ledger_helper}" "${d}/scripts/lib/ci_image_compose.py"
  printf '#!/usr/bin/env bash\neshu_ci_compose_prepare() { export ESHU_CI_COMPOSE_OVERLAY="${1:-overlay.yaml}"; }\n' >"${d}/scripts/lib/ci-image-compose.sh"
  printf 'services:\n  db:\n    image: %s\n' "${ledger_runtime_source}" >"${d}/docker-compose.fixture-ledger.yaml"
  printf 'services:\n  db:\n    image: redis:7\n' >"${d}/docker-compose.fixture-other.yaml"
  printf 'services:\n  api:\n    build:\n      context: .\n      dockerfile: Dockerfile\n' >"${d}/docker-compose.fixture-root-build.yaml"
  printf 'services:\n  api:\n    build:\n      context: .\n      dockerfile: Dockerfile.fixture-hub\n' >"${d}/docker-compose.fixture-other-build.yaml"
  printf 'ARG BASE=alpine:3.21\nFROM ${BASE}\n' >"${d}/Dockerfile.fixture-arg"
  printf 'ARG BASE=%s\nFROM ${BASE}\n' "${ledger_pin_alpine}" >"${d}/Dockerfile.fixture-arg-ghcr"
  QUEUE_DIR="${d}"
}

# lput <dir> <name>: writes scripts/<name> from stdin behind a bash shebang.
lput() { { printf '#!/usr/bin/env bash\n'; cat; } >"$1/scripts/$2"; }

# lj <dir> <job> <run> <ci 0|1> [extra env JSON]: a queue job; ci=1 puts the job in
# CI image mode from the ledger.
lj() {
  local jobenv='{}'
  [[ "$4" != 1 ]] || jobenv="${ledger_ci_env}"
  [[ -z "${5:-}" ]] || jobenv="$(jq -c -n --argjson a "${jobenv}" --argjson b "$5" '$a + $b')"
  qjob "$1" "$2" "$(jq -c -n --arg run "$3" --argjson env "${jobenv}" \
    '{"runs-on": "ubuntu-latest", "env": $env, "steps": [{"uses": "actions/checkout@v5"}, {"run": $run}]}')"
}

# --- Dockerfile ARGs and build args (raw judgment, no CI mode needed) --------------
ledger_tree queue-arg-cases
d="${QUEUE_DIR}"
lj "${d}" g-build-arg-names-a-ledger-pin "docker build --build-arg BASE=${ledger_pin_alpine} -f Dockerfile.fixture-arg ."  0
lj "${d}" g-arg-default-is-ledger-pin 'docker build -f Dockerfile.fixture-arg-ghcr .' 0
lj "${d}" g-build-arg-from-env-reference 'docker build --build-arg BASE=${BASE} -f Dockerfile.fixture-arg .' 0 "{\"BASE\": \"${ledger_pin_alpine}\"}"
expect_green queue-path-build-args-settle-a-dockerfile-arg "${d}"
lj "${d}" r-arg-default-hub-without-build-arg 'docker build -f Dockerfile.fixture-arg .' 0
lj "${d}" r-env-does-not-reach-an-arg 'docker build -f Dockerfile.fixture-arg .' 0 "{\"BASE\": \"${ledger_pin_alpine}\"}"
lj "${d}" r-build-arg-overrides-to-hub 'docker build --build-arg BASE=alpine:3.21 -f Dockerfile.fixture-arg-ghcr .' 0
lj "${d}" r-build-arg-from-unset-env-reference 'docker build --build-arg BASE=${BASE} -f Dockerfile.fixture-arg-ghcr .' 0
expect_queue_jobs_red queue-path-dockerfile-arg-bounds-fail "${d}" r-arg-default-hub-without-build-arg \
  r-env-does-not-reach-an-arg r-build-arg-overrides-to-hub r-build-arg-from-unset-env-reference

# --- the operand cache is keyed by path and content --------------------------------
# One script, two workflows: the script settles -f "${COMPOSE_FILE}" from the
# workflow env, so a result cached by path alone would give the second workflow
# the first one's answer. Only the second workflow (a non-ledger compose file) is red.
ledger_tree queue-cache-two-workflows
d="${QUEUE_DIR}"
lput "${d}" fixture-l-compose-file-var.sh <<'EOF'
docker compose -f "${COMPOSE_FILE}" up -d
EOF
yq e -n '.name = "fixture-queue-b" | .on.merge_group.types = ["checks_requested"] | .on.pull_request = null | .jobs = {}' >"${d}/.github/workflows/fixture-queue-b.yml"
lj "${d}" c-first-workflow-ledger-file 'bash scripts/fixture-l-compose-file-var.sh' 0 '{"COMPOSE_FILE": "docker-compose.fixture-ghcr.yaml"}'
qjob_b() { yq e -i ".jobs[\"$2\"] = $3" "$1/.github/workflows/fixture-queue-b.yml"; }
qjob_b "${d}" c-second-workflow-other-file "$(jq -c -n --argjson env '{"COMPOSE_FILE": "docker-compose.fixture-ledger.yaml"}' \
  '{"runs-on": "ubuntu-latest", "env": $env, "steps": [{"uses": "actions/checkout@v5"}, {"run": "bash scripts/fixture-l-compose-file-var.sh"}]}')"
if out="$(run_verifier "${d}")"; then
  printf 'not ok - compose-operand-cache-keyed-by-content: the second workflow was not named\n'; failures=$((failures + 1))
elif [[ "${out}" == *"fixture-queue-b.yml:c-second-workflow-other-file: queue-path job"* && "${out}" != *"fixture-queue.yml:c-first-workflow-ledger-file"* ]]; then
  printf 'ok - compose-operand-cache-keyed-by-content\n'
else
  printf 'not ok - compose-operand-cache-keyed-by-content: unexpected output:\n%s\n' "${out}"; failures=$((failures + 1))
fi

# --- a conditional literal in a workflow env settles to its branches ---------------
ledger_tree queue-conditional-literal
d="${QUEUE_DIR}"
lput "${d}" fixture-l-compose-file-var.sh <<'EOF'
docker compose -f "${COMPOSE_FILE}" up -d
EOF
lj "${d}" l-both-branches-ledger 'bash scripts/fixture-l-compose-file-var.sh' 0 \
  '{"COMPOSE_FILE": "${{ github.event_name == '"'"'merge_group'"'"' && '"'"'docker-compose.fixture-ghcr.yaml'"'"' || '"'"'docker-compose.fixture-ghcr.yaml'"'"' }}"}'
expect_green queue-path-conditional-literal-compose-file-with-ledger-branches "${d}"
lj "${d}" l-one-branch-other-image 'bash scripts/fixture-l-compose-file-var.sh' 0 \
  '{"COMPOSE_FILE": "${{ github.event_name == '"'"'merge_group'"'"' && '"'"'docker-compose.fixture-ghcr.yaml'"'"' || '"'"'docker-compose.fixture-hub.yaml'"'"' }}"}'
expect_queue_jobs_red queue-path-conditional-literal-with-a-non-ledger-branch "${d}" l-one-branch-other-image

# --- the runner shape: a compose file cut out of a row vs selected by name ---------
# scripts/run-live-backend-tests.sh builds rows from two compose variables. Cutting
# the file out of the row with ${rest%%|*} leaves the -f operand to text no static
# reader settles (RED); selecting the file by the row's backend name onto the same
# two literal variables lets the gate settle it (GREEN). Both fixtures name only
# ghcr.io compose files, so only the way the operand is built differs.
ledger_tree queue-runner-compose-selection
d="${QUEUE_DIR}"
lput "${d}" fixture-l-runner-mapfile-cut.sh <<'EOF2'
nornicdb_compose="docker-compose.fixture-ghcr.yaml"
neo4j_compose="docker-compose.fixture-ghcr.yaml"
runs=("nornicdb|${nornicdb_compose}|a" "neo4j|${neo4j_compose}|b")
for ((i = 0; i < ${#runs[@]}; i++)); do
  rest="${runs[$i]#*|}"
  compose_file="${rest%%|*}"
  docker compose -f "${compose_file}" up -d
done
EOF2
lj "${d}" r-runner-compose-file-cut-from-a-row 'bash scripts/fixture-l-runner-mapfile-cut.sh' 0
expect_queue_jobs_red queue-path-compose-file-cut-from-a-mapfile-row "${d}" r-runner-compose-file-cut-from-a-row
ledger_tree queue-runner-case-select
d="${QUEUE_DIR}"
lput "${d}" fixture-l-runner-case-select.sh <<'EOF2'
nornicdb_compose="docker-compose.fixture-ghcr.yaml"
neo4j_compose="docker-compose.fixture-ghcr.yaml"
runs=("nornicdb|${nornicdb_compose}|a" "neo4j|${neo4j_compose}|b")
for ((i = 0; i < ${#runs[@]}; i++)); do
  name="${runs[$i]%%|*}"
  case "${name}" in
    nornicdb) compose_file="${nornicdb_compose}" ;;
    neo4j) compose_file="${neo4j_compose}" ;;
    *) exit 1 ;;
  esac
  docker compose -f "${compose_file}" up -d
done
EOF2
lj "${d}" g-runner-compose-file-selected-by-name 'bash scripts/fixture-l-runner-case-select.sh' 0
expect_green queue-path-compose-file-selected-by-backend-name "${d}"

# A longer chain settles every literal: the first arm is a non-ledger image.
ledger_tree queue-conditional-chain
d="${QUEUE_DIR}"
lput "${d}" fixture-l-compose-file-var.sh <<'EOF'
docker compose -f "${COMPOSE_FILE}" up -d
EOF
lj "${d}" l-chain-all-ledger 'bash scripts/fixture-l-compose-file-var.sh' 0 \
  '{"COMPOSE_FILE": "${{ github.event_name == '"'"'push'"'"' && '"'"'docker-compose.fixture-ghcr.yaml'"'"' || github.event_name == '"'"'merge_group'"'"' && '"'"'docker-compose.fixture-ghcr.yaml'"'"' || '"'"'docker-compose.fixture-ghcr.yaml'"'"' }}"}'
expect_green queue-path-conditional-chain-with-ledger-arms "${d}"
lj "${d}" l-chain-first-arm-other 'bash scripts/fixture-l-compose-file-var.sh' 0 \
  '{"COMPOSE_FILE": "${{ github.event_name == '"'"'push'"'"' && '"'"'docker-compose.fixture-hub.yaml'"'"' || github.event_name == '"'"'merge_group'"'"' && '"'"'docker-compose.fixture-ghcr.yaml'"'"' || '"'"'docker-compose.fixture-ghcr.yaml'"'"' }}"}'
lj "${d}" l-chain-bare-value-arm 'bash scripts/fixture-l-compose-file-var.sh' 0 \
  '{"COMPOSE_FILE": "${{ inputs.compose_file || '"'"'docker-compose.fixture-ghcr.yaml'"'"' }}"}'
expect_queue_jobs_red queue-path-conditional-chain-first-arm-and-bare-arm "${d}" l-chain-first-arm-other l-chain-bare-value-arm

# An unreadable ledger is said out loud and fails closed: CI image mode cannot be
# active, so a wrapper call is judged raw and its Hub compose file is a reach.
ledger_tree queue-ledger-unreadable
d="${QUEUE_DIR}"
rm -f "${d}/specs/ci-images.v1.json"
mkdir -p "${d}/scripts/ci"
cp "${repo_root}/scripts/ci/compose.sh" "${d}/scripts/ci/compose.sh"
lj "${d}" u-ledger-missing 'bash scripts/ci/compose.sh -f docker-compose.fixture-ledger.yaml -- up -d' 1
if out="$(run_verifier "${d}")"; then
  printf 'not ok - ledger-unreadable-fails-closed: the job passed without a ledger\n'; failures=$((failures + 1))
elif [[ "${out}" == *"the CI image ledger is unreadable"* && "${out}" == *"u-ledger-missing"* ]]; then
  printf 'ok - ledger-unreadable-fails-closed\n'
else
  printf 'not ok - ledger-unreadable-fails-closed: unexpected output:\n%s\n' "${out}"; failures=$((failures + 1))
fi
