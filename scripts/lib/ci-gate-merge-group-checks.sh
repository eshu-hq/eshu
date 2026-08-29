#!/usr/bin/env bash
#
# ci-gate-merge-group-checks.sh — the merge_group (#5814) structural
# assertions for scripts/verify-docs-only-ci-skip.sh. Sourced by that script
# and nothing else; it calls the caller's ok(), bad(), job_block(), and rg,
# all already defined in the sourcing script's scope by the time this file is
# sourced. It lives here rather than inline so the parent stays under the
# repo's 500-line file rule, and it is named ci-gate-*.sh so the trigger lists
# that select ci-gate related scripts pick up an edit here too.
#
# The seam: `step_block` plus the merge_group assertion group are a
# self-contained unit used by nothing else in the parent script (job_block,
# job_gated, and job_alwayson stay in the parent because those three are used
# throughout — the always-on-job checks, the umbrella guard-body checks, and
# so on — not just here).
#
# What this guards, in dependency order:
#   1. the `on:` trigger block actually listens for merge_group;
#   2. the `changes` job's own paths-filter step is SKIPPED on merge_group —
#      dorny/paths-filter's merge_group support is real (it seeds base/head
#      from the event) but has never been proven against this job's shallow
#      `fetch-depth: 2` checkout, so the fix does not lean on it;
#   3. `changes` instead reports code=true directly on merge_group, so the
#      job always SUCCEEDS on that event and its output still resolves.
# Item 3 is the one that actually matters: go-core-complete/go-race-complete
# both require `needs.changes.result == 'success'` (checked separately, in the
# parent script's umbrella loop), and a `changes` job that merely runs-but-
# fails on merge_group would jam both umbrellas exactly like an unhandled
# failure does today.

# step_block <job_block_text> <step_id> — the YAML lines of the ONE step inside
# a job block (as produced by job_block) whose `id: <step_id>` field appears in
# it, from that step's "      - " list-item marker through the line before the
# next step marker (or the end of the job). Steps are unkeyed YAML list items,
# so — unlike job_block, which can match on a literal "  job:" header line —
# isolating one requires buffering each item and checking which one contains
# the id line. This exists so a caller can assert two substrings belong to the
# SAME step rather than merely co-occurring anywhere in the job: two
# independent `rg -qF` searches over a whole job block would false-pass if a
# future unrelated step carried one of the two substrings on its own.
step_block() {
	local block="$1" needle="        id: $2"
	# Exact-line match ($0 == needle), NOT substring containment. A prior
	# version used index(cur, needle) > 0, which matched a step whose id is a
	# PREFIX of the target (id: merge_group_code_v2 contains "id: merge_group_code"
	# as a substring) or a step whose run: body happens to echo the literal
	# 8-space-indented id line as text, in both cases silently returning the
	# WRONG step. Tracking a per-step boolean set only on an exact line match
	# closes both: a longer id never equals the shorter needle, and an
	# embedded line inside a run: body is data, not itself a YAML id: field.
	awk -v needle="${needle}" '
		BEGIN { cur = ""; found = 0; hit = 0 }
		/^      - / {
			if (found) { exit }
			if (hit) { printf "%s", cur; found = 1 }
			cur = $0 "\n"
			hit = 0
			next
		}
		{
			cur = cur $0 "\n"
			if ($0 == needle) { hit = 1 }
		}
		END {
			if (!found && hit) { printf "%s", cur }
		}
	' < <(printf '%s\n' "${block}")
}

# run_merge_group_checks <test.yml path> — the four merge_group assertions.
# Assumes ok(), bad(), and job_block() are already defined in the caller.
run_merge_group_checks() {
	local t="$1" on_block changes_block merge_group_code_step

	on_block="$(awk '/^on:$/{f=1;print;next} f&&/^[A-Za-z]/{exit} f{print}' "${t}")"
	if rg -qF 'merge_group:' < <(printf '%s\n' "${on_block}"); then
		ok "test.yml on: block listens for merge_group"
	else
		bad "test.yml on: block must add a merge_group trigger (required checks never report on a merge-queue entry otherwise)"
	fi

	changes_block="$(job_block "${t}" changes)"
	if rg -qF "if: \${{ github.event_name != 'merge_group' }}" < <(printf '%s\n' "${changes_block}"); then
		ok "test.yml changes job skips the paths-filter diff on merge_group"
	else
		bad "test.yml changes job's Filter changed paths step must guard if: \${{ github.event_name != 'merge_group' }} (do not depend on paths-filter's unproven merge_group behavior)"
	fi
	# Scoped to the single step carrying `id: merge_group_code`, not the whole
	# job block: two independent whole-block substring searches would false-pass
	# if some future unrelated step carried a merge_group guard and a separate
	# step elsewhere in the job happened to contain the literal text "code=true".
	merge_group_code_step="$(step_block "${changes_block}" merge_group_code)"
	if [[ -n "${merge_group_code_step}" ]] \
		&& rg -qF "if: \${{ github.event_name == 'merge_group' }}" < <(printf '%s\n' "${merge_group_code_step}") \
		&& rg -qF 'code=true' < <(printf '%s\n' "${merge_group_code_step}"); then
		ok "test.yml changes job forces code=true on merge_group instead of depending on paths-filter"
	else
		bad "test.yml changes job must have a single step (id: merge_group_code) that sets code=true directly when github.event_name == 'merge_group'"
	fi
	if rg -qF 'steps.filter.outputs.code || steps.merge_group_code.outputs.code' < <(printf '%s\n' "${changes_block}"); then
		ok "test.yml changes job output falls back to the merge_group step when the filter step is skipped"
	else
		bad "test.yml changes job outputs.code must fall back to the merge_group step's output (steps.filter.outputs.code || steps.merge_group_code.outputs.code)"
	fi
}
