#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# The shared_intent_lock generic blocker mechanism (split out of
# ifa_fault_generic_cells.sh, scripts/lib/ifa_family_registry.sh's companion
# library, to keep each mechanism file well under the 500-line cap with room
# for the eight more families still to land). This is the ONE mechanism this
# PR actually wires to a live consumer: code_calls and rationale_edges both
# migrate their kill-worker cell to this generic shape (see
# ifa_fault_generic_cells.sh's cell_killworker_family dispatcher and the
# coordination note in that file's header for the exact driver-side swap).
#
# Sourced by ifa_fault_generic_cells.sh, which also supplies the driver-owned
# globals this file reads (bg_pids, log_dir, use_compose, compose_file,
# FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN) and the shared
# ifa_fault_start_shared_intent_lock / ifa_fault_release_shared_intent_lock
# primitives (ifa_fault_injection_common.sh) this mechanism calls directly
# rather than reimplementing.

# _ifa_generic_require_intent_writer is the shared_intent_lock precondition:
# THE MANDATORY PRECONDITION ASSERT for this mechanism, modeled on
# ifa_deployable_unit_require_admission_decisions_written
# (ifa_fault_injection_deployable_unit_lock.sh) but static rather than live.
# A shared_intent_lock blocker only ever locks shared_projection_intents, and
# a handler with no IntentWriter field can never write there regardless of
# what any live run observes -- the defect is provable from source, before
# spending a single Compose cycle on it. This is exactly the check that
# catches codeowners_ownership_edges (ifa_family_registry.sh's blocker_kind
# row comment for that family): a blocker aimed at a table/queue a family's
# handler never touches does not engage, so Handle runs to completion and
# acks before kill -9 lands, and the cell then proves ordinary baseline
# recovery under a name that claims domain-scoped reclaim
# (ifa_fault_injection_deployable_unit_lock.sh:81-86 records this happening
# once already, in CI, for a different family's first lock target). This
# assert MUST fail loudly -- never silently pass -- when the blocker would
# not have engaged.
_ifa_generic_require_intent_writer() {
	local family="$1" handler_file
	handler_file="$(ifa_family_handler_go_file "${family}")" || return 1
	if [[ -z "${handler_file}" ]]; then
		printf 'cell_killworker_family (%s): PRECONDITION FAILED: no handler_go_file registered in ifa_family_registry.sh -- cannot statically verify this shared_intent_lock blocker engages a real write\n' "${family}" >&2
		return 1
	fi
	if [[ ! -f "${handler_file}" ]]; then
		printf 'cell_killworker_family (%s): PRECONDITION FAILED: registered handler_go_file %s does not exist\n' "${family}" "${handler_file}" >&2
		return 1
	fi
	# Matches a FIELD DECLARATION, not the word anywhere in the file. The
	# earlier form was `rg --fixed-strings 'IntentWriter'`, which every one of
	# these handlers satisfies twice over without having the field: each
	# declares its writer INTERFACE in the same file as the struct
	# (CodeCallIntentWriter at code_call_materialization.go:32,
	# RationaleEdgeIntentWriter at rationale_edge_materialization.go:26). A
	# refactor that drops the struct field but keeps the interface -- or that
	# leaves the word in a comment -- passed. Proven by feeding it a handler
	# whose only occurrence was in a comment: rc=0, "precondition confirmed".
	# The declaration form is uniform across every current writer
	# (`rg -n '^\s+IntentWriter\s' go/internal/reducer/*.go`), so anchoring on
	# it is not a guess.
	# rg's exit code is THREE-valued and the difference matters: 0 match, 1 no
	# match, anything else an error (127 when rg is not installed at all). An
	# `if ! rg ...` collapses all of those into "no match", which is how #6173's
	# first CI run reported "declares no IntentWriter" for a handler that holds
	# the field on line 47 -- the fault-injection job simply had no ripgrep, and
	# the precondition turned a missing tool into a confident false diagnosis of
	# the code. A guard that cannot tell "I looked and it is absent" from "I
	# could not look" is the defect class this whole mechanism exists to prevent,
	# so it is spelled out rather than folded back into an if.
	local rg_rc=0
	rg --quiet '^[[:space:]]+IntentWriter[[:space:]]' "${handler_file}" || rg_rc=$?
	if [[ "${rg_rc}" -gt 1 ]]; then
		printf 'cell_killworker_family (%s): PRECONDITION INDETERMINATE: could not search %s (rg exit %s -- not installed, or unreadable). This is NOT a verdict about the handler; the cell refuses rather than guessing.\n' \
			"${family}" "${handler_file}" "${rg_rc}" >&2
		return 1
	fi
	if [[ "${rg_rc}" -eq 1 ]]; then
		printf 'cell_killworker_family (%s): PRECONDITION FAILED: %s declares no IntentWriter. The shared_intent_lock blocker targets shared_projection_intents, a table this handler never writes to, so the lock cannot engage -- Handle would run to completion and ack before kill -9 lands, proving ordinary baseline recovery under a name that claims domain-scoped reclaim (the exact defect class ifa_fault_injection_deployable_unit_lock.sh:81-86 already documents for a different family and lock target). Register this family'"'"'s true blocker_kind in ifa_family_registry.sh instead of forcing shared_intent_lock.\n' \
			"${family}" "${handler_file}" >&2
		return 1
	fi
	printf 'cell_killworker_family (%s): precondition confirmed: %s declares an IntentWriter -- the shared_intent_lock blocker targets a table this handler genuinely writes to\n' "${family}" "${handler_file}"
}

# _ifa_generic_cell_killworker_shared_intent_lock is the mechanism's thin
# wrapper: run the precondition BEFORE calling the shared skeleton
# (ifa_fault_generic_cells.sh's _ifa_generic_cell_killworker_body) -- the
# precondition must fire before any lock is even attempted, not buried
# inside the shared body.
_ifa_generic_cell_killworker_shared_intent_lock() {
	local family="$1" cell="genkillworker${1//_/}"
	_ifa_generic_require_intent_writer "${family}" \
		|| die "${cell}: mandatory precondition failed (see above) -- refusing to run a kill cell whose blocker cannot engage"
	_ifa_generic_cell_killworker_body "${family}" "${cell}" shared_intent_lock "${family}"
}
