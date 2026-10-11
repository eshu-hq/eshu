#!/usr/bin/env bash
#
# dockerhub-login-test-ledger.sh - seeded RED/GREEN cases for the CI image ledger
# model of scripts/verify-dockerhub-login.sh (#7886). Sourced, never run, from
# scripts/test-verify-dockerhub-login.sh after the queue batch. It uses queue_tree,
# qjob and expect_queue_jobs_red from lib/dockerhub-login-test-queue.sh and the
# test's helpers.
#
# specs/ci-images.v1.json is the only approved-image list, and the gate reads it
# from the tree under test at run time. These cases read the same file for every
# pin they need, so no digest is copied here: a job in CI image mode (ESHU_CI_IMAGES=1
# and the required env pins equal to the ledger) may run a Compose stack whose
# service images are ledger runtime sources, and the root Dockerfile gets the
# ledger build args; outside that mode the images the files name apply, so the
# same stack reaches Docker Hub. Each RED job breaks one bound.
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

# --- GREEN: CI image mode settles what the overlay rewrites -------------------------
ledger_tree queue-green-ci-image-mode
d="${QUEUE_DIR}"
lj "${d}" g-ledger-runtime-source 'docker compose -f docker-compose.fixture-ledger.yaml up -d' 1
lj "${d}" g-root-build-gets-ledger-build-args 'docker compose -f docker-compose.fixture-root-build.yaml up -d --build' 1
lput "${d}" fixture-l-prepare-then-overlay.sh <<'EOF'
source "$(dirname "$0")/lib/ci-image-compose.sh"
eshu_ci_compose_prepare docker-compose.fixture-ledger.yaml
docker compose -f docker-compose.fixture-ledger.yaml -f "${ESHU_CI_COMPOSE_OVERLAY}" up -d
EOF
lj "${d}" g-overlay-operand-after-prepare 'bash scripts/fixture-l-prepare-then-overlay.sh' 1
lj "${d}" g-build-arg-names-a-ledger-pin "docker build --build-arg BASE=${ledger_pin_alpine} -f Dockerfile.fixture-arg ."  0
lj "${d}" g-arg-default-is-ledger-pin 'docker build -f Dockerfile.fixture-arg-ghcr .' 0
lj "${d}" g-build-arg-from-env-reference 'docker build --build-arg BASE=${BASE} -f Dockerfile.fixture-arg .' 0 "{\"BASE\": \"${ledger_pin_alpine}\"}"
expect_green queue-path-ci-image-mode-and-build-args-settle-the-stack "${d}"

# --- RED: each bound of the model --------------------------------------------------
ledger_tree queue-red-ci-image-mode-bounds
d="${QUEUE_DIR}"
lj "${d}" r-ledger-source-without-ci-mode 'docker compose -f docker-compose.fixture-ledger.yaml up -d' 0
lj "${d}" r-ci-mode-non-ledger-image 'docker compose -f docker-compose.fixture-other.yaml up -d' 1
lj "${d}" r-ci-mode-non-root-build 'docker compose -f docker-compose.fixture-other-build.yaml up -d --build' 1
lj "${d}" r-root-build-without-ci-mode 'docker compose -f docker-compose.fixture-root-build.yaml up -d --build' 0
lj "${d}" r-ci-mode-one-pin-missing 'docker compose -f docker-compose.fixture-ledger.yaml up -d' 1 '{"ESHU_GO_IMAGE": "golang:1.26"}'
lj "${d}" r-ci-mode-flag-without-pins 'docker compose -f docker-compose.fixture-ledger.yaml up -d' 0 '{"ESHU_CI_IMAGES": "1"}'
lput "${d}" fixture-l-overlay-without-prepare.sh <<'EOF'
docker compose -f docker-compose.fixture-ledger.yaml -f "${ESHU_CI_COMPOSE_OVERLAY}" up -d
EOF
lj "${d}" r-overlay-operand-without-prepare 'bash scripts/fixture-l-overlay-without-prepare.sh' 1
lput "${d}" fixture-l-overlay-before-prepare.sh <<'EOF'
source "$(dirname "$0")/lib/ci-image-compose.sh"
docker compose -f docker-compose.fixture-ledger.yaml -f "${ESHU_CI_COMPOSE_OVERLAY}" up -d
eshu_ci_compose_prepare docker-compose.fixture-ledger.yaml
EOF
lj "${d}" r-overlay-operand-before-prepare 'bash scripts/fixture-l-overlay-before-prepare.sh' 1
lj "${d}" r-arg-default-hub-without-build-arg 'docker build -f Dockerfile.fixture-arg .' 0
lj "${d}" r-env-does-not-reach-an-arg 'docker build -f Dockerfile.fixture-arg .' 0 "{\"BASE\": \"${ledger_pin_alpine}\"}"
lj "${d}" r-build-arg-overrides-to-hub 'docker build --build-arg BASE=alpine:3.21 -f Dockerfile.fixture-arg-ghcr .' 0
lj "${d}" r-build-arg-from-unset-env-reference 'docker build --build-arg BASE=${BASE} -f Dockerfile.fixture-arg-ghcr .' 0
expect_queue_jobs_red queue-path-ci-image-mode-bounds-fail "${d}" r-ledger-source-without-ci-mode r-ci-mode-non-ledger-image \
  r-ci-mode-non-root-build r-root-build-without-ci-mode r-ci-mode-one-pin-missing r-ci-mode-flag-without-pins \
  r-overlay-operand-without-prepare r-overlay-operand-before-prepare r-arg-default-hub-without-build-arg \
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
lj "${d}" c-first-workflow-ledger-file 'bash scripts/fixture-l-compose-file-var.sh' 1 '{"COMPOSE_FILE": "docker-compose.fixture-ledger.yaml"}'
qjob_b() { yq e -i ".jobs[\"$2\"] = $3" "$1/.github/workflows/fixture-queue-b.yml"; }
qjob_b "${d}" c-second-workflow-other-file "$(jq -c -n --argjson env "$(jq -c -n --argjson a "${ledger_ci_env}" '$a + {"COMPOSE_FILE": "docker-compose.fixture-other.yaml"}')" \
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
lj "${d}" l-both-branches-ledger 'docker compose -f "${COMPOSE_FILE}" up -d' 1 \
  '{"COMPOSE_FILE": "${{ github.event_name == '"'"'merge_group'"'"' && '"'"'docker-compose.fixture-ledger.yaml'"'"' || '"'"'docker-compose.fixture-root-build.yaml'"'"' }}"}'
expect_green queue-path-conditional-literal-compose-file-with-ledger-branches "${d}"
lj "${d}" l-one-branch-other-image 'docker compose -f "${COMPOSE_FILE}" up -d' 1 \
  '{"COMPOSE_FILE": "${{ github.event_name == '"'"'merge_group'"'"' && '"'"'docker-compose.fixture-ledger.yaml'"'"' || '"'"'docker-compose.fixture-other.yaml'"'"' }}"}'
expect_queue_jobs_red queue-path-conditional-literal-with-a-non-ledger-branch "${d}" l-one-branch-other-image
