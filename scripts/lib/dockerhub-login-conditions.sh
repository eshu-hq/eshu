#!/usr/bin/env bash
# Accept only the secret flag, optionally preceded by this job's queue selector.

login_condition_tokenize() { # <expression>; writes condition_tokens
  local input="$1" char pair start index=0 size="${#1}"
  condition_tokens=()
  while ((index < size)); do
    char="${input:index:1}"
    if [[ "${char}" =~ [[:space:]] ]]; then
      index=$((index + 1))
      continue
    fi
    if [[ "${input:index:3}" == '${{' ]]; then
      condition_tokens+=('${{')
      index=$((index + 3))
      continue
    fi
    pair="${input:index:2}"
    case "${pair}" in
      '}}'|'=='|'!='|'&&'|'||')
        condition_tokens+=("${pair}")
        index=$((index + 2))
        continue ;;
    esac
    if [[ "${char}" == "'" ]]; then
      start="${index}"
      index=$((index + 1))
      while ((index < size)) && [[ "${input:index:1}" != "'" ]]; do
        index=$((index + 1))
      done
      ((index < size)) || return 1
      condition_tokens+=("${input:start:index-start+1}")
      index=$((index + 1))
      continue
    fi
    if [[ "${char}" =~ ^[A-Za-z_]$ ]]; then
      start="${index}"
      index=$((index + 1))
      while ((index < size)) && [[ "${input:index:1}" =~ ^[A-Za-z0-9_-]$ ]]; do
        index=$((index + 1))
      done
      condition_tokens+=("${input:start:index-start}")
      continue
    fi
    case "${char}" in
      '.'|'('|')'|',') condition_tokens+=("${char}"); index=$((index + 1)) ;;
      *) return 1 ;;
    esac
  done
}

login_condition_matches() { # <actual expression> <accepted literal expression>
  local actual_expression="$1" accepted_expression="$2" index
  local -a actual=()
  login_condition_tokenize "${actual_expression}" || return 1
  actual=("${condition_tokens[@]}")
  if ((${#actual[@]} >= 2)) && [[ "${actual[0]}" == '${{' && "${actual[-1]}" == '}}' ]]; then
    actual=("${actual[@]:1:${#actual[@]}-2}")
  fi
  login_condition_tokenize "${accepted_expression}" || return 1
  ((${#actual[@]} == ${#condition_tokens[@]})) || return 1
  for ((index = 0; index < ${#actual[@]}; index++)); do
    [[ "${actual[index]}" == "${condition_tokens[index]}" ]] || return 1
  done
}

login_condition_is_safe() { # <step if> <job id>
  local expression="$1" job="$2"
  local guard="env.DOCKERHUB_LOGIN_ENABLED == 'true'" queue queue_grouped

  # Job IDs enter a quoted queue literal. Reject punctuation that could alter
  # the expression grammar before constructing the accepted literal.
  [[ "${job}" =~ ^[A-Za-z_][A-Za-z0-9_-]*$ ]] || return 1
  queue="(github.event_name != 'merge_group' || contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), '${job}')) && ${guard}"
  queue_grouped="(github.event_name != 'merge_group' || (contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), '${job}'))) && ${guard}"
  login_condition_matches "${expression}" "${guard}" ||
    login_condition_matches "${expression}" "${queue}" ||
    login_condition_matches "${expression}" "${queue_grouped}"
}
