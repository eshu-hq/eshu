#!/usr/bin/env bash
# Assert the two workflow gates call the tested capture-staging helper. Sourced
# by golden-corpus-mirror-workflow-paths.sh after ${workflow} is set.
# shellcheck disable=SC2016,SC2034,SC2154

extract_capture_run() {
  local step="$1"
  awk -v step="      - name: ${step}" '
    $0 == step { in_step = 1; next }
    in_step && /^      - name: / { exit }
    in_step && /^        run: / {
      sub(/^        run: /, "")
      if ($0 != ">") { print; exit }
      folded = 1
      next
    }
    folded && /^          / {
      sub(/^          /, "")
      printf "%s ", $0
      next
    }
    folded { exit }
  ' "${workflow}"
}

capture_check_run="$(extract_capture_run 'Require non-empty capture before upload')"
[[ "${capture_check_run}" == 'bash scripts/lib/stage-differential-capture.sh check-leg "${{ runner.temp }}/diff-capture/pair${{ matrix.pair }}/${{ matrix.graph_backend }}"' ]] ||
  fail 'differential capture leg must validate its own non-empty recordings'
require_in 'differential legs use one canonical absolute repos path' "${workflow}" \
  'ESHU_REPOS_DIR: ${{ runner.temp }}/eshu-diff-corpus'

capture_stage_run="$(extract_capture_run 'Require and stage all four recordings')"
[[ "${capture_stage_run}" == *'bash scripts/lib/stage-differential-capture.sh stage '* ]] ||
  fail 'differential join must run the staging helper'
[[ "${capture_stage_run}" == *'"${{ runner.temp }}/diff-artifacts" '* ]] ||
  fail 'differential join must stage downloaded artifacts'
[[ "${capture_stage_run}" == *'"${{ runner.temp }}/diff-capture" '* ]] ||
  fail 'differential join must reconstruct the compare/coverage paths'
[[ "${capture_stage_run}" == *'"${{ github.run_attempt }}" '* ]] ||
  fail 'differential join must select the current attempt'
[[ "${capture_stage_run}" == *'"${{ needs.differential-capture.result }}"'* ]] ||
  fail 'differential join must block when a matrix leg failed'
join_header="$(sed -n '/^  differential:$/,/^    steps:$/p' "${workflow}")"
rg -q '^    needs: \[queue-selection, differential-capture\]$' <<<"${join_header}" ||
  fail 'differential join must depend on all capture legs'
rg -q '^    if: always\(\)$' <<<"${join_header}" ||
  fail 'differential join must run after a failed or cancelled capture leg'

require_in 'golden-corpus-gate.yml Compare step id (#6965)' "${workflow}" 'id: compare'
require_in 'golden-corpus-gate.yml capture upload on advisory failure (#6965)' "${workflow}" \
  "failure() || steps.compare.outcome == 'failure'"

bash "${repo_root}/scripts/test-stage-differential-capture.sh" ||
  fail 'differential capture staging cases failed'
capture_leg_cases_completed=1
