#!/usr/bin/env bash

# shellcheck disable=SC2034 # Globals are consumed by the sourcing classifier.

# Assignment-state helpers for the documented parser command interpreter.
# The caller owns tokenization and the shell-state globals these functions
# update.

documented_shell_command_has_stateful_preamble() {
  local command="$1" segment token i start relevant=false
  documented_shell_first_operator "$command" || return 1
  segment="${command:0:PARSER_DOCUMENTED_SHELL_OPERATOR_INDEX}"
  documented_parser_command_tokens "$segment"
  [ "$PARSER_COMMAND_TOKENS_VALID" = true ] || return 1
  ((${#PARSER_COMMAND_TOKENS[@]} > 0)) || return 1
  token="${PARSER_COMMAND_TOKENS[0]}"
  if [ "$token" = export ]; then
    start=1
    if [ "${PARSER_COMMAND_TOKENS[start]:-}" = -- ]; then
      ((start++))
    fi
  elif [[ "$token" =~ ^[A-Za-z_][A-Za-z0-9_]*= ]]; then
    start=0
  else
    return 1
  fi
  for ((i = start; i < ${#PARSER_COMMAND_TOKENS[@]}; i++)); do
    token="${PARSER_COMMAND_TOKENS[i]}"
    case "$token" in
      [A-Za-z_][A-Za-z0-9_]*|[A-Za-z_][A-Za-z0-9_]*=*) ;;
      *) return 1 ;;
    esac
    if ((start == 0)) && [[ "$token" != *=* ]]; then
      return 1
    fi
    case "$token" in
      GOFLAGS|GOFLAGS=*|GOFLAGS+=*) relevant=true ;;
    esac
  done
  [ "$relevant" = true ]
}

documented_record_export_preamble() {
  local i token
  for ((i = 1; i < ${#PARSER_COMMAND_TOKENS[@]}; i++)); do
    token="${PARSER_COMMAND_TOKENS[i]}"
    if ((i == 1)) && [ "$token" = -- ]; then
      continue
    fi
    case "$token" in
      [A-Za-z_][A-Za-z0-9_]*=*)
        documented_record_shell_assignment \
          "$token" "${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[i]}"
        case "$token" in
          GOFLAGS=*|GOFLAGS+=*)
            PARSER_DOCUMENTED_GOFLAGS_EXPORTED=true
            PARSER_DOCUMENTED_GOFLAGS_EFFECTIVE=true
            ;;
        esac
        if [ "${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[i]}" = true ]; then
          PARSER_DOCUMENTED_SHELL_ASSIGNMENT_DYNAMIC=true
        fi
        ;;
      [A-Za-z_][A-Za-z0-9_]*)
        if [ "$token" = GOFLAGS ]; then
          PARSER_DOCUMENTED_GOFLAGS_EXPORTED=true
          PARSER_DOCUMENTED_GOFLAGS_EFFECTIVE="$PARSER_DOCUMENTED_GOFLAGS_PRESENT"
        fi
        ;;
      *) return 1 ;;
    esac
  done
}

documented_record_assignment_preamble() {
  local i token
  for ((i = 0; i < ${#PARSER_COMMAND_TOKENS[@]}; i++)); do
    token="${PARSER_COMMAND_TOKENS[i]}"
    case "$token" in
      [A-Za-z_][A-Za-z0-9_]*=*)
        documented_record_shell_assignment \
          "$token" "${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[i]}"
        if [ "${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[i]}" = true ]; then
          PARSER_DOCUMENTED_SHELL_ASSIGNMENT_DYNAMIC=true
        fi
        ;;
      *) return 1 ;;
    esac
  done
  PARSER_DOCUMENTED_GOFLAGS_EFFECTIVE="$PARSER_DOCUMENTED_GOFLAGS_EXPORTED"
}

documented_shell_wrapper_from_command_prefix() {
  local i prefix_end shell_index token token_count="${#PARSER_COMMAND_TOKENS[@]}"
  local -a original_tokens=("${PARSER_COMMAND_TOKENS[@]}")
  local -a original_expansions=("${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[@]}")
  PARSER_DOCUMENTED_SHELL_WRAPPER_COMMAND=''
  PARSER_DOCUMENTED_SHELL_WRAPPER_DYNAMIC=false
  shell_index="$({
    for ((i = 0; i < token_count; i++)); do
      [ "${original_expansions[i]}" = false ] || continue
      case "${original_tokens[i]##*/}" in
        sh|bash|zsh|dash|ksh) ;;
        *) continue ;;
      esac
      PARSER_COMMAND_TOKENS=("${original_tokens[@]:0:i}" go test)
      PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS=(
        "${original_expansions[@]:0:i}" false false
      )
      documented_go_test_prefix || continue
      printf '%s' "$i"
      exit 0
    done
    exit 1
  })" || return 1
  PARSER_COMMAND_TOKENS=("${original_tokens[@]:0:shell_index}" go test)
  PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS=(
    "${original_expansions[@]:0:shell_index}" false false
  )
  if ! documented_go_test_prefix; then
    PARSER_COMMAND_TOKENS=("${original_tokens[@]}")
    PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS=("${original_expansions[@]}")
    return 1
  fi
  PARSER_COMMAND_TOKENS=("${original_tokens[@]}")
  PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS=("${original_expansions[@]}")
  prefix_end="$shell_index"
  for ((i = prefix_end + 1; i + 1 < token_count; i++)); do
    token="${PARSER_COMMAND_TOKENS[i]}"
    [[ "$token" =~ ^-[[:alpha:]]*c[[:alpha:]]*$ ]] || continue
    PARSER_DOCUMENTED_SHELL_WRAPPER_COMMAND="${PARSER_COMMAND_TOKENS[i+1]}"
    PARSER_DOCUMENTED_SHELL_WRAPPER_DYNAMIC="${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[i+1]}"
    [ "$PARSER_DOCUMENTED_GOFLAGS_EFFECTIVE" = true ] &&
      PARSER_DOCUMENTED_GOFLAGS_EXPORTED=true
    return 0
  done
  return 1
}

PARSER_DOCUMENTED_SHELL_OPERATOR=''
PARSER_DOCUMENTED_SHELL_OPERATOR_INDEX=-1
PARSER_DOCUMENTED_SHELL_OPERATOR_LENGTH=0

documented_shell_first_operator() {
  local input="$1" char next previous quote='' at_word_start=true
  local i length="${#input}" brace_depth=0 paren_depth=0
  PARSER_DOCUMENTED_SHELL_OPERATOR=''
  PARSER_DOCUMENTED_SHELL_OPERATOR_INDEX=-1
  PARSER_DOCUMENTED_SHELL_OPERATOR_LENGTH=0
  for ((i = 0; i < length; i++)); do
    char="${input:i:1}"
    next=''
    previous=''
    ((i + 1 < length)) && next="${input:i+1:1}"
    ((i > 0)) && previous="${input:i-1:1}"
    if [ -n "$quote" ]; then
      if [ "$char" = "$quote" ]; then
        quote=''
      elif [ "$quote" = '"' ] && [ "$char" = '\' ]; then
        ((i++))
      fi
      continue
    fi
    case "$char" in
      "'"|'"')
        quote="$char"
        at_word_start=false
        ;;
      '\')
        ((i++))
        at_word_start=false
        ;;
      '{')
        if [ "$at_word_start" = true ] &&
          { [ -z "$next" ] || [[ "$next" =~ [[:space:]\{] ]]; }; then
          ((brace_depth++))
        fi
        at_word_start=true
        ;;
      '}')
        if ((brace_depth > 0)); then
          ((brace_depth--))
        fi
        at_word_start=false
        ;;
      '(')
        if [ "$previous" != '$' ]; then
          ((paren_depth++))
        fi
        at_word_start=true
        ;;
      ')')
        if ((paren_depth > 0)); then
          ((paren_depth--))
        fi
        at_word_start=false
        ;;
      '#')
        if [ "$at_word_start" = true ]; then
          while ((i + 1 < length)); do
            next="${input:i+1:1}"
            case "$next" in
              $'\r'|$'\n') break ;;
            esac
            ((i++))
          done
          continue
        fi
        at_word_start=false
        ;;
      ';')
        if ((brace_depth > 0 || paren_depth > 0)); then
          at_word_start=true
          continue
        fi
        PARSER_DOCUMENTED_SHELL_OPERATOR=';'
        PARSER_DOCUMENTED_SHELL_OPERATOR_INDEX="$i"
        PARSER_DOCUMENTED_SHELL_OPERATOR_LENGTH=1
        return 0
        ;;
      '&')
        if [ "$previous" = '>' ] || [ "$previous" = '<' ] ||
          [ "$next" = '>' ]; then
          at_word_start=false
          continue
        fi
        if ((brace_depth > 0 || paren_depth > 0)); then
          at_word_start=true
          [ "$next" = '&' ] && ((i++))
          continue
        fi
        PARSER_DOCUMENTED_SHELL_OPERATOR='&'
        PARSER_DOCUMENTED_SHELL_OPERATOR_LENGTH=1
        if [ "$next" = '&' ]; then
          PARSER_DOCUMENTED_SHELL_OPERATOR='&&'
          PARSER_DOCUMENTED_SHELL_OPERATOR_LENGTH=2
        fi
        PARSER_DOCUMENTED_SHELL_OPERATOR_INDEX="$i"
        return 0
        ;;
      '|')
        if [ "$previous" = '>' ]; then
          at_word_start=false
          continue
        fi
        if ((brace_depth > 0 || paren_depth > 0)); then
          at_word_start=true
          [ "$next" = '|' ] && ((i++))
          continue
        fi
        PARSER_DOCUMENTED_SHELL_OPERATOR='|'
        PARSER_DOCUMENTED_SHELL_OPERATOR_LENGTH=1
        if [ "$next" = '|' ]; then
          PARSER_DOCUMENTED_SHELL_OPERATOR='||'
          PARSER_DOCUMENTED_SHELL_OPERATOR_LENGTH=2
        fi
        PARSER_DOCUMENTED_SHELL_OPERATOR_INDEX="$i"
        return 0
        ;;
      $'\r')
        if ((brace_depth > 0 || paren_depth > 0)); then
          at_word_start=true
          continue
        fi
        PARSER_DOCUMENTED_SHELL_OPERATOR=$'\n'
        PARSER_DOCUMENTED_SHELL_OPERATOR_INDEX="$i"
        PARSER_DOCUMENTED_SHELL_OPERATOR_LENGTH=1
        [ "$next" = $'\n' ] && PARSER_DOCUMENTED_SHELL_OPERATOR_LENGTH=2
        return 0
        ;;
      $'\n')
        if ((brace_depth > 0 || paren_depth > 0)); then
          at_word_start=true
          continue
        fi
        PARSER_DOCUMENTED_SHELL_OPERATOR=$'\n'
        PARSER_DOCUMENTED_SHELL_OPERATOR_INDEX="$i"
        PARSER_DOCUMENTED_SHELL_OPERATOR_LENGTH=1
        return 0
        ;;
      ' '|$'\t') at_word_start=true ;;
      *) at_word_start=false ;;
    esac
  done
  return 1
}

documented_strip_shell_heredoc_bodies() {
  local input="$1" line rest body_line comparable delimiter
  local heredoc_re="<<(-?)[[:space:]]*['\\\"]?([A-Za-z_][A-Za-z0-9_]*)['\\\"]?"
  local strip_tabs=false output=''
  while [[ "$input" == *$'\n'* ]]; do
    line="${input%%$'\n'*}"
    rest="${input#*$'\n'}"
    output+="$line"$'\n'
    if [[ "$line" =~ $heredoc_re ]]; then
      [ "${BASH_REMATCH[1]}" = - ] && strip_tabs=true
      delimiter="${BASH_REMATCH[2]}"
      while :; do
        if [[ "$rest" == *$'\n'* ]]; then
          body_line="${rest%%$'\n'*}"
          rest="${rest#*$'\n'}"
        else
          body_line="$rest"
          rest=''
        fi
        comparable="$body_line"
        if [ "$strip_tabs" = true ]; then
          comparable="${comparable#"${comparable%%[!$'\t']*}"}"
        fi
        if [ "$comparable" = "$delimiter" ]; then
          output+="$body_line"$'\n'
          break
        fi
        [ -n "$rest" ] || return 2
      done
      strip_tabs=false
    fi
    input="$rest"
  done
  output+="$input"
  PARSER_DOCUMENTED_COMMAND_WITHOUT_HEREDOC_BODIES="$output"
}

documented_reset_parser_command_state() {
  documented_clear_goflags_assignment
  PARSER_DOCUMENTED_PRESERVE_GOFLAGS=false
  PARSER_DOCUMENTED_PRESERVE_CWD=false
  PARSER_DOCUMENTED_INHERITED_CWD=''
  PARSER_DOCUMENTED_INHERITED_CWD_DYNAMIC=false
  PARSER_DOCUMENTED_SHELL_ASSIGNMENT_DYNAMIC=false
}

documented_parser_command_list_is_stale() {
  local command="$1" scan_command tail_index segment first_token='' group=''
  local first_operator=''
  local go_test_prefilter="(^|[[:space:]/\"'])go([\"']?)[[:space:]].*test"
  local saved_goflags_present saved_goflags saved_goflags_dynamic
  local saved_goflags_exported saved_goflags_effective saved_assignment_dynamic
  local saved_cwd saved_cwd_dynamic
  documented_strip_shell_heredoc_bodies "$command" || return 0
  command="$PARSER_DOCUMENTED_COMMAND_WITHOUT_HEREDOC_BODIES"
  scan_command="${command//$'\\\r\n'/}"
  scan_command="${scan_command//$'\\\n'/}"
  if [[ "$scan_command" != *GOFLAGS* ]] &&
    ! [[ "$scan_command" =~ $go_test_prefilter ]]; then
    return 1
  fi
  command="${command#"${command%%[![:space:]]*}"}"
  command="${command%"${command##*[![:space:]]}"}"
  if [[ "$command" == \(*\) ]]; then
    command="${command:1:${#command}-2}"
  elif [[ "$command" == \{*\} ]]; then
    command="${command:1:${#command}-2}"
    command="${command%"${command##*[![:space:]]}"}"
    command="${command%;}"
  fi
  if documented_shell_first_operator "$command"; then
    first_operator="$PARSER_DOCUMENTED_SHELL_OPERATOR"
    segment="${command:0:PARSER_DOCUMENTED_SHELL_OPERATOR_INDEX}"
    documented_parser_command_tokens "$segment"
    if [ "$PARSER_COMMAND_TOKENS_VALID" = true ]; then
      first_token="${PARSER_COMMAND_TOKENS[0]:-}"
    fi
  fi
  if documented_parser_command_is_stale "$command"; then
    return 0
  fi
  if { [ "$first_token" = true ] && [ "$first_operator" = '||' ]; } ||
    { [ "$first_token" = false ] && [ "$first_operator" = '&&' ]; }; then
    return 1
  fi
  documented_shell_first_operator "$command" || return 1
  segment="${command:0:PARSER_DOCUMENTED_SHELL_OPERATOR_INDEX}"
  segment="${segment#"${segment%%[![:space:]]*}"}"
  segment="${segment%"${segment##*[![:space:]]}"}"
  if [[ "$segment" == \{*\} ]] || [[ "$segment" == \(*\) ]]; then
    group="${segment:1:${#segment}-2}"
    if [[ "$segment" == \(*\) ]]; then
      saved_goflags_present="$PARSER_DOCUMENTED_GOFLAGS_PRESENT"
      saved_goflags="$PARSER_DOCUMENTED_GOFLAGS"
      saved_goflags_dynamic="$PARSER_DOCUMENTED_GOFLAGS_DYNAMIC"
      saved_goflags_exported="$PARSER_DOCUMENTED_GOFLAGS_EXPORTED"
      saved_goflags_effective="$PARSER_DOCUMENTED_GOFLAGS_EFFECTIVE"
      saved_assignment_dynamic="$PARSER_DOCUMENTED_SHELL_ASSIGNMENT_DYNAMIC"
      saved_cwd="$PARSER_DOCUMENTED_INHERITED_CWD"
      saved_cwd_dynamic="$PARSER_DOCUMENTED_INHERITED_CWD_DYNAMIC"
    fi
    PARSER_DOCUMENTED_PRESERVE_GOFLAGS=true
    PARSER_DOCUMENTED_PRESERVE_CWD=true
    documented_parser_command_list_is_stale "$group" && return 0
    if [[ "$segment" == \(*\) ]]; then
      PARSER_DOCUMENTED_GOFLAGS_PRESENT="$saved_goflags_present"
      PARSER_DOCUMENTED_GOFLAGS="$saved_goflags"
      PARSER_DOCUMENTED_GOFLAGS_DYNAMIC="$saved_goflags_dynamic"
      PARSER_DOCUMENTED_GOFLAGS_EXPORTED="$saved_goflags_exported"
      PARSER_DOCUMENTED_GOFLAGS_EFFECTIVE="$saved_goflags_effective"
      PARSER_DOCUMENTED_SHELL_ASSIGNMENT_DYNAMIC="$saved_assignment_dynamic"
      PARSER_DOCUMENTED_INHERITED_CWD="$saved_cwd"
      PARSER_DOCUMENTED_INHERITED_CWD_DYNAMIC="$saved_cwd_dynamic"
    fi
  fi
  tail_index=$((PARSER_DOCUMENTED_SHELL_OPERATOR_INDEX +
    PARSER_DOCUMENTED_SHELL_OPERATOR_LENGTH))
  command="${command:tail_index}"
  command="${command#"${command%%[![:space:]]*}"}"
  [ -n "$command" ] || return 1
  if [ -n "$PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY" ]; then
    PARSER_DOCUMENTED_INHERITED_CWD="$PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY"
    PARSER_DOCUMENTED_INHERITED_CWD_DYNAMIC="$PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY_DYNAMIC"
  fi
  PARSER_DOCUMENTED_PRESERVE_GOFLAGS=true
  PARSER_DOCUMENTED_PRESERVE_CWD=true
  documented_parser_command_list_is_stale "$command"
}
