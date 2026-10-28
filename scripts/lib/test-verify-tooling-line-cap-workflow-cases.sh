#!/usr/bin/env bash
# Workflow mirror for the scripts-file-cap CI job (#8221), modelled on
# scripts/lib/test-verify-markdown-line-cap-workflow-cases.sh. This is a narrow
# check of the job's block-style YAML, not a YAML parser. It pins what a green
# gate depends on: the job schedules only through the plan selection
# (never a code-path guard that skips a scripts-only change), and it runs the
# real cap self-test and the real --all scan with TOOLING_LINE_CAP_REQUIRE_BASE=1
# so a missing base ref fails the job instead of passing without a growth check.
# shellcheck disable=SC2154  # real_root is defined by the sourcing test.

tlcap_check_workflow_job() {
	awk '
		function normalize_if(line, value) {
			value = line
			sub(/^[[:space:]]*(-[[:space:]]+)?if:[[:space:]]*/, "", value)
			gsub(/\$\{\{|\}\}/, "", value)
			gsub(/[[:space:]()]/, "", value)
			gsub(/"/, "\047", value)
			return value
		}
		function plan_if(line, value, quote, expected) {
			value = normalize_if(line)
			quote = sprintf("%c", 39)
			expected = "github.event_name!=" quote "pull_request" quote "||" \
				"needs.pr-plan.result==" quote "success" quote "&&" \
				"containsfromJSONneeds.pr-plan.outputs.jobs||" quote "{}" quote \
				"[" quote "test.yml" quote "]," quote "scripts-file-cap" quote
			return value == expected
		}
		/^  scripts-file-cap:[[:space:]]*$/ { inside=1; found=1; next }
		inside && /^  [^[:space:]#][^:]*:/ { inside=0 }
		!inside || /^[[:space:]]*#/ { next }
		/^[[:space:]]+(-[[:space:]]+)?needs:/ {
			value=$0
			sub(/^[[:space:]]+(-[[:space:]]+)?needs:[[:space:]]*/, "", value)
			gsub(/[[:space:]]/, "", value)
			if ($0 !~ /^    needs:/ || value != "[pr-plan]") guarded=1
			if (value == "[pr-plan]") planned=1
		}
		/^[[:space:]]+(-[[:space:]]+)?if:/ {
			if ($0 ~ /^    if:/) {
				if (plan_if($0)) plan_guard=1
				else guarded=1
			} else guarded=1
		}
		/^[[:space:]]+(run:[[:space:]]+)?(bash[[:space:]]+)?scripts\/test-verify-tooling-line-cap\.sh[[:space:]]*$/ {
			selftest=1
		}
		/^[[:space:]]+(run:[[:space:]]+)?TOOLING_LINE_CAP_REQUIRE_BASE=1[[:space:]]+(bash[[:space:]]+)?scripts\/verify-tooling-line-cap\.sh[[:space:]]+--all[[:space:]]*$/ {
			scan=1
		}
		END {
			if (!found) print "tooling workflow: missing scripts-file-cap job"
			if (planned && !plan_guard) guarded=1
			if (guarded) print "tooling workflow: scripts-file-cap has a non-plan guard"
			if (!selftest) print "tooling workflow: missing real cap selftest command"
			if (!scan) print "tooling workflow: missing real cap --all command with TOOLING_LINE_CAP_REQUIRE_BASE=1"
			exit (!found || guarded || !selftest || !scan)
		}
	' "$1"
}

# tlcap_assert_workflow_exit LABEL WANT FILE runs the checker and records it.
tlcap_assert_workflow_exit() {
	local label="$1" want="$2" file="$3" output status
	output="$(tlcap_check_workflow_job "${file}" 2>&1)"
	status=$?
	if [[ "${status}" -eq "${want}" ]]; then
		record_pass "${label}"
	else
		record_fail "${label}" "exit ${status}, want ${want}: ${output}"
	fi
}

test_workflow_job_is_wired() {
	local workflow="${real_root}/.github/workflows/test.yml" dir job
	dir="$(mktemp -d)"
	scratch_dirs+=("${dir}")
	tlcap_assert_workflow_exit "live test.yml runs the cap self-test and --all scan on the plan guard" 0 "${workflow}"

	# Seed the mutations from the real job text so the check bites on the
	# wiring that ships, not on a hand-written lookalike.
	job="${dir}/job.yml"
	{
		printf 'name: fixture\njobs:\n'
		awk '
			/^  scripts-file-cap:[[:space:]]*$/ { inside = 1 }
			inside && /^  [^[:space:]#][^:]*:/ && !/^  scripts-file-cap:/ { exit }
			inside { print }
		' "${workflow}"
	} >"${job}"
	tlcap_assert_workflow_exit "extracted live job is accepted" 0 "${job}"

	sed 's/TOOLING_LINE_CAP_REQUIRE_BASE=1 //' "${job}" >"${dir}/no-require-base.yml"
	cmp -s "${job}" "${dir}/no-require-base.yml" && record_fail "require-base mutation changed nothing" ""
	tlcap_assert_workflow_exit "dropping TOOLING_LINE_CAP_REQUIRE_BASE=1 is rejected" 1 "${dir}/no-require-base.yml"

	sed "s/'scripts-file-cap'/'renamed-job'/" "${job}" >"${dir}/renamed-guard.yml"
	cmp -s "${job}" "${dir}/renamed-guard.yml" && record_fail "renamed-guard mutation changed nothing" ""
	tlcap_assert_workflow_exit "renaming the job in the plan guard is rejected" 1 "${dir}/renamed-guard.yml"

	sed 's/^    needs: \[pr-plan\]$/    needs: [pr-plan, changes]/' "${job}" >"${dir}/other-needs.yml"
	cmp -s "${job}" "${dir}/other-needs.yml" && record_fail "other-needs mutation changed nothing" ""
	tlcap_assert_workflow_exit "a second needs entry is rejected" 1 "${dir}/other-needs.yml"

	awk '{ print } /^    timeout-minutes: 10$/ { print "    if: needs.changes.outputs.code == \047true\047" }' "${job}" >"${dir}/job-if.yml"
	cmp -s "${job}" "${dir}/job-if.yml" && record_fail "job-if mutation changed nothing" ""
	tlcap_assert_workflow_exit "a code-path job guard is rejected" 1 "${dir}/job-if.yml"

	sed '/scripts\/test-verify-tooling-line-cap\.sh/d' "${job}" >"${dir}/no-selftest.yml"
	tlcap_assert_workflow_exit "dropping the cap self-test is rejected" 1 "${dir}/no-selftest.yml"

	sed '/scripts\/verify-tooling-line-cap\.sh/d' "${job}" >"${dir}/no-scan.yml"
	tlcap_assert_workflow_exit "dropping the --all scan is rejected" 1 "${dir}/no-scan.yml"

	awk '/scripts\// { print "#" $0; next } { print }' "${job}" >"${dir}/commented.yml"
	tlcap_assert_workflow_exit "commented-out commands are rejected" 1 "${dir}/commented.yml"

	sed 's/^  scripts-file-cap:/  unrelated-job:/' "${job}" >"${dir}/missing.yml"
	tlcap_assert_workflow_exit "a missing scripts-file-cap job is rejected" 1 "${dir}/missing.yml"
}
