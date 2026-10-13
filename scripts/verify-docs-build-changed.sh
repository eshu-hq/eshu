#!/usr/bin/env bash
#
# verify-docs-build-changed.sh — run mkdocs build --strict only when docs-,
# navigation-, or project-guidance files have changed.
#
# Default mode is branch diff (vs origin/main). Pass --staged to check only
# staged files (pre-commit hook) or --branch for diff mode (pre-push / manual).
#
# Usage:
#   bash scripts/verify-docs-build-changed.sh          # branch mode (default)
#   bash scripts/verify-docs-build-changed.sh --staged # staged mode
#   bash scripts/verify-docs-build-changed.sh --branch # explicit branch mode
#
# Exits 0 if no relevant files changed or if mkdocs build passes.
# Exits non-zero if mkdocs fails or a required tool is missing.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"

log() {
  printf 'docs: %s\n' "$*" >&2
}

mode="${ESHU_VERIFY_DOCS_MODE:-branch}"
for arg in "$@"; do
  case "${arg}" in
    --staged) mode="staged" ;;
    --branch) mode="branch" ;;
    *)
      log "unknown argument: ${arg}"
      exit 2
      ;;
  esac
done

case "${mode}" in
  branch | staged) ;;
  *)
    log "unknown ESHU_VERIFY_DOCS_MODE: ${mode}"
    exit 2
    ;;
esac

# ── Trigger patterns ──────────────────────────────────────────────────
# Files whose changes should trigger a docs build.
is_docs_trigger() {
  local path="$1"
  case "${path}" in
    # docs/ tree
    docs/*) return 0 ;;
    # Root guidance files
    README.md | AGENTS.md | CLAUDE.md) return 0 ;;
    # Project guidance / agent skill files
    .opencode/agent/*.md) return 0 ;;
    .agents/skills/*.md) return 0 ;;
    .agents/skills/*/*.md) return 0 ;;
    # The mkdocs config itself
    docs/mkdocs.yml) return 0 ;;
    # The verifier scripts (self-test when they change)
    scripts/verify-docs-build-changed.sh | scripts/test-verify-docs-build-changed.sh) return 0 ;;
    # The pre-commit config that wires this hook
    .pre-commit-config.yaml) return 0 ;;
    *) return 1 ;;
  esac
}

# ── Git helpers ───────────────────────────────────────────────────────

commit_exists() {
  git rev-parse --verify --quiet "$1^{commit}" >/dev/null
}

maybe_fetch_base_ref() {
  local base_name="$1"
  [[ -z "${base_name}" ]] && return 0
  # Never change the depth of a complete clone: every worktree shares one
  # .git/shallow, so a --depth fetch from here shallows all of them (#8032).
  if commit_exists "origin/${base_name}" || commit_exists "${base_name}"; then
    return 0
  fi
  if [[ "$(git rev-parse --is-shallow-repository 2>/dev/null)" == "false" ]]; then
    git fetch --no-tags origin "${base_name}" >/dev/null 2>&1 || true
  else
    # Shallow checkout (CI fetch-depth): keep the bounded depth fetch.
    git fetch --no-tags --depth=50 origin "${base_name}" >/dev/null 2>&1 || true
  fi
}

base_ref_from_env() {
  if [[ -n "${ESHU_VERIFY_DOCS_BASE_REF:-}" ]]; then
    printf '%s\n' "${ESHU_VERIFY_DOCS_BASE_REF}"
    return 0
  fi

  if [[ "${GITHUB_EVENT_NAME:-}" == "push" ]] && [[ -z "${GITHUB_BASE_REF:-}" ]]; then
    local push_ref_name="${GITHUB_REF_NAME:-${GITHUB_REF:-}}"
    push_ref_name="${push_ref_name#refs/heads/}"
    case "${push_ref_name}" in
      main)
        printf '%s\n' "HEAD~1"
        return 0
        ;;
    esac
  fi

  if [[ -n "${GITHUB_BASE_SHA:-}" ]] && commit_exists "${GITHUB_BASE_SHA}"; then
    printf '%s\n' "${GITHUB_BASE_SHA}"
    return 0
  fi

  if [[ -n "${GITHUB_BASE_REF:-}" ]]; then
    local base_name="${GITHUB_BASE_REF#refs/heads/}"
    maybe_fetch_base_ref "${base_name}"
    if commit_exists "origin/${base_name}"; then
      printf '%s\n' "origin/${base_name}"
      return 0
    fi
    printf '%s\n' "${GITHUB_BASE_REF}"
    return 0
  fi

  printf '%s\n' "origin/main"
}

resolve_diff_left() {
  local base_ref="$1"
  local merge_base=""

  if commit_exists "${base_ref}"; then
    if merge_base="$(git merge-base "${base_ref}" HEAD 2>/dev/null)"; then
      printf '%s\n' "${merge_base}"
      return 0
    fi
    # No merge base (orphan or unrelated history). Use two-dot diff.
    printf '%s\n' "${base_ref}"
    return 0
  fi

  if commit_exists "HEAD~1"; then
    log "base ref ${base_ref} is unavailable; falling back to HEAD~1"
    printf '%s\n' "HEAD~1"
    return 0
  fi

  log "base ref ${base_ref} is unavailable and HEAD has no parent; using empty tree"
  git hash-object -t tree /dev/null
}

# ── Changed-file detection ────────────────────────────────────────────

changed_files=()
file_label="changed"

if [[ "${mode}" == "staged" ]]; then
  file_label="staged"
  while IFS= read -r -d '' file_path; do
    if is_docs_trigger "${file_path}"; then
      changed_files+=("${file_path}")
    fi
  done < <(git diff --cached --name-only -z --diff-filter=ACMRD)
else
  base_ref="$(base_ref_from_env)"
  diff_left="$(resolve_diff_left "${base_ref}")"

  while IFS= read -r -d '' file_path; do
    if is_docs_trigger "${file_path}"; then
      changed_files+=("${file_path}")
    fi
  done < <(git diff --name-only -z --diff-filter=ACMRD "${diff_left}" HEAD)
fi

log "${#changed_files[@]} ${file_label} docs/navigation/project-guidance files"
if [[ "${#changed_files[@]}" -eq 0 ]]; then
  if [[ "${mode}" == "staged" ]]; then
    log "no staged docs/navigation/project-guidance files; skipping docs build"
  else
    log "no docs/navigation/project-guidance changes in this branch; skipping docs build"
  fi
  exit 0
fi

# ── Docs build ──────────────────────────────────────────────────────────

docs_cmd="uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file ${repo_root}/docs/mkdocs.yml"

if ! command -v uv &>/dev/null; then
  log "uv is required for the docs build but is not in PATH."
  log ""
  log "Install it with: curl -LsSf https://astral.sh/uv/install.sh | sh"
  log "  or: brew install uv"
  log ""
  log "Then re-run, or run the docs build manually:"
  log "  ${docs_cmd}"
  exit 1
fi

# ── Build timeout ─────────────────────────────────────────────────────
# A push-time hook that runs for minutes with no output reads as a hung push:
# pre-commit captures a passing hook's output until the hook exits, so git
# prints nothing while `uv run` resolves dependencies and mkdocs builds the
# whole tree. Issue #7983: three pushes stalled silently this way while another
# worktree ran a live gate. Bound the step and fail LOUD past the bound: a
# timeout still blocks the push, so the `make pre-push` floor is not weakened
# by this bound — it only replaces a silent stall with a named one.
docs_timeout_default=600
docs_timeout_raw="${ESHU_DOCS_BUILD_TIMEOUT_SECONDS:-${docs_timeout_default}}"
# Width cap first: past int64 range bash arithmetic wraps mod 2^64, and a
# wrapped value landing in range would pass validation (#7983 review). Five
# or more digits exceed 3600 anyway, so `^[0-9]{1,4}$` loses nothing.
if [[ ! "${docs_timeout_raw}" =~ ^[0-9]{1,4}$ ]] || (( 10#${docs_timeout_raw} < 1 || 10#${docs_timeout_raw} > 3600 )); then
  log "ESHU_DOCS_BUILD_TIMEOUT_SECONDS must be an integer from 1 through 3600 seconds; got: ${docs_timeout_raw}"
  exit 2
fi
docs_timeout="${docs_timeout_raw}"

# Bounded, process-group-aware execution for the build step. The old backend
# chain was platform-split: GNU `timeout` kills the child process group, but
# the perl `alarm shift; exec` fallback signalled only the exec'd `uv`,
# orphaning its resolver/mkdocs grandchildren past the bound (observed live
# in RED probe /tmp/redprobe2-7983.sh: the sleep grandchild survived the
# 2s bound under the perl path, #7983 review). Those orphans kept burning
# CPU under the contention being diagnosed and could race the documented
# rerun with concurrent `site/` writes. Monitor mode puts the background job
# in its own process group with shell builtins alone (no external timing
# dependency, and the same code path runs on Linux and macOS), so killing
# the group reaps the whole tree on every platform; a platform that refuses
# the group kill still gets the leader kill fallback below. Returns 124 on
# timeout. `set -m` is shell-global, so every return path restores
# `set +m` explicitly.
run_bounded_tree() {
  local budget="$1"
  shift
  set -m
  "$@" &
  local child=$!
  local waited=0
  while kill -0 "${child}" 2>/dev/null; do
    if (( waited >= budget )); then
      kill -KILL -- "-${child}" 2>/dev/null || true
      kill -KILL "${child}" 2>/dev/null || true
      wait "${child}" 2>/dev/null || true
      set +m
      return 124
    fi
    sleep 1
    waited=$((waited + 1))
  done
  # Guarded so `set -e` callers cannot skip the `set +m` restore below.
  local status=0
  wait "${child}" || status=$?
  set +m
  return "${status}"
}

# Best-effort contention report for the timeout message. Never fails: every
# read is guarded and the function always returns 0, so a broken diagnostic
# cannot mask the timeout it explains. ESHU_LIVE_GATE_LOCK_DIR is the same
# relocation the lock lib itself honours for tests, so cases can plant a
# holder without touching the real lock under .git.
describe_push_contention() {
  local common lock_target holder holder_pid holder_where load
  # The override names the lock DIRECTORY, exactly like the lock lib itself:
  # the candidate is always ${lock_home}/eshu-live-gate.lock.
  if [[ -n "${ESHU_LIVE_GATE_LOCK_DIR:-}" ]]; then
    lock_target="${ESHU_LIVE_GATE_LOCK_DIR}/eshu-live-gate.lock"
  else
    common="$(git -C "${repo_root}" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || git -C "${repo_root}" rev-parse --git-common-dir 2>/dev/null || true)"
    lock_target="${common:+${common}/eshu-live-gate.lock}"
  fi
  if [[ -z "${lock_target:-}" ]]; then
    log "could not locate the live-gate lock; holder ownership unknown."
  elif [[ -L "${lock_target}" ]]; then
    holder="$(readlink "${lock_target}" 2>/dev/null || true)"
    if [[ -n "${holder}" ]]; then
      holder_pid="${holder%%:*}"
      holder_where="${holder#*:}"
      holder_where="${holder_where#*:}"
      log "live-gate lock is HELD while this hook timed out: pid ${holder_pid}, worktree '${holder_where}' (raw payload '${holder}')."
      log "A live gate binds the fixed host ports and starves this build of CPU and Docker I/O; wait for that run to finish, or stop it, then push again."
    else
      log "live-gate lock path exists but carries no readable holder; ownership unknown."
    fi
  else
    log "live-gate lock is not held: no live gate is serializing this host right now."
  fi
  load="$(uptime 2>/dev/null || echo unavailable)"
  log "host load at timeout: ${load}"
  return 0
}

# Array form executes without eval so the watchdog supervises the real child
# process group. docs_cmd keeps its string form for the failure message
# below, which pins the exact command.
docs_argv=(uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file "${repo_root}/docs/mkdocs.yml")
log "running mkdocs build (${#changed_files[@]} trigger files changed; bound ${docs_timeout}s)"
build_status=0
run_bounded_tree "${docs_timeout}" "${docs_argv[@]}" || build_status=$?
# 124 is the watchdog's own timeout status. A build that genuinely exits 124
# reads as a timeout; mkdocs exits 0/1, so that misread is not a live shape
# today.
if [[ "${build_status}" -eq 124 ]]; then
  log ""
  log "mkdocs build TIMED OUT after ${docs_timeout}s (this step covers uv dependency resolution plus mkdocs build --strict)."
  log "The push stays blocked, but loudly: an unbounded silent stall is the #7983 failure this bound replaces."
  describe_push_contention
  log ""
  log "Rerun the exact docs command, or raise the bound for one push:"
  log "  ${docs_cmd}"
  log "  ESHU_DOCS_BUILD_TIMEOUT_SECONDS=<seconds> git push"
  exit 1
fi
if [[ "${build_status}" -ne 0 ]]; then
  log ""
  log "mkdocs build FAILED. Fix the docs errors above and re-run."
  log ""
  log "The exact docs command is:"
  log "  ${docs_cmd}"
  exit 1
fi

log "docs build passed"
