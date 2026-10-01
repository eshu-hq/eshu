#!/usr/bin/env bash

# Fixture git for the parser-relationship-kit self-test (#7229).
#
# Sourced by test-verify-parser-relationship-kit.sh before any fixture so
# every scratch-repo git command below runs through fixture_git. Two risks
# motivate the wrapper, and both have bitten macOS runs:
#
# 1. Operator config inheritance. git reads ~/.gitconfig, /etc/gitconfig,
#    conditional includes, and the GIT_CONFIG_COUNT/ GIT_CONFIG_PARAMETERS
#    families for EVERY command, and any of them can change what a fixture
#    stages (clean/smudge filters, autocrlf, excludes) or whether its
#    commit runs at all (hooksPath, gpgsign pinentry). golden-corpus-git.sh
#    switches the whole global/system layer off per command for exactly
#    this reason; fixture_git mirrors that set verbatim below (keep in
#    sync with scripts/lib/golden-corpus-git.sh). The local
#    .git/config that init_repo writes stays in effect.
# 2. Wedged git. A commit that waits (a hook, a prompt, a daemon) blocks
#    promotion until an outer timeout. with_timeout bounds every fixture
#    git call so a future hang fails fast naming the operation.
#
# Bash 3.2 compatible (the script also runs under /bin/bash): no wait -n,
# no associative arrays, no mapfile.

# with_timeout SECS LABEL CMD... — portable supervisor. Runs CMD in the
# background and kills it after SECS (TERM, then KILL after 5s), printing
# a diagnostic that names LABEL. Returns 124 on timeout (GNU timeout
# convention), else CMD's status. A sentinel file distinguishes our kill
# from CMD dying by signal on its own.
with_timeout() {
  local secs label sentinel rc pid watcher
  secs="$1"
  label="$2"
  shift 2
  sentinel="$(mktemp "${TMPDIR:-/tmp}/eshu-with-timeout.XXXXXX")"
  rm -f "$sentinel"
  # stdin from /dev/null so a prompting command fails fast instead of
  # reading the script's stdin; stdout stays so CMD output is preserved
  # (it closes on CMD exit and cannot stall). The watcher below gets
  # neither: its sleep must not inherit a command substitution's capture
  # pipe, or every $(init_repo) would stall until the orphaned sleep
  # exits. Its diagnostic stays on stderr.
  "$@" </dev/null &
  pid=$!
  (
    sleep "$secs"
    if kill -0 "$pid" 2>/dev/null; then
      : >"$sentinel"
      printf 'with_timeout: timed out after %ss: %s\n' "$secs" "$label" >&2
      kill -TERM "$pid" 2>/dev/null || true
      sleep 5
      kill -KILL "$pid" 2>/dev/null || true
    fi
  ) </dev/null >/dev/null &
  watcher=$!
  if wait "$pid"; then
    rc=0
  else
    rc=$?
  fi
  kill "$watcher" 2>/dev/null || true
  wait "$watcher" 2>/dev/null || true
  if [ -e "$sentinel" ]; then
    rc=124
  fi
  rm -f "$sentinel"
  return "$rc"
}

# fixture_git LABEL GITARGS... — the only way this self-test runs git
# against a scratch fixture repo: golden-corpus-git.sh isolation applied
# per command inside a subshell (so the parent environment is untouched),
# plus the hang watchdog. LABEL names the operation for timeout output.
fixture_git() {
  local label timeout_secs
  label="$1"
  shift
  # A non-numeric override would fail confusingly deep inside the
  # watchdog (sleep errors, then the command is TERM/KILLed with a
  # misleading timeout diagnostic), so validate here and fail fast
  # naming the bad value.
  timeout_secs="${FIXTURE_GIT_TIMEOUT_SECS-60}"
  case "${timeout_secs}" in
    ''|*[!0-9]*)
      printf 'fixture_git %s: refusing non-numeric FIXTURE_GIT_TIMEOUT_SECS=%s\n' \
        "${label}" "${FIXTURE_GIT_TIMEOUT_SECS-(unset)}" >&2
      return 1
      ;;
  esac
  if [ "${timeout_secs}" -le 0 ]; then
    printf 'fixture_git %s: refusing non-positive FIXTURE_GIT_TIMEOUT_SECS=%s\n' \
      "${label}" "${timeout_secs}" >&2
    return 1
  fi
  (
    export GIT_CONFIG_GLOBAL=/dev/null
    export GIT_CONFIG_SYSTEM=/dev/null
    export GIT_ATTR_NOSYSTEM=1
    unset GIT_CONFIG_COUNT GIT_CONFIG_PARAMETERS \
      GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_COMMON_DIR \
      GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES \
      GIT_DEFAULT_HASH GIT_TEMPLATE_DIR GIT_ATTR_SOURCE \
      GIT_NAMESPACE GIT_CEILING_DIRECTORIES
    with_timeout "${timeout_secs}" "$label" \
      git -c core.attributesFile=/dev/null "$@"
  )
}

# fixture_add_commit DIR MSG — stage everything and commit with MSG, the
# exact operation init_repo and every second-commit pair perform.
fixture_add_commit() {
  local dir msg
  dir="$1"
  msg="$2"
  fixture_git "stage ${dir}" -C "${dir}" add .
  fixture_git "commit ${dir}" -C "${dir}" commit -q -m "${msg}"
}
