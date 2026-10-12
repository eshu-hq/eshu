#!/usr/bin/env bash
# Login conditions must imply the secret-derived flag on every selected path.

login_condition_case() { # <name> <if expression> <green|red>
  local dir
  dir="$(new_case "$1")"
  add_job "${dir}" verify-agent-hygiene fixture-docker 'docker pull postgres:18'
  LOGIN_IF="$2" yq e -i '.jobs.fixture-docker.steps = [{"uses": "docker/login-action@v3", "if": strenv(LOGIN_IF), "with": {"username": "${{ secrets.DOCKERHUB_USERNAME }}", "password": "${{ secrets.DOCKERHUB_TOKEN }}"}}] + .jobs.fixture-docker.steps' "${dir}/.github/workflows/verify-agent-hygiene.yml"
  ENABLED_EXPR="${enabled_json}" yq e -i '.jobs.fixture-docker.env.DOCKERHUB_LOGIN_ENABLED = strenv(ENABLED_EXPR)' "${dir}/.github/workflows/verify-agent-hygiene.yml"
  if [[ "$3" == green ]]; then
    expect_green "$1" "${dir}"
  else
    expect_red "$1" "${dir}" 'must run only when DOCKERHUB_LOGIN_ENABLED is true'
  fi
}

queue_condition="(github.event_name != 'merge_group' || contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), 'fixture-docker')) && env.DOCKERHUB_LOGIN_ENABLED == 'true'"
login_condition_case direct-guard "env.DOCKERHUB_LOGIN_ENABLED == 'true'" green
login_condition_case wrapped-guard "\${{ env.DOCKERHUB_LOGIN_ENABLED == 'true' }}" green
login_condition_case queue-and-guard "${queue_condition}" green
login_condition_case queue-and-grouped-contains "\${{ (github.event_name != 'merge_group' || (contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), 'fixture-docker'))) && env.DOCKERHUB_LOGIN_ENABLED == 'true' }}" green
login_condition_case wrapped-queue-and-guard "\${{ ${queue_condition} }}" green
login_condition_case queue-and-guard-extra-whitespace "\${{ ( github.event_name != 'merge_group' || contains( fromJSON( needs.queue-selection.outputs.jobs || '[]' ), 'fixture-docker' ) ) && env.DOCKERHUB_LOGIN_ENABLED == 'true' }}" green

login_condition_case or-bypasses-guard "(github.event_name != 'merge_group' || env.DOCKERHUB_LOGIN_ENABLED == 'true')" red
login_condition_case split-identifier "en v.DOCKERHUB_LOGIN_ENABLED == 'true'" red
login_condition_case split-operator "${queue_condition/&&/\& \&}" red
login_condition_case negated-guard "!env.DOCKERHUB_LOGIN_ENABLED == 'true'" red
login_condition_case quoted-guard-spoof "contains('env.DOCKERHUB_LOGIN_ENABLED == '\''true'\''', 'true')" red
login_condition_case malformed-wrapper "\${{ env.DOCKERHUB_LOGIN_ENABLED == 'true' }" red
login_condition_case mismatched-parentheses "(env.DOCKERHUB_LOGIN_ENABLED == 'true'" red
login_condition_case wrong-env "env.DOCKERHUB_LOGIN_ENABLE == 'true'" red
login_condition_case wrong-value "env.DOCKERHUB_LOGIN_ENABLED == 'false'" red
login_condition_case quoted-whitespace-value "env.DOCKERHUB_LOGIN_ENABLED == 'tru e'" red
newline_value="env.DOCKERHUB_LOGIN_ENABLED == 'tr"$'\n'"ue'"
login_condition_case quoted-newline-value "${newline_value}" red
# YAML's escaped NUL reaches yq's base64 output. Bash command substitution
# must not erase that byte and turn a quoted lookalike into the true flag.
dir="$(new_case quoted-nul-value)"
cat >"${dir}/.github/workflows/verify-agent-hygiene.yml" <<'YAML'
jobs:
  fixture-docker:
    runs-on: ubuntu-latest
    env:
      DOCKERHUB_LOGIN_ENABLED: ${{ secrets.DOCKERHUB_USERNAME != '' && secrets.DOCKERHUB_TOKEN != '' }}
    steps:
      - uses: docker/login-action@v3
        if: "env.DOCKERHUB_LOGIN_ENABLED == 'tr\0ue'"
        with:
          username: ${{ secrets.DOCKERHUB_USERNAME }}
          password: ${{ secrets.DOCKERHUB_TOKEN }}
      - run: docker pull postgres:18
YAML
expect_red quoted-nul-value "${dir}" 'must run only when DOCKERHUB_LOGIN_ENABLED is true'
login_condition_case quoted-whitespace-job "\${{ (github.event_name != 'merge_group' || contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), 'fixture -docker')) && env.DOCKERHUB_LOGIN_ENABLED == 'true' }}" red
login_condition_case wrong-job "\${{ (github.event_name != 'merge_group' || contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), 'other-job')) && env.DOCKERHUB_LOGIN_ENABLED == 'true' }}" red
login_condition_case suffix-bypass "env.DOCKERHUB_LOGIN_ENABLED == 'true' || always()" red

# A malformed job id must never become part of an accepted expression literal.
# shellcheck source=lib/dockerhub-login-conditions.sh
source "${repo_root}/scripts/lib/dockerhub-login-conditions.sh"
if login_condition_is_safe "env.DOCKERHUB_LOGIN_ENABLED == 'true'" "fixture'docker"; then
  echo 'not ok - malformed-current-job-id'; failures=$((failures + 1))
else
  echo 'ok - malformed-current-job-id'
fi

# GitHub env precedence is step, then job, then workflow. Presence matters:
# an explicitly empty value shadows the less specific scope.
login_flag_case() { # <name> <step|job> <value|missing> <green|red> [job value] [workflow value]
  local dir wf
  dir="$(new_case "$1")"
  wf="${dir}/.github/workflows/verify-agent-hygiene.yml"
  add_job "${dir}" verify-agent-hygiene fixture-docker 'docker pull postgres:18'
  ENABLED_EXPR="${enabled_json}" yq e -i '.env.DOCKERHUB_LOGIN_ENABLED = strenv(ENABLED_EXPR) | .jobs.fixture-docker.env.DOCKERHUB_LOGIN_ENABLED = strenv(ENABLED_EXPR) | .jobs.fixture-docker.steps = [{"uses": "docker/login-action@v3", "if": "env.DOCKERHUB_LOGIN_ENABLED == '\''true'\''", "with": {"username": "${{ secrets.DOCKERHUB_USERNAME }}", "password": "${{ secrets.DOCKERHUB_TOKEN }}"}}] + .jobs.fixture-docker.steps' "${wf}"
  if [[ ${5+x} ]]; then
    LOGIN_FLAG_VALUE="$5" yq e -i '.jobs.fixture-docker.env.DOCKERHUB_LOGIN_ENABLED = strenv(LOGIN_FLAG_VALUE)' "${wf}"
  fi
  if [[ ${6+x} ]]; then
    LOGIN_FLAG_VALUE="$6" yq e -i '.env.DOCKERHUB_LOGIN_ENABLED = strenv(LOGIN_FLAG_VALUE)' "${wf}"
  fi
  if [[ "$3" == missing ]]; then
    case "$2" in
      step) yq e -i 'del(.jobs.fixture-docker.steps[0].env.DOCKERHUB_LOGIN_ENABLED)' "${wf}" ;;
      job) yq e -i 'del(.jobs.fixture-docker.env.DOCKERHUB_LOGIN_ENABLED)' "${wf}" ;;
    esac
  else
    case "$2" in
      step) LOGIN_FLAG_VALUE="$3" yq e -i '.jobs.fixture-docker.steps[0].env.DOCKERHUB_LOGIN_ENABLED = strenv(LOGIN_FLAG_VALUE)' "${wf}" ;;
      job) LOGIN_FLAG_VALUE="$3" yq e -i '.jobs.fixture-docker.env.DOCKERHUB_LOGIN_ENABLED = strenv(LOGIN_FLAG_VALUE)' "${wf}" ;;
    esac
  fi
  if [[ "$4" == green ]]; then
    expect_green "$1" "${dir}"
  else
    expect_red "$1" "${dir}" 'must define DOCKERHUB_LOGIN_ENABLED from both secrets'
  fi
}

login_flag_case step-overrides-valid-job step false red
login_flag_case empty-step-overrides-valid-job step '' red
login_flag_case valid-step-overrides-job step "${enabled_json}" green false
login_flag_case missing-step-uses-job step missing green
login_flag_case empty-job-overrides-valid-workflow job '' red
login_flag_case missing-job-uses-workflow job missing green
login_flag_case valid-job-overrides-workflow job "${enabled_json}" green "${enabled_json}" false
login_flag_case false-workflow-without-job job missing red "${enabled_json}" false
