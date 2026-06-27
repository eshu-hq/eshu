#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
cd "${repo_root}"

log() {
  printf 'format: %s\n' "$*" >&2
}

mode="${ESHU_FORMAT_MODE:-branch}"
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
    log "unknown ESHU_FORMAT_MODE: ${mode}"
    exit 2
    ;;
esac

commit_exists() {
  git rev-parse --verify --quiet "$1^{commit}" >/dev/null
}

maybe_fetch_base_ref() {
  local base_name="$1"
  [[ -z "${base_name}" ]] && return 0
  git fetch --no-tags --depth=50 origin "${base_name}" >/dev/null 2>&1 || true
}

base_ref_from_env() {
  if [[ -n "${ESHU_FORMAT_BASE_REF:-}" ]]; then
    printf '%s\n' "${ESHU_FORMAT_BASE_REF}"
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
      log "using merge-base ${merge_base} for ${base_ref}..HEAD"
      printf '%s\n' "${merge_base}"
      return 0
    fi

    log "no merge base between ${base_ref} and HEAD; falling back to two-dot diff"
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

is_frontend_js_ts() {
  local file_path="$1"
  case "${file_path}" in
    src/* | apps/console/src/*) ;;
    *) return 1 ;;
  esac

  case "${file_path}" in
    *.ts | *.tsx | *.js | *.jsx | *.mjs | *.cjs) return 0 ;;
    *) return 1 ;;
  esac
}

changed_files=()
file_label="changed"
if [[ "${mode}" == "staged" ]]; then
  file_label="staged"
  while IFS= read -r -d '' file_path; do
    if is_frontend_js_ts "${file_path}"; then
      changed_files+=("${file_path}")
    fi
  done < <(git diff --cached --name-only -z --diff-filter=ACMR -- src apps/console/src)
else
  base_ref="$(base_ref_from_env)"
  diff_left="$(resolve_diff_left "${base_ref}")"

  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    {
      printf 'base_ref=%s\n' "${base_ref}"
      printf 'diff_left=%s\n' "${diff_left}"
    } >>"${GITHUB_OUTPUT}"
  fi

  while IFS= read -r -d '' file_path; do
    if is_frontend_js_ts "${file_path}"; then
      changed_files+=("${file_path}")
    fi
  done < <(git diff --name-only -z --diff-filter=ACMR "${diff_left}" HEAD -- src apps/console/src)
fi

log "${#changed_files[@]} ${file_label} JS/TS files"
if [[ "${#changed_files[@]}" -eq 0 ]]; then
  if [[ "${mode}" == "staged" ]]; then
    log "no staged JS/TS files; skipping prettier check"
  else
    log "no changed JS/TS files in this PR; skipping prettier check"
  fi
  exit 0
fi

prettier_cmd=()
if [[ -n "${ESHU_PRETTIER_BIN:-}" ]]; then
  prettier_cmd=("${ESHU_PRETTIER_BIN}")
elif [[ -d "${repo_root}/node_modules/prettier" ]]; then
  prettier_cmd=(npx --no-install prettier)
else
  log "node_modules/prettier is missing; run 'npm ci' before this gate"
  exit 1
fi

log "checking prettier on ${#changed_files[@]} ${file_label} files"
"${prettier_cmd[@]}" --check "${changed_files[@]}"
