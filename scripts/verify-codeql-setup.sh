#!/usr/bin/env bash
set -euo pipefail

repo_root="${1:-${ESHU_CODEQL_SETUP_REPO_ROOT:-}}"
if [ -z "${repo_root}" ]; then
	repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
		|| (cd "$(dirname "$0")/.." && pwd))"
fi

workflow_dir="${repo_root}/.github/workflows"
if [ ! -d "${workflow_dir}" ]; then
	printf 'verify-codeql-setup: no GitHub workflows directory\n'
	exit 0
fi

workflow_files=()
while IFS= read -r path; do
	workflow_files+=("${path}")
done < <(rg --files "${workflow_dir}" -g '*.yml' -g '*.yaml' 2>/dev/null || true)

if [ "${#workflow_files[@]}" -eq 0 ]; then
	printf 'verify-codeql-setup: no GitHub workflow files\n'
	exit 0
fi

codeql_action_pattern='github/codeql-action/(init|analyze|autobuild)'
codeql_cli_pattern='(^|[^[:alnum:]_-])codeql[[:space:]]+(database[[:space:]]+(create|analyze|interpret-results)|github[[:space:]]+upload-results)'
codeql_workflow_pattern="${codeql_action_pattern}|${codeql_cli_pattern}"
advanced_setup_matches="$(rg -n -i "${codeql_workflow_pattern}" "${workflow_files[@]}" || true)"
if [ -n "${advanced_setup_matches}" ]; then
	{
		printf 'verify-codeql-setup: checked-in CodeQL advanced setup or CodeQL result upload is not allowed while GitHub default setup is the repository model\n'
		printf '%s\n' "${advanced_setup_matches}"
		printf '\nKeep CodeQL in GitHub default setup, or change the documented model and this verifier in the same PR after disabling default setup.\n'
	} >&2
	exit 1
fi

printf 'verify-codeql-setup: default setup model has no checked-in CodeQL workflow\n'
