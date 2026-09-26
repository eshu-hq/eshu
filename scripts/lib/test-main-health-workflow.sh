#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# test-main-health-workflow.sh -- static mirror assertions for
# .github/workflows/main-health.yml and the write discipline of
# scripts/ci/main-health.sh. Split out of scripts/test-main-health.sh to keep
# it under the repo's 500-line file cap.
#
# Sourced by scripts/test-main-health.sh after its behavioral cases; expects
# `repo_root`, `target`, and `check` to be defined.

# gh_call_sites prints "<enclosing-function> <line>" for every line of the
# watcher script that invokes `gh` (comments skipped). The only permitted
# enclosing functions are the two readers and the one guarded writer.
gh_call_sites() {
	awk '
		/^[A-Za-z_][A-Za-z0-9_]*\(\) *\{/ { fn = $1; sub(/\(\).*/, "", fn) }
		/^[ \t]*#/ { next }
		/(^|[^A-Za-z0-9_.-])["\047]?gh["\047]?[ \t]/ { print (fn == "" ? "<top-level>" : fn) " " NR }
		/^\}/ || /^[A-Za-z_][A-Za-z0-9_]*\(\) *\{.*\} *$/ { fn = "" }' "$1"
}

# gh_if_eval <if-expression> <context-json> prints true or false: the job
# `if:` evaluated against a `github` context. The expression language subset
# the guard uses (==, !=, &&, ||, parentheses, 'string' literals, github.*
# paths) is transliterated to jq, whose and/or/== precedence matches Actions.
# Any other construct makes jq fail, which fails the check instead of
# silently passing.
gh_if_eval() {
	local expr
	expr="$(printf '%s' "$1" | tr '\n' ' ' |
		sed -E -e 's/^[[:space:]]*\$\{\{//' -e 's/\}\}[[:space:]]*$//' \
			-e "s/'([^']*)'/\"\\1\"/g" -e 's/&&/ and /g' -e 's/\|\|/ or /g' \
			-e 's/(^|[^.A-Za-z0-9_])github\./\1.github./g')"
	jq -n --argjson ctx "$2" "\$ctx | (${expr})"
}

# wr_ctx <workflow-name> <workflow_run.event> [head-repo]: a workflow_run
# context for the watcher's job guard. head_branch is always main here: GitHub
# reports a run's head_branch as the branch it ran on, and a workflow_run-,
# schedule- or push-triggered run of main runs on main. (A workflow_run run
# fired by a PR's run also reports main, the default branch; see N3 in the
# watcher doc.)
wr_ctx() {
	jq -cn --arg n "$1" --arg e "$2" --arg r "${3:-eshu-hq/eshu}" \
		'{github:{event_name:"workflow_run",repository:"eshu-hq/eshu",
		  event:{workflow_run:{name:$n,event:$e,head_branch:"main",head_repository:{full_name:$r}}}}}'
}

watcher_wf="${repo_root}/.github/workflows/main-health.yml"
if [ ! -f "${watcher_wf}" ]; then
	check "workflow: .github/workflows/main-health.yml exists" 1
	return 0
fi

# N1: the watcher wakes on exactly the workflows it judges (the script's own
# derivation), and never on the aggregator. Every Required Gates publisher run
# reports head_branch main (about 18 a minute live), so as a trigger it would
# flood the watcher; its scheduled ruleset verification is covered by the
# watcher's own schedule, which fires after it, and by every evaluation.
agg_wf="${repo_root}/.github/workflows/required-gates.yml"
want="$(MAIN_HEALTH_REPO_ROOT="${repo_root}" "${target}" --print-required | sort -u)"
have="$(yq '.on.workflow_run.workflows[]' "${watcher_wf}" | sort -u)"
check "N1: workflow_run list == the workflows the script judges" "$([ -n "${want}" ] && [ "${want}" = "${have}" ] && echo 0 || echo 1)"
check "N1: the aggregator itself is never a trigger" \
	"$(printf '%s\n' "${have}" | rg -qxF -- "$(yq '.name' "${agg_wf}")" && echo 1 || echo 0)"
agg_cron="$(yq '.on.schedule[0].cron' "${agg_wf}")"
own_cron="$(yq '.on.schedule[0].cron' "${watcher_wf}")"
check "N1: the watcher's schedule runs after the ruleset verifier's, same hours" \
	"$([ "${agg_cron#* }" = "${own_cron#* }" ] && [ "${own_cron%% *}" -gt "${agg_cron%% *}" ] 2>/dev/null && echo 0 || echo 1)"
check "N1: no workflow-level concurrency (a guard-skipped run would hold it)" "$([ "$(yq '.concurrency' "${watcher_wf}")" = "null" ] && echo 0 || echo 1)"
check "N1: the watch job owns the concurrency group" "$([ "$(yq '.jobs.watch.concurrency.group // ""' "${watcher_wf}")" != "" ] && echo 0 || echo 1)"
check "workflow: main-only workflow_run" "$([ "$(yq '.on.workflow_run.branches[0]' "${watcher_wf}")" = "main" ] && echo 0 || echo 1)"
check "workflow: has a 6h schedule" "$([ -n "$(yq '.on.schedule[0].cron' "${watcher_wf}")" ] && echo 0 || echo 1)"
check "workflow: workflow_dispatch has a dry_run input" "$([ "$(yq '.on.workflow_dispatch.inputs.dry_run.type' "${watcher_wf}")" = "boolean" ] && echo 0 || echo 1)"
perms="$(yq -o=json -I=0 '.permissions' "${watcher_wf}")"
check "workflow: least-privilege permissions" "$([ "${perms}" = '{"actions":"read","contents":"read","issues":"write","statuses":"write"}' ] && echo 0 || echo 1)"
check "workflow: serialised, never cancelled mid-write" "$([ "$(yq '.jobs.watch.concurrency.cancel-in-progress' "${watcher_wf}")" = "false" ] && echo 0 || echo 1)"
unpinned="$(yq '.jobs[].steps[].uses | select(. != null)' "${watcher_wf}" | rg -v '^actions/(checkout)@v[0-9]+$|@[0-9a-f]{40}$' || true)"
check "workflow: every action is first-party checkout or SHA-pinned" "$([ -z "${unpinned}" ] && echo 0 || echo 1)"
check "workflow: never interpolates untrusted context into run:" "$(! yq '.jobs[].steps[].run | select(. != null)' "${watcher_wf}" | rg -q '\$\{\{ *github\.event\.' && echo 0 || echo 1)"

# F4: a dry-run dispatch must never share a concurrency group with a live
# evaluation. GitHub keeps one pending run per group, so a shared group lets a
# queued dry run evict the queued live run whose trigger will not repeat. The
# group is keyed on the same predicate that sets MAIN_HEALTH_DRY_RUN.
dry_pred="github.event_name == 'workflow_dispatch' && inputs.dry_run"
group="$(yq '.jobs.watch.concurrency.group' "${watcher_wf}")"
dry_env="$(yq '.jobs.watch.steps[] | select(.env.MAIN_HEALTH_DRY_RUN != null) | .env.MAIN_HEALTH_DRY_RUN' "${watcher_wf}")"
check "F4: concurrency group splits dry runs from live runs" \
	"$([ "${group}" = "main-health-\${{ ${dry_pred} && 'dry' || 'live' }}" ] && echo 0 || echo 1)"
check "F4: MAIN_HEALTH_DRY_RUN uses the same dry-run predicate as the group" \
	"$([ "${dry_env}" = "\${{ ${dry_pred} && 'true' || 'false' }}" ] && echo 0 || echo 1)"

# F6: the job guard is an allow-list. Every context below is evaluated against
# the real `if:`; a deny-list (or a deleted guard) lets the hostile ones run.
guard="$(yq '.jobs.watch.if // ""' "${watcher_wf}")"
check "F6: the watch job has an if: guard" "$([ -n "${guard}" ] && echo 0 || echo 1)"
guard_is() { # <want true|false> <description> <context-json>
	local got
	got="$(gh_if_eval "${guard}" "$3" 2>/dev/null || echo error)"
	check "F6: guard $1 for $2" "$([ "${got}" = "$1" ] && echo 0 || echo 1)"
}
guard_is true "the 6h schedule" '{"github":{"event_name":"schedule","repository":"eshu-hq/eshu"}}'
guard_is true "a manual dispatch" '{"github":{"event_name":"workflow_dispatch","repository":"eshu-hq/eshu"}}'
guard_is true "a required workflow's push run" "$(wr_ctx 'Build Test' push)"
guard_is true "a required workflow's scheduled run" "$(wr_ctx 'Security Scan' schedule)"
guard_is true "Security Scan's workflow_run (image scan) run" "$(wr_ctx 'Security Scan' workflow_run)"
guard_is false "a fork pull_request run" "$(wr_ctx 'Build Test' pull_request 'fork/eshu')"
guard_is false "a same-repo pull_request run" "$(wr_ctx 'Build Test' pull_request)"
guard_is false "a pull_request_target run" "$(wr_ctx 'Build Test' pull_request_target)"
guard_is false "an issue_comment run" "$(wr_ctx 'Build Test' issue_comment)"
guard_is false "a merge_group run" "$(wr_ctx 'Build Test' merge_group)"
guard_is false "a push run from a fork repository" "$(wr_ctx 'Build Test' push 'fork/eshu')"
guard_is false "a push of the watcher itself" '{"github":{"event_name":"push","repository":"eshu-hq/eshu"}}'

# P3-1: the call-site detector itself sees quoted and `command` forms.
probe="$(mktemp)"
printf 'f() {\n\t"gh" api -X POST x\n}\ng() {\n\tcommand gh api -X POST y\n}\n' >"${probe}"
check "P3-1: the gh call-site detector sees \"gh\" and command gh" "$([ "$(gh_call_sites "${probe}" | wc -l | tr -d ' ')" -eq 2 ] && echo 0 || echo 1)"
rm -f "${probe}"

# F3: every gh call in the watcher goes through gh_get / gh_list / gh_log
# (reads) or write (the one dry-run-guarded mutator). A raw `gh api` anywhere
# else is a write path the dry-run cases cannot see.
sites="$(gh_call_sites "${target}")"
bypass="$(printf '%s\n' "${sites}" | awk 'NF && $1 != "gh_get" && $1 != "gh_list" && $1 != "gh_log" && $1 != "write" { print $1 "@line" $2 }')"
policy_lib="${repo_root}/scripts/lib/main-health-policy.sh"
check "F3: the gh-free policy helpers make no gh call" "$([ -f "${policy_lib}" ] && [ -z "$(gh_call_sites "${policy_lib}")" ] && echo 0 || echo 1)"
check "F3: every gh call goes through gh_get, gh_list, gh_log, or write" "$([ -z "${bypass}" ] && echo 0 || echo 1)"
[ -z "${bypass}" ] || printf '%s\n' "${bypass}" | sed 's/^/    gh call outside the guarded helpers: /' >&2
reader_writes="$(awk '/^gh_(get|list|log)\(\)/ && /-X|graphql|--method|--input|-f |-F /' "${target}")"
check "F3: the read helpers cannot mutate (no method, graphql or fields)" "$([ -z "${reader_writes}" ] && echo 0 || echo 1)"
check "F3: write() is the only gh call site that can mutate" \
	"$([ "$(printf '%s\n' "${sites}" | awk '$1 == "write"' | wc -l | tr -d ' ')" -eq 1 ] && echo 0 || echo 1)"
write_body="$(awk '/^write\(\) *\{/,/^\}/' "${target}")"
check "F3: write() returns before calling gh in dry-run" \
	"$(printf '%s\n' "${write_body}" | awk '/dry_run.*== "true"/ { g = NR } /return 0/ && g { r = NR } /gh api/ { a = NR } END { exit !(g && r && a && g < r && r < a) }' && echo 0 || echo 1)"
