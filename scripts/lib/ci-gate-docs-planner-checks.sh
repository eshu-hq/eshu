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
		printf '%s\n' "${paths}" | rg -qFx '  - .agents/**' &&
		[[ "${caller}" == *'uses: ./.github/workflows/build.yml'* ]] &&
		[[ "${caller_if}" == "${expected_if}" ]]; then
		ok 'build.yml docs-only PR filter is owned by the trusted planner and called from test.yml'
	else
		bad 'build.yml needs trusted fragment docs/**, *.md, .agents/** and a planned callable caller'
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
