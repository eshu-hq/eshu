#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Hermetic cases for scripts/ci/main-health.sh (#7111 F2), the post-merge
# main-health watcher. No GitHub, no network: a fake `gh` on PATH serves
# recorded JSON for each API path and logs every call it receives as one JSON
# array per line, so the cases assert the upsert/close DECISIONS by looking at
# which writes were (and were not) attempted.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="${repo_root}/scripts/ci/main-health.sh"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

for tool in jq yq rg; do
	command -v "${tool}" >/dev/null || {
		echo "test-main-health: ${tool} is required" >&2
		exit 1
	}
done

pass=0
fail=0
check() {
	local desc="$1" status="$2"
	if [ "${status}" -eq 0 ]; then
		printf 'PASS: %s\n' "${desc}"
		pass=$((pass + 1))
	else
		printf 'FAIL: %s\n' "${desc}"
		fail=$((fail + 1))
	fi
}

if [ ! -x "${target}" ]; then
	echo "test-main-health: missing executable script at ${target}" >&2
	exit 1
fi

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
    types: [in_progress, completed]
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
while [[ $# -gt 0 ]]; do
	case "$1" in
	-X) method="$2"; shift 2 ;;
	-f | -F)
		[[ "$2" == body=* ]] && printf '%s' "${2#body=}" >"${FIXTURES}/last-body.txt"
		shift 2 ;;
	--jq | --input) shift 2 ;;
	--paginate) shift ;;
	*) [[ -z "${path}" ]] && path="$1"; shift ;;
	esac
done
serve() { cat "${FIXTURES}/$1" 2>/dev/null || { echo "fake gh: no fixture $1 for ${path}" >&2; exit 1; }; }
if [[ "${method}" != "GET" || "${path}" == graphql ]]; then
	[[ -f "${FIXTURES}/write-fails" ]] && grep -qx "${path}" "${FIXTURES}/write-fails" && exit 1
	echo '{"number":99,"node_id":"N99","html_url":"https://github.example/eshu-hq/eshu/issues/99"}'
	exit 0
fi
case "${path}" in
repos/*/commits/main) serve tip.json ;;
repos/*/commits/*/statuses*) serve statuses.json ;;
repos/*/actions/workflows/*/runs*) serve ruleset-runs.json ;;
repos/*/actions/runs/*/jobs*) id="${path#*/runs/}"; serve "jobs-${id%%/*}.json" ;;
repos/*/actions/runs\?*) serve runs.json ;;
repos/*/actions/jobs/*/logs) id="${path#*/jobs/}"; serve "log-${id%%/*}.txt" ;;
repos/*/issues\?*) serve issues.json ;;
*) echo "fake gh: unhandled ${path}" >&2; exit 1 ;;
esac
FAKE
chmod +x "${fake_bin}/gh"

# --- fixture builders --------------------------------------------------------
# run <id> <name> <status> <conclusion> [run_number] [attempt]
run() {
	jq -cn --arg id "$1" --arg name "$2" --arg st "$3" --arg c "$4" \
		--argjson n "${5:-1}" --argjson a "${6:-1}" --arg sha "${SHA}" \
		'{id:($id|tonumber),name:$name,status:$st,conclusion:(if $c=="" then null else $c end),
		  run_number:$n,run_attempt:$a,event:"push",head_branch:"main",head_sha:$sha,
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

set_issues() { printf '%s\n' "$@" | jq -s '.' >"${case_dir}/issues.json"; }

failing_job() { # <run-id> <job-id> <job-name> <log-text>
	jq -cn --argjson j "$2" --arg n "$3" \
		'{jobs:[{id:$j,name:$n,conclusion:"failure",
		  steps:[{name:"Run tests",conclusion:"failure"}]},
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

calls() { jq -r 'join(" ")' "${case_dir}/calls.log"; }
count_calls() { calls | { rg -c -- "$1" || true; } | awk '{s+=$1} END{print s+0}'; }
called() { [ "$(count_calls "$1")" -gt 0 ]; }
not_called() { [ "$(count_calls "$1")" -eq 0 ]; }
out_has() { rg -q -- "$1" "${case_dir}/out.txt"; }

ISSUES_POST='^api -X POST repos/eshu-hq/eshu/issues( |$)'
ISSUE_PATCH='^api -X PATCH repos/eshu-hq/eshu/issues/[0-9]+'
patch_of() { printf '^api -X PATCH repos/eshu-hq/eshu/issues/%s( |$).*' "$1"; }
STATUS_POST='^api -X POST repos/eshu-hq/eshu/statuses/'
ANY_WRITE='^api -X (POST|PATCH|PUT|DELETE)|^api graphql'

# 1. red, no open issue: exactly one issue is opened with the failing job and
#    verdict line, a failure status is posted on the tip.
new_case red-opens "${TIP}"
set_runs "$(run 1 'Build Test' completed failure)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' in_progress '')"
failing_job 1 501 'go-core (shard 2)' $'2026-09-25T10:00:00.1234567Z --- FAIL: TestGuardIndexKeyWrites (0.01s)\n2026-09-25T10:00:01.0000000Z ##[error]Process completed with exit code 1.'
run_watcher
check "red: watcher exits 0" "${last_rc}"
check "red: opens exactly one issue" "$([ "$(count_calls "${ISSUES_POST}")" -eq 1 ] && echo 0 || echo 1)"
check "red: issue title names the short sha" "$(called 'main is red @aaaaaaaaaa' && echo 0 || echo 1)"
check "red: issue body names the failing workflow, job and verdict line" "$(called 'Build Test.*go-core \(shard 2\).*--- FAIL: TestGuardIndexKeyWrites' && echo 0 || echo 1)"
check "red: pinned via graphql (best effort)" "$(called '^api graphql' && echo 0 || echo 1)"
check "red: failure status posted on the tip under main-health" "$(called "${STATUS_POST}${TIP}.*state=failure.*context=main-health" && echo 0 || echo 1)"
check "red: decision line printed" "$(out_has 'state=red.*action=open-issue' && echo 0 || echo 1)"

# 2. dry run: same red input, decision printed, zero writes.
export MAIN_HEALTH_DRY_RUN=true
: >"${case_dir}/calls.log"
run_watcher
unset MAIN_HEALTH_DRY_RUN
check "dry-run: exits 0" "${last_rc}"
check "dry-run: no write call of any kind" "$(not_called "${ANY_WRITE}" && echo 0 || echo 1)"
check "dry-run: prints the would-be action" "$(out_has 'DRY-RUN.*open-issue' && echo 0 || echo 1)"

# 3. idempotent: an open issue for the same sha with the same body is left alone.
new_case red-idempotent "${TIP}"
set_runs "$(run 1 'Build Test' completed failure)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)"
failing_job 1 501 'go-core' $'##[error]Process completed with exit code 1.'
run_watcher
new_body_issue="$(jq -cn --rawfile b "${case_dir}/last-body.txt" \
	'{number:41,node_id:"N41",title:"main is red @aaaaaaaaaa",state:"open",html_url:"https://github.example/eshu-hq/eshu/issues/41",body:$b,labels:[{name:"main-health"}]}')"
set_issues "${new_body_issue}"
: >"${case_dir}/calls.log"
run_watcher
check "idempotent: second run opens no second issue" "$(not_called "${ISSUES_POST}" && echo 0 || echo 1)"
check "idempotent: second run patches nothing when the body is unchanged" "$(not_called "${ISSUE_PATCH}" && echo 0 || echo 1)"
check "idempotent: decision is noop" "$(out_has 'action=noop' && echo 0 || echo 1)"

# 4. red on a newer sha while an issue for the older sha is open: retitle the
#    one issue, never open a second.
new_case red-newer-sha "${NEXT}"
set_runs "$(run 1 'Build Test' completed failure)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)"
failing_job 1 501 'go-core' $'##[error]Process completed with exit code 1.'
set_issues "$(open_issue 41 'main is red @aaaaaaaaaa' "${TIP}")"
run_watcher
check "newer red sha: no second issue" "$(not_called "${ISSUES_POST}" && echo 0 || echo 1)"
check "newer red sha: the one issue is retitled to the new sha" "$(called "$(patch_of 41)title=main is red @bbbbbbbbbb" && echo 0 || echo 1)"

# 5. newer green sha supersedes an older red: issue is closed, status success.
new_case green-supersedes "${NEXT}"
green_runs
set_issues "$(open_issue 41 'main is red @aaaaaaaaaa' "${TIP}")"
run_watcher
check "green supersedes red: issue closed" "$(called "$(patch_of 41)state=closed" && echo 0 || echo 1)"
check "green supersedes red: success status on the newest sha" "$(called "${STATUS_POST}${NEXT}.*state=success.*context=main-health" && echo 0 || echo 1)"
check "green supersedes red: no new issue" "$(not_called "${ISSUES_POST}" && echo 0 || echo 1)"

# 6. green with no open issue: only the status, no issue writes.
new_case green-quiet "${TIP}"
green_runs
run_watcher
check "green, nothing open: no issue writes" "$(not_called "${ISSUES_POST}|${ISSUE_PATCH}" && echo 0 || echo 1)"
check "green, nothing open: success status posted" "$(called "state=success" && echo 0 || echo 1)"

# 7. a status identical to the latest main-health status is not reposted.
echo '[{"context":"main-health","state":"success","description":"All required workflows green on aaaaaaaaaa"}]' >"${case_dir}/statuses.json"
: >"${case_dir}/calls.log"
run_watcher
check "unchanged status is not reposted" "$(not_called "${STATUS_POST}" && echo 0 || echo 1)"

# 8. pending: a still-running workflow with no red leaves the older issue open.
new_case pending-holds "${NEXT}"
set_runs "$(run 1 'Build Test' completed success)" \
	"$(run 2 'Static Contract Gates' in_progress '')" \
	"$(run 3 'Frontend' completed success)"
set_issues "$(open_issue 41 'main is red @aaaaaaaaaa' "${TIP}")"
run_watcher
check "pending: issue is not closed" "$(not_called "${ISSUE_PATCH}|${ISSUES_POST}" && echo 0 || echo 1)"
check "pending: pending status posted" "$(called "state=pending" && echo 0 || echo 1)"

# 9. the source workflow has no run yet: cannot call it green.
new_case sentinel-missing "${NEXT}"
set_runs "$(run 2 'Static Contract Gates' completed success)"
set_issues "$(open_issue 41 'main is red @aaaaaaaaaa' "${TIP}")"
run_watcher
check "no Build Test verdict yet: not green, issue kept" "$(not_called "${ISSUE_PATCH}" && echo 0 || echo 1)"
check "no Build Test verdict yet: pending" "$(called "state=pending" && echo 0 || echo 1)"

# 10. flapping: an older red superseded by a newer green run of the SAME
#     workflow is green; the reverse is red.
new_case flap-red-then-green "${TIP}"
set_runs "$(run 1 'Build Test' completed failure 10 1)" \
	"$(run 11 'Build Test' completed success 10 2)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)"
run_watcher
check "flap red->green (rerun attempt 2): green" "$(out_has 'state=green' && echo 0 || echo 1)"
new_case flap-green-then-red "${TIP}"
set_runs "$(run 1 'Build Test' completed success 10 1)" \
	"$(run 11 'Build Test' completed failure 10 2)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)"
failing_job 11 502 'go-core' $'##[error]Process completed with exit code 1.'
run_watcher
check "flap green->red (rerun attempt 2): red" "$(out_has 'state=red' && echo 0 || echo 1)"

# 11. cancelled is not red and not green: no issue change, error status.
new_case cancelled-unproven "${NEXT}"
set_runs "$(run 1 'Build Test' completed cancelled)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)"
set_issues "$(open_issue 41 'main is red @aaaaaaaaaa' "${TIP}")"
run_watcher
check "cancelled: issue untouched" "$(not_called "${ISSUE_PATCH}|${ISSUES_POST}" && echo 0 || echo 1)"
check "cancelled: error (no verdict) status" "$(called "state=error" && echo 0 || echo 1)"

# 12. a required workflow with no run on this sha (path-filtered) is not red.
new_case path-filtered "${TIP}"
set_runs "$(run 1 'Build Test' completed success)" \
	"$(run 2 'Static Contract Gates' completed success)"
run_watcher
check "no run for a path-filtered workflow: still green" "$(out_has 'state=green' && echo 0 || echo 1)"

# 13. runs of other events (PR runs on the same sha) never count.
new_case pr-runs-ignored "${TIP}"
set_runs "$(run 1 'Build Test' completed success)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)" \
	"$(run 4 'Frontend' completed failure 9 | jq -c '.event="pull_request"')"
run_watcher
check "pull_request run on the same sha is ignored" "$(out_has 'state=green' && echo 0 || echo 1)"

# 14. verify-live-ruleset failure routes to the same issue while CI is green.
new_case ruleset-red "${TIP}"
green_runs
jq -cn '{workflow_runs:[{id:70,name:"Required Gates",status:"completed",conclusion:"failure",
	event:"schedule",run_number:5,run_attempt:1,head_sha:"cccc",
	html_url:"https://github.example/runs/70"}]}' >"${case_dir}/ruleset-runs.json"
failing_job 70 701 'verify-live-ruleset' $'ruleset 19745843 (main protection) does not own one merge_queue rule\n##[error]Process completed with exit code 1.'
run_watcher
check "ruleset verifier red with green CI: red" "$(out_has 'state=red' && echo 0 || echo 1)"
check "ruleset verifier red: routed into the main-health issue with its verdict line" "$(called 'verify-live-ruleset.*does not own one merge_queue rule' && echo 0 || echo 1)"
check "ruleset verifier red: still exactly one issue" "$([ "$(count_calls "${ISSUES_POST}")" -eq 1 ] && echo 0 || echo 1)"

# 15. two open main-health issues: keep the oldest, close the duplicate.
new_case duplicates "${TIP}"
set_runs "$(run 1 'Build Test' completed failure)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)"
failing_job 1 501 'go-core' $'##[error]Process completed with exit code 1.'
set_issues "$(open_issue 50 'main is red @dup' "${TIP}")" "$(open_issue 41 'main is red @aaaaaaaaaa' "${TIP}")"
run_watcher
check "duplicates: no new issue" "$(not_called "${ISSUES_POST}" && echo 0 || echo 1)"
check "duplicates: the newer duplicate is closed" "$(called "$(patch_of 50)state=closed" && echo 0 || echo 1)"
check "duplicates: the oldest issue (41) is kept open" "$(not_called "$(patch_of 41)state=closed" && echo 0 || echo 1)"

# 16. pin failure is not fatal.
new_case pin-fails "${TIP}"
set_runs "$(run 1 'Build Test' completed failure)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)"
failing_job 1 501 'go-core' $'##[error]Process completed with exit code 1.'
echo graphql >"${case_dir}/write-fails"
run_watcher
check "pin failure: watcher still exits 0 and opened the issue" "$([ "${last_rc}" -eq 0 ] && called "${ISSUES_POST}" && echo 0 || echo 1)"

# 17. log text cannot ping people or break markdown: @ and backticks neutralised.
new_case sanitised "${TIP}"
set_runs "$(run 1 'Build Test' completed failure)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)"
failing_job 1 501 'go-core' $'--- FAIL: TestX `boom` cc @some-user please look'
run_watcher
check "verdict is sanitised (no raw @user mention)" "$(called '@some-user' && echo 1 || echo 0)"

# 18. derivation: the required list is read from the policy files, and against
#     the REAL repo it is non-empty and includes the registry's source workflow.
listed="$(MAIN_HEALTH_REPO_ROOT="${repo_root}" "${target}" --print-required)"
check "real repo: derived required list is non-empty" "$([ -n "${listed}" ] && echo 0 || echo 1)"
check "real repo: derived list includes the aggregator's source workflow" "$(printf '%s\n' "${listed}" | rg -qx 'Build Test' && echo 0 || echo 1)"
expected_n="$(yq '.on.workflow_run.workflows | length' "${repo_root}/.github/workflows/required-gates.yml")"
check "real repo: derived list covers every workflow the aggregator listens to" "$([ "$(printf '%s\n' "${listed}" | wc -l | tr -d ' ')" -ge "${expected_n}" ] && echo 0 || echo 1)"

# 19. the watcher workflow listens to exactly the aggregator's workflows plus
#     the aggregator itself (the scheduled ruleset verifier), and stays
#     least-privilege.
watcher_wf="${repo_root}/.github/workflows/main-health.yml"
if [ -f "${watcher_wf}" ]; then
	want="$( { yq '.on.workflow_run.workflows[]' "${repo_root}/.github/workflows/required-gates.yml"; yq '.name' "${repo_root}/.github/workflows/required-gates.yml"; } | sort -u)"
	have="$(yq '.on.workflow_run.workflows[]' "${watcher_wf}" | sort -u)"
	check "workflow: workflow_run list == aggregator list + aggregator" "$([ "${want}" = "${have}" ] && echo 0 || echo 1)"
	check "workflow: main-only workflow_run" "$([ "$(yq '.on.workflow_run.branches[0]' "${watcher_wf}")" = "main" ] && echo 0 || echo 1)"
	check "workflow: has a 6h schedule" "$([ -n "$(yq '.on.schedule[0].cron' "${watcher_wf}")" ] && echo 0 || echo 1)"
	check "workflow: workflow_dispatch has a dry_run input" "$([ "$(yq '.on.workflow_dispatch.inputs.dry_run.type' "${watcher_wf}")" = "boolean" ] && echo 0 || echo 1)"
	perms="$(yq -o=json -I=0 '.permissions' "${watcher_wf}")"
	check "workflow: least-privilege permissions" "$([ "${perms}" = '{"actions":"read","contents":"read","issues":"write","statuses":"write"}' ] && echo 0 || echo 1)"
	check "workflow: serialised, never cancelled mid-write" "$([ "$(yq '.concurrency.cancel-in-progress' "${watcher_wf}")" = "false" ] && echo 0 || echo 1)"
	unpinned="$(yq '.jobs[].steps[].uses | select(. != null)' "${watcher_wf}" | rg -v '^actions/(checkout)@v[0-9]+$|@[0-9a-f]{40}$' || true)"
	check "workflow: every action is first-party checkout or SHA-pinned" "$([ -z "${unpinned}" ] && echo 0 || echo 1)"
	check "workflow: never interpolates untrusted context into run:" "$(! yq '.jobs[].steps[].run | select(. != null)' "${watcher_wf}" | rg -q '\$\{\{ *github\.event\.' && echo 0 || echo 1)"
else
	check "workflow: .github/workflows/main-health.yml exists" 1
fi

printf '\ntest-main-health: %d passed, %d failed\n' "${pass}" "${fail}"
[ "${fail}" -eq 0 ]
