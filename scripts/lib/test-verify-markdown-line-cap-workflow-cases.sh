#!/usr/bin/env bash
# #6545: docs-only changes must reach the real cap selftests and full scan.
# This is a narrow check of this workflow's block-style jobs, not a YAML parser.
# Queue selection may schedule the job, but its guards must still run the cap
# commands on every docs PR. The old code-only owner omitted them there.
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
		function queue_only_if(line, value, quote, event, selected) {
			value = normalize_if(line)
			quote = sprintf("%c", 39)
			event = "github.event_name!=" quote "merge_group" quote
			selected = "containsfromJSONneeds.queue-selection.outputs.jobs||" quote "[]" quote "," quote "markdown-file-cap" quote
			return value == event "||" selected || value == selected "||" event
		}
		function plan_if(line, value, quote, expected) {
			value = normalize_if(line)
			quote = sprintf("%c", 39)
			expected = "github.event_name==" quote "merge_group" quote "||" \
				"github.event_name!=" quote "pull_request" quote "||" \
				"needs.pr-plan.result==" quote "success" quote "&&" \
				"containsfromJSONneeds.pr-plan.outputs.jobs||" quote "{}" quote \
				"[" quote "test.yml" quote "]," quote "markdown-file-cap" quote
			return value == expected
		}
		function selector_failure_if(line, value, quote) {
			value = normalize_if(line)
			quote = sprintf("%c", 39)
			return value == "always&&github.event_name==" quote "merge_group" quote
		}
		/^  markdown-file-cap:[[:space:]]*$/ { inside=1; found=1; next }
		inside && /^  [^[:space:]#][^:]*:/ { inside=0 }
		!inside || /^[[:space:]]*#/ { next }
		/^      - / {
			step_name=""
			if ($0 ~ /^      - name:[[:space:]]*/) {
				step_name=$0
				sub(/^      - name:[[:space:]]*/, "", step_name)
			}
		}
		/^[[:space:]]+(-[[:space:]]+)?needs:/ {
			value=$0
			sub(/^[[:space:]]+(-[[:space:]]+)?needs:[[:space:]]*/, "", value)
			gsub(/[[:space:]]/, "", value)
			if ($0 !~ /^    needs:/ || (value != "queue-selection" && value != "[queue-selection]" && value != "[queue-selection,pr-plan]")) guarded=1
			if (value == "[queue-selection,pr-plan]") planned=1
		}
		/^[[:space:]]+(-[[:space:]]+)?if:/ {
			if ($0 ~ /^    if:/) {
				if (plan_if($0)) plan_guard=1
				else if (!queue_only_if($0)) guarded=1
			} else if (!queue_only_if($0) && !(step_name == "Require queue selection to succeed" && selector_failure_if($0))) guarded=1
		}
		/^[[:space:]]+(run:[[:space:]]+)?(bash[[:space:]]+)?scripts\/test-verify-markdown-line-cap\.sh[[:space:]]*$/ {
			if (step_name == "Require queue selection to succeed") guarded=1
			else selftest=1
		}
		/^[[:space:]]+(run:[[:space:]]+)?(MARKDOWN_LINE_CAP_REQUIRE_BASE=1[[:space:]]+)?(bash[[:space:]]+)?scripts\/verify-markdown-line-cap\.sh[[:space:]]+--all[[:space:]]*$/ {
			if (step_name == "Require queue selection to succeed") guarded=1
			else scan=1
		}
		END {
			if (!found) print "markdown workflow: missing markdown-file-cap job"
			if (planned && !plan_guard) guarded=1
			if (guarded) print "markdown workflow: markdown-file-cap has a non-queue guard"
			if (!selftest) print "markdown workflow: missing real cap selftest command"
			if (!scan) print "markdown workflow: missing real cap --all command"
			exit (!found || guarded || !selftest || !scan)
		}
	' "$1"
}

run_markdown_workflow_cases() {
	run_markdown_rollout_workflow_cases
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
    needs: [queue-selection]
    runs-on: ubuntu-latest
    steps:
      - name: Require queue selection to succeed
        if: ${{ always() && github.event_name == 'merge_group' }}
        run: test "${{ needs.queue-selection.result }}" = success
YAML
	# Keep each static fixture fragment below the 512-byte heredoc pipe budget.
	cat >>"${fixture_dir}/queued.yml" <<'YAML'
      - name: Verify Markdown file cap
        if: ${{ github.event_name != 'merge_group' || contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), 'markdown-file-cap') }}
        run: |
          scripts/test-verify-markdown-line-cap.sh
          MARKDOWN_LINE_CAP_REQUIRE_BASE=1 scripts/verify-markdown-line-cap.sh --all
  next-job:
    if: false
YAML
	output="$(mdcap_check_workflow_job "${fixture_dir}/queued.yml" 2>&1)"
	status=$?
	assert_exit "${status}" 0 "workflow checker accepts queue-only scheduling and guards"
	awk '
		/^  markdown-file-cap:/ {
			print
			print "    if: ${{ contains(fromJSON(needs.queue-selection.outputs.jobs || \"[]\"), \"markdown-file-cap\") || github.event_name != \"merge_group\" }}"
			next
		}
		{ print }
	' "${fixture_dir}/queued.yml" >"${fixture_dir}/queued-job-if.yml"
	output="$(mdcap_check_workflow_job "${fixture_dir}/queued-job-if.yml" 2>&1)"
	status=$?
	assert_exit "${status}" 0 "workflow checker accepts equivalent queue-only job guard"
	for mutation in queued-job-code queued-step-code queued-other-needs; do
		awk -v mutation="${mutation}" '
			/^  markdown-file-cap:/ {
				print
				if (mutation == "queued-job-code") print "    if: needs.changes.outputs.code == '\''true'\''"
				next
			}
			/^    needs: \[queue-selection\]/ && mutation == "queued-other-needs" {
				print "    needs: [queue-selection, changes]"; next
			}
			/^        if:.*github.event_name !=/ && mutation == "queued-step-code" {
				print "        if: needs.changes.outputs.code == '\''true'\''"; next
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

# During the registry migration, base-branch CI still credits verify-contracts
# for this gate. Keep its real invocations until that base mapping has landed.
mdcap_check_rollout_job() {
	local source="$1" action="${2:-}" found_use
	if [[ -n "${action}" ]]; then
		found_use="$(awk '
			/^  verify-contracts:[[:space:]]*$/ { inside=1; next }
			inside && /^  [^[:space:]#][^:]*:/ { inside=0 }
			inside && /^        uses: \.\/\.github\/actions\/ci-verify-contracts[[:space:]]*$/ { found++ }
			END { print found+0 }
		' "${source}")"
		if [[ "${found_use}" != 1 ]]; then
			printf 'markdown rollout: verify-contracts must call the local proof action once\n'
			return 1
		fi
		source="${action}"
	fi
	awk -v action_mode="${action:+1}" '
		BEGIN { if (action_mode) { inside=1; found=1 } }
		/^  verify-contracts:[[:space:]]*$/ { inside=1; found=1; next }
		!action_mode && inside && /^  [^[:space:]#][^:]*:/ { inside=0 }
		!inside || /^[[:space:]]*#/ { next }
		/^[[:space:]]+(run:[[:space:]]+)?(bash[[:space:]]+)?scripts\/test-verify-markdown-line-cap\.sh[[:space:]]*$/ { selftest=1 }
		/^[[:space:]]+(run:[[:space:]]+)?(MARKDOWN_LINE_CAP_REQUIRE_BASE=1[[:space:]]+)?(bash[[:space:]]+)?scripts\/verify-markdown-line-cap\.sh[[:space:]]+--all[[:space:]]*$/ { scan=1 }
		END {
			if (!found) print "markdown rollout: missing verify-contracts job"
			if (!selftest) print "markdown rollout: verify-contracts missing real cap selftest command"
			if (!scan) print "markdown rollout: verify-contracts missing real cap --all command"
			exit (!found || !selftest || !scan)
		}
	' "${source}"
}

run_markdown_rollout_workflow_cases() {
	local fixture_dir fixture output status mutation
	fixture_dir="$(mktemp -d)"
	fixture="${fixture_dir}/valid.yml"
	local live_workflow="${script_root}/../.github/workflows/test.yml"
	local live_action="${script_root}/../.github/actions/ci-verify-contracts/action.yml"
	output="$(mdcap_check_rollout_job "${live_workflow}" "${live_action}" 2>&1)"
	status=$?
	printf 'workflow rollout live CLI wiring exit=%d\n%s\n' "${status}" "${output}"
	assert_exit "${status}" 0 "live verify-contracts retains cap selftests and --all during registry rollout"
	local disabled="${fixture_dir}/disabled-action.yml"
	sed 's|uses: ./.github/actions/ci-verify-contracts|run: true|' "${live_workflow}" >"${disabled}"
	output="$(mdcap_check_rollout_job "${disabled}" "${live_action}" 2>&1)"
	status=$?
	assert_exit "${status}" 1 "rollout checker rejects a disconnected proof action"
	sed '/scripts\/test-verify-markdown-line-cap\.sh/d' "${live_action}" >"${fixture_dir}/no-action-selftest.yml"
	output="$(mdcap_check_rollout_job "${live_workflow}" "${fixture_dir}/no-action-selftest.yml" 2>&1)"
	status=$?
	assert_exit "${status}" 1 "rollout checker rejects missing composite-action cap selftest"
	cat >"${fixture}" <<'YAML'
name: fixture
jobs:
  verify-contracts:
    needs: changes
    runs-on: ubuntu-latest
    steps:
      - run: |
          scripts/test-verify-markdown-line-cap.sh
          MARKDOWN_LINE_CAP_REQUIRE_BASE=1 scripts/verify-markdown-line-cap.sh --all
  markdown-file-cap:
    runs-on: ubuntu-latest
    steps:
      - run: |
          scripts/test-verify-markdown-line-cap.sh
          MARKDOWN_LINE_CAP_REQUIRE_BASE=1 scripts/verify-markdown-line-cap.sh --all
YAML
	output="$(mdcap_check_rollout_job "${fixture}" 2>&1)"
	status=$?
	assert_exit "${status}" 0 "rollout checker accepts existing verify-contracts invocations"
	for mutation in no-selftest no-scan comment-selftest comment-scan; do
		awk -v mutation="${mutation}" '
			/^  markdown-file-cap:/ { outside=1 }
			!outside && /scripts\/test-verify/ {
				if (mutation == "no-selftest") next
				if (mutation == "comment-selftest") { print "#" $0; next }
			}
			!outside && /scripts\/verify/ {
				if (mutation == "no-scan") next
				if (mutation == "comment-scan") { print "#" $0; next }
			}
			{ print }
		' "${fixture}" >"${fixture_dir}/${mutation}.yml"
		output="$(mdcap_check_rollout_job "${fixture_dir}/${mutation}.yml" 2>&1)"
		status=$?
		assert_exit "${status}" 1 "rollout checker rejects ${mutation} despite dedicated job commands"
	done
	rm -rf "${fixture_dir}"
}
