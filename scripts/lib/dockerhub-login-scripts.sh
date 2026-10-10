#!/usr/bin/env bash
#
# dockerhub-login-scripts.sh - script-graph helpers for verify-dockerhub-login.sh
# (#7886). Sourced, never run, before the batch scan. It lexes shell text through
# lib/dockerhub-login-lex.awk, resolves the scripts a command names, and walks the
# transitive closure of the scripts a workflow step can reach.
#
# The caller defines: root, tmp, lex_awk, max_depth and the fact sets other_files,
# compose_files, unclass_files, var_files, dock_files and lexed_files.
#
# shellcheck disable=SC2034,SC2154  # globals are shared with the sourcing verifier

# mkkey <string>: sets KEY to an injective shell-variable-safe form.
mkkey() {
  local k="$1"
  k="${k//_/_U}"; k="${k//\//_S}"; k="${k//./_D}"; k="${k//-/_H}"
  KEY="${k//[^A-Za-z0-9_]/_X}"
}

# norm_path <path>: sets NORM to the path with . and .. collapsed, or "" when it
# escapes the repo root.
norm_path() {
  local seg IFS=/
  local -a parts=()
  NORM=""
  for seg in $1; do
    case "${seg}" in
      ""|.) ;;
      ..) [[ "${#parts[@]}" -gt 0 ]] || return 0; unset "parts[$((${#parts[@]} - 1))]"; parts=("${parts[@]+"${parts[@]}"}") ;;
      *) parts+=("${seg}") ;;
    esac
  done
  [[ "${#parts[@]}" -gt 0 ]] || return 0
  NORM="${parts[*]}"
}

# resolve_token <token> <dir>: sets RESOLVED to the repo-relative existing file a
# script token names (repo-root relative, scripts/ relative, or <dir> relative).
resolve_token() {
  local tok="$1" dir="$2" cand
  tok="${tok#./}"; tok="${tok#/}"; RESOLVED=""
  for cand in "${tok}" "scripts/${tok}" "${dir}/${tok}"; do
    cand="${cand#./}"
    # Fast path: a clean relative path needs no normalization.
    if [[ "${cand}" != *..* && "${cand}" != */./* && "${cand}" != ./* && -f "${root}/${cand}" ]]; then RESOLVED="${cand}"; return 0; fi
    norm_path "${cand}"
    if [[ -n "${NORM}" && -f "${root}/${NORM}" ]]; then RESOLVED="${NORM}"; return 0; fi
  done
  return 0
}

# lex_load <repo|run> <file>...: lexes the files in one awk run and records, per
# file, the docker facts (FO_/RO_), the unclassifiable docker mentions (FU_/RU_),
# the variable-as-command records (FV_/RV_) and the sets other_files/compose_files/
# unclass_files/var_files/dock_files (repo) or runs_other/runs_compose/
# runs_unclass/runs_var/runs_dock (run text, named by step sequence).
lex_load() {
  local mode="$1" f kind rest rel cur pfx=R
  shift
  [[ "$#" -gt 0 ]] || return 0
  [[ "${mode}" != repo ]] || pfx=F
  while IFS=$'\t' read -r f kind rest; do
    [[ -n "${f}" ]] || continue
    if [[ "${mode}" == repo ]]; then
      rel="${f#"${root}"/}"; mkkey "${rel}"
    else
      rel="${f##*/}"; KEY="${rel}"
    fi
    case "${kind}" in
      O) cur="${pfx}O_${KEY}"; printf -v "${cur}" '%s%s\n' "${!cur:-}" "${rest}"
         if [[ "${mode}" == repo ]]; then other_files="${other_files}${rel}"$'\n'; else runs_other="${runs_other}${rel}"$'\n'; fi ;;
      C) if [[ "${mode}" == repo ]]; then compose_files="${compose_files}${rel}"$'\n'; else runs_compose="${runs_compose}${rel}"$'\n'; fi ;;
      U) cur="${pfx}U_${KEY}"; printf -v "${cur}" '%s%s\n' "${!cur:-}" "${rest}"
         if [[ "${mode}" == repo ]]; then unclass_files="${unclass_files}${rel}"$'\n'; else runs_unclass="${runs_unclass}${rel}"$'\n'; fi ;;
      V) cur="${pfx}V_${KEY}"; printf -v "${cur}" '%s%s\n' "${!cur:-}" "${rest}"
         if [[ "${mode}" == repo ]]; then var_files="${var_files}${rel}"$'\n'; else runs_var="${runs_var}${rel}"$'\n'; fi ;;
      D) if [[ "${mode}" == repo ]]; then dock_files="${dock_files}${rel}"$'\n'; else runs_dock="${runs_dock}${rel}"$'\n'; fi ;;
      A | X | S | F | P | B) cur="${pfx}${kind}_${KEY}"; printf -v "${cur}" '%s%s\n' "${!cur:-}" "${rest}" ;;
    esac
  done < <(awk -f "${lex_awk}" -f "${lex_params_awk}" "$@")
}

# file_kind <repo-relative file>: sets KIND to other | compose | none. A docker
# word the lexer cannot classify counts as other (fail closed).
file_kind() {
  local kinds
  KIND=none
  if [[ "${other_files}" == *$'\n'"$1"$'\n'* || "${unclass_files}" == *$'\n'"$1"$'\n'* ]]; then KIND=other
  elif [[ "${compose_files}" == *$'\n'"$1"$'\n'* ]]; then KIND=compose
  elif [[ "$1" == *.yml || "$1" == *.yaml ]]; then
    # A local composite action file is not in the shell batch: lex its run steps.
    yq e '(.runs.steps // []) | .[] | .run // ""' "${root}/$1" >"${tmp}/action-run.txt" 2>/dev/null || true
    kinds="$(awk -f "${lex_awk}" -f "${lex_params_awk}" "${tmp}/action-run.txt" | cut -f2 | sort -u)"
    if [[ "${kinds}" == *[OU]* ]] || [[ "${kinds}" == *V* && "${kinds}" == *A* && -n "$(awk -f "${lex_awk}" -f "${lex_params_awk}" "${tmp}/action-run.txt" | awk -F'\t' '$2 == "A" { a[$3] = 1 } $2 == "V" { split($3, w, "\037"); n = w[1]; gsub(/["{}$]|\[[@*]\]/, "", n); sub(/[^A-Za-z0-9_].*$/, "", n); v[n] = 1 } END { for (n in v) if (n in a) print n }')" ]]; then KIND=other
    elif [[ "${kinds}" == *C* ]]; then KIND=compose; fi
  fi
}

# has_vd <newline-separated files> [<run text has a docker word 0|1>]: sets HAS_V /
# HAS_D to 1 when any file has a variable invoked as a command (V) or a docker
# word in a command (D). The batch scan lexes only files with a docker word; a
# file without one can only add V, so it is lexed here, on demand, and only when
# a docker word is in play (lexed_files remembers what was lexed).
# var_conflict (below) is the rule built on it.
has_vd() {
  local f
  local -a lazy=()
  HAS_V=0; HAS_D="${2:-0}"
  while IFS= read -r f; do
    [[ -n "${f}" ]] || continue
    [[ "${dock_files}" != *$'\n'"${f}"$'\n'* ]] || HAS_D=1
    if [[ "${lexed_files}" != *$'\n'"${f}"$'\n'* && ( "${f}" == *.sh || "${f}" == *.bash ) && -f "${root}/${f}" ]]; then lazy+=("${root}/${f}"); fi
  done <<<"$1"
  if [[ "${HAS_D}" -eq 1 && "${#lazy[@]}" -gt 0 ]]; then
    for f in "${lazy[@]}"; do lexed_files="${lexed_files}${f#"${root}"/}"$'\n'; done
    lex_load repo "${lazy[@]}"
  fi
  while IFS= read -r f; do
    [[ -n "${f}" ]] || continue
    [[ "${var_files}" != *$'\n'"${f}"$'\n'* ]] || HAS_V=1
  done <<<"$1"
}

# var_conflict <run seq> <closure files> <step env64> <job>: sets VCONF to the
# first fact of the step that may run docker, "" when none (A decision, #7886).
# A variable invoked as a command (V) is settled by its assignments, by name:
#   - a docker value assigned to it in the run text, any reached script, or the
#     step, job or workflow env (A) may run docker;
#   - an unresolvable value ($(...), a backquote, ${!x}, read) assigned in a script
#     that itself names docker (X), or an unresolved ${{ }} env value read by a
#     script that names docker (E), may run docker;
#   - a variable with no assignment anywhere in the reached scripts or env takes
#     its value from outside the repository, and passes (a documented limit).
# A bare docker passed as an argument (B) is settled by its callee: a function or
# script that invokes a variable assigned from that parameter, or a callee that
# no reached script defines, may run docker (see lib/dockerhub-login-vconf.awk).
# Reached files without a docker word are lexed on demand, and only when docker
# or a docker-valued env name is in play (lexed_files remembers what was).
var_conflict() {
  local seq="$1" files="$2" env64="$3" job="$4" f cur has_d=0 line cmd tok dir key rel kind rest
  VCONF=""
  dock_env_re='^[A-Za-z_][A-Za-z0-9_]*=(.*[^A-Za-z0-9_.$-])?docker(-compose)?([^A-Za-z0-9_./-].*)?$'
  expr_env_re='^[A-Za-z_][A-Za-z0-9_]*=.*\$\{\{'
  { [[ -z "${env64}" ]] || printf '%s' "${env64}" | b64d
    printf '\n'
    awk -F'\t' -v j="${job}" '$1 == j { print $2 "=" $3 }' "${tmp}/jobenv.tsv"
    awk -F'\t' '{ print $1 "=" $2 }' "${tmp}/wfenv.tsv"
  } >"${tmp}/vc.env"
  [[ "${runs_dock}" != *$'\n'"${seq}"$'\n'* ]] || has_d=1
  while IFS= read -r f; do
    [[ -n "${f}" && "${dock_files}" == *$'\n'"${f}"$'\n'* ]] && has_d=1
  done <<<"${files}"
  if [[ "${has_d}" -eq 1 ]] || rg -q -N -e "${dock_env_re}" "${tmp}/vc.env"; then has_vd "${files}" 1; else return 0; fi
  {
    { rg -N -e "${dock_env_re}" "${tmp}/vc.env" || true; } | sed 's/=.*//; s/^/A\tenv\t/'
    { rg -N -e "${expr_env_re}" "${tmp}/vc.env" || true; } | sed 's/=.*//; s/^/E\tenv\t/'
    emit_vc_facts "run" "${seq}" "." "$([[ "${runs_dock}" == *$'\n'"${seq}"$'\n'* ]] && echo 1 || echo 0)"
    while IFS= read -r f; do
      [[ -n "${f}" ]] || continue
      dir="."; [[ "${f}" != */* ]] || dir="${f%/*}"
      mkkey "${f}"
      emit_vc_facts "${f}" "${KEY}" "${dir}" "$([[ "${dock_files}" == *$'\n'"${f}"$'\n'* ]] && echo 1 || echo 0)"
    done <<<"${files}"
  } >"${tmp}/vc.facts"
  VCONF="$(awk -f "${repo_root}/scripts/lib/dockerhub-login-vconf.awk" "${tmp}/vc.facts")"
  return 0
}

# emit_vc_facts <source> <key> <dir> <names docker 0|1>: prints the vconf facts
# of one lexed source (the run text, key = its sequence; or a reached script).
emit_vc_facts() {
  local src="$1" key="$2" dir="$3" nd="$4" pfx=F line cmd tok pos txt cur
  [[ "${src}" != run ]] || pfx=R
  cur="${pfx}A_${key}"; printf '%s' "${!cur:-}" | sed "/^\$/d; s|^|A\t${src}\t|"
  cur="${pfx}X_${key}"; printf '%s' "${!cur:-}" | sed "/^\$/d; s|^|X\t${src}\t|"
  cur="${pfx}S_${key}"; printf '%s' "${!cur:-}" | sed "/^\$/d; s|^|S\t${src}\t|"
  cur="${pfx}F_${key}"; printf '%s' "${!cur:-}" | sed '/^$/d; s/^/F\t/'
  cur="${pfx}P_${key}"; printf '%s' "${!cur:-}" | sed "/^\$/d; s|^|P\t${src}\t|"
  cur="${pfx}V_${key}"; printf '%s' "${!cur:-}" | sed "/^\$/d; s|^|V\t${src}\t${nd}\t|"
  cur="${pfx}B_${key}"
  while IFS=$'\t' read -r cmd pos txt; do
    [[ -n "${cmd}" ]] || continue
    tok="${cmd//[\"\']/}"; tok="$(printf '%s' "${tok}" | sed -E 's/^\$\{?[A-Za-z_][A-Za-z0-9_]*\}?\///')"
    if [[ "${tok}" == *.sh || "${tok}" == *.bash ]]; then
      resolve_token "${tok}" "${dir}"
      printf 'BS\t%s\t%s\t%s\t%s\n' "${src}" "${RESOLVED:-?}" "${pos}" "${txt}"
    else
      printf 'B\t%s\t%s\t%s\t%s\n' "${src}" "${cmd}" "${pos}" "${txt}"
    fi
  done <<<"${!cur:-}"
}

# closure <script>: sets closure_kind (other|compose|none), closure_chain and
# closure_files (newline-separated, every file reached). Breadth-first,
# cycle-safe, depth-limited, cached per start script.
closure() {
  local start="$1" i=0 depth path chain ref cache
  mkkey "${start}"; cache="CL_${KEY}"
  if [[ -n "${!cache:-}" ]]; then
    { IFS= read -r closure_kind; IFS= read -r closure_chain; closure_files="$(cat)"; } <<<"${!cache}"
    return 0
  fi
  local -a q_path=("${start}") q_depth=(0) q_chain=("${start}")
  local seen="|${start}|"
  closure_kind=none; closure_chain=""; closure_files=""
  while [[ "${i}" -lt "${#q_path[@]}" ]]; do
    path="${q_path[$i]}"; depth="${q_depth[$i]}"; chain="${q_chain[$i]}"; i=$((i + 1))
    closure_files="${closure_files}${path}"$'\n'
    file_kind "${path}"
    if [[ "${KIND}" == other && "${closure_kind}" != other ]]; then closure_kind=other; closure_chain="${chain}"; fi
    if [[ "${KIND}" == compose && "${closure_kind}" == none ]]; then closure_kind=compose; closure_chain="${chain}"; fi
    [[ "${depth}" -lt "${max_depth}" ]] || continue
    mkkey "${path}"; cur="FR_${KEY}"
    while IFS= read -r ref; do
      [[ -n "${ref}" && "${seen}" != *"|${ref}|"* ]] || continue
      seen="${seen}${ref}|"
      q_path+=("${ref}"); q_depth+=($((depth + 1))); q_chain+=("${chain} -> ${ref}")
    done <<<"${!cur:-}"
  done
  mkkey "${start}"
  printf -v "CL_${KEY}" '%s\n%s\n%s' "${closure_kind}" "${closure_chain}" "${closure_files}"
}
