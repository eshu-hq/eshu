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
# under the repo's 500-line file rule, the same seam the merge_group checks use.

# run_predicate_quantifier_check <workflow...> — asserts each workflow's
# changes/filter step sets the quantifier, reporting through the caller's ok/bad
# (shell functions are global, so the calling script's counters stay accurate).
# Requires job_block from the caller and step_block from
# ci-gate-merge-group-checks.sh.
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
