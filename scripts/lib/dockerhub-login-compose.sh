#!/usr/bin/env bash
#
# dockerhub-login-compose.sh - docker compose file resolution for
# verify-dockerhub-login.sh (#7886). Sourced, never run, after
# dockerhub-login-images.sh.
#
# A compose file reaches Docker Hub when any service in it, or in a file it
# pulls in, can: by an image: ref whose host is Docker Hub, or by a build: whose
# Dockerfile has a Docker Hub FROM. A service that extends another service
# (extends: {file, service} or extends: <service>) takes the image or build the
# base defines unless it overrides it, so the base is followed, file and
# service. A top-level include: (a path string, a list, or a map with path:) is
# followed file by file. Anything that cannot be resolved counts as Docker Hub:
# a missing file or service, a parse error, a build context that is a URL or
# leaves the repo, an inline Dockerfile, a cycle or a chain deeper than
# compose_max_depth.
#
# The caller defines root, tmp, mkkey, expand, image_is_hub, dockerfile_is_hub,
# compose_file_re and the E_/J_/W_/V_ tables.
#
# shellcheck disable=SC2034,SC2154  # globals are shared with the sourcing verifier

compose_max_depth=6
# One row per include (I|path) and per service
# (S|name|image|context|dockerfile|extends file|extends service|inline|has build).
# A build map without a context leaves the context empty, so an extending
# service inherits the base's.
compose_rows_expr='explode(.) as $r
| ( (($r.include // []) | .[] | ((select(tag == "!!str") | [.]), (select(tag == "!!map") | .path | ((select(tag == "!!str") | [.]), (select(tag == "!!seq") | .)))) | .[] | "I|" + .),
    (($r.services // {}) | to_entries | .[] | .key as $n | .value as $v
      | "S|" + $n + "|" + ($v.image // "") + "|"
        + (if ($v.build | tag) == "!!str" then $v.build elif ($v.build | tag) == "!!map" then ($v.build.context // "") else "" end) + "|"
        + (if ($v.build | tag) == "!!map" then ($v.build.dockerfile // "") else "" end) + "|"
        + (if ($v.extends | tag) == "!!map" then ($v.extends.file // "") else "" end) + "|"
        + (if ($v.extends | tag) == "!!str" then $v.extends elif ($v.extends | tag) == "!!map" then ($v.extends.service // "") else "" end) + "|"
        + (if ($v.build | tag) == "!!map" and $v.build.dockerfile_inline != null then "inline" else "" end) + "|"
        + (if ($v | has("build")) then "build" else "" end)) )'

# dir_join <dir> <relative path>: sets JOIN to the normalised repo-relative path
# ("." for the root) or "" when the path is absolute or leaves the repo.
dir_join() {
  local seg IFS=/
  local -a parts=()
  JOIN=""
  case "$2" in /*) return 0 ;; esac
  for seg in $1/$2; do
    case "${seg}" in
      "" | .) ;;
      ..) [[ "${#parts[@]}" -gt 0 ]] || return 0; unset "parts[$((${#parts[@]} - 1))]"; parts=("${parts[@]+"${parts[@]}"}") ;;
      *) parts+=("${seg}") ;;
    esac
  done
  if [[ "${#parts[@]}" -eq 0 ]]; then JOIN="."; else JOIN="${parts[*]}"; fi
}

# compose_rows <repo-relative file>: sets ROWS (cached per file); the text
# @PARSE_ERROR@ when yq cannot read it.
compose_rows() {
  local c out
  mkkey "$1"; c="CR_${KEY}"
  if [[ -z "${!c+x}" ]]; then
    out="$(yq e "${compose_rows_expr}" "${root}/$1" 2>/dev/null)" || out="@PARSE_ERROR@"
    printf -v "${c}" '%s' "${out}"
  fi
  ROWS="${!c}"
}

# svc_effective <file> <service> <depth>: resolves a service through extends.
# Sets EFF_FAIL (1 when it cannot be resolved), EFF_IMG, EFF_BUILD (1 when the
# service builds), EFF_CTX (repo-relative build context), EFF_DF (Dockerfile
# name inside the context) and EFF_INLINE (1 for an inline Dockerfile).
svc_effective() {
  local f="$1" s="$2" d="$3" dir found=0 bf
  local r_kind r_name r_img r_ctx r_df r_ef r_es r_inl r_build
  local bimg="" bbuild=0 bctx="" bdf="" binl=0
  EFF_FAIL=1; EFF_IMG=""; EFF_BUILD=0; EFF_CTX=""; EFF_DF=""; EFF_INLINE=0
  [[ "${d}" -le "${compose_max_depth}" ]] || return 0
  compose_rows "${f}"
  [[ "${ROWS}" != "@PARSE_ERROR@" ]] || return 0
  if [[ "${f}" == */* ]]; then dir="${f%/*}"; else dir="."; fi
  while IFS='|' read -r r_kind r_name r_img r_ctx r_df r_ef r_es r_inl r_build; do
    [[ "${r_kind}" == S && "${r_name}" == "${s}" ]] || continue
    found=1; break
  done <<<"${ROWS}"
  [[ "${found}" -eq 1 ]] || return 0
  if [[ -n "${r_es}" ]]; then
    if [[ -n "${r_ef}" ]]; then
      expand "${r_ef}" ""
      [[ "${EXP}" != *@UNRESOLVED@* && "${EXP}" != *'$'* ]] || return 0
      dir_join "${dir}" "${EXP}"; bf="${JOIN}"
    else
      bf="${f}"
    fi
    [[ -n "${bf}" && -f "${root}/${bf}" ]] || return 0
    svc_effective "${bf}" "${r_es}" $((d + 1))
    [[ "${EFF_FAIL}" -eq 0 ]] || return 0
    bimg="${EFF_IMG}"; bbuild="${EFF_BUILD}"; bctx="${EFF_CTX}"; bdf="${EFF_DF}"; binl="${EFF_INLINE}"
  fi
  EFF_FAIL=1
  EFF_IMG="${r_img:-${bimg}}"
  EFF_INLINE="${binl}"
  if [[ -n "${r_build}" ]]; then
    EFF_BUILD=1
    if [[ -n "${r_ctx}" ]]; then
      expand "${r_ctx}" ""
      case "${EXP}" in *@UNRESOLVED@* | *'$'* | *://* | git@* | github.com/*) return 0 ;; esac
      dir_join "${dir}" "${EXP}"; EFF_CTX="${JOIN}"
    elif [[ "${bbuild}" -eq 1 ]]; then
      EFF_CTX="${bctx}"
    else
      dir_join "${dir}" "."; EFF_CTX="${JOIN}"
    fi
    [[ -n "${EFF_CTX}" ]] || return 0
    EFF_DF="${r_df:-${bdf:-Dockerfile}}"
    [[ -z "${r_inl}" ]] || EFF_INLINE=1
  else
    EFF_BUILD="${bbuild}"; EFF_CTX="${bctx}"; EFF_DF="${bdf}"
  fi
  EFF_FAIL=0
}

# compose_is_hub <repo-relative compose file> [<depth>]: returns 0 when any
# service, here or in an included file, can pull from Docker Hub or cannot be
# resolved; 1 when every service is provably non-Hub.
compose_is_hub() {
  local f="$1" d="${2:-0}" dir rows
  local r_kind r_name r_img r_ctx r_df r_ef r_es r_inl r_build
  [[ "${d}" -le "${compose_max_depth}" && -f "${root}/${f}" ]] || return 0
  compose_rows "${f}"
  rows="${ROWS}"
  [[ "${rows}" != "@PARSE_ERROR@" ]] || return 0
  if [[ "${f}" == */* ]]; then dir="${f%/*}"; else dir="."; fi
  while IFS='|' read -r r_kind r_name r_img r_ctx r_df r_ef r_es r_inl r_build; do
    case "${r_kind}" in
      I)
        expand "${r_name}" ""
        [[ "${EXP}" != *@UNRESOLVED@* && "${EXP}" != *'$'* ]] || return 0
        dir_join "${dir}" "${EXP}"
        [[ -n "${JOIN}" ]] || return 0
        if compose_is_hub "${JOIN}" $((d + 1)); then return 0; fi ;;
      S)
        svc_effective "${f}" "${r_name}" 0
        [[ "${EFF_FAIL}" -eq 0 ]] || return 0
        if [[ -n "${EFF_IMG}" ]] && image_is_hub "${EFF_IMG}"; then return 0; fi
        if [[ "${EFF_BUILD}" -eq 1 ]]; then
          [[ "${EFF_INLINE}" -eq 0 ]] || return 0
          expand "${EFF_DF}" ""
          [[ "${EXP}" != *@UNRESOLVED@* && "${EXP}" != *'$'* ]] || return 0
          dir_join "${EFF_CTX}" "${EXP}"
          [[ -n "${JOIN}" ]] || return 0
          if dockerfile_is_hub "${JOIN}"; then return 0; fi
        fi ;;
    esac
  done <<<"${rows}"
  return 1
}

# compose_step_is_hub <run file> <closure files>: the step reaches docker
# compose. Exempt only when it names at least one compose file and every named
# file derives non-Hub; a step that names none cannot be derived.
compose_step_is_hub() {
  local tok cf found=0 f
  local -a texts=("$1")
  while IFS= read -r f; do [[ -z "${f}" ]] || texts+=("${root}/${f}"); done <<<"$2"
  while IFS= read -r tok; do
    tok="${tok#./}"; tok="${tok#/}"; cf=""
    if [[ -f "${root}/${tok}" ]]; then cf="${tok}"; elif [[ -f "${root}/${tok##*/}" ]]; then cf="${tok##*/}"; fi
    [[ -n "${cf}" ]] || continue
    found=1
    if compose_is_hub "${cf}"; then return 0; fi
  done < <({ rg -o -N --no-filename -e "${compose_file_re}" -- "${texts[@]}" || true; } | sort -u)
  [[ "${found}" -eq 1 ]] || return 0
  return 1
}
