#!/usr/bin/env bash
#
# dockerhub-login-shadow.sh - PATH-shadowed docker for verify-dockerhub-login.sh
# (#7886). Sourced, never run, after dockerhub-login-scripts.sh.
#
# A test that puts a stub named docker first on PATH has no real docker reach for
# itself or the scripts it runs. The model is narrow and lexical (the reader is
# lib/dockerhub-login-compose-flow.awk, mode shadow):
#
#   - an executable named docker is written into a directory D (a redirect, cp,
#     install, ln, mv or tee), and D is prepended to PATH ("PATH=D:$PATH", with or
#     without export), the prepend not inside a subshell: the later of the two
#     lines starts the region;
#   - the region ends at the first later PATH overwrite, env -i or unset PATH;
#   - a file's own docker facts are shadowed only when it has docker command
#     words and every one is the bare word inside the region (an absolute path or
#     command -p docker is never shadowed, a word before the region is not);
#   - a script the file runs or sources inherits the shadow when every reference
#     to it in the file lies inside the region and the child itself has no PATH
#     overwrite, env -i or absolute docker; the inheritance is transitive.
#
# There is no list of scripts: the shadow is derived from the text, per file.
#
# The caller defines: root, mkkey, flow_awk (compose-flow.awk path), the FR_ refs.
#
# shellcheck disable=SC2034,SC2154  # globals are shared with the sourcing verifier

SHADOWED=$'\n'

# shadow_region <repo-relative file>: sets SH_HAS (a region exists), SH_L0, SH_L1
# (first line past the region), SH_BAD (a docker word outside the region or an
# absolute one) and SH_RESET (any PATH overwrite, env -i or unset PATH) and
# SH_ABS (any absolute docker word).
shadow_region() {
  local f="$1" c rec kind a b d lw lp l0 best="" x
  local -a ws=() ps=() rs=() ds=()
  mkkey "SH:${f}"; c="SHR_${KEY}"
  if [[ -z "${!c+x}" ]]; then
    printf -v "${c}" '%s' "$(LC_ALL=C awk -v mode=shadow -f "${flow_awk}" "${root}/${f}" 2>/dev/null || true)"
  fi
  rec="${!c}"
  SH_HAS=0; SH_L0=0; SH_L1=999999999; SH_BAD=0; SH_RESET=0; SH_ABS=0
  while IFS=$'\036' read -r kind a b d; do
    case "${kind}" in
      W) ws+=("${a}|${b}") ;;
      P) [[ "${d}" != 1 ]] || continue; ps+=("${a}|${b}") ;;
      R) rs+=("${a}"); SH_RESET=1 ;;
      D) ds+=("${a}|${b}"); [[ "${b}" != abs ]] || SH_ABS=1 ;;
    esac
  done <<<"${rec}"
  for x in "${ps[@]+"${ps[@]}"}"; do
    lp="${x%%|*}"; d="${x#*|}"
    for rec in "${ws[@]+"${ws[@]}"}"; do
      [[ "${rec#*|}" == "${d}" ]] || continue
      lw="${rec%%|*}"
      l0=$((lp > lw ? lp : lw))
      if [[ -z "${best}" || "${l0}" -lt "${best}" ]]; then best="${l0}"; fi
    done
  done
  [[ -n "${best}" ]] || { for x in "${ds[@]+"${ds[@]}"}"; do SH_BAD=1; done; return 0; }
  SH_HAS=1; SH_L0="${best}"
  for x in "${rs[@]+"${rs[@]}"}"; do
    if [[ "${x}" -ge "${SH_L0}" && "${x}" -lt "${SH_L1}" ]]; then SH_L1="${x}"; fi
  done
  for x in "${ds[@]+"${ds[@]}"}"; do
    lp="${x%%|*}"
    if [[ "${x#*|}" != plain || "${lp}" -lt "${SH_L0}" || "${lp}" -ge "${SH_L1}" ]]; then SH_BAD=1; fi
  done
  return 0
}

# shadow_ref_lines_ok <parent> <child> <l0> <l1>: 0 when the parent names the child
# at least once outside comments and every such line lies in [l0, l1).
shadow_ref_lines_ok() {
  local parent="$1" child="$2" l0="$3" l1="$4" n any=0 line
  while IFS=: read -r n line; do
    [[ -n "${n}" ]] || continue
    [[ "${line}" =~ ^[[:space:]]*# ]] && continue
    any=1
    if [[ "${n}" -lt "${l0}" || "${n}" -ge "${l1}" ]]; then return 1; fi
  done < <(rg -n --no-heading -F -- "${child##*/}" "${root}/${parent}" 2>/dev/null || true)
  [[ "${any}" -eq 1 ]]
}

# shadow_files <newline-separated repo-relative files>: sets SHADOWED to the
# files whose docker facts are shadowed (own region or inherited).
shadow_files() {
  local list="$1" f c ref changed=1 iter=0 has bad l0 l1 reset abs
  local -a files=() inh=()
  SHADOWED=$'\n'
  while IFS= read -r f; do [[ -z "${f}" ]] || files+=("${f}"); done <<<"${list}"
  while [[ "${changed}" -eq 1 && "${iter}" -lt 6 ]]; do
    changed=0; iter=$((iter + 1))
    for f in "${files[@]+"${files[@]}"}"; do
      [[ "${f}" == *.sh || "${f}" == *.bash ]] || continue
      [[ "${SHADOWED}" != *$'\n'"${f}"$'\n'* ]] || continue
      shadow_region "${f}"
      if [[ "${SH_HAS}" -eq 1 && "${SH_BAD}" -eq 0 ]]; then SHADOWED="${SHADOWED}${f}"$'\n'; changed=1; fi
    done
    for f in "${files[@]+"${files[@]}"}"; do
      [[ "${f}" == *.sh || "${f}" == *.bash ]] || continue
      shadow_region "${f}"
      has="${SH_HAS}"; l0="${SH_L0}"; l1="${SH_L1}"
      if [[ "${has}" -eq 0 ]]; then
        [[ "${SHADOWED}" == *$'\n'"${f}"$'\n'* ]] || continue
        l0=0; l1=999999999
      fi
      mkkey "${f}"; c="FR_${KEY}"
      while IFS= read -r ref; do
        [[ -n "${ref}" && "${SHADOWED}" != *$'\n'"${ref}"$'\n'* ]] || continue
        [[ $'\n'"${list}"$'\n' == *$'\n'"${ref}"$'\n'* ]] || continue
        [[ "${ref}" == *.sh || "${ref}" == *.bash ]] || continue
        shadow_ref_lines_ok "${f}" "${ref}" "${l0}" "${l1}" || continue
        shadow_region "${ref}"
        [[ "${SH_RESET}" -eq 0 && "${SH_ABS}" -eq 0 ]] || continue
        SHADOWED="${SHADOWED}${ref}"$'\n'; changed=1
      done <<<"${!c:-}"
    done
  done
}
