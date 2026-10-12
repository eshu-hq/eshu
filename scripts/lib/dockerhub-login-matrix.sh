#!/usr/bin/env bash
#
# dockerhub-login-matrix.sh - static matrix expansion of the run texts of one
# workflow for verify-dockerhub-login.sh (#7886). Sourced, never run. The caller
# defines repo_root and tmp, and has written one run text per step to
# ${tmp}/runs/<n>. See lib/dockerhub-login-matrix.awk for the cell rules.
#
# shellcheck disable=SC2154  # repo_root and tmp come from the verifier

# matrix_expand_runs <workflow file>: a step that reads ${{ matrix.KEY }} is
# rewritten in place once per static matrix cell of its job.
matrix_expand_runs() {
  local wf="$1" sn sjob
  rg -q -F -e '${{ matrix.' -e '${{matrix.' "${tmp}/runs" 2>/dev/null || return 0
  yq e '.jobs | to_entries | .[] | .key as $j | ((.value.steps // []) | .[] | $j + "")' "${wf}" >"${tmp}/stepjobs.txt"
  yq e '.jobs | to_entries | .[] | .key as $j | (.value.strategy.matrix | select(tag == "!!map")) as $m | (
      (($m.include // []) | .[] | select(tag == "!!map") | ["I", $j, (to_entries | map(select((.value | tag) != "!!map" and (.value | tag) != "!!seq") | .key + "=" + (.value | tostring)) | join(";;"))] | join("\t")),
      ($m | to_entries | .[] | select(.key != "include" and .key != "exclude" and (.value | tag) == "!!seq") | ["X", $j, .key, ((.value | map(tostring)) | join("@@"))] | join("\t")))' "${wf}" >"${tmp}/matrix.tsv"
  sn=0
  while IFS= read -r sjob; do
    sn=$((sn + 1))
    [[ -f "${tmp}/runs/${sn}" ]] && rg -q -e '\$\{\{ *matrix\.' "${tmp}/runs/${sn}" || continue
    awk -v job="${sjob}" -v mfile="${tmp}/matrix.tsv" -f "${repo_root}/scripts/lib/dockerhub-login-matrix.awk" "${tmp}/runs/${sn}" >"${tmp}/runs/${sn}.x"
    mv "${tmp}/runs/${sn}.x" "${tmp}/runs/${sn}"
  done <"${tmp}/stepjobs.txt"
}
