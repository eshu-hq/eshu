#!/usr/bin/env bash

# shellcheck disable=SC2034 # Globals are consumed by the command classifier.

PARSER_COMMAND_ARGUMENT_START=0
PARSER_COMMAND_HAS_CHANGE_DIRECTORY=false
PARSER_COMMAND_CHANGE_DIRECTORY=''
PARSER_COMMAND_CHANGE_DIRECTORY_DYNAMIC=false
PARSER_COMMAND_CHANGE_DIRECTORY_NEXT=0
PARSER_DOCUMENTED_CWD_CANDIDATES=()
PARSER_DOCUMENTED_PACKAGE_KIND='other'
PARSER_DOCUMENTED_SHELL_COMMAND=''
PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY=''
PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY_DYNAMIC=false
PARSER_DOCUMENTED_SHELL_ASSIGNMENT_DYNAMIC=false
PARSER_DOCUMENTED_SHELL_PREFIX_INVALID=false
PARSER_DOCUMENTED_GOFLAGS_PRESENT=false
PARSER_DOCUMENTED_GOFLAGS=''
PARSER_DOCUMENTED_GOFLAGS_DYNAMIC=false
PARSER_DOCUMENTED_GOFLAGS_EXPORTED=false
PARSER_DOCUMENTED_GOFLAGS_EFFECTIVE=false
PARSER_DOCUMENTED_ENV_SPLIT_STRING=''

documented_record_shell_assignment() {
  local assignment="$1" dynamic="$2"
  case "$assignment" in
    GOFLAGS+=*)
      PARSER_DOCUMENTED_GOFLAGS_PRESENT=true
      PARSER_DOCUMENTED_GOFLAGS+="${assignment#*+=}"
      PARSER_DOCUMENTED_GOFLAGS_DYNAMIC="$dynamic"
      ;;
    GOFLAGS=*)
      PARSER_DOCUMENTED_GOFLAGS_PRESENT=true
      PARSER_DOCUMENTED_GOFLAGS="${assignment#*=}"
      PARSER_DOCUMENTED_GOFLAGS_DYNAMIC="$dynamic"
      ;;
  esac
}

documented_clear_goflags_assignment() {
  PARSER_DOCUMENTED_GOFLAGS_PRESENT=false
  PARSER_DOCUMENTED_GOFLAGS=''
  PARSER_DOCUMENTED_GOFLAGS_DYNAMIC=false
  PARSER_DOCUMENTED_GOFLAGS_EXPORTED=false
  PARSER_DOCUMENTED_GOFLAGS_EFFECTIVE=false
}

documented_record_command_assignment() {
  local assignment="$1" dynamic="$2"
  documented_record_shell_assignment "$assignment" "$dynamic"
  case "$assignment" in
    GOFLAGS=*) PARSER_DOCUMENTED_GOFLAGS_EFFECTIVE=true ;;
  esac
}

documented_extract_shell_change_directory() {
  local input="$1" first_line shell_prefix
  PARSER_DOCUMENTED_SHELL_COMMAND="$input"
  PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY=''
  PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY_DYNAMIC=false
  PARSER_DOCUMENTED_SHELL_ASSIGNMENT_DYNAMIC=false
  PARSER_DOCUMENTED_SHELL_PREFIX_INVALID=false
  [[ "$input" == cd[[:space:]]* ]] || return 1
  first_line="${input%%$'\n'*}"
  if [[ "$first_line" == *'&&'* ]]; then
    shell_prefix="${input%%&&*}"
    PARSER_DOCUMENTED_SHELL_COMMAND="${input#*&&}"
  elif [[ "$input" == *$'\n'* ]]; then
    shell_prefix="${input%%$'\n'*}"
    PARSER_DOCUMENTED_SHELL_COMMAND="${input#*$'\n'}"
  else
    return 1
  fi
  documented_parser_command_tokens "$shell_prefix"
  if [ "$PARSER_COMMAND_TOKENS_VALID" = true ] &&
    [ "${#PARSER_COMMAND_TOKENS[@]}" -eq 2 ] &&
    [ "${PARSER_COMMAND_TOKENS[0]}" = cd ]; then
    PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY="${PARSER_COMMAND_TOKENS[1]}"
    PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY_DYNAMIC="${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[1]}"
  else
    PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY_DYNAMIC=true
  fi
  PARSER_DOCUMENTED_SHELL_COMMAND="${PARSER_DOCUMENTED_SHELL_COMMAND#"${PARSER_DOCUMENTED_SHELL_COMMAND%%[![:space:]]*}"}"
  if [[ "$PARSER_DOCUMENTED_SHELL_COMMAND" == \\$'\r\n'* ]]; then
    PARSER_DOCUMENTED_SHELL_COMMAND="${PARSER_DOCUMENTED_SHELL_COMMAND:3}"
  elif [[ "$PARSER_DOCUMENTED_SHELL_COMMAND" == \\$'\n'* ]]; then
    PARSER_DOCUMENTED_SHELL_COMMAND="${PARSER_DOCUMENTED_SHELL_COMMAND:2}"
  fi
  PARSER_DOCUMENTED_SHELL_COMMAND="${PARSER_DOCUMENTED_SHELL_COMMAND#"${PARSER_DOCUMENTED_SHELL_COMMAND%%[!$' \t\r\n']*}"}"
}

documented_invalid_shell_prefix_is_stale() {
  local cwd_index
  [ -n "$PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY" ] || return 0
  [ "$PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY_DYNAMIC" = false ] || return 0
  PARSER_COMMAND_CHANGE_DIRECTORY="$PARSER_DOCUMENTED_SHELL_CHANGE_DIRECTORY"
  PARSER_COMMAND_CHANGE_DIRECTORY_DYNAMIC=false
  documented_change_directory_candidates || return 0
  for ((cwd_index = 0; cwd_index < ${#PARSER_DOCUMENTED_CWD_CANDIDATES[@]}; cwd_index++)); do
    classify_documented_package_from_directory \
      "${PARSER_DOCUMENTED_CWD_CANDIDATES[cwd_index]}" .
    case "$PARSER_DOCUMENTED_PACKAGE_KIND" in
      parent|parent_child|child) return 0 ;;
    esac
  done
  return 1
}

documented_change_directory_at() {
  local index="$1" token value_index
  token="${PARSER_COMMAND_TOKENS[index]}"
  PARSER_COMMAND_CHANGE_DIRECTORY_DYNAMIC=false
  case "$token" in
    -C|--C)
      value_index=$((index + 1))
      if ((value_index >= ${#PARSER_COMMAND_TOKENS[@]})); then
        return 1
      fi
      PARSER_COMMAND_CHANGE_DIRECTORY="${PARSER_COMMAND_TOKENS[value_index]}"
      if [ "${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[index]}" = true ] ||
        [ "${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[value_index]}" = true ]; then
        PARSER_COMMAND_CHANGE_DIRECTORY_DYNAMIC=true
      fi
      PARSER_COMMAND_CHANGE_DIRECTORY_NEXT=$((value_index + 1))
      ;;
    -C=*|--C=*)
      PARSER_COMMAND_CHANGE_DIRECTORY="${token#*=}"
      PARSER_COMMAND_CHANGE_DIRECTORY_DYNAMIC="${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[index]}"
      PARSER_COMMAND_CHANGE_DIRECTORY_NEXT=$((index + 1))
      ;;
    *) return 1 ;;
  esac
  [ -n "$PARSER_COMMAND_CHANGE_DIRECTORY" ]
}

documented_apply_env_change_directory() {
  local path="$1" dynamic="$2"
  [ -n "$path" ] || return 0
  if [ "$PARSER_COMMAND_HAS_CHANGE_DIRECTORY" = true ]; then
    PARSER_COMMAND_CHANGE_DIRECTORY="${path%/}/${PARSER_COMMAND_CHANGE_DIRECTORY}"
    [ "$dynamic" = true ] && PARSER_COMMAND_CHANGE_DIRECTORY_DYNAMIC=true
  else
    PARSER_COMMAND_HAS_CHANGE_DIRECTORY=true
    PARSER_COMMAND_CHANGE_DIRECTORY="$path"
    PARSER_COMMAND_CHANGE_DIRECTORY_DYNAMIC="$dynamic"
  fi
}

documented_go_test_prefix() {
  local index=0 token unset_name env_change_directory='' env_directory
  local env_change_directory_dynamic=false env_directory_dynamic=false
  local env_split_string=false token_count="${#PARSER_COMMAND_TOKENS[@]}"
  PARSER_COMMAND_ARGUMENT_START=0
  PARSER_COMMAND_HAS_CHANGE_DIRECTORY=false
  PARSER_COMMAND_CHANGE_DIRECTORY=''
  PARSER_COMMAND_CHANGE_DIRECTORY_DYNAMIC=false
  PARSER_DOCUMENTED_ENV_SPLIT_STRING=''
  PARSER_DOCUMENTED_GOFLAGS_EFFECTIVE="$PARSER_DOCUMENTED_GOFLAGS_EXPORTED"
  ((token_count >= 2)) || return 1
  documented_record_command_assignment_prefix "$index"
  index="$PARSER_DOCUMENTED_ASSIGNMENT_PREFIX_NEXT"
  while ((index < token_count)); do
    case "${PARSER_COMMAND_TOKENS[index]}" in
      time)
        ((index++))
        ((index < token_count)) || return 2
        if [ "${PARSER_COMMAND_TOKENS[index]}" = -p ]; then
          ((index++))
          ((index < token_count)) || return 2
        elif [[ "${PARSER_COMMAND_TOKENS[index]}" == -* ]]; then
          return 2
        fi
        ;;
      env|/usr/bin/env)
        ((index++))
        env_directory=''
        env_directory_dynamic=false
        env_split_string=false
        while ((index < token_count)); do
          token="${PARSER_COMMAND_TOKENS[index]}"
          case "$token" in
            --)
              ((index++))
              break
              ;;
            -|-i|--ignore-environment)
              documented_clear_goflags_assignment
              ((index++))
              ;;
            -v)
              ((index++))
              ;;
            -P)
              ((index++))
              ((index < token_count)) || return 2
              ((index++))
              ;;
            -P?*)
              ((index++))
              ;;
            -C|--chdir)
              ((index++))
              ((index < token_count)) || return 2
              env_directory="${PARSER_COMMAND_TOKENS[index]}"
              env_directory_dynamic="${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[index]}"
              ((index++))
              ;;
            -C?*)
              env_directory="${token#-C}"
              env_directory_dynamic="${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[index]}"
              ((index++))
              ;;
            --chdir=*)
              env_directory="${token#*=}"
              env_directory_dynamic="${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[index]}"
              ((index++))
              ;;
            -S)
              # BSD env has its own token grammar. Route it through the
              # conservative split-string classifier instead of guessing.
              ((index++))
              ((index < token_count)) || return 2
              PARSER_DOCUMENTED_ENV_SPLIT_STRING="${PARSER_COMMAND_TOKENS[index]}"
              env_split_string=true
              break
              ;;
            -S?*)
              PARSER_DOCUMENTED_ENV_SPLIT_STRING="${token#-S}"
              env_split_string=true
              break
              ;;
            -u|--unset)
              ((index++))
              ((index < token_count)) || return 1
              unset_name="${PARSER_COMMAND_TOKENS[index]}"
              if [ "${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[index]}" = true ]; then
                PARSER_DOCUMENTED_GOFLAGS_PRESENT=true
                PARSER_DOCUMENTED_GOFLAGS_DYNAMIC=true
              elif [ "$unset_name" = GOFLAGS ]; then
                documented_clear_goflags_assignment
              fi
              ((index++))
              ;;
            -u=*|--unset=*)
              unset_name="${token#*=}"
              if [ "${PARSER_COMMAND_TOKEN_SHELL_EXPANSIONS[index]}" = true ]; then
                PARSER_DOCUMENTED_GOFLAGS_PRESENT=true
                PARSER_DOCUMENTED_GOFLAGS_DYNAMIC=true
              elif [ "$unset_name" = GOFLAGS ]; then
                documented_clear_goflags_assignment
              fi
              ((index++))
              ;;
            -*)
              documented_apply_env_short_option_cluster "$token" || return 2
              ((index++))
              ;;
            *) break ;;
          esac
        done
        if [ -n "$env_directory" ]; then
          if [ -n "$env_change_directory" ]; then
            env_change_directory="${env_change_directory%/}/${env_directory}"
          else
            env_change_directory="$env_directory"
          fi
          [ "$env_directory_dynamic" = true ] &&
            env_change_directory_dynamic=true
        fi
        if [ "$env_split_string" = true ]; then
          documented_apply_env_change_directory \
            "$env_change_directory" "$env_change_directory_dynamic"
          return 2
        fi
        ;;
      command)
        ((index++))
        ((index < token_count)) || return 1
        if [ "${PARSER_COMMAND_TOKENS[index]}" = -p ]; then
          ((index++))
          ((index < token_count)) || return 1
        fi
        if [ "${PARSER_COMMAND_TOKENS[index]}" = -- ]; then
          ((index++))
          ((index < token_count)) || return 1
        elif [[ "${PARSER_COMMAND_TOKENS[index]}" == -* ]]; then
          return 2
        fi
        ;;
      exec)
        ((index++))
        ((index < token_count)) || return 1
        while ((index < token_count)); do
          case "${PARSER_COMMAND_TOKENS[index]}" in
            -c|-cl|-lc)
              documented_clear_goflags_assignment
              ((index++))
              ;;
            -l)
              ((index++))
              ;;
            -a)
              ((index++))
              ((index < token_count)) || return 2
              ((index++))
              ;;
            *) break ;;
          esac
        done
        ((index < token_count)) || return 1
        if [ "${PARSER_COMMAND_TOKENS[index]}" = -- ]; then
          ((index++))
          ((index < token_count)) || return 1
        elif [[ "${PARSER_COMMAND_TOKENS[index]}" == -* ]]; then
          return 2
        fi
        ;;
      *) break ;;
    esac
    documented_record_command_assignment_prefix "$index"
    index="$PARSER_DOCUMENTED_ASSIGNMENT_PREFIX_NEXT"
  done
  ((index < token_count)) || return 1
  [ "${PARSER_COMMAND_TOKENS[index]##*/}" = go ] || return 1
  ((index++))
  ((index < token_count)) || return 1

  if documented_change_directory_at "$index"; then
    PARSER_COMMAND_HAS_CHANGE_DIRECTORY=true
    index="$PARSER_COMMAND_CHANGE_DIRECTORY_NEXT"
    ((index < token_count)) || return 1
    [ "${PARSER_COMMAND_TOKENS[index]}" = test ] || return 1
    ((index++))
  elif [ "${PARSER_COMMAND_TOKENS[index]}" = test ]; then
    ((index++))
    if ((index < token_count)) && documented_change_directory_at "$index"; then
      PARSER_COMMAND_HAS_CHANGE_DIRECTORY=true
      index="$PARSER_COMMAND_CHANGE_DIRECTORY_NEXT"
    fi
  else
    return 1
  fi
  documented_apply_env_change_directory \
    "$env_change_directory" "$env_change_directory_dynamic"
  PARSER_COMMAND_ARGUMENT_START="$index"
}

normalize_documented_repo_path() {
  local base="$1" path="$2" component last normalized=''
  local -a components segments
  case "$path" in
    /*|'') return 1 ;;
  esac
  if [ "$base" = '.' ]; then
    path="${path#./}"
  else
    path="$base/${path#./}"
  fi
  IFS='/' read -r -a components <<<"$path"
  segments=()
  for component in "${components[@]}"; do
    case "$component" in
      ''|'.') ;;
      '..')
        if [ "${#segments[@]}" -eq 0 ]; then
          return 1
        fi
        last=$((${#segments[@]} - 1))
        unset "segments[$last]"
        ;;
      *) segments+=("$component") ;;
    esac
  done
  if [ "${#segments[@]}" -eq 0 ]; then
    printf '.'
    return
  fi
  normalized="${segments[0]}"
  for ((last = 1; last < ${#segments[@]}; last++)); do
    normalized+="/${segments[last]}"
  done
  printf '%s' "$normalized"
}

documented_change_directory_candidates() {
  local base candidate existing duplicate
  PARSER_DOCUMENTED_CWD_CANDIDATES=()
  [ "$PARSER_COMMAND_CHANGE_DIRECTORY_DYNAMIC" = false ] || return 1
  for base in . go; do
    candidate="$(normalize_documented_repo_path "$base" \
      "$PARSER_COMMAND_CHANGE_DIRECTORY")" || continue
    duplicate=false
    for existing in "${PARSER_DOCUMENTED_CWD_CANDIDATES[@]}"; do
      [ "$existing" = "$candidate" ] && duplicate=true
    done
    [ "$duplicate" = true ] || PARSER_DOCUMENTED_CWD_CANDIDATES+=("$candidate")
  done
  [ "${#PARSER_DOCUMENTED_CWD_CANDIDATES[@]}" -gt 0 ]
}

normalize_documented_relative_package_path() {
  local path="$1" component normalized='.' last
  local -a components segments

  case "$path" in
    ./*) path="${path#./}" ;;
    *) return 1 ;;
  esac
  IFS='/' read -r -a components <<<"$path"
  segments=()
  for component in "${components[@]}"; do
    case "$component" in
      ''|'.') ;;
      '..')
        if [ "${#segments[@]}" -eq 0 ]; then
          return 1
        fi
        last=$((${#segments[@]} - 1))
        unset "segments[$last]"
        ;;
      *) segments+=("$component") ;;
    esac
  done
  for component in "${segments[@]}"; do
    normalized+="/$component"
  done
  printf '%s' "$normalized"
}

is_documented_parent_parser_package() {
  local normalized
  case "$1" in
    internal/parser|github.com/eshu-hq/eshu/go/internal/parser) return 0 ;;
  esac
  normalized="$(normalize_documented_relative_package_path "$1")" || return 1
  [ "$normalized" = './internal/parser' ]
}

is_documented_rust_parser_package() {
  local normalized
  case "$1" in
    github.com/eshu-hq/eshu/go/internal/parser/rust|github.com/eshu-hq/eshu/go/internal/parser/rust/...) return 0 ;;
  esac
  normalized="$(normalize_documented_relative_package_path "$1")" || return 1
  case "$normalized" in
    ./internal/parser/rust|./internal/parser/rust/...) return 0 ;;
  esac
  return 1
}

is_documented_recursive_parent_parser_package() {
  local normalized
  case "$1" in
    github.com/eshu-hq/eshu/go/internal/parser/...) return 0 ;;
  esac
  normalized="$(normalize_documented_relative_package_path "$1")" || return 1
  [ "$normalized" = './internal/parser/...' ]
}

is_documented_ambiguous_relative_parser_package() {
  case "$1" in
    .|..|../*|./parser|./parser/...|./parser/rust|./parser/rust/...|./rust|./rust/...) return 0 ;;
  esac
  return 1
}

classify_documented_package_from_directory() {
  local directory="$1" package="$2" resolved
  PARSER_DOCUMENTED_PACKAGE_KIND=other
  case "$package" in
    github.com/eshu-hq/eshu/go/internal/parser)
      PARSER_DOCUMENTED_PACKAGE_KIND=parent
      return
      ;;
    github.com/eshu-hq/eshu/go/internal/parser/...)
      PARSER_DOCUMENTED_PACKAGE_KIND=parent_child
      return
      ;;
    github.com/eshu-hq/eshu/go/internal/parser/rust|github.com/eshu-hq/eshu/go/internal/parser/rust/...)
      PARSER_DOCUMENTED_PACKAGE_KIND=child
      return
      ;;
    .|..|./*|../*) ;;
    *) return ;;
  esac
  resolved="$(normalize_documented_repo_path "$directory" "$package")" || {
    PARSER_DOCUMENTED_PACKAGE_KIND=invalid
    return
  }
  case "$resolved" in
    go/internal/parser) PARSER_DOCUMENTED_PACKAGE_KIND=parent ;;
    go/internal/parser/...) PARSER_DOCUMENTED_PACKAGE_KIND=parent_child ;;
    go/internal/parser/rust|go/internal/parser/rust/...) PARSER_DOCUMENTED_PACKAGE_KIND=child ;;
  esac
}
