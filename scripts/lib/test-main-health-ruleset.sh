#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# test-main-health-ruleset.sh -- the #7430 / #7448 cases of
# scripts/test-main-health.sh: the ruleset-verifier verdict is the newest
# completed scheduled run in a bounded page, chosen by created_at, and a run
# older than 12h is unknown. Split out to keep that mirror under the repo's
# 500-line file rule.
#
# Sourced by scripts/test-main-health.sh; not meant to run standalone. Expects
# the fixture builders of test-main-health-fake.sh and the `check` / `ok`
# helpers of the mirror.

# 19b. #7430 / #7448: the ruleset verifier verdict is the NEWEST completed
#      scheduled run in a bounded page, chosen by created_at, never row 0 of
#      an unstable listing. GitHub's listing returned a 9-day-old failing run
#      on one call and the current success on the next three, and main-health
#      posted "main is red" from the stale row. A newest run older than two
#      cron periods (12h) is no evidence either way: unknown, not red.
new_case r7430-stale-first "${TIP}"
green_runs
ruleset_runs "60:failure:777600" "71:success:7200" "70:success:28800"
run_watcher
check "#7430 T1: a stale failing row listed first does not make main red" "$(ok out_has 'state=green')"
check "#7430 T1: the verdict comes from the newest run by created_at" "$(ok out_has 'ruleset_conclusion=success')"
new_case r7430-newest-red-last "${TIP}"
green_runs
ruleset_runs "62:success:28800" "63:success:14400" "72:failure:3600"
run_watcher
check "#7430 T2: a genuinely newest failing run listed last is still red" "$(ok out_has 'state=red.*ruleset_red=true')"
check "#7430 T2: the red names the failing conclusion" "$(ok rg -qF 'conclusion `failure`' "${case_dir}/last-body.txt")"
new_case r7430-fresh-red "${TIP}"
green_runs
ruleset_runs "72:failure:3600" "62:success:28800"
run_watcher
check "#7430 T3: a fresh newest failure in normal order stays red" "$(ok out_has 'state=red.*ruleset_red=true')"
for concl in failure success; do
	new_case "r7430-stale-${concl}" "${NEXT}"
	green_runs
	ruleset_runs "60:${concl}:108000"
	set_issues "$(open_issue 41 'main is red @aaaaaaaaaa' "${TIP}")"
	run_watcher
	check "#7430 T4: newest completed run ${concl} 30h old: unknown, not red or green" "$(ok out_has 'state=unknown')"
	check "#7430 T4: stale ${concl}: not treated as a ruleset failure" "$(ok out_has 'ruleset_red=false')"
	check "#7430 T4: stale ${concl}: the issue stays open" "$(ok not_called "$(patch_of 41)state=closed")"
done
new_case r7430-stale-desc "${NEXT}"
green_runs
ruleset_runs "60:success:108000"
run_watcher
check "#7430 T5: the status says the verification is stale, with its age" "$(ok called "${STATUS_POST}.*state=error.*description=.*ruleset verification.*older than 12h")"

# T6: a newest run with no created_at has no provable age, so it cannot prove the
# ruleset is current: unknown, never green (fail closed).
new_case r7430-no-created-at "${NEXT}"
green_runs
ruleset_runs "70:success:none"
run_watcher
check "#7430 T6: a newest run with no created_at is unknown, not green" "$(ok out_has 'state=unknown')"
# T7: the 12h bound is pinned on both sides (a 9h or a 24h bound must fail).
new_case r7430-just-fresh "${TIP}"
green_runs
ruleset_runs "70:success:42840"
run_watcher
check "#7430 T7: a success 11.9h old is still fresh: green" "$(ok out_has 'state=green')"
new_case r7430-just-stale "${NEXT}"
green_runs
ruleset_runs "70:success:43560"
run_watcher
check "#7430 T7: a success 12.1h old is stale: unknown" "$(ok out_has 'state=unknown')"
