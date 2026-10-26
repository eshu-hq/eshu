#!/usr/bin/env bash
# Check the trusted PR build filter and the shared Go completion assertion.

run_docs_planner_checks() {
	local test_workflow="$1" build_workflow="$2" build_fragment="$3" assertion="$4"
	local paths caller caller_if expected_if block lane lane_result selected_job

	# The callable build workflow has no pull_request path filter. The trusted
	# PR planner owns it, and test.yml must invoke that callable on selection.
	paths="$(awk '/^  paths_ignore:/{f=1;next} f&&/^  - /{print;next} f{exit}' "${build_fragment}")"
	caller="$(rg '^  call-build:' "${test_workflow}")"
	caller_if="$(printf '%s\n' "${caller}" | sed -n 's/.*if: "\([^"]*\)".*/\1/p')"
	expected_if="$(cat <<'EOF'
${{ github.event_name == 'pull_request' && needs.pr-plan.result == 'success' && needs.pr-plan.outputs.mode != 'reuse' && contains(fromJSON(needs.pr-plan.outputs.workflows || '[]'), 'build.yml') }}
EOF
)"
	if has "${build_workflow}" '  workflow_call:' &&
		! has "${build_workflow}" '  pull_request:' &&
		has "${build_fragment}" '  synchronize: true' &&
		printf '%s\n' "${paths}" | rg -qFx '  - docs/**' &&
		printf '%s\n' "${paths}" | rg -qFx "  - '*.md'" &&
		printf '%s\n' "${paths}" | rg -qFx "  - '**/*.md'" &&
		printf '%s\n' "${paths}" | rg -qFx "  - '*.json'" &&
		printf '%s\n' "${paths}" | rg -qFx '  - docs/**/*.json' &&
		printf '%s\n' "${paths}" | rg -qFx '  - scripts/**/*.json' &&
		printf '%s\n' "${paths}" | rg -qFx '  - .agents/**' &&
		[[ "${caller}" == *'uses: ./.github/workflows/build.yml'* ]] &&
		[[ "${caller_if}" == "${expected_if}" ]]; then
		ok 'build.yml docs-only PR filter is owned by the trusted planner and called from test.yml'
	else
		bad 'build.yml needs trusted fragment docs/**, nested md/json ignores, .agents/** and a planned callable caller'
	fi

	# Both required Go status jobs must invoke the real assertion script with
	# the lane, plan, changes and head inputs. Static wiring is complemented by
	# scripts/ci/test-assert-go-complete.sh, which exercises the script behavior.
	for lane in go-race go-core; do
		printf -v lane_result 'LANE_RESULT: ${{ needs.%s.result }}' "${lane}"
		printf -v selected_job "SELECTED: \${{ contains(fromJSON(needs.pr-plan.outputs.jobs || '{}')['test.yml'], '%s') }}" "${lane}"
		block="$(job_block "${test_workflow}" "${lane}-complete")"
		if job_needs "${test_workflow}" "${lane}-complete" changes &&
			job_needs "${test_workflow}" "${lane}-complete" "${lane}" &&
			job_needs "${test_workflow}" "${lane}-complete" pr-plan &&
			[[ "${block}" == *'if: ${{ always() }}'* ]] &&
			[[ "${block}" == *'uses: actions/checkout@v5'* ]] &&
			[[ "${block}" == *'run: bash scripts/ci/assert-go-complete.sh'* ]] &&
			[[ "${block}" == *"LANE: ${lane}"* ]] &&
			[[ "${block}" == *"${lane_result}"* ]] &&
			[[ "${block}" == *'CHANGES_RESULT: ${{ needs.changes.result }}'* ]] &&
			[[ "${block}" == *'CODE_CHANGED: ${{ needs.changes.outputs.code }}'* ]] &&
			[[ "${block}" == *'PLAN_RESULT: ${{ needs.pr-plan.result }}'* ]] &&
			[[ "${block}" == *'MODE: ${{ needs.pr-plan.outputs.mode }}'* ]] &&
			[[ "${block}" == *'HEAD_SHA: ${{ needs.pr-plan.outputs.head_sha }}'* ]] &&
			[[ "${block}" == *'PR_HEAD_SHA: ${{ github.event.pull_request.head.sha }}'* ]] &&
			[[ "${block}" == *'SOURCE_SHA: ${{ needs.pr-plan.outputs.source_sha }}'* ]] &&
			[[ "${block}" == *'TREE_SHA: ${{ needs.pr-plan.outputs.tree }}'* ]] &&
			[[ "${block}" == *"${selected_job}"* ]]; then
			ok "${lane}-complete runs the shared assertion with trusted plan, changes, and lane inputs"
		else
			bad "${lane}-complete must wire the checked-out shared assertion and trusted inputs"
		fi
	done

	if has "${assertion}" '[[ "${CHANGES_RESULT:-}" == success ]] || fail' &&
		has "${assertion}" '[[ "${LANE_RESULT:-}" == success ]] || fail "selected full' &&
		has "${assertion}" '[[ "${LANE_RESULT:-}" == success || "${LANE_RESULT:-}" == skipped ]] || fail' &&
		has "${assertion}" '[[ "${SELECTED:-}" == true ]] || fail "full PR plan did not select'; then
		ok 'shared Go assertion rejects failed changes and skipped selected full lanes while allowing docs-only skips'
	else
		bad 'shared Go assertion must fail closed for changes and selected full lanes'
	fi
}

# run_docs_set_checks <test.yml> <security-scan.yml> <mcp-schema-drift.yml>
# <specs/ci-pr-workflows.d dir> — the #9200 docs-set proof: every code filter
# must negate nested markdown and the scoped JSON set (guard 1 above proves
# the three copies match each other; this proves they carry the negations at
# all), and every broad heavy-workflow fragment must exclude markdown.
run_docs_set_checks() {
	local t="$1" s="$2" m="$3" fragdir="$4"
	local wf block ok_all line
	ok_all=1
	for wf in "${t}" "${s}" "${m}"; do
		block="$(code_filter_block "${wf}")"
		for line in "- '!**/*.md'" "- '!*.json'" "- '!docs/**/*.json'" "- '!scripts/**/*.json'"; do
			if ! printf '%s\n' "${block}" | rg -qF -- "${line}"; then
				bad "$(basename "${wf}") code: filter lacks ${line} (#9200 docs set)"
				ok_all=0
			fi
		done
	done
	if [[ "${ok_all}" -eq 1 ]]; then
		ok 'all three code: filters negate nested markdown and the scoped JSON set'
	fi
	ok_all=1
	for frag in bench.yml e2e-tests.yml live-backend-tests.yml read-api-latency-gate.yml sdk-go.yml; do
		if ! rg -qF -- "- '!**/*.md'" "${fragdir}/${frag}"; then
			bad "${frag} fragment lacks the '!**/*.md' exclusion (#9200)"
			ok_all=0
			continue
		fi
		neg_line=$(rg -n -F -- "- '!**/*.md'" "${fragdir}/${frag}" | head -1 | cut -d: -f1)
		jobs_line=$(rg -n -F -- "  jobs:" "${fragdir}/${frag}" | head -1 | cut -d: -f1)
		if [[ -z "${neg_line}" || -z "${jobs_line}" ]]; then
			bad "${frag} fragment has no readable paths/jobs anchor (#9200)"
			ok_all=0
		elif [[ "${jobs_line}" -gt "$((neg_line + 1))" ]] && sed -n "$((neg_line + 1)),$((jobs_line - 1))p" "${fragdir}/${frag}" | rg -q -- "- "; then
			bad "${frag} fragment: a positive path after the '!**/*.md' exclusion would silently re-select markdown (last-match-wins)"
			ok_all=0
		fi
	done
	if ! rg -qF -- "- '**/*.md'" "${fragdir}/build.yml"; then
		bad "build.yml fragment lacks the '**/*.md' ignore (#9200)"
		ok_all=0
	fi
	if [[ "${ok_all}" -eq 1 ]]; then
		ok 'heavy fragments exclude nested markdown from PR selection'
	fi
}
