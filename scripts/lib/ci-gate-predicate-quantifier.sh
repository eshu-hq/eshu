#!/usr/bin/env bash
#
# ci-gate-predicate-quantifier.sh — the #5896 guard for verify-docs-only-ci-skip.sh.
#
# dorny/paths-filter@v3 defaults to `predicate-quantifier: some`, under which a
# file is included as soon as ONE pattern matches. `**` always matches, so every
# `!` negation in the shared `code` filter is dead text and `code` is true for
# every PR — including a docs-only one, which is how #5818 burned ~118
# runner-minutes. Only `predicate-quantifier: 'every'` makes a negation mean
# anything, so its presence is asserted rather than remembered.
#
# Two things this deliberately does NOT do, both learned from review:
#
#   - It does not demand a quoting style. YAML reads every, 'every' and "every"
#     identically and dorny accepts all three, so pinning one style would fail a
#     PR whose filter is correct. The VALUE is still exact, because v3 accepts
#     only {'every','some'} and silently treats anything else as the default —
#     the plausible typo 'all' is indistinguishable from deleting the line.
#   - It does not search the file. A file-wide match would still pass if the
#     line were moved out of the paths-filter step, or if a SECOND dorny step
#     carried it while the `code` filter reverted to the default (#5956 review,
#     codex). The check is scoped to the `filter` step of the `changes` job —
#     the step that actually computes `code` — and fails closed when that step
#     cannot be found.
#
# Lives in scripts/lib/ rather than inline to keep verify-docs-only-ci-skip.sh
# under the repo's 500-line file rule.

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
	# PREFIX of the target, or a step whose run: body merely mentions the
	# id text with extra characters or different indentation, in both cases
	# silently returning the WRONG step. Tracking a per-step boolean set
	# only on an exact line match stops those: a longer id never equals the
	# shorter needle, and a casual echo never equals the bare field line.
	# Residual: the match has no YAML-structure awareness, so a run:
	# literal block containing an exactly-8-space-indented "id: <step>"
	# line still sets the flag.
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

# run_predicate_quantifier_check <workflow...> — asserts each workflow's
# changes/filter step sets the quantifier, reporting through the caller's ok/bad
# (shell functions are global, so the calling script's counters stay accurate).
# Requires job_block from the caller.
run_predicate_quantifier_check() {
	local wf step missing=""
	for wf in "$@"; do
		step="$(step_block "$(job_block "${wf}" changes)" filter)"
		if [[ -z "${step}" ]] || ! rg -q \
			"^[[:space:]]*predicate-quantifier:[[:space:]]*(every|'every'|\"every\")[[:space:]]*$" \
			<<<"${step}"; then
			missing="${missing} $(basename "${wf}")"
		fi
	done
	if [[ -n "${missing}" ]]; then
		bad "the changes/filter step is missing \`predicate-quantifier: 'every'\` in:${missing} — without it dorny defaults to \`some\`, \`**\` short-circuits, and every \`!\` negation in the code: filter is inert (#5896)"
	else
		ok "all three code: filters set predicate-quantifier to every, so their negations actually exclude"
	fi
}
