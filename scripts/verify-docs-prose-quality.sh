#!/usr/bin/env bash
#
# verify-docs-prose-quality.sh - advisory prose-quality gate for human-facing
# docs-catalog pages.
set -euo pipefail

repo_root="${ESHU_DOCS_PROSE_REPO_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
docs_root="${repo_root}/docs/public"
enforce="${DOCS_PROSE_ENFORCE:-false}"

failures=0
checked=0

log() {
  printf 'docs-prose: %s\n' "$*" >&2
}

die() {
  log "$*"
  exit 1
}

command -v rg >/dev/null 2>&1 || die "missing required tool: rg"
command -v awk >/dev/null 2>&1 || die "missing required tool: awk"
[[ -d "${docs_root}" ]] || die "docs root not found: ${docs_root}"

metadata_value() {
  local file="$1"
  local key="$2"
  awk -v wanted="${key}" '
    /^<!-- docs-catalog$/ { in_block = 1; next }
    in_block && /^-->$/ { exit }
    in_block {
      split($0, parts, ":")
      if (parts[1] == wanted) {
        sub("^[^:]*:[[:space:]]*", "")
        print
        exit
      }
    }
  ' "${file}"
}

is_generated_doc() {
  rg -q -i '^<!--[[:space:]]*(generated|.*generated from|.*do not edit by hand)' "$1"
}

is_human_type() {
  case "$1" in
    concept | how-to | operate | project | tutorial)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

max_lines_for_type() {
  case "$1" in
    concept)
      printf '180'
      ;;
    how-to | operate | tutorial)
      printf '220'
      ;;
    project)
      printf '260'
      ;;
    *)
      printf '200'
      ;;
  esac
}

report() {
  local rel="$1"
  local line="$2"
  local rule="$3"
  local message="$4"
  failures=$((failures + 1))
  log "${rel}:${line}: ${rule}: ${message}"
}

check_h1() {
  local rel="$1"
  local file="$2"
  local count
  count="$(rg -n '^# ' "${file}" || true)"
  count="$(printf '%s\n' "${count}" | awk 'NF { n++ } END { print n + 0 }')"
  if [[ "${count}" -ne 1 ]]; then
    report "${rel}" 1 "one-purpose" "human-facing docs should have exactly one H1"
  fi
}

check_length() {
  local rel="$1"
  local file="$2"
  local type="$3"
  local max_lines
  local lines
  max_lines="$(max_lines_for_type "${type}")"
  lines="$(awk 'END { print NR + 0 }' "${file}")"
  if [[ "${lines}" -gt "${max_lines}" ]]; then
    report "${rel}" 1 "page-length" "${type} page has ${lines} lines; guidance is ${max_lines} or fewer"
  fi
}

check_banned_filler() {
  local rel="$1"
  local file="$2"
  local matches
  matches="$(rg -n -i '\b(cutting-edge|easy to use|game-changing|leverage|powerful|robust|seamless|unlock|world-class)\b' "${file}" || true)"
  if [[ -n "${matches}" ]]; then
    # Process substitution + printf '%s\n', not a `<<<` here-string: bash
    # 5.1+ writes the whole here-string body to a pipe before forking the
    # reader, and a body strictly between 512 bytes and macOS's pipe-buffer
    # ceiling deadlocks under that bash (#5098; same class as #4718/#5074).
    # printf '%s\n' restores the trailing newline `<<<` would have added, so
    # the last match line is still read after matches was captured via
    # command substitution (which strips it).
    while IFS= read -r match; do
      [[ -z "${match}" ]] && continue
      report "${rel}" "${match%%:*}" "banned-filler" "replace vague launch prose with concrete task language"
    done < <(printf '%s\n' "${matches}")
  fi
}

check_command_formatting() {
  local rel="$1"
  local file="$2"
  local prompt_matches
  local bare_fences
  prompt_matches="$(rg -n '^[[:space:]]*\$ ' "${file}" || true)"
  if [[ -n "${prompt_matches}" ]]; then
    # See the comment in check_banned_filler: process substitution +
    # printf '%s\n' avoids the bash 5.1+ here-string pipe deadlock (#5098).
    while IFS= read -r match; do
      [[ -z "${match}" ]] && continue
      report "${rel}" "${match%%:*}" "prompt-prefix" "commands should omit shell prompts and live in bash fences"
    done < <(printf '%s\n' "${prompt_matches}")
  fi

  bare_fences="$(
    awk '
      /^```/ {
        if (!in_code && $0 == "```") {
          print FNR ":bare"
        }
        in_code = !in_code
      }
    ' "${file}"
  )"
  if [[ -n "${bare_fences}" ]]; then
    # See the comment in check_banned_filler: process substitution +
    # printf '%s\n' avoids the bash 5.1+ here-string pipe deadlock (#5098).
    while IFS= read -r match; do
      [[ -z "${match}" ]] && continue
      report "${rel}" "${match%%:*}" "code-fence-language" "code fences should name a language such as bash, text, json, or yaml"
    done < <(printf '%s\n' "${bare_fences}")
  fi
}

check_readability() {
  local rel="$1"
  local file="$2"
  local type="$3"
  local matches
  case "${type}" in
    tutorial | how-to)
      ;;
    *)
      return
      ;;
  esac

  matches="$(
    awk '
      /^```/ { in_code = !in_code; next }
      in_code { next }
      /^[[:space:]]*$/ { next }
      /^<!--/ { next }
      /^#/ { next }
      {
        count = 0
        n = split($0, words, /[^[:alnum:]_'\''-]+/)
        for (i = 1; i <= n; i++) {
          if (words[i] != "") count++
        }
        if (count > 35) {
          printf "%d:%d\n", FNR, count
        }
      }
    ' "${file}"
  )"

  if [[ -n "${matches}" ]]; then
    # See the comment in check_banned_filler: process substitution +
    # printf '%s\n' avoids the bash 5.1+ here-string pipe deadlock (#5098).
    while IFS=: read -r line words; do
      [[ -z "${line}" ]] && continue
      report "${rel}" "${line}" "long-line" "tutorial/how-to prose line has ${words} words; split dense guidance"
    done < <(printf '%s\n' "${matches}")
  fi
}

while IFS= read -r rel; do
  file="${docs_root}/${rel}"
  type="$(metadata_value "${file}" type)"
  if ! is_human_type "${type}"; then
    continue
  fi
  if is_generated_doc "${file}"; then
    continue
  fi

  checked=$((checked + 1))
  check_h1 "${rel}" "${file}"
  check_length "${rel}" "${file}" "${type}"
  check_banned_filler "${rel}" "${file}"
  check_command_formatting "${rel}" "${file}"
  check_readability "${rel}" "${file}" "${type}"
done < <(cd "${docs_root}" && rg --files -g '*.md' | LC_ALL=C sort)

if [[ "${failures}" -gt 0 ]]; then
  log "${failures} prose-quality finding(s) across ${checked} human-facing docs"
  if [[ "${enforce}" == "true" ]]; then
    exit 1
  fi
  log "ADVISORY (DOCS_PROSE_ENFORCE!=true) - not failing"
  exit 0
fi

log "checked ${checked} human-facing docs; no prose-quality findings"
