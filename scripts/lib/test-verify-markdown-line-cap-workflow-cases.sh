#!/usr/bin/env bash
# #6545: docs-only changes must reach the real cap selftests and full scan.
# This is a narrow check of this workflow's block-style jobs, not a YAML parser.
# The dedicated job intentionally has no job/step if or needs guard: the old
# code-only verify-contracts owner silently omitted the cap on docs-only PRs.
mdcap_check_workflow_job() {
	awk '
		/^  markdown-file-cap:[[:space:]]*$/ { inside=1; found=1; next }
		inside && /^  [^[:space:]#][^:]*:/ { inside=0 }
		!inside || /^[[:space:]]*#/ { next }
		/^[[:space:]]+(-[[:space:]]+)?(if|needs):/ { guarded=1 }
		/^[[:space:]]+(run:[[:space:]]+)?(bash[[:space:]]+)?scripts\/test-verify-markdown-line-cap\.sh[[:space:]]*$/ { selftest=1 }
		/^[[:space:]]+(run:[[:space:]]+)?(MARKDOWN_LINE_CAP_REQUIRE_BASE=1[[:space:]]+)?(bash[[:space:]]+)?scripts\/verify-markdown-line-cap\.sh[[:space:]]+--all[[:space:]]*$/ { scan=1 }
		END {
			if (!found) print "markdown workflow: missing unconditional markdown-file-cap job"
			if (guarded) print "markdown workflow: markdown-file-cap has an if/needs guard"
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
