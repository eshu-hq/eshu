#!/usr/bin/env bash

# shellcheck disable=SC2034 # Globals are consumed by the sourcing command scanner.

PARSER_SELECTOR_CACHE_KEYS=()
PARSER_SELECTOR_CACHE_RESULTS=()
PARSER_SELECTOR_TEST_BINARY=''
PARSER_SELECTOR_MATCHER_OUTPUT=''
PARSER_TOP_LEVEL_RUN_SELECTOR=''
PARSER_GO_TEST_FLAG_KIND=''
PARSER_RELOCATED_RUST_TEST_SOURCE_ROOT="$PARSER_DOCUMENTED_COMMANDS_ROOT"
PARSER_RELOCATED_RUST_TEST_NAMES=()

documented_top_level_run_selector() {
  local selector="$1" char next segment='' result='' joiner=''
  local i length class_depth=0 paren_depth=0 skip_rest=false
  length="${#selector}"

  for ((i = 0; i < length; i++)); do
    char="${selector:i:1}"
    next=''
    if ((i + 1 < length)); then
      next="${selector:i+1:1}"
    fi
    case "$char" in
      '\')
        if [ "$skip_rest" = false ]; then
          segment+="$char$next"
        fi
        ((i++))
        ;;
      '[')
        ((class_depth++))
        [ "$skip_rest" = true ] || segment+="$char"
        ;;
      ']')
        ((class_depth--))
        if ((class_depth < 0)); then class_depth=0; fi
        [ "$skip_rest" = true ] || segment+="$char"
        ;;
      '(')
        if ((class_depth == 0)); then ((paren_depth++)); fi
        [ "$skip_rest" = true ] || segment+="$char"
        ;;
      ')')
        if ((class_depth == 0)); then ((paren_depth--)); fi
        [ "$skip_rest" = true ] || segment+="$char"
        ;;
      '/')
        if ((class_depth == 0 && paren_depth == 0)); then
          skip_rest=true
        elif [ "$skip_rest" = false ]; then
          segment+="$char"
        fi
        ;;
      '|')
        if ((class_depth == 0 && paren_depth == 0)); then
          result+="${joiner}(${segment})"
          joiner='|'
          segment=''
          skip_rest=false
        elif [ "$skip_rest" = false ]; then
          segment+="$char"
        fi
        ;;
      *)
        [ "$skip_rest" = true ] || segment+="$char"
        ;;
    esac
  done
  if [ -n "$joiner" ]; then
    PARSER_TOP_LEVEL_RUN_SELECTOR="${result}${joiner}(${segment})"
  else
    PARSER_TOP_LEVEL_RUN_SELECTOR="$segment"
  fi
}

documented_go_test_flag_kind() {
  local token="$1" flag
  PARSER_GO_TEST_FLAG_KIND=unknown
  case "$token" in
    --) PARSER_GO_TEST_FLAG_KIND=terminator; return ;;
    -*) ;;
    *) PARSER_GO_TEST_FLAG_KIND=not-flag; return ;;
  esac
  flag="${token%%=*}"
  flag="${flag#-}"
  flag="${flag#-}"
  flag="${flag#test.}"
  case "$flag" in
    args) PARSER_GO_TEST_FLAG_KIND=terminator ;;
    run) PARSER_GO_TEST_FLAG_KIND=run ;;
    C|p|covermode|coverpkg|asmflags|buildmode|compiler|exec|gccgoflags|gcflags|installsuffix|ldflags|mod|modfile|o|overlay|pgo|pkgdir|tags|toolexec|bench|benchtime|blockprofile|blockprofilerate|count|coverprofile|cpu|cpuprofile|fuzz|fuzzminimizetime|fuzztime|list|memprofile|memprofilerate|mutexprofile|mutexprofilefraction|outputdir|parallel|shuffle|skip|timeout|trace|vet)
      PARSER_GO_TEST_FLAG_KIND=value
      ;;
    a|artifacts|asan|benchmem|buildvcs|c|cover|failfast|fullpath|h|help|json|linkshared|modcacherw|msan|n|race|short|trimpath|v|work|x)
      PARSER_GO_TEST_FLAG_KIND=boolean
      ;;
  esac
}

load_documented_relocated_rust_test_names() {
  local output name rc
  if [ "${#PARSER_RELOCATED_RUST_TEST_NAMES[@]}" -gt 0 ]; then
    return 0
  fi
  if ! ensure_documented_selector_test_binary; then
    printf '%s\n' \
      'verify-parser-relationship-kit: could not build documented -run matcher' >&2
    return 1
  fi
  if output="$("$PARSER_SELECTOR_TEST_BINARY" --inventory \
    "$PARSER_RELOCATED_RUST_TEST_SOURCE_ROOT/go/internal/parser/rust" 2>&1)"; then
    :
  else
    rc=$?
    printf 'verify-parser-relationship-kit: could not derive relocated Rust test inventory (matcher exit %d): %s\n' \
      "$rc" "$output" >&2
    return 1
  fi
  while IFS= read -r name; do
    [ -n "$name" ] && PARSER_RELOCATED_RUST_TEST_NAMES+=("$name")
  done <<<"$output"
  if [ "${#PARSER_RELOCATED_RUST_TEST_NAMES[@]}" -eq 0 ]; then
    printf '%s\n' \
      'verify-parser-relationship-kit: relocated Rust test inventory is empty' >&2
    return 1
  fi
}

documented_relocated_rust_literal_selector_matches() {
  local selector="$1" name
  case "$selector" in
    *[![:alnum:]_]*) return 2 ;;
  esac
  for name in "${PARSER_RELOCATED_RUST_TEST_NAMES[@]}"; do
    case "$name" in
      *"$selector"*) return 0 ;;
    esac
  done
  return 1
}

cleanup_documented_selector_test_binary() {
  if [ -n "$PARSER_SELECTOR_TEST_BINARY" ] && [ -e "$PARSER_SELECTOR_TEST_BINARY" ]; then
    unlink "$PARSER_SELECTOR_TEST_BINARY"
  fi
  PARSER_SELECTOR_TEST_BINARY=''
}

ensure_documented_selector_test_binary() {
  local build_output matcher_source
  if [ -n "$PARSER_SELECTOR_TEST_BINARY" ] && [ -x "$PARSER_SELECTOR_TEST_BINARY" ]; then
    return 0
  fi
  matcher_source="$PARSER_DOCUMENTED_COMMANDS_ROOT/scripts/lib/parser_selector_matcher.go"
  if [ -n "$PARSER_SELECTOR_MATCHER_OUTPUT" ]; then
    PARSER_SELECTOR_TEST_BINARY="$PARSER_SELECTOR_MATCHER_OUTPUT"
  else
    PARSER_SELECTOR_TEST_BINARY="$(mktemp "${TMPDIR:-/tmp}/eshu-parser-selector.XXXXXX")"
  fi
  if build_output="$(go build -o "$PARSER_SELECTOR_TEST_BINARY" "$matcher_source" 2>&1)"; then
    return 0
  fi
  printf '%s\n' "$build_output" >&2
  cleanup_documented_selector_test_binary
  return 1
}

documented_relocated_rust_selector_matches() {
  local selector="$1" output result=1 rc i
  if ! load_documented_relocated_rust_test_names; then
    return 2
  fi
  documented_top_level_run_selector "$selector"
  selector="$PARSER_TOP_LEVEL_RUN_SELECTOR"
  for ((i = 0; i < ${#PARSER_SELECTOR_CACHE_KEYS[@]}; i++)); do
    if [ "${PARSER_SELECTOR_CACHE_KEYS[i]}" = "$selector" ]; then
      return "${PARSER_SELECTOR_CACHE_RESULTS[i]}"
    fi
  done

  if documented_relocated_rust_literal_selector_matches "$selector"; then
    result=0
  else
    rc=$?
    if [ "$rc" -ne 2 ]; then
      PARSER_SELECTOR_CACHE_KEYS+=("$selector")
      PARSER_SELECTOR_CACHE_RESULTS+=("$result")
      return "$result"
    fi
  fi
  if [ "$result" -eq 0 ]; then
    PARSER_SELECTOR_CACHE_KEYS+=("$selector")
    PARSER_SELECTOR_CACHE_RESULTS+=("$result")
    return 0
  fi

  if ! ensure_documented_selector_test_binary; then
    printf '%s\n' \
      'verify-parser-relationship-kit: could not build documented -run matcher' >&2
    result=2
  elif output="$("$PARSER_SELECTOR_TEST_BINARY" "$selector" \
    "${PARSER_RELOCATED_RUST_TEST_NAMES[@]}" 2>&1)"; then
    if [ -n "$output" ]; then
      result=0
    fi
  else
    rc=$?
    printf 'verify-parser-relationship-kit: could not evaluate documented -run %q (matcher exit %d)\n' \
      "$selector" "$rc" >&2
    result=2
  fi
  PARSER_SELECTOR_CACHE_KEYS+=("$selector")
  PARSER_SELECTOR_CACHE_RESULTS+=("$result")
  return "$result"
}

documented_legacy_cargo_selector_matches() {
  local selector="$1" output rc
  documented_top_level_run_selector "$selector"
  selector="$PARSER_TOP_LEVEL_RUN_SELECTOR"
  if ! ensure_documented_selector_test_binary; then
    printf '%s\n' \
      'verify-parser-relationship-kit: could not build documented -run matcher' >&2
    return 2
  fi
  if output="$("$PARSER_SELECTOR_TEST_BINARY" "$selector" \
    TestDefaultEngineParsePathCargo 2>&1)"; then
    [ -n "$output" ]
    return
  else
    rc=$?
  fi
  printf 'verify-parser-relationship-kit: could not evaluate documented -run %q (matcher exit %d)\n' \
    "$selector" "$rc" >&2
  return 2
}
