#!/usr/bin/env bash
#
# dockerhub-login-queue.sh - trigger-derived queue-path scope for
# verify-dockerhub-login.sh (#7886). Sourced, never run.
#
# A workflow is on the queue path, derived from its on: and never from a list,
# when
#   - it runs on merge_group, pull_request or pull_request_target (the string,
#     list or map form of on:); or
#   - it runs on workflow_run and its workflows: (a list, or one scalar name)
#     names, by name:, a workflow that is on the queue path (transitively,
#     cycle-safe; a name two files share names both): a run that follows a
#     queue-path run and goes red ejects a queued PR the same way; or
#   - a queue-path workflow calls it as a reusable workflow (a job-level uses: of
#     ./.github/workflows/<file>), which the callee inherits.
# A workflow name defaults to its path when name: is absent, as GitHub does. Both
# .yml and .yaml workflow files are read. Every job of a queue-path workflow,
# whatever its own if: says, must not reach Docker Hub: a login does not make a
# reach acceptable there. Jobs of every other workflow keep the
# authenticated-pull rule of verify-dockerhub-login.sh.
#
# The caller defines: root, tmp.
#
# shellcheck disable=SC2034,SC2154  # globals are shared with the sourcing verifier

# workflow_files: sets wf_files to the workflow files, .yml then .yaml (an
# unmatched glob is dropped).
workflow_files() {
  local f
  wf_files=()
  for f in "${root}"/.github/workflows/*.yml "${root}"/.github/workflows/*.yaml; do
    [[ -f "${f}" ]] && wf_files+=("${f}")
  done
  return 0
}

# queue_path_workflows: sets queue_wfs to a newline-delimited set of the
# basenames of the queue-path workflow files (leading and trailing newline).
queue_path_workflows() {
  local wf name callee pass changed wname target nm joined
  workflow_files
  queue_wfs=$'\n'
  : >"${tmp}/wfnames.tsv"
  for wf in ${wf_files[@]+"${wf_files[@]}"}; do
    name="$(basename "${wf}")"
    wname="$(yq e '.name // ""' "${wf}" 2>/dev/null || true)"
    [[ -n "${wname}" && "${wname}" != null ]] || wname=".github/workflows/${name}"
    printf '%s\t%s\n' "${wname}" "${name}" >>"${tmp}/wfnames.tsv"
    if [[ "$(yq e '[(.on | select(type == "!!str")), (.on | select(type == "!!seq") | .[]), (.on | select(type == "!!map") | keys | .[])] | map(select(. == "merge_group" or . == "pull_request" or . == "pull_request_target")) | length' "${wf}" 2>/dev/null || echo 0)" -gt 0 ]]; then
      queue_wfs="${queue_wfs}${name}"$'\n'
    fi
  done
  # Reusable callees and workflow_run followers of a queue-path workflow join it.
  # Iterate to a fixed point (a chain is a few files deep; a cycle adds nothing).
  for pass in 1 2 3 4 5 6 7 8 9 10 11 12; do
    changed=0
    for wf in ${wf_files[@]+"${wf_files[@]}"}; do
      name="$(basename "${wf}")"
      if [[ "${queue_wfs}" == *$'\n'"${name}"$'\n'* ]]; then
        while IFS= read -r callee; do
          callee="${callee##*/}"
          [[ -n "${callee}" && "${queue_wfs}" != *$'\n'"${callee}"$'\n'* && -f "${root}/.github/workflows/${callee}" ]] || continue
          queue_wfs="${queue_wfs}${callee}"$'\n'
          changed=1
        done < <(yq e '.jobs[] | (.uses // "") | select(test("^\\./\\.github/workflows/"))' "${wf}" 2>/dev/null || true)
      else
        # workflows: is a list, or one name as a scalar. A name that two files
        # share names both, so the follower follows the queue-path one.
        joined=0
        while IFS= read -r nm; do
          [[ -n "${nm}" && "${joined}" -eq 0 ]] || continue
          while IFS= read -r target; do
            if [[ -n "${target}" && "${queue_wfs}" == *$'\n'"${target}"$'\n'* ]]; then
              queue_wfs="${queue_wfs}${name}"$'\n'
              changed=1; joined=1
              break
            fi
          done < <(awk -F'\t' -v n="${nm}" '$1 == n { print $2 }' "${tmp}/wfnames.tsv")
        done < <(yq e '.on | select(type == "!!map") | (.workflow_run.workflows // []) | ((select(tag == "!!seq") | .[]), (select(tag == "!!str")))' "${wf}" 2>/dev/null || true)
      fi
    done
    [[ "${changed}" -eq 1 ]] || break
  done
}
