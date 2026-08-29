#!/usr/bin/env bash

PARSER_DOCUMENTED_COMMANDS_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/parser_documented_test_selector.sh
. "$PARSER_DOCUMENTED_COMMANDS_ROOT/scripts/lib/parser_documented_test_selector.sh"
# shellcheck source=scripts/lib/parser_documented_test_workdir.sh
. "$PARSER_DOCUMENTED_COMMANDS_ROOT/scripts/lib/parser_documented_test_workdir.sh"

documented_shell_expansion_starts() {
  local char="$1" next="$2" quote="${3:-}" token_started="${4:-false}"
  if [ -z "$quote" ]; then
    case "$char" in
      '*'|'?'|'['|'{') return 0 ;;
      '~') [ "$token_started" = false ] && return 0 ;;
    esac
  fi
  if [ "$char" = '`' ]; then
    return 0
  fi
  [ "$char" = '$' ] || return 1
  case "$next" in
    "'"|'"') [ -z "$quote" ] && return 0 ;;
    [[:alnum:]_]|'('|\{|'$'|'?'|'#'|'@'|'*'|'-'|'!') return 0 ;;
  esac
  return 1
}

documented_parser_command_tokens() {
  local input="$1" char next quote='' token='' last_token=''
  local i length token_started=false redirection_operator=false
  local token_shell_expansion=false
  PARSER_COMMAND_TOKENS=()
  PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS=()
  PARSER_COMMAND_TOKENS_VALID=true
  length="${#input}"

  for ((i = 0; i < length; i++)); do
    char="${input:i:1}"
    next=''
    if ((i + 1 < length)); then
      next="${input:i+1:1}"
    fi
    if [ -n "$quote" ]; then
      if [ "$char" = "$quote" ]; then
        quote=''
      elif [ "$quote" = '"' ] && [ "$char" = '\' ] && ((i + 1 < length)); then
        case "$next" in
          '$'|'`'|'"'|'\')
            ((i++))
            token+="$next"
            ;;
          $'\n')
            ((i++))
            ;;
          *) token+="$char" ;;
        esac
        redirection_operator=false
      else
        token+="$char"
        if [ "$quote" = '"' ] &&
          documented_shell_expansion_starts "$char" "$next" "$quote"; then
          token_shell_expansion=true
        fi
        redirection_operator=false
      fi
      continue
    fi

    case "$char" in
      "'"|'"')
        quote="$char"
        token_started=true
        redirection_operator=false
        ;;
      '#')
        # Shell comments begin only between words; a literal # inside a word
        # remains part of that argument.
        if [ "$token_started" = false ]; then
          break
        fi
        token+="$char"
        redirection_operator=false
        ;;
      '\')
        if ((i + 1 < length)); then
          ((i++))
          if [ "$next" != $'\n' ]; then
            token+="$next"
            token_started=true
            redirection_operator=false
          fi
        fi
        ;;
      ';')
        if [ "$token_started" = true ]; then
          PARSER_COMMAND_TOKENS+=("$token")
          PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS+=("$token_shell_expansion")
        fi
        break
        ;;
      '&')
        if [ "$redirection_operator" = true ] ||
          { [ "$token_started" = false ] && [ "$next" = '>' ]; }; then
          token+="$char"
          token_started=true
          redirection_operator=false
          continue
        fi
        if [ "$token_started" = true ]; then
          PARSER_COMMAND_TOKENS+=("$token")
          PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS+=("$token_shell_expansion")
        fi
        break
        ;;
      '|')
        if [ "$redirection_operator" = true ]; then
          token+="$char"
          redirection_operator=false
          continue
        fi
        if [ "$token_started" = true ]; then
          PARSER_COMMAND_TOKENS+=("$token")
          PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS+=("$token_shell_expansion")
        fi
        break
        ;;
      $'\n'|$'\r')
        if [ "$token_started" = true ]; then
          PARSER_COMMAND_TOKENS+=("$token")
          PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS+=("$token_shell_expansion")
          last_token="$token"
          token=''
          token_started=false
          token_shell_expansion=false
          redirection_operator=false
        fi
        case "$last_token" in
          -run|--run|-test.run|--test.run) ;;
          *) break ;;
        esac
        ;;
      ' '|$'\t')
        if [ "$token_started" = true ]; then
          PARSER_COMMAND_TOKENS+=("$token")
          PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS+=("$token_shell_expansion")
          last_token="$token"
          token=''
          token_started=false
          token_shell_expansion=false
          redirection_operator=false
        fi
        ;;
      *)
        if documented_shell_expansion_starts \
          "$char" "$next" '' "$token_started"; then
          token_shell_expansion=true
        fi
        token+="$char"
        token_started=true
        case "$char" in
          '<'|'>') redirection_operator=true ;;
          *) redirection_operator=false ;;
        esac
        ;;
    esac
  done
  if [ "$token_started" = true ]; then
    PARSER_COMMAND_TOKENS+=("$token")
    PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS+=("$token_shell_expansion")
  fi
  [ -z "$quote" ] || PARSER_COMMAND_TOKENS_VALID=false
}

# shellcheck source=scripts/lib/parser_documented_test_goflags.sh
. "$PARSER_DOCUMENTED_COMMANDS_ROOT/scripts/lib/parser_documented_test_goflags.sh"

documented_parser_command_is_stale() {
  local command="$1" token selector='' selector_rc legacy_rc flag_kind
  local selector_shell_expansion=false
  local i token_count cwd_index package_kind
  local inherited_cwd='' inherited_cwd_dynamic=false
  local has_parent=false has_invalid_parent=false has_child=false has_run_flag=false
  local has_shell_dynamic_argument=false
  local has_shell_dynamic_package=false has_unknown_pattern=false
  local has_ambiguous_relative_package=false
  local has_package_argument=false
  local has_parent_without_child=false
  local package_args=true
  local goflags_rc prefix_rc
  local -a cwd_parent cwd_child

  command="${command#"${command%%[![:space:]]*}"}"
  command="${command%"${command##*[![:space:]]}"}"
  if [[ "$command" == \(*\) ]]; then
    command="${command:1:${#command}-2}"
  elif [[ "$command" == \{*\} ]]; then
    command="${command:1:${#command}-2}"
    command="${command%"${command##*[![:space:]]}"}"
    command="${command%;}"
  fi

  if [ "$PARSER_DOCUMENTED_PRESERVE_GOFLAGS" = true ]; then
    PARSER_DOCUMENTED_PRESERVE_GOFLAGS=false
  else
    documented_clear_goflags_assignment
  fi
  if [ "$PARSER_DOCUMENTED_PRESERVE_CWD" = true ]; then
    inherited_cwd="$PARSER_DOCUMENTED_INHERITED_CWD"
    inherited_cwd_dynamic="$PARSER_DOCUMENTED_INHERITED_CWD_DYNAMIC"
    PARSER_DOCUMENTED_PRESERVE_CWD=false
  fi
  PARSER_DOCUMENTED_ENV_SPLIT_STRING=''
  PARSER_DOCUMENTED_ENV_SPLIT_EXECUTES_GO=false
  PARSER_DOCUMENTED_SHELL_WRAPPER_COMMAND=''
  PARSER_DOCUMENTED_SHELL_WRAPPER_DYNAMIC=false
  documented_extract_shell_change_directory "$command" || :
  if [ -n "$inherited_cwd" ]; then
    if [ -n "$PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY" ]; then
      PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY="${inherited_cwd%/}/${PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY}"
    else
      PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY="$inherited_cwd"
    fi
  fi
  if [ "$inherited_cwd_dynamic" = true ]; then
    PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY_DYNAMIC=true
  fi
  if [ "$PARSER_DOCUMENTED_SHELL_PREFIX_INVALID" = true ]; then
    documented_invalid_shell_prefix_is_stale
    return $?
  fi
  command="$PARSER_DOCUMENTED_SHELL_COMMAND"
  command="${command#"${command%%[![:space:]]*}"}"
  if documented_shell_command_has_stateful_preamble "$command"; then
    documented_shell_command_after_preamble "$command"
    case $? in
      0) ;;
      1) return 1 ;;
      *) return 0 ;;
    esac
    [ "$PARSER_DOCUMENTED_SHELL_WRAPPER_COMMAND" != "$command" ] || return 1
    PARSER_DOCUMENTED_INHERITED_CWD="$PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY"
    PARSER_DOCUMENTED_INHERITED_CWD_DYNAMIC="$PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY_DYNAMIC"
    PARSER_DOCUMENTED_PRESERVE_GOFLAGS=true
    PARSER_DOCUMENTED_PRESERVE_CWD=true
    documented_parser_command_is_stale \
      "$PARSER_DOCUMENTED_SHELL_WRAPPER_COMMAND"
    return $?
  fi
  documented_parser_command_tokens "$command"
  if [ "$PARSER_COMMAND_TOKENS_VALID" = false ]; then
    documented_invalid_shell_prefix_is_stale
    return $?
  fi
  token_count="${#PARSER_COMMAND_TOKENS[@]}"
  if documented_shell_wrapper_from_command_prefix; then
    [ "$PARSER_DOCUMENTED_SHELL_WRAPPER_DYNAMIC" = false ] || return 0
    PARSER_DOCUMENTED_PRESERVE_GOFLAGS=true
    PARSER_DOCUMENTED_INHERITED_CWD="$PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY"
    PARSER_DOCUMENTED_INHERITED_CWD_DYNAMIC="$PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY_DYNAMIC"
    PARSER_DOCUMENTED_PRESERVE_CWD=true
    documented_parser_command_list_is_stale \
      "$PARSER_DOCUMENTED_SHELL_WRAPPER_COMMAND"
    return $?
  else
    prefix_rc=$?
    [ "$prefix_rc" -eq 2 ] && return 0
  fi
  if documented_go_test_prefix; then
    :
  else
    prefix_rc=$?
    if [ "$prefix_rc" -eq 2 ]; then
      documented_unsupported_env_prefix_is_stale
      return $?
    fi
    return 1
  fi
  if [ -n "$PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY" ]; then
    if [ "$PARSER_COMMAND_HAS_CHANGE_DIRECTORY" = true ]; then
      PARSER_COMMAND_CHANGE_DIRECTORY="${PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY%/}/${PARSER_COMMAND_CHANGE_DIRECTORY}"
      if [ "$PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY_DYNAMIC" = true ]; then
        PARSER_COMMAND_CHANGE_DIRECTORY_DYNAMIC=true
      fi
    else
      PARSER_COMMAND_HAS_CHANGE_DIRECTORY=true
      PARSER_COMMAND_CHANGE_DIRECTORY="$PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY"
      PARSER_COMMAND_CHANGE_DIRECTORY_DYNAMIC="$PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY_DYNAMIC"
    fi
  elif [ "$PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY_DYNAMIC" = true ]; then
    PARSER_COMMAND_HAS_CHANGE_DIRECTORY=true
    PARSER_COMMAND_CHANGE_DIRECTORY_DYNAMIC=true
  fi
  for ((i = 1; i < token_count; i++)); do
    if [ "${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[i]}" = true ]; then
      has_shell_dynamic_argument=true
      break
    fi
  done
  if [ "$PARSER_COMMAND_HAS_CHANGE_DIRECTORY" = true ]; then
    if documented_change_directory_candidates; then
      for ((cwd_index = 0; cwd_index < ${#PARSER_DOCUMENTED_CWD_CANDIDATES[@]}; cwd_index++)); do
        cwd_parent+=(false)
        cwd_child+=(false)
      done
    else
      PARSER_COMMAND_CHANGE_DIRECTORY_DYNAMIC=true
    fi
  fi

  for ((i = PARSER_COMMAND_ARGUMENT_START; i < token_count; i++)); do
    token="${PARSER_COMMAND_TOKENS[i]}"
    documented_go_test_flag_kind "$token"
    flag_kind="$PARSER_GO_TEST_FLAG_KIND"
    case "$flag_kind" in
      terminator)
        # Remaining arguments are not `go test` package arguments. Continue
        # scanning for a run selector, but never accept a later Rust path as a
        # child package that exempts the parent.
        package_args=false
        continue
        ;;
      run)
        has_run_flag=true
        if [[ "$token" == *=* ]]; then
          selector="${token#*=}"
          selector_shell_expansion="${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[i]}"
        elif ((i + 1 < token_count)); then
          selector="${PARSER_COMMAND_TOKENS[i+1]}"
          selector_shell_expansion="${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[i+1]}"
          ((i++))
        fi
        continue
        ;;
      value)
        if [[ "$token" != *=* ]]; then
          if ((i + 1 < token_count)); then
            ((i++))
          else
            package_args=false
          fi
        fi
        continue
        ;;
      boolean) continue ;;
      unknown)
        # An unknown flag begins test-binary arguments. Go requires the real
        # package list before that point, so a later child cannot exempt it.
        package_args=false
        continue
        ;;
    esac

    if [ "$package_args" = true ]; then
      has_package_argument=true
      if [ "${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[i]}" = true ]; then
        has_shell_dynamic_package=true
      else
        case "$token" in
          *...*) has_unknown_pattern=true ;;
        esac
        if [ "$PARSER_COMMAND_HAS_CHANGE_DIRECTORY" = true ]; then
          for ((cwd_index = 0; cwd_index < ${#PARSER_DOCUMENTED_CWD_CANDIDATES[@]}; cwd_index++)); do
            classify_documented_package_from_directory \
              "${PARSER_DOCUMENTED_CWD_CANDIDATES[cwd_index]}" "$token"
            package_kind="$PARSER_DOCUMENTED_PACKAGE_KIND"
            case "$package_kind" in
              parent) cwd_parent[cwd_index]=true ;;
              parent_child)
                cwd_parent[cwd_index]=true
                cwd_child[cwd_index]=true
                ;;
              child) cwd_child[cwd_index]=true ;;
            esac
          done
        elif is_documented_rust_parser_package "$token"; then
          has_child=true
        elif is_documented_recursive_parent_parser_package "$token"; then
          has_parent=true
          has_child=true
        elif is_documented_parent_parser_package "$token"; then
          has_parent=true
          if [ "$token" = 'internal/parser' ]; then
            # Unlike the other spellings, this is not a module-relative package
            # argument. Keep rejecting it even in an otherwise valid mixed list.
            has_invalid_parent=true
          fi
        else
          if is_documented_ambiguous_relative_parser_package "$token"; then
            has_ambiguous_relative_package=true
          fi
        fi
      fi
    fi
  done

  if [ "$PARSER_COMMAND_HAS_CHANGE_DIRECTORY" = true ]; then
    if [ "$has_package_argument" = false ]; then
      for ((cwd_index = 0; cwd_index < ${#PARSER_DOCUMENTED_CWD_CANDIDATES[@]}; cwd_index++)); do
        classify_documented_package_from_directory \
          "${PARSER_DOCUMENTED_CWD_CANDIDATES[cwd_index]}" .
        case "$PARSER_DOCUMENTED_PACKAGE_KIND" in
          parent) cwd_parent[cwd_index]=true ;;
          parent_child)
            cwd_parent[cwd_index]=true
            cwd_child[cwd_index]=true
            ;;
          child) cwd_child[cwd_index]=true ;;
        esac
      done
    fi
    for ((cwd_index = 0; cwd_index < ${#PARSER_DOCUMENTED_CWD_CANDIDATES[@]}; cwd_index++)); do
      if [ "${cwd_child[cwd_index]}" = true ]; then
        has_child=true
      fi
      if [ "${cwd_parent[cwd_index]}" = true ]; then
        has_parent=true
        if [ "${cwd_child[cwd_index]}" = false ]; then
          has_parent_without_child=true
        fi
      fi
    done
  fi

  if [ "$has_run_flag" = false ]; then
    if documented_goflags_run_selector; then
      selector="$PARSER_DOCUMENTED_GOFLAGS_SELECTOR"
      has_run_flag=true
    else
      goflags_rc=$?
      if [ "$goflags_rc" -eq 2 ] &&
        { [ "$has_parent" = true ] ||
          [ "$has_child" = true ] ||
          [ "$has_unknown_pattern" = true ] ||
          [ "$has_ambiguous_relative_package" = true ] ||
          [ "$PARSER_COMMAND_CHANGE_DIRECTORY_DYNAMIC" = true ]; }; then
        return 0
      fi
      return 1
    fi
  fi
  if [ "$has_shell_dynamic_argument" = true ] &&
    { [ "$has_parent" = true ] ||
      [ "$has_shell_dynamic_package" = true ] ||
      [ "$PARSER_COMMAND_CHANGE_DIRECTORY_DYNAMIC" = true ] ||
      [ "$has_unknown_pattern" = true ] ||
      [ "$has_ambiguous_relative_package" = true ]; }; then
    return 0
  fi
  if [ "$has_parent" = false ] &&
    [ "$has_unknown_pattern" = false ] &&
    [ "$has_ambiguous_relative_package" = false ] &&
    [ "$PARSER_COMMAND_CHANGE_DIRECTORY_DYNAMIC" = false ] &&
    { [ "$PARSER_DOCUMENTED_SHELL_ASSIGNMENT_DYNAMIC" = false ] ||
      [ "$has_child" = false ]; }; then
    return 1
  fi
  if [ "$selector_shell_expansion" = true ]; then
    return 0
  fi
  if documented_relocated_rust_selector_matches "$selector"; then
    if [ "$PARSER_DOCUMENTED_SHELL_ASSIGNMENT_DYNAMIC" = true ] &&
      { [ "$has_parent" = true ] ||
        [ "$has_child" = true ] ||
        [ "$has_unknown_pattern" = true ] ||
        [ "$has_ambiguous_relative_package" = true ] ||
        [ "$PARSER_COMMAND_CHANGE_DIRECTORY_DYNAMIC" = true ]; }; then
      return 0
    fi
    [ "$has_parent_without_child" = true ] && return 0
    [ "$has_ambiguous_relative_package" = true ] && return 0
    [ "$has_unknown_pattern" = true ] && [ "$has_child" = false ] && return 0
    [ "$has_child" = true ] && [ "$has_invalid_parent" = false ] && return 1
    return 0
  else
    selector_rc=$?
  fi
  [ "$selector_rc" -eq 2 ] && return 0
  if documented_legacy_cargo_selector_matches "$selector"; then
    return 0
  else
    legacy_rc=$?
  fi
  [ "$legacy_rc" -eq 2 ]
}

# shellcheck source=scripts/lib/parser_documented_test_scan.sh
. "$PARSER_DOCUMENTED_COMMANDS_ROOT/scripts/lib/parser_documented_test_scan.sh"
