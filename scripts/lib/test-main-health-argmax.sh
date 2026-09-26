#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# test-main-health-argmax.sh -- the oversized run-listing cases of
# scripts/test-main-health.sh (#7111). Split out to keep that mirror under the
# repo's 500-line file cap.
#
# Sourced by scripts/test-main-health.sh; not meant to run standalone. Expects
# the fixture builders of test-main-health-fake.sh and the `check` / `ok`
# helpers of the mirror.
#
# Why. The watcher once merged each run listing into its accumulator by
# passing the page as a `jq --argjson` argument. One argv string is capped
# (Linux MAX_ARG_STRLEN, 128 KiB; macOS caps the whole argv+env at ARG_MAX,
# 1 MiB), and exec reports E2BIG, which bash surfaces as exit 126 with
# "Argument list too long". A commit that a re-triggered workflow buried in
# runs made the listing that large, and the watcher failed on every event.
# These cases feed a listing over BOTH limits (a run object carries a
# `pad` field, so the size does not depend on which fields the real API
# returns) and assert the behaviour: the watcher completes and classifies the
# large listing correctly.

# case_dir and last_rc are set by the sourcing mirror and its fixture library.
# shellcheck disable=SC2154

# big_listing <newest-conclusion>: 95 superseded `Build Test` push runs, each
# ~14 KiB, so one 100-run listing page is ~1.3 MiB, above the macOS ARG_MAX
# and ten times the Linux per-argument cap; then the newest `Build Test` run
# with the given conclusion, plus a green Static Contract Gates and Frontend.
big_listing() {
	{
		local i
		for ((i = 1; i <= 95; i++)); do
			run "$((1000 + i))" 'Build Test' completed failure "${i}" 1
		done
		run 1 'Build Test' completed "$1" 200 1
		run 2 'Static Contract Gates' completed success
		run 3 'Frontend' completed success
	} | jq -c 'if .id > 1000 then .pad = ("x" * 14000) else . end' |
		jq -s '{workflow_runs: .}' >"${case_dir}/runs.json"
}

new_case argmax-green "${TIP}"
big_listing success
# 1048576 is the macOS ARG_MAX; the Linux per-argument cap (131072) is lower.
check "argmax fixture: the run listing exceeds both argv limits" \
	"$([ "$(wc -c <"${case_dir}/runs.json" | tr -d ' ')" -gt 1048576 ] && echo 0 || echo 1)"
run_watcher
check "argmax: a listing over the argv limit completes (exit 0)" "${last_rc}"
check "argmax: no 'Argument list too long' from any command" "$([ ! -s "${case_dir}/err.txt" ] || ! rg -q 'Argument list too long' "${case_dir}/err.txt" && echo 0 || echo 1)"
check "argmax: the superseded failures do not make a green tip red" "$(ok out_has 'state=green')"
check "argmax: a full listing is not reported truncated" "$(ok out_has ' truncated=false ')"

new_case argmax-red "${TIP}"
big_listing failure
failing_job 1 501 'go-core' $'##[error]Process completed with exit code 1.'
run_watcher
check "argmax red: a listing over the argv limit completes (exit 0)" "${last_rc}"
check "argmax red: the newest failing run is judged red" "$(ok out_has 'state=red')"
check "argmax red: exactly one issue naming the failing workflow" \
	"$([ "$(count_calls "${ISSUES_POST}")" -eq 1 ] && called 'Build Test.*go-core' && echo 0 || echo 1)"
