#!/usr/bin/env bash
# Evaluate local composite run steps with the caller's effective environment.
# Sourced by verify-dockerhub-login.sh; its lexer and image helpers own the
# host decision. A Docker command in an action is not itself a Hub pull.

composite_hub_why() {
  local action_file="$1" action_dir="$2" caller_env64="$3" job="$4" caller_seq="$5"
  local index run64 child_env64 runf merged_env64 cfiles ref d_other d_comp
  HUBWHY=""
  if ! yq e -o=json -I=0 '.runs.steps // []' "${root}/${action_file}" >"${tmp}/action-steps.json"; then
    HUBWHY="local action ${action_file} cannot be parsed"
    return 0
  fi
  while IFS=$'\t' read -r index run64 child_env64; do
    runf="${tmp}/runs/a${caller_seq}_${index}"
    printf '%s' "${run64}" | b64d >"${runf}"
    lex_load run "${runf}"
    cfiles=""
    while IFS= read -r ref; do
      [[ -n "${ref}" ]] || continue
      resolve_token "${ref}" "${action_dir}"
      [[ -n "${RESOLVED}" ]] || continue
      closure "${RESOLVED}"
      cfiles="${cfiles}${closure_files}"$'\n'
    done < <(LC_ALL=C awk -v bare=1 -f "${refs_awk}" "${runf}")
    # Later env scopes override earlier ones: workflow/job are already loaded,
    # then caller step env, then this composite run step's env.
    merged_env64="$({ printf '%s' "${caller_env64}" | b64d; printf '\n';
      printf '%s' "${child_env64}" | b64d; } |
      awk -F= 'index($0,"=") { k=$1; if (!(k in seen)) keys[++n]=k; seen[k]=substr($0,index($0,"=")+1) }
        END { for (i=1; i<=n; i++) print keys[i] "=" seen[keys[i]] }' |
      base64 | tr -d '\n')"
    d_other=0; d_comp=0
    [[ "${runs_other}" != *$'\n'"${runf##*/}"$'\n'* ]] || d_other=1
    [[ "${runs_compose}" != *$'\n'"${runf##*/}"$'\n'* ]] || d_comp=1
    step_hub_why "${runf}" "${d_other}" "${d_comp}" "${cfiles}" "${merged_env64}" "${job}"
    [[ -z "${HUBWHY}" ]] || return 0
  done < <(jq -r 'to_entries[] | select(.value.run != null) |
    [.key, (.value.run | @base64), ((.value.env // {}) | to_entries |
    map(.key + "=" + (.value | tostring)) | join("\n") | @base64)] | @tsv' "${tmp}/action-steps.json")
}
