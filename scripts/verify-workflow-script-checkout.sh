#!/usr/bin/env bash
# verify-workflow-script-checkout.sh - fails when a workflow job runs a
# repository shell script with no preceding actions/checkout step in that job
# (#8008).
#
# Why: the trivy-image job in security-scan.yml ran
# `bash scripts/ci/resolve-image-scan-ref.sh` in a workspace with no checkout,
# so the step exited 127 and every workflow_run image scan skipped Trivy. A
# job without a checkout has no repository tree, so any repo script it invokes
# fails the same way.
#
# Rule: in every .github/workflows/*.yml|*.yaml job that has a steps:
# sequence, the first step whose `uses:` is actions/checkout must come before
# every step whose `run:` invokes a repository shell script. A script
# invocation is `scripts/<path>.sh|.bash`, `./scripts/<path>.sh|.bash`, or
# `../scripts/<path>.sh|.bash` (the working-directory: go form) at a command
# boundary, with or without a
# `bash`/`sh`/`source`/`. ` prefix. Full-line and trailing ` #` shell
# comments are stripped before matching, so a commented-out invocation does
# not count. Step `if:` conditions are NOT evaluated: a conditional checkout
# still counts as preceding (queue-selection.yml relies on this; a static
# gate cannot evaluate expressions).
#
# Out of scope: reusable-call jobs (no steps: of their own), local composite
# actions (none exist; .github/actions/ is absent), and non-shell repo
# scripts. A checkout that the runner skips at runtime is still a finding for
# a human, not for this gate.
#
# Usage:
#   scripts/verify-workflow-script-checkout.sh [--help]
#
# Environment:
#   ESHU_WORKFLOW_SCRIPT_CHECKOUT_ROOT  Repository root to scan
#                                       (default: this script's repo root).
#                                       The self-test points it at scratch trees.
#
# Exit codes:
#   0 - every script-invoking step has a preceding checkout.
#   1 - at least one violation; each names the workflow, job, and step.
#   2 - fail closed: bad usage, missing yq, or a workflow yq cannot parse.
set -euo pipefail

tool="verify-workflow-script-checkout"

usage() {
	printf 'usage: scripts/verify-workflow-script-checkout.sh [--help]\n' >&2
}

case "${1:-}" in
	"")
		;;
	--help|-h)
		sed -n '2,/^set -euo pipefail$/p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	*)
		printf '%s: unknown argument %s\n' "${tool}" "$1" >&2
		usage
		exit 2
		;;
esac

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
root="${ESHU_WORKFLOW_SCRIPT_CHECKOUT_ROOT:-${repo_root}}"
workflows_dir="${root}/.github/workflows"

command -v yq >/dev/null 2>&1 || {
	printf '%s: FAIL-CLOSED: required tool not found: yq\n' "${tool}" >&2
	exit 2
}
[ -d "${workflows_dir}" ] || {
	printf '%s: FAIL-CLOSED: workflows directory not found: %s\n' "${tool}" "${workflows_dir}" >&2
	exit 2
}

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

# One TSV row per violating step: job, step index, step name.
cat >"${work}/violations.yq" <<'PROG'
.jobs | to_entries | .[] |
select(.value.steps | type == "!!seq") |
.key as $job |
(
  .value.steps | to_entries |
  map(select(((.value.uses // "") | tostring) | test("^actions/checkout(@|/|$)")) | .key)
  | .[0]
) as $co |
.value.steps | to_entries | .[] |
(
  (((.value.run // "") | tostring) | split("\n")
    | map(sub("^[ \t]*#.*$"; "") | sub("[ \t]+#.*$"; ""))
    | join("\n"))
) as $code |
select($code | test("([ \t\n;|&(){}\"'`=!]|^)(\\./|\\.\\./)?scripts/[A-Za-z0-9_@./-]*\\.(sh|bash)([^A-Za-z0-9_.-]|$)")) |
select($co == null or .key < $co) |
[$job, (.key | tostring), (.value.name // "" | tostring)] | @tsv
PROG

shopt -s nullglob
workflow_files=("${workflows_dir}"/*.yml "${workflows_dir}"/*.yaml)
shopt -u nullglob
[ "${#workflow_files[@]}" -gt 0 ] || {
	printf '%s: FAIL-CLOSED: no workflow files in %s\n' "${tool}" "${workflows_dir}" >&2
	exit 2
}

violations=0
failed=0
checked=0
tab="$(printf '\t')"
for wf in "${workflow_files[@]}"; do
	checked=$((checked + 1))
	if ! yq e --from-file "${work}/violations.yq" "${wf}" >"${work}/out.tsv" 2>"${work}/yq.err"; then
		printf '%s: FAIL-CLOSED: cannot parse %s:\n' "${tool}" "${wf}" >&2
		sed 's/^/  /' "${work}/yq.err" >&2
		failed=$((failed + 1))
		continue
	fi
	while IFS="${tab}" read -r job idx name; do
		[ -n "${job}" ] || continue
		if [ -n "${name}" ]; then
			printf '%s: %s: job <%s> step %s (%s) runs a repository script with no preceding actions/checkout step\n' \
				"${tool}" "${wf}" "${job}" "${idx}" "${name}"
		else
			printf '%s: %s: job <%s> step %s runs a repository script with no preceding actions/checkout step\n' \
				"${tool}" "${wf}" "${job}" "${idx}"
		fi
		violations=$((violations + 1))
	done <"${work}/out.tsv"
done

if [ "${failed}" -gt 0 ]; then
	printf '%s: FAIL-CLOSED: %d workflow(s) could not be parsed\n' "${tool}" "${failed}" >&2
	exit 2
fi
if [ "${violations}" -gt 0 ]; then
	printf '%s: %d violation(s) in %d workflow(s)\n' "${tool}" "${violations}" "${checked}" >&2
	exit 1
fi
printf '%s: %d workflow(s) checked, no violations\n' "${tool}" "${checked}"
