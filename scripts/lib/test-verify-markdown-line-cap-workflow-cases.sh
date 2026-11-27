#!/usr/bin/env bash
# #6545: docs-only changes must reach the real cap selftests and full scan.
# This is a narrow check of this workflow's block-style jobs, not a YAML parser.
# The plan may schedule the job, but its steps must still run the cap commands
# on every docs PR. The old code-only owner omitted them there.
# shellcheck disable=SC2154  # script_root is defined by the sourcing test.
mdcap_check_workflow_job() {
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
				"[" quote "test.yml" quote "]," quote "markdown-file-cap" quote
			return value == expected
		}
		/^  markdown-file-cap:[[:space:]]*$/ { inside=1; found=1; next }
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
		/^[[:space:]]+(run:[[:space:]]+)?(bash[[:space:]]+)?scripts\/test-verify-markdown-line-cap\.sh[[:space:]]*$/ {
			selftest=1
		}
		/^[[:space:]]+(run:[[:space:]]+)?(MARKDOWN_LINE_CAP_REQUIRE_BASE=1[[:space:]]+)?(bash[[:space:]]+)?scripts\/verify-markdown-line-cap\.sh[[:space:]]+--all[[:space:]]*$/ {
			scan=1
		}
		END {
			if (!found) print "markdown workflow: missing markdown-file-cap job"
			if (planned && !plan_guard) guarded=1
			if (guarded) print "markdown workflow: markdown-file-cap has a non-plan guard"
			if (!selftest) print "markdown workflow: missing real cap selftest command"
			if (!scan) print "markdown workflow: missing real cap --all command"
			exit (!found || guarded || !selftest || !scan)
		}
	' "$1"
}

run_markdown_workflow_cases() {
	local fixture_dir fixture output status mutation
	fixture_dir="$(mktemp -d)"
	fixture="${fixture_dir}/valid.yml"
	output="$(mdcap_check_workflow_job "${script_root}/../.github/workflows/test.yml" 2>&1)"
	status=$?
	printf 'workflow live CLI wiring exit=%d\n%s\n' "${status}" "${output}"
	assert_exit "${status}" 0 "live workflow always runs cap selftests and --all for docs"
	cat >"${fixture}" <<'YAML'
name: fixture
jobs:
  markdown-file-cap:
    runs-on: ubuntu-latest
    steps:
      - run: |
          scripts/test-verify-markdown-line-cap.sh
          MARKDOWN_LINE_CAP_REQUIRE_BASE=1 scripts/verify-markdown-line-cap.sh --all
  next-job:
    if: false
    runs-on: ubuntu-latest
YAML
	output="$(mdcap_check_workflow_job "${fixture}" 2>&1)"
	status=$?
	assert_exit "${status}" 0 "workflow checker accepts unconditional real commands"
	cat >"${fixture_dir}/queued.yml" <<'YAML'
name: fixture
jobs:
  markdown-file-cap:
    needs: [pr-plan]
    if: ${{ github.event_name != 'pull_request' || (needs.pr-plan.result == 'success' && contains(fromJSON(needs.pr-plan.outputs.jobs || '{}')['test.yml'], 'markdown-file-cap')) }}
    runs-on: ubuntu-latest
    steps:
      - name: Verify Markdown file cap
        run: |
          scripts/test-verify-markdown-line-cap.sh
          MARKDOWN_LINE_CAP_REQUIRE_BASE=1 scripts/verify-markdown-line-cap.sh --all
  next-job:
    if: false
YAML
	output="$(mdcap_check_workflow_job "${fixture_dir}/queued.yml" 2>&1)"
	status=$?
	assert_exit "${status}" 0 "workflow checker accepts plan scheduling without step guards"
	awk '
		/^  markdown-file-cap:/ {
			print
			print "    if: ${{ ((github.event_name != '\''pull_request'\'' || (needs.pr-plan.result == '\''success'\'' && contains(fromJSON(needs.pr-plan.outputs.jobs || '\''{}'\'')['\''test.yml'\''], '\''markdown-file-cap'\'')))) }}"
			next
		}
		{ print }
	' "${fixture_dir}/queued.yml" >"${fixture_dir}/queued-job-if.yml"
	output="$(mdcap_check_workflow_job "${fixture_dir}/queued-job-if.yml" 2>&1)"
	status=$?
	assert_exit "${status}" 0 "workflow checker accepts an equivalent plan job guard"
	for mutation in queued-job-code queued-step-code queued-other-needs; do
		awk -v mutation="${mutation}" '
			/^  markdown-file-cap:/ {
				print
				if (mutation == "queued-job-code") print "    if: needs.changes.outputs.code == '\''true'\''"
				next
			}
			/^    needs: \[pr-plan\]/ && mutation == "queued-other-needs" {
				print "    needs: [pr-plan, changes]"; next
			}
			/^          scripts\/test-verify-markdown-line-cap\.sh$/ && mutation == "queued-step-code" {
				print "        if: needs.changes.outputs.code == '\''true'\''"; print; next
			}
			{ print }
		' "${fixture_dir}/queued.yml" >"${fixture_dir}/${mutation}.yml"
		output="$(mdcap_check_workflow_job "${fixture_dir}/${mutation}.yml" 2>&1)"
		status=$?
		assert_exit "${status}" 1 "workflow checker rejects ${mutation}"
	done
	for mutation in missing job-if needs step-if no-selftest no-scan commented; do
		awk -v mutation="${mutation}" '
			/markdown-file-cap:/ {
				if (mutation == "missing") { print "  unrelated-job:"; next }
				print
				if (mutation == "job-if") print "    if: needs.changes.outputs.code == '\''true'\''"
				if (mutation == "needs") print "    needs: changes"
				next
			}
			/      - run:/ && mutation == "step-if" {
				print "      - if: needs.changes.outputs.code == '\''true'\''"
				print "        run: |"; next
			}
			/scripts\/test-verify/ && mutation == "no-selftest" { next }
			/scripts\/verify/ && mutation == "no-scan" { next }
			/scripts\// && mutation == "commented" { print "#" $0; next }
			{ print }
		' "${fixture}" >"${fixture_dir}/${mutation}.yml"
		output="$(mdcap_check_workflow_job "${fixture_dir}/${mutation}.yml" 2>&1)"
		status=$?
		assert_exit "${status}" 1 "workflow checker rejects ${mutation} mutation"
	done
	rm -rf "${fixture_dir}"
}
