#!/usr/bin/env bash
# Accept only the secret flag, optionally preceded by this job's queue selector.

login_condition_is_safe() { # <step if> <job id>
  local expression="$1" job="$2" compact="" quoted=0 char i
  local guard="env.DOCKERHUB_LOGIN_ENABLED=='true'" queue queue_grouped

  # Job IDs enter the quoted queue literal below. Reject punctuation that could
  # alter its expression grammar before constructing that literal.
  [[ "${job}" =~ ^[A-Za-z_][A-Za-z0-9_-]*$ ]] || return 1

  # Whitespace is insignificant in GitHub expressions except inside strings.
  # Keep each quoted literal byte-for-byte; a malformed quote is rejected.
  for ((i = 0; i < ${#expression}; i++)); do
    char="${expression:i:1}"
    if [[ "${char}" == "'" ]]; then
      quoted=$((1 - quoted))
    elif [[ "${quoted}" -eq 0 && "${char}" =~ [[:space:]] ]]; then
      continue
    fi
    compact+="${char}"
  done
  [[ "${quoted}" -eq 0 ]] || return 1

  if [[ "${compact}" == \$\{\{*\}\} ]]; then
    compact="${compact:3:$((${#compact} - 5))}"
  fi

  [[ "${compact}" == "${guard}" ]] && return 0
  # Both forms below are already used by the workflow set: some selectors
  # parenthesize contains(), while the verify-contracts selector does not.
  queue="github.event_name!='merge_group'||contains(fromJSON(needs.queue-selection.outputs.jobs||'[]'),'${job}')"
  queue_grouped="github.event_name!='merge_group'||(contains(fromJSON(needs.queue-selection.outputs.jobs||'[]'),'${job}'))"
  [[ "${compact}" == "(${queue})&&${guard}" || "${compact}" == "(${queue_grouped})&&${guard}" ]]
}
