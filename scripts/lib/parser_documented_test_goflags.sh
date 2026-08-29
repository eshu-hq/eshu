#!/usr/bin/env bash

PARSER_DOCUMENTED_ENV_SPLIT_EXECUTES_GO=false
PARSER_DOCUMENTED_SHELL_WRAPPER_COMMAND=''
PARSER_DOCUMENTED_SHELL_WRAPPER_DYNAMIC=false
PARSER_DOCUMENTED_PRESERVE_GOFLAGS=false
PARSER_DOCUMENTED_PRESERVE_CWD=false
PARSER_DOCUMENTED_INHERITED_CWD=''
PARSER_DOCUMENTED_INHERITED_CWD_DYNAMIC=false

documented_apply_env_short_option_cluster() {
  local cluster="${1#-}" char i
  [[ "$cluster" =~ ^[0iv]+$ ]] || return 1
  for ((i = 0; i < ${#cluster}; i++)); do
    char="${cluster:i:1}"
    case "$char" in
      0) return 2 ;;
      i) documented_clear_goflags_assignment ;;
      v) ;;
    esac
  done
}

documented_goflags_run_selector() {
  local token flag_kind found=false i token_count
  PARSER_DOCUMENTED_GOFLAGS_SELECTOR=''
  [ "$PARSER_DOCUMENTED_GOFLAGS_PRESENT" = true ] || return 1
  [ "$PARSER_DOCUMENTED_GOFLAGS_EFFECTIVE" = true ] || return 1
  [ "$PARSER_DOCUMENTED_GOFLAGS_DYNAMIC" = false ] || return 2
  documented_parser_command_tokens "$PARSER_DOCUMENTED_GOFLAGS"
  [ "$PARSER_COMMAND_TOKENS_VALID" = true ] || return 2
  token_count="${#PARSER_COMMAND_TOKENS[@]}"
  for ((i = 0; i < token_count; i++)); do
    token="${PARSER_COMMAND_TOKENS[i]}"
    documented_go_test_flag_kind "$token"
    flag_kind="$PARSER_GO_TEST_FLAG_KIND"
    [ "$flag_kind" = run ] || continue
    found=true
    if [[ "$token" == *=* ]]; then
      PARSER_DOCUMENTED_GOFLAGS_SELECTOR="${token#*=}"
    else
      # GOFLAGS values must use the standalone -flag=value form.
      return 2
    fi
  done
  [ "$found" = true ] || return 1
  [ -n "$PARSER_DOCUMENTED_GOFLAGS_SELECTOR" ] || return 2
}

documented_record_static_command_tokens() {
  local index="$1" token quoted
  PARSER_DOCUMENTED_SHELL_WRAPPER_COMMAND=''
  PARSER_DOCUMENTED_SHELL_WRAPPER_DYNAMIC=false
  for ((; index < ${#PARSER_COMMAND_TOKENS[@]}; index++)); do
    token="${PARSER_COMMAND_TOKENS[index]}"
    printf -v quoted '%q' "$token"
    if [ -n "$PARSER_DOCUMENTED_SHELL_WRAPPER_COMMAND" ]; then
      PARSER_DOCUMENTED_SHELL_WRAPPER_COMMAND+=" "
    fi
    PARSER_DOCUMENTED_SHELL_WRAPPER_COMMAND+="$quoted"
    if [ "${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[index]}" = true ]; then
      PARSER_DOCUMENTED_SHELL_WRAPPER_DYNAMIC=true
    fi
  done
}

documented_append_env_change_directory() {
  local path="$1" dynamic="$2"
  [ -n "$path" ] || return 0
  if [ "$PARSER_COMMAND_HAS_CHANGE_DIRECTORY" = true ]; then
    PARSER_COMMAND_CHANGE_DIRECTORY="${PARSER_COMMAND_CHANGE_DIRECTORY%/}/${path}"
    [ "$dynamic" = true ] && PARSER_COMMAND_CHANGE_DIRECTORY_DYNAMIC=true
  else
    PARSER_COMMAND_HAS_CHANGE_DIRECTORY=true
    PARSER_COMMAND_CHANGE_DIRECTORY="$path"
    PARSER_COMMAND_CHANGE_DIRECTORY_DYNAMIC="$dynamic"
  fi
}

documented_record_env_split_assignment_prefix() {
  local i=0 j rc token unset_name token_count env_change_directory=''
  local env_change_directory_dynamic=false
  PARSER_DOCUMENTED_ENV_SPLIT_EXECUTES_GO=false
  PARSER_DOCUMENTED_SHELL_WRAPPER_COMMAND=''
  PARSER_DOCUMENTED_SHELL_WRAPPER_DYNAMIC=false
  documented_parser_command_tokens "$PARSER_DOCUMENTED_ENV_SPLIT_STRING"
  [ "$PARSER_COMMAND_TOKENS_VALID" = true ] || return 2
  token_count="${#PARSER_COMMAND_TOKENS[@]}"
  while ((i < token_count)); do
    while ((i < token_count)); do
      token="${PARSER_COMMAND_TOKENS[i]}"
      case "$token" in
        --) ((i++)); break ;;
        -|--ignore-environment)
          documented_clear_goflags_assignment
          ((i++))
          ;;
        -u|--unset)
          ((i++))
          ((i < token_count)) || return 2
          unset_name="${PARSER_COMMAND_TOKENS[i]}"
          if [ "${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[i]}" = true ]; then
            PARSER_DOCUMENTED_GOFLAGS_PRESENT=true
            PARSER_DOCUMENTED_GOFLAGS_DYNAMIC=true
          elif [ "$unset_name" = GOFLAGS ]; then
            documented_clear_goflags_assignment
          fi
          ((i++))
          ;;
        -u=*|--unset=*)
          unset_name="${token#*=}"
          [ "$unset_name" = GOFLAGS ] && documented_clear_goflags_assignment
          ((i++))
          ;;
        -P)
          ((i += 2))
          ((i <= token_count)) || return 2
          ;;
        -P?*) ((i++)) ;;
        -C)
          ((i++))
          ((i < token_count)) || return 2
          env_change_directory="${PARSER_COMMAND_TOKENS[i]}"
          env_change_directory_dynamic="${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[i]}"
          ((i++))
          ;;
        -C?*)
          env_change_directory="${token#-C}"
          env_change_directory_dynamic="${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[i]}"
          ((i++))
          ;;
        -S|-S?*) return 2 ;;
        -*)
          documented_apply_env_short_option_cluster "$token"
          rc=$?
          [ "$rc" -eq 0 ] || return 2
          ((i++))
          ;;
        *) break ;;
      esac
    done
    while ((i < token_count)); do
      token="${PARSER_COMMAND_TOKENS[i]}"
      case "$token" in
        [A-Za-z_][A-Za-z0-9_]*=*)
          documented_record_command_assignment \
            "$token" "${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[i]}"
          ((i++))
          ;;
        *) break ;;
      esac
    done
    ((i < token_count)) || break
    documented_append_env_change_directory \
      "$env_change_directory" "$env_change_directory_dynamic"
    env_change_directory=''
    env_change_directory_dynamic=false
    token="${PARSER_COMMAND_TOKENS[i]}"
    case "$token" in
      env|/usr/bin/env) ((i++)) ;;
      go)
        documented_record_static_command_tokens "$i"
        PARSER_DOCUMENTED_ENV_SPLIT_EXECUTES_GO=true
        break
        ;;
      *)
        case "${token##*/}" in
          sh|bash|zsh|dash|ksh) ;;
          *) return 0 ;;
        esac
        for ((j = i + 1; j < token_count; j++)); do
          token="${PARSER_COMMAND_TOKENS[j]}"
          [[ "$token" =~ ^-[[:alpha:]]*c[[:alpha:]]*$ ]] || continue
          ((j++))
          ((j < token_count)) || return 2
          PARSER_DOCUMENTED_SHELL_WRAPPER_COMMAND="${PARSER_COMMAND_TOKENS[j]}"
          PARSER_DOCUMENTED_SHELL_WRAPPER_DYNAMIC="${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[j]}"
          PARSER_DOCUMENTED_ENV_SPLIT_EXECUTES_GO=true
          break
        done
        break
        ;;
    esac
  done
}

documented_current_goflags_is_relevant() {
  local rc selector
  if documented_goflags_run_selector; then
    selector="$PARSER_DOCUMENTED_GOFLAGS_SELECTOR"
  else
    rc=$?
    [ "$rc" -eq 2 ] && return 0
    return 1
  fi
  documented_relocated_rust_selector_matches "$selector" && return 0
  rc=$?
  [ "$rc" -eq 2 ] && return 0
  documented_legacy_cargo_selector_matches "$selector" && return 0
  rc=$?
  [ "$rc" -eq 2 ]
}

# shellcheck source=scripts/lib/parser_documented_test_preamble_assignments.sh
. "$PARSER_DOCUMENTED_COMMANDS_ROOT/scripts/lib/parser_documented_test_preamble_assignments.sh"

documented_shell_command_after_preamble() {
  local command="$1" segment operator token trimmed_segment i tail_index
  local skip_next=false
  local saved_goflags_present saved_goflags saved_goflags_dynamic
  local saved_goflags_exported saved_goflags_effective saved_assignment_dynamic
  while documented_shell_first_operator "$command"; do
    segment="${command:0:PARSER_DOCUMENTED_SHELL_OPERATOR_INDEX}"
    operator="$PARSER_DOCUMENTED_SHELL_OPERATOR"
    tail_index=$((PARSER_DOCUMENTED_SHELL_OPERATOR_INDEX +
      PARSER_DOCUMENTED_SHELL_OPERATOR_LENGTH))
    documented_parser_command_tokens "$segment"
    [ "$PARSER_COMMAND_TOKENS_VALID" = true ] || return 2
    token="${PARSER_COMMAND_TOKENS[0]:-}"
    case "$token" in
      '')
        trimmed_segment="${segment#"${segment%%[![:space:]]*}"}"
        [ "$operator" = $'\n' ] || return 2
        { [ -z "$trimmed_segment" ] || [[ "$trimmed_segment" == \#* ]]; } ||
          return 2
        ;;
      unset)
        case "$operator" in
          ';'|'&&'|$'\n') ;;
          *) return 2 ;;
        esac
        ((${#PARSER_COMMAND_TOKENS[@]} >= 2)) || return 2
        for ((i = 1; i < ${#PARSER_COMMAND_TOKENS[@]}; i++)); do
          [[ "${PARSER_COMMAND_TOKENS[i]}" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] ||
            return 2
          [ "${PARSER_COMMAND_TOKENS[i]}" = GOFLAGS ] &&
            documented_clear_goflags_assignment
        done
        ;;
      cd)
        case "$operator" in
          ';'|'&&'|$'\n') ;;
          *) return 2 ;;
        esac
        [ "${#PARSER_COMMAND_TOKENS[@]}" -eq 2 ] || return 2
        if [ -n "$PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY" ]; then
          PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY="${PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY%/}/${PARSER_COMMAND_TOKENS[1]}"
        else
          PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY="${PARSER_COMMAND_TOKENS[1]}"
        fi
        if [ "${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[1]}" = true ]; then
          PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY_DYNAMIC=true
        fi
        ;;
      export)
        saved_goflags_present="$PARSER_DOCUMENTED_GOFLAGS_PRESENT"
        saved_goflags="$PARSER_DOCUMENTED_GOFLAGS"
        saved_goflags_dynamic="$PARSER_DOCUMENTED_GOFLAGS_DYNAMIC"
        saved_goflags_exported="$PARSER_DOCUMENTED_GOFLAGS_EXPORTED"
        saved_goflags_effective="$PARSER_DOCUMENTED_GOFLAGS_EFFECTIVE"
        saved_assignment_dynamic="$PARSER_DOCUMENTED_SHELL_ASSIGNMENT_DYNAMIC"
        documented_record_export_preamble || return 2
        case "$operator" in
          ';'|'&&'|$'\n') ;;
          '||')
            PARSER_DOCUMENTED_GOFLAGS_PRESENT="$saved_goflags_present"
            PARSER_DOCUMENTED_GOFLAGS="$saved_goflags"
            PARSER_DOCUMENTED_GOFLAGS_DYNAMIC="$saved_goflags_dynamic"
            PARSER_DOCUMENTED_GOFLAGS_EXPORTED="$saved_goflags_exported"
            PARSER_DOCUMENTED_GOFLAGS_EFFECTIVE="$saved_goflags_effective"
            PARSER_DOCUMENTED_SHELL_ASSIGNMENT_DYNAMIC="$saved_assignment_dynamic"
            return 1
            ;;
          '|'|'&')
            PARSER_DOCUMENTED_GOFLAGS_PRESENT="$saved_goflags_present"
            PARSER_DOCUMENTED_GOFLAGS="$saved_goflags"
            PARSER_DOCUMENTED_GOFLAGS_DYNAMIC="$saved_goflags_dynamic"
            PARSER_DOCUMENTED_GOFLAGS_EXPORTED="$saved_goflags_exported"
            PARSER_DOCUMENTED_GOFLAGS_EFFECTIVE="$saved_goflags_effective"
            PARSER_DOCUMENTED_SHELL_ASSIGNMENT_DYNAMIC="$saved_assignment_dynamic"
            ;;
          *) return 2 ;;
        esac
        ;;
      [A-Za-z_][A-Za-z0-9_]*=*)
        saved_goflags_present="$PARSER_DOCUMENTED_GOFLAGS_PRESENT"
        saved_goflags="$PARSER_DOCUMENTED_GOFLAGS"
        saved_goflags_dynamic="$PARSER_DOCUMENTED_GOFLAGS_DYNAMIC"
        saved_goflags_exported="$PARSER_DOCUMENTED_GOFLAGS_EXPORTED"
        saved_goflags_effective="$PARSER_DOCUMENTED_GOFLAGS_EFFECTIVE"
        saved_assignment_dynamic="$PARSER_DOCUMENTED_SHELL_ASSIGNMENT_DYNAMIC"
        documented_record_assignment_preamble || return 2
        case "$operator" in
          ';'|'&&'|$'\n') ;;
          '||')
            PARSER_DOCUMENTED_GOFLAGS_PRESENT="$saved_goflags_present"
            PARSER_DOCUMENTED_GOFLAGS="$saved_goflags"
            PARSER_DOCUMENTED_GOFLAGS_DYNAMIC="$saved_goflags_dynamic"
            PARSER_DOCUMENTED_GOFLAGS_EXPORTED="$saved_goflags_exported"
            PARSER_DOCUMENTED_GOFLAGS_EFFECTIVE="$saved_goflags_effective"
            PARSER_DOCUMENTED_SHELL_ASSIGNMENT_DYNAMIC="$saved_assignment_dynamic"
            return 1
            ;;
          '|'|'&')
            PARSER_DOCUMENTED_GOFLAGS_PRESENT="$saved_goflags_present"
            PARSER_DOCUMENTED_GOFLAGS="$saved_goflags"
            PARSER_DOCUMENTED_GOFLAGS_DYNAMIC="$saved_goflags_dynamic"
            PARSER_DOCUMENTED_GOFLAGS_EXPORTED="$saved_goflags_exported"
            PARSER_DOCUMENTED_GOFLAGS_EFFECTIVE="$saved_goflags_effective"
            PARSER_DOCUMENTED_SHELL_ASSIGNMENT_DYNAMIC="$saved_assignment_dynamic"
            ;;
          *) return 2 ;;
        esac
        ;;
      exit)
        case "$operator" in
          '|'|'&') return 2 ;;
          *) return 1 ;;
        esac
        ;;
      true|false)
        case "$operator" in
          ';'|$'\n') ;;
          '&&') [ "$token" = true ] || skip_next=true ;;
          '||') [ "$token" = false ] || skip_next=true ;;
          *) return 2 ;;
        esac
        if [ "$skip_next" = true ]; then
          skip_next=false
          command="${command:tail_index}"
          command="${command#"${command%%[![:space:]]*}"}"
          documented_shell_first_operator "$command" || return 1
          case "$PARSER_DOCUMENTED_SHELL_OPERATOR" in
            ';'|$'\n') ;;
            *) return 2 ;;
          esac
          tail_index=$((PARSER_DOCUMENTED_SHELL_OPERATOR_INDEX +
            PARSER_DOCUMENTED_SHELL_OPERATOR_LENGTH))
        fi
        ;;
      *) return 2 ;;
    esac
    command="${command:tail_index}"
    command="${command#"${command%%[![:space:]]*}"}"
  done
  PARSER_DOCUMENTED_SHELL_WRAPPER_COMMAND="$command"
}

documented_unsupported_env_prefix_is_stale() {
  if [ -n "$PARSER_DOCUMENTED_ENV_SPLIT_STRING" ]; then
    documented_record_env_split_assignment_prefix || return 0
    [ "$PARSER_DOCUMENTED_ENV_SPLIT_EXECUTES_GO" = true ] || return 1
  fi
  if [ -n "$PARSER_DOCUMENTED_SHELL_WRAPPER_COMMAND" ]; then
    [ "$PARSER_DOCUMENTED_SHELL_WRAPPER_DYNAMIC" = false ] || return 0
    documented_shell_command_after_preamble \
      "$PARSER_DOCUMENTED_SHELL_WRAPPER_COMMAND"
    case $? in
      0) ;;
      1) return 1 ;;
      *) return 0 ;;
    esac
    PARSER_DOCUMENTED_INHERITED_CWD="$PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY"
    PARSER_DOCUMENTED_INHERITED_CWD_DYNAMIC="$PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY_DYNAMIC"
    if [ "$PARSER_COMMAND_HAS_CHANGE_DIRECTORY" = true ]; then
      if [ -n "$PARSER_DOCUMENTED_INHERITED_CWD" ]; then
        PARSER_DOCUMENTED_INHERITED_CWD="${PARSER_DOCUMENTED_INHERITED_CWD%/}/${PARSER_COMMAND_CHANGE_DIRECTORY}"
      else
        PARSER_DOCUMENTED_INHERITED_CWD="$PARSER_COMMAND_CHANGE_DIRECTORY"
      fi
      # shellcheck disable=SC2034 # Read by the recursive command classifier.
      [ "$PARSER_COMMAND_CHANGE_DIRECTORY_DYNAMIC" = true ] &&
        PARSER_DOCUMENTED_INHERITED_CWD_DYNAMIC=true
    fi
    # A command-prefix assignment is exported to the env/shell wrapper and
    # therefore becomes inherited state for the recursively classified child.
    if [ "$PARSER_DOCUMENTED_GOFLAGS_EFFECTIVE" = true ]; then
      PARSER_DOCUMENTED_GOFLAGS_EXPORTED=true
    fi
    # shellcheck disable=SC2034 # Read by the recursive command classifier.
    PARSER_DOCUMENTED_PRESERVE_GOFLAGS=true
    # shellcheck disable=SC2034 # Read by the recursive command classifier.
    PARSER_DOCUMENTED_PRESERVE_CWD=true
    documented_parser_command_is_stale \
      "$PARSER_DOCUMENTED_SHELL_WRAPPER_COMMAND"
    return $?
  fi
  documented_current_goflags_is_relevant && return 0
  return 1
}
