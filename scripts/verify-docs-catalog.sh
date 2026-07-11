#!/usr/bin/env bash
#
# verify-docs-catalog.sh - validate the first GetEshu docs metadata subset.
#
# Requires bash >= 4.4: this script uses `declare -A` associative arrays
# (a bash 4.0+ feature) to track required pages and landing-page links. On
# macOS the default /bin/bash is 3.2.57, which lacks `declare -A` and fails
# with a cryptic "declare: -A: invalid option" deep in the script. Check the
# running bash's version before `set -u` so BASH_VERSINFO can never itself
# trip nounset, and fail with a clear message instead (#5050).
if (( BASH_VERSINFO[0] < 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] < 4) )); then
  printf '%s: requires bash >= 4.4 (running under %s); this script uses `declare -A`\n' \
    "${0##*/}" "${BASH_VERSION:-non-bash shell}" >&2
  printf '  re-run under bash >= 4.4 (e.g. /opt/homebrew/bin/bash, or `brew install bash`).\n' >&2
  exit 1
fi
set -euo pipefail

repo_root="${ESHU_DOCS_CATALOG_REPO_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
docs_root="${repo_root}/docs/public"
docs_root_real="$(cd "${docs_root}" && pwd -P)"

allowed_types=" tutorial how-to concept reference operate proof project "
required_pages=(
  "index.md"
  "start-here.md"
  "getting-started/first-successful-run.md"
  "mcp/index.md"
  "tutorials/index.md"
  "tutorials/trace-vulnerable-dependency.md"
  "tutorials/ask-from-assistant.md"
  "tutorials/index-repositories.md"
  "tutorials/deploy-kubernetes.md"
  "tutorials/debug-stale-answers.md"
  "use/index.md"
  "use/code-questions.md"
  "use/index-repositories.md"
  "use/trace-infrastructure.md"
  "concepts/how-it-works.md"
  "understand/index.md"
  "reference/index.md"
  "reference/contracts.md"
  "reference/proof-and-validation.md"
  "operate/index.md"
  "operate/health-checks.md"
  "operate/freshness-convergence.md"
  "operate/troubleshooting.md"
)

log() {
  printf 'docs-catalog: %s\n' "$*" >&2
}

trim() {
  local value="$*"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "${value}"
}

has_metadata() {
  rg -q '^<!-- docs-catalog$' "$1"
}

metadata_block_terminated() {
  awk '
    /^<!-- docs-catalog$/ { in_block = 1; next }
    in_block && /^-->$/ { found = 1; exit }
    END { exit found ? 0 : 1 }
  ' "$1"
}

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

normalize_link() {
  local landing_rel="$1"
  local target="$2"
  target="${target%%#*}"
  target="${target%%\?*}"
  target="$(trim "${target}")"

  case "${target}" in
    "" | "#"* | http://* | https://* | mailto:* | tel:* | javascript:*)
      return 1
      ;;
  esac

  local landing_dir
  landing_dir="$(dirname "${landing_rel}")"
  [[ "${landing_dir}" == "." ]] && landing_dir=""

  local raw_base="${target#/}"
  local candidates=()
  if [[ "${raw_base}" == *.md ]]; then
    candidates+=("${raw_base}")
  elif [[ "${raw_base}" == */ ]]; then
    candidates+=("${raw_base%/}.md" "${raw_base}index.md")
  else
    candidates+=("${raw_base}.md" "${raw_base}/index.md")
  fi

  local candidate raw dir full_dir full rel
  for candidate in "${candidates[@]}"; do
    if [[ "${target}" == /* || -z "${landing_dir}" ]]; then
      raw="${candidate}"
    else
      raw="${landing_dir}/${candidate}"
    fi
    dir="$(dirname "${raw}")"
    if ! full_dir="$(cd "${docs_root}/${dir}" 2>/dev/null && pwd -P)"; then
      continue
    fi
    full="${full_dir}/$(basename "${raw}")"
    [[ -f "${full}" ]] || continue
    rel="${full#"${docs_root_real}/"}"
    printf '%s\n' "${rel}"
    return 0
  done

  return 1
}

declare -A required=()
declare -A landing_links=()
for page in "${required_pages[@]}"; do
  required["${page}"]=1
done

failures=0

validate_metadata_file() {
  local rel="$1"
  local file="${docs_root}/${rel}"
  local title description type audience entrypoint landing

  if ! metadata_block_terminated "${file}"; then
    log "${rel}: docs-catalog block is missing closing -->"
    failures=$((failures + 1))
    return
  fi

  for key in title description type audience entrypoint; do
    if [[ -z "$(metadata_value "${file}" "${key}")" ]]; then
      log "${rel}: missing docs-catalog ${key}"
      failures=$((failures + 1))
    fi
  done

  title="$(trim "$(metadata_value "${file}" title)")"
  description="$(trim "$(metadata_value "${file}" description)")"
  type="$(trim "$(metadata_value "${file}" type)")"
  audience="$(trim "$(metadata_value "${file}" audience)")"
  entrypoint="$(trim "$(metadata_value "${file}" entrypoint)")"
  landing="$(trim "$(metadata_value "${file}" landing)")"

  if [[ -n "${type}" && "${allowed_types}" != *" ${type} "* ]]; then
    log "${rel}: invalid docs-catalog type ${type}"
    failures=$((failures + 1))
  fi
  if [[ -n "${entrypoint}" && "${entrypoint}" != "true" && "${entrypoint}" != "false" ]]; then
    log "${rel}: entrypoint must be true or false"
    failures=$((failures + 1))
  fi
  if [[ -n "${landing}" && "${landing}" != "true" && "${landing}" != "false" ]]; then
    log "${rel}: landing must be true or false"
    failures=$((failures + 1))
  fi
  if [[ -z "${title}" || -z "${description}" || -z "${audience}" ]]; then
    return
  fi
}

collect_landing_links() {
  local landing_rel="$1"
  local landing_file="${docs_root}/${landing_rel}"
  local token target normalized

  while IFS= read -r token; do
    target=""
    if [[ "${token}" == href=\"* ]]; then
      target="${token#href=\"}"
      target="${target%\"}"
    elif [[ "${token}" == \]\(* ]]; then
      target="${token#](}"
      target="${target%)}"
    fi
    if normalized="$(normalize_link "${landing_rel}" "${target}")"; then
      landing_links["${normalized}"]=1
    fi
  done < <(rg -o 'href="[^"]+"|\]\([^)]+\)' "${landing_file}" || true)
}

while IFS= read -r rel; do
  file="${docs_root}/${rel}"
  if has_metadata "${file}"; then
    validate_metadata_file "${rel}"
  fi
done < <(cd "${docs_root}" && rg --files -g '*.md')

for page in "${required_pages[@]}"; do
  file="${docs_root}/${page}"
  if [[ ! -f "${file}" ]]; then
    log "missing required docs page ${page}"
    failures=$((failures + 1))
    continue
  fi
  if ! has_metadata "${file}"; then
    log "${page}: missing required docs-catalog block"
    failures=$((failures + 1))
  fi
done

while IFS= read -r rel; do
  file="${docs_root}/${rel}"
  [[ "$(trim "$(metadata_value "${file}" landing)")" == "true" ]] || continue
  collect_landing_links "${rel}"
done < <(cd "${docs_root}" && rg --files -g '*.md')

for page in "${required_pages[@]}"; do
  file="${docs_root}/${page}"
  [[ -f "${file}" ]] || continue
  [[ "$(trim "$(metadata_value "${file}" entrypoint)")" == "true" ]] || continue
  if [[ -z "${landing_links[${page}]:-}" ]]; then
    log "${page}: entrypoint is not reachable from a docs-catalog landing page"
    failures=$((failures + 1))
  fi
done

if [[ "${failures}" -ne 0 ]]; then
  log "metadata check failed with ${failures} issue(s)"
  exit 1
fi

log "metadata check passed for ${#required_pages[@]} required page(s)"
