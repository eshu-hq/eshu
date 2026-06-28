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
  git fetch --no-tags --depth=50 origin "${base_name}" >/dev/null 2>&1 || true
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

log "running mkdocs build (${#changed_files[@]} trigger files changed)"
if ! eval "${docs_cmd}"; then
  log ""
  log "mkdocs build FAILED. Fix the docs errors above and re-run."
  log ""
  log "The exact docs command is:"
  log "  ${docs_cmd}"
  exit 1
fi

log "docs build passed"
