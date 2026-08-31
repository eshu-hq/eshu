#!/usr/bin/env bash

# validate_documented_parser_test_commands rejects documented commands that
# select relocated Rust engine tests from the former parent package without
# also listing the Rust child package as a real `go test` package argument.
documented_command_is_known_nonrunnable_overlay_example() {
  local command="$1" pattern
  pattern=$'^[[:blank:]]*go[[:blank:]]+test[[:blank:]]+\\./internal/reducer[[:blank:]]+-overlay=<promotion[[:blank:]]+rule[[:blank:]]+replaced[[:blank:]]+with[[:blank:]]+`return[[:blank:]]+true`>[[:blank:]]*\\\\[[:space:]]+-run[[:blank:]]+\'Branch3\\|RegardlessOfEnvironmentEvidence\\|ContradictingDigest\'[[:blank:]]*$'
  [[ "$command" =~ $pattern ]]
}

validate_documented_parser_test_commands() {
  local root="$1" matches precise_matches line_matches records_file path line command stale=''
  local precise_rc=0 line_rc=0 bare_run_pattern
  local relocated_name_pattern='' documented_test_signal name
  local go_executable go_test_start shell_command_start shell_cd_prefix assignment_value command_join
  local command_assignment command_env command_env_option command_env_options command_prefix
  local command_wrapper exec_wrapper
  local goflags_env_prefix shell_prefix_token
  local go_test_command prefixed_goflags_candidate cd_goflags_candidate
  local export_goflags_candidate export_goflags_segment safe_export_separator
  local assign_export_goflags_candidate assignment_token assignment_list
  local assignment_list_with_goflags export_token export_goflags_name_segment
  local env_split_candidate env_split_option env_split_options split_command_prefix
  local split_go_test_value split_goflags_go_test_value
  local inline_code_candidate logical_continuation_candidate
  local go_candidate cd_candidate run_flag candidate_pattern decode_error=false

  if ! load_documented_relocated_rust_test_names; then
    cleanup_documented_selector_test_binary
    return 1
  fi
  for name in "${PARSER_RELOCATED_RUST_TEST_NAMES[@]}"; do
    [ -z "$relocated_name_pattern" ] || relocated_name_pattern+='|'
    relocated_name_pattern+="$name"
  done
  go_executable="(?:go|\"go\"|'go'|\"(?:\\\\.|[^\"\\\\])*/go\"|'[^']*/go'|(?:\\\\.|[^\\s\\\\;&|\\x60])*/go)"
  shell_command_start='(?:^|\x60|\r?\n)[\t ]*(?:\$\s+)?\K'
  shell_cd_prefix='cd(?:(?!&&|\x60)[\s\S])+?(?:&&[\t ]*(?:\\\r?\n[\t ]*)?|\r?\n[\t ]*)'
  assignment_value="(?:\"(?:\\\\.|[^\"\\\\])*\"|'[^']*'|[^\\s;&|\\x60]+)?"
  command_join='(?:[\t ]+|[\t ]*\\\r?\n[\t ]*)'
  command_assignment="[A-Za-z_][A-Za-z0-9_]*=${assignment_value}${command_join}"
  command_env='(?:env|/usr/bin/env)'
  shell_prefix_token="(?:\"(?:\\\\.|[^\"\\\\])*\"|'[^']*'|[^\\s;&|\\x60]+)"
  go_test_start="${go_executable}\\s+(?:--?C(?:=|\\s)${shell_prefix_token}\\s+)?test"
  command_env_option="(?:-|-[0iv]+|--ignore-environment|--chdir=${shell_prefix_token}|(?:-u|--unset|-P|-C)${command_join}${shell_prefix_token}|(?:-u=|--unset=|-P|-C)${shell_prefix_token})"
  command_env_options="(?:${command_env_option}${command_join})*"
  command_wrapper="command(?:${command_join}-p)?"
  exec_wrapper="exec(?:${command_join}(?:-c|-l|-cl|-lc|-a${command_join}${shell_prefix_token}))*"
  command_prefix="(?:(?:${command_assignment})|(?:${command_env}${command_join}${command_env_options}(?:--${command_join})?)|(?:time(?:${command_join}-p)?${command_join})|(?:(?:${command_wrapper}|${exec_wrapper})${command_join}(?:--${command_join})?))*"
  goflags_env_prefix="(?=(?:${shell_prefix_token}${command_join})*GOFLAGS=)(?:${shell_prefix_token}${command_join})*"
  command_segment="(?:(?!${go_test_start}|\\x60)[\\s\\S])*"
  run_flag='--?(?:test[.])?run'
  documented_test_signal="(?:GOFLAGS(?:\\+?=|\\s)|(?<![[:alnum:]_-])${run_flag}(?:=|\\s))"
  bare_run_pattern='^(?![^\r\n]*\x60)(?![^\r\n]*\\[[:blank:]]*$)[[:blank:]]*(?:\$[[:blank:]]+)?(?=(?:GOFLAGS(?:=|[[:blank:]])|[^\r\n]*[[:blank:]](?:GOFLAGS(?:=|[[:blank:]])|--?(?:test[.])?run(?:=|[[:blank:]]))))[^\r\n]+$'
  go_test_command="${go_test_start}${command_segment}"
  go_candidate="${go_test_start}(?=${command_segment}\\s${run_flag}(?:=|\\s))${command_segment}"
  cd_candidate="${shell_command_start}(?:${shell_cd_prefix})?${command_prefix}${go_candidate}"
  cd_goflags_candidate="${shell_command_start}${shell_cd_prefix}${goflags_env_prefix}${go_test_command}"
  prefixed_goflags_candidate="${shell_command_start}${goflags_env_prefix}${go_test_command}"
  safe_export_separator='(?:[\t ]*;[\t ]*|[\t ]*&&[\t ]*|[\t ]*(?:#[^\r\n]*)?\r?\n(?:[\t ]*(?:#[^\r\n]*)?\r?\n)*[\t ]*)'
  export_goflags_segment="export${command_join}(?=(?:(?![;&|\x60\r\n])[\s\S])*GOFLAGS=)(?:(?![;&|\x60\r\n])[\s\S])+"
  export_goflags_candidate="${shell_command_start}${export_goflags_segment}${safe_export_separator}(?:(?!${go_test_start}|\x60)[\s\S])*${go_test_command}"
  assignment_token="[A-Za-z_][A-Za-z0-9_]*=${assignment_value}"
  assignment_list="${assignment_token}(?:${command_join}${assignment_token})*"
  assignment_list_with_goflags="(?=(?:${assignment_token}${command_join})*GOFLAGS=)${assignment_list}"
  export_token="[A-Za-z_][A-Za-z0-9_]*(?:=${assignment_value})?"
  export_goflags_name_segment="export${command_join}(?=(?:${export_token}${command_join})*GOFLAGS(?:${command_join}|[;&|\r\n]))${export_token}(?:${command_join}${export_token})*"
  assign_export_goflags_candidate="${shell_command_start}${assignment_list_with_goflags}${safe_export_separator}${export_goflags_name_segment}${safe_export_separator}(?:(?!${go_test_start}|\x60)[\s\S])*${go_test_command}"
  split_go_test_value="(?:\"(?:\\\\.|[^\"\\\\])*${go_test_start}(?:\\\\.|[^\"\\\\])*\"|'[^']*${go_test_start}[^']*')"
  split_goflags_go_test_value="(?:\"(?=[^\"]*GOFLAGS=)(?=[^\"]*${go_test_start})[^\"]*\"|'(?=[^']*GOFLAGS=)(?=[^']*${go_test_start})[^']*')"
  env_split_option="${command_env_option}"
  env_split_options="(?:${env_split_option}${command_join})*"
  split_command_prefix="${shell_command_start}(?:${shell_cd_prefix})?"
  env_split_candidate="${split_command_prefix}(?:${goflags_env_prefix}${command_env}${command_join}${env_split_options}-S(?:${command_join})?${split_go_test_value}|${command_env}${command_join}${env_split_options}-S(?:${command_join})?${split_goflags_go_test_value})"
  inline_code_candidate="(?:(?<!\\x60)\\x60(?!\\x60)(?=(?:(?!\\x60)[\\s\\S])*${documented_test_signal})(?:(?!\\x60)[\\s\\S])+\\x60(?!\\x60)|(?<!\\x60)(?<inline_ticks>\\x60{2,})(?!\\x60)(?=(?:(?!\\k<inline_ticks>)[^\\r\\n])*${documented_test_signal})(?:(?!\\k<inline_ticks>)[^\\r\\n])+\\k<inline_ticks>(?!\\x60))"
  logical_continuation_candidate="(?:^|\\r?\\n)[\\t ]*(?:\\$\\s+)?\\K(?:[^\\r\\n]*\\\\[\\t ]*\\r?\\n)+[^\\r\\n]+"
  candidate_pattern="(?s)(?:${inline_code_candidate}|${logical_continuation_candidate}|${env_split_candidate}|${assign_export_goflags_candidate}|${export_goflags_candidate}|${cd_goflags_candidate}|${prefixed_goflags_candidate}|${cd_candidate})"

  if precise_matches="$(rg -n -U --json --pcre2 --glob '*.md' \
    -e "$candidate_pattern" "$root/docs" "$root/go/internal/parser" 2>&1)"; then
    :
  else
    precise_rc=$?
    if [ "$precise_rc" -ne 1 ]; then
      printf '%s\n' "$precise_matches" >&2
      printf 'verify-parser-relationship-kit: documented Rust command scan failed (rg exit %d)\n' "$precise_rc" >&2
      cleanup_documented_selector_test_binary
      return 1
    fi
  fi
  if line_matches="$(rg -n --json --pcre2 --glob '*.md' \
    -e "$bare_run_pattern" \
    "$root/docs" "$root/go/internal/parser" 2>&1)"; then
    :
  else
    line_rc=$?
    if [ "$line_rc" -ne 1 ]; then
      printf '%s\n' "$line_matches" >&2
      printf 'verify-parser-relationship-kit: documented Rust line scan failed (rg exit %d)\n' "$line_rc" >&2
      cleanup_documented_selector_test_binary
      return 1
    fi
  fi
  if [ "$precise_rc" -eq 1 ] && [ "$line_rc" -eq 1 ]; then
    cleanup_documented_selector_test_binary
    return 0
  fi
  matches="$precise_matches"
  if [ "$line_rc" -eq 0 ]; then
    [ -z "$matches" ] || matches+=$'\n'
    matches+="$line_matches"
  fi

  if ! records_file="$(mktemp "${TMPDIR:-/tmp}/eshu-parser-command-records.XXXXXX")"; then
    printf '%s\n' \
      'verify-parser-relationship-kit: could not create documented command record file' >&2
    cleanup_documented_selector_test_binary
    return 1
  fi
  if ! jq -j '
    def strip_command_substitution_newlines: sub("\n+$"; "");
    def strip_markdown_fence:
      if test("^[ ]{0,3}(?:`{3,}|~{3,})[^\\n]*\\n") then
        sub("^[^\\n]*\\n"; "") |
        sub("\\r?\\n[ ]{0,3}(?:`{3,}|~{3,})[ \\t]*\\r?$"; "")
      elif startswith("`") then
        sub("^`+"; "") |
        sub("`+$"; "")
      else . end;
    def strip_shell_prompts:
      gsub("(?m)^\\$[ \\t]+"; "");
    select(.type == "match") |
    .data as $data |
    $data.submatches[] |
    [$data.path.text, ($data.line_number | tostring),
     (.match.text | strip_markdown_fence | strip_shell_prompts)] as $fields |
    if ($fields | any(contains("\u0000"))) then
      error("embedded NUL in parser command record")
    else
      ($fields[] | strip_command_substitution_newlines, "\u0000")
    end
  ' <<<"$matches" >"$records_file"; then
    unlink "$records_file"
    printf '%s\n' \
      'verify-parser-relationship-kit: could not decode documented Rust command matches' >&2
    cleanup_documented_selector_test_binary
    return 1
  fi
  if [ ! -s "$records_file" ]; then
    unlink "$records_file"
    cleanup_documented_selector_test_binary
    return 0
  fi

  while IFS= read -r -d '' path; do
    if ! IFS= read -r -d '' line || ! IFS= read -r -d '' command; then
      decode_error=true
      break
    fi
    if documented_command_is_known_nonrunnable_overlay_example "$command"; then
      continue
    fi
    documented_reset_parser_command_state
    if documented_parser_command_list_is_stale "$command"; then
      stale+="${path}:${line}:${command}"$'\n'
    fi
  done <"$records_file"
  unlink "$records_file"
  cleanup_documented_selector_test_binary

  if [ "$decode_error" = true ]; then
    printf '%s\n' \
      'verify-parser-relationship-kit: incomplete documented Rust command record' >&2
    return 1
  fi

  if [ -z "$stale" ]; then
    return 0
  fi
  printf '%s' "$stale" >&2
  printf '%s\n' \
    'verify-parser-relationship-kit: relocated Rust test commands must target ./internal/parser/rust' >&2
  return 1
}
