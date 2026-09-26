#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# test-main-health-fake.sh -- fixture policy tree, fake `gh`, fixture
# builders, and call-log assertions for scripts/test-main-health.sh. Split out
# to keep that mirror under the repo's 500-line file cap.
#
# Sourced by scripts/test-main-health.sh; not meant to run standalone. Expects
# `set -euo pipefail`, and `work` (a scratch directory) and `repo_root` to be
# set. Provides policy_root, fake_bin, the case builders (new_case, run,
# set_runs, green_runs, open_issue, set_issues, failing_job), run_watcher, and
# the call-log helpers (calls, count_calls, called, not_called, out_has).
#
# The fake `gh` honors the query filters the real Actions API applies
# (head_sha, event, branch, status, per_page on a single page, and the
# workflow file of /actions/workflows/<file>/runs), so a case can prove the
# script asked for the bounded listing instead of filtering client-side, and
# an unfiltered request still sees every run of every event. It records each
# call's full argv, flags included, as one JSON array per line.

REPO="eshu-hq/eshu"
TIP="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
NEXT="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

# --- fixture repo root: the source of truth the script must derive from ------
policy_root="${work}/policy"
mkdir -p "${policy_root}/.github/workflows" "${policy_root}/specs"
cat >"${policy_root}/.github/workflows/required-gates.yml" <<'YAML'
name: Required Gates
on:
  workflow_run:
    workflows:
      - Build Test
      - Static Contract Gates
      - Frontend
      - Security Scan
    types: [in_progress, completed]
YAML
cat >"${policy_root}/.github/workflows/test.yml" <<'YAML'
name: Build Test
on: [push, pull_request, merge_group]
YAML
cat >"${policy_root}/.github/workflows/static.yml" <<'YAML'
name: Static Contract Gates
on:
  push:
  pull_request:
YAML
cat >"${policy_root}/.github/workflows/frontend.yml" <<'YAML'
name: Frontend
on:
  push:
YAML
cat >"${policy_root}/.github/workflows/security-scan.yml" <<'YAML'
name: Security Scan
on:
  push:
  schedule:
    - cron: "0 6 * * 1"
  workflow_run:
    workflows: ["Publish Image and Helm Chart"]
    types: [completed]
YAML
cat >"${policy_root}/specs/ci-gates.v1.yaml" <<'YAML'
version: v1
required_status_checks:
  - context: go-core-complete
    workflow: test.yml
    job: go-core-complete
    aggregates_blocking_gates: false
  - context: required-gates-complete
    workflow: required-gates.yml
    job: aggregate
    source_workflow: Build Test
    aggregates_blocking_gates: true
# Blocking-ness is read from here (N2): a failed job counts toward red only
# when a blocking gate claims it, or when no gate claims it at all.
gates:
  - id: go-test
    blocking: true
    ci: {workflow: test.yml, job: go-core}
  - id: gosec
    blocking: true
    ci: {workflow: security-scan.yml, job: "gosec (Go static analysis)"}
  - id: trivy-fs
    blocking: false
    ci: {workflow: security-scan.yml, job: "Trivy filesystem scan (vuln + secret + config)"}
  - id: trivy-image
    blocking: false
    ci: {workflow: security-scan.yml, job: "Trivy image scan (ghcr.io/eshu-hq/eshu)"}
YAML

# --- fake gh -----------------------------------------------------------------
fake_bin="${work}/bin"
mkdir -p "${fake_bin}"
cat >"${fake_bin}/gh" <<'FAKE'
#!/usr/bin/env bash
# Serves $FIXTURES/<name> for a read endpoint; records every call.
set -euo pipefail
jq -cn '$ARGS.positional' --args -- "$@" >>"${FIXTURES}/calls.log"
[[ "$1" == "api" ]] || { echo "fake gh: unsupported: $*" >&2; exit 2; }
shift
method=GET
path=""
allow_esc=0
while [[ $# -gt 0 ]]; do
	case "$1" in
	-X) method="$2"; shift 2 ;;
	-f | -F)
		[[ "$2" == body=* ]] && printf '%s' "${2#body=}" >"${FIXTURES}/last-body.txt"
		shift 2 ;;
	--jq | --input) shift 2 ;;
	--paginate) shift ;;
	--allow-escape-sequences) allow_esc=1; shift ;;
	*) [[ -z "${path}" ]] && path="$1"; shift ;;
	esac
done
serve() { cat "${FIXTURES}/$1" 2>/dev/null || { echo "fake gh: no fixture $1 for ${path}" >&2; exit 1; }; }
# runs_filtered <fixture> [workflow-file]: apply the API's query filters.
runs_filtered() {
	local query="${path#*\?}" q='{}' kv
	[[ "${path}" == *\?* ]] || query=""
	for kv in ${query//&/ }; do
		q="$(jq -c --arg k "${kv%%=*}" --arg v "${kv#*=}" '. + {($k): $v}' <<<"${q}")"
	done
	jq -c --argjson q "${q}" --arg wf "${2:-}" \
		--argjson total "$(cat "${FIXTURES}/total-count" 2>/dev/null || echo null)" '
		[.workflow_runs[]
		 | select(($wf == "") or (.path == (".github/workflows/" + $wf)))
		 | select(($q.head_sha // .head_sha) == .head_sha)
		 | select(($q.event // .event) == .event)
		 | select(($q.branch // .head_branch) == .head_branch)
		 | select(($q.status // .status) == .status)] as $m
		| {total_count: ($total // ($m | length)),
		   workflow_runs: (if $q.per_page then $m[0:($q.per_page | tonumber)] else $m end)}' "${FIXTURES}/$1"
}
if [[ "${method}" != "GET" || "${path}" == graphql ]]; then
	[[ -f "${FIXTURES}/write-fails" ]] && rg -qxF -- "${path}" "${FIXTURES}/write-fails" && exit 1
	echo '{"number":99,"node_id":"N99","html_url":"https://github.example/eshu-hq/eshu/issues/99"}'
	exit 0
fi
case "${path}" in
repos/*/commits/main) serve tip.json ;;
repos/*/commits/*/status) jq '{state: "pending", statuses: .}' "${FIXTURES}/statuses.json" ;;
repos/*/actions/workflows/required-gates.yml/runs*) runs_filtered ruleset-runs.json ;;
repos/*/actions/workflows/*/runs*) wf="${path#*/workflows/}"; runs_filtered runs.json "${wf%%/*}" ;;
repos/*/actions/runs/*/jobs*) id="${path#*/runs/}"; serve "jobs-${id%%/*}.json" ;;
repos/*/actions/runs\?*) runs_filtered runs.json ;;
repos/*/actions/jobs/*/logs)
	# gh >= 2.101 refuses to print a response carrying terminal escape
	# sequences unless --allow-escape-sequences is passed (real CI logs do).
	id="${path#*/jobs/}"
	if [[ "${allow_esc}" -eq 0 ]] && rg -q $'\x1b' "${FIXTURES}/log-${id%%/*}.txt" 2>/dev/null; then
		echo "the response contains terminal escape sequences; pass --allow-escape-sequences to output it anyway" >&2
		exit 1
	fi
	serve "log-${id%%/*}.txt" ;;
repos/*/issues\?*) serve issues.json ;;
*) echo "fake gh: unhandled ${path}" >&2; exit 1 ;;
esac
FAKE
chmod +x "${fake_bin}/gh"

# --- fixture builders --------------------------------------------------------
# workflow_file maps a fixture workflow name to its file (the API's run.path).
workflow_file() {
	case "$1" in
	'Build Test') echo test.yml ;;
	'Static Contract Gates') echo static.yml ;;
	'Frontend') echo frontend.yml ;;
	'Security Scan') echo security-scan.yml ;;
	*) echo other.yml ;;
	esac
}

# run <id> <name> <status> <conclusion> [run_number] [attempt] [event]
run() {
	jq -cn --arg id "$1" --arg name "$2" --arg st "$3" --arg c "$4" \
		--argjson n "${5:-1}" --argjson a "${6:-1}" --arg ev "${7:-push}" --arg sha "${SHA}" \
		--arg path ".github/workflows/$(workflow_file "$2")" \
		'{id:($id|tonumber),name:$name,path:$path,status:$st,conclusion:(if $c=="" then null else $c end),
		  run_number:$n,run_attempt:$a,event:$ev,head_branch:"main",head_sha:$sha,
		  html_url:("https://github.example/runs/"+$id)}'
}

new_case() { # <name> <tip-sha>
	case_dir="${work}/$1"
	mkdir -p "${case_dir}"
	export FIXTURES="${case_dir}"
	SHA="$2"
	printf '{"sha":"%s"}\n' "$2" >"${case_dir}/tip.json"
	echo '[]' >"${case_dir}/issues.json"
	echo '[]' >"${case_dir}/statuses.json"
	echo '{"workflow_runs":[]}' >"${case_dir}/ruleset-runs.json"
	echo '{"workflow_runs":[]}' >"${case_dir}/runs.json"
	: >"${case_dir}/calls.log"
}

set_runs() { # run-json...
	printf '%s\n' "$@" | jq -s '{workflow_runs: .}' >"${case_dir}/runs.json"
}

green_runs() {
	set_runs "$(run 1 'Build Test' completed success)" \
		"$(run 2 'Static Contract Gates' completed success)" \
		"$(run 3 'Frontend' completed success)"
}

open_issue() { # <number> <title> <sha-in-body>
	jq -cn --argjson n "$1" --arg t "$2" --arg s "$3" \
		'{number:$n,node_id:("N"+($n|tostring)),title:$t,state:"open",
		  html_url:("https://github.example/eshu-hq/eshu/issues/"+($n|tostring)),
		  body:("<!-- main-health:sha="+$s+" -->\nold body"),labels:[{name:"main-health"}]}'
}

# red_issue <number> <title> <sha> <red-set-json>: an open watcher issue whose
# body records the blocking red set of the evaluation that last wrote it.
red_issue() {
	open_issue "$1" "$2" "$3" | jq -c --arg s "$3" --arg r "$4" \
		'.body = ("<!-- main-health:sha=" + $s + " -->\n<!-- main-health:red=" + $r + " -->\nold body")'
}

set_issues() { printf '%s\n' "$@" | jq -s '.' >"${case_dir}/issues.json"; }

failing_job() { # <run-id> <job-id> <job-name> <log-text> [failed-step-name]
	jq -cn --argjson j "$2" --arg n "$3" --arg s "${5-Run tests}" \
		'{jobs:[{id:$j,name:$n,conclusion:"failure",
		  steps:(if $s == "" then [] else [{name:$s,conclusion:"failure"}] end)},
		  {id:900,name:"other",conclusion:"success",steps:[]}]}' >"${case_dir}/jobs-$1.json"
	printf '%s\n' "$4" >"${case_dir}/log-$2.txt"
}

run_watcher() { # extra env assignments via caller; records the exit code
	set +e
	MAIN_HEALTH_REPO_ROOT="${policy_root}" GITHUB_REPOSITORY="${REPO}" \
		PATH="${fake_bin}:${PATH}" "${target}" >"${case_dir}/out.txt" 2>"${case_dir}/err.txt"
	last_rc=$?
	set -e
	[ "${last_rc}" -eq 0 ] || sed 's/^/    watcher stderr: /' "${case_dir}/err.txt" >&2
	return 0
}

# dry_run_watcher: rerun the current case in dry-run mode on a fresh call log.
dry_run_watcher() {
	: >"${case_dir}/calls.log"
	MAIN_HEALTH_DRY_RUN=true run_watcher
}

calls() { jq -r 'join(" ")' "${case_dir}/calls.log"; }
count_calls() { calls | { rg -c -- "$1" || true; } | awk '{s+=$1} END{print s+0}'; }
called() { [ "$(count_calls "$1")" -gt 0 ]; }
not_called() { [ "$(count_calls "$1")" -eq 0 ]; }
out_has() { rg -q -- "$1" "${case_dir}/out.txt"; }
ok() { "$@" && echo 0 || echo 1; }

ISSUES_POST='^api -X POST repos/eshu-hq/eshu/issues( |$)'
ISSUE_PATCH='^api -X PATCH repos/eshu-hq/eshu/issues/[0-9]+'
patch_of() { printf '^api -X PATCH repos/eshu-hq/eshu/issues/%s( |$).*' "$1"; }
STATUS_POST='^api -X POST repos/eshu-hq/eshu/statuses/'
COMMENT_POST='^api -X POST repos/eshu-hq/eshu/issues/[0-9]+/comments'
ANY_WRITE='^api -X (POST|PATCH|PUT|DELETE)|^api graphql'
