#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# code_calls-targeted live fault cells (#5991). This function library is sourced
# by verify-ifa-fault-injection.sh; the driver owns strict mode and globals.

# The live driver sources the common library first. Focused helper tests source
# this file alone, so load the shared implementation only in that case.
if ! declare -F ifa_fault_require_fresh_domain_intents >/dev/null; then
	# shellcheck source=scripts/lib/ifa_fault_injection_common.sh
	source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/ifa_fault_injection_common.sh"
fi

# Compatibility wrappers keep the focused helper regressions stable while the
# live cells use the shared helpers directly. New domains must call the shared
# functions rather than copy this SQL/lock lifecycle again.
ifa_code_call_require_fresh_intents() {
	local cell="$1" compose_project="$2" use_compose_arg="$3" postgres_dsn="$4" compose_file_arg="$5"
	ifa_fault_require_fresh_domain_intents \
		"${cell}" code_calls "${compose_project}" "${use_compose_arg}" "${postgres_dsn}" "${compose_file_arg}"
}

# cell_killworker_code_calls proves a genuinely in-flight code-call handler is
# reclaimed after process death; cell_failgraphwrite_code_calls fails the live
# CALLS MERGE exactly once and proves the durable marker names that write.
# Both now delegate to the generic, registry-driven dispatcher
# (scripts/lib/ifa_fault_generic_cells.sh): the code_calls row in
# scripts/lib/ifa_family_registry/rows/02_code_calls.sh declares
# blocker_kind=shared_intent_lock and wait_key=code_call_materialization,
# which is exactly the shape this file used to hand-write, before the registry
# existed -- see that dispatcher's header for the shared kill/reclaim/drain/
# assert skeleton and the uniform fail-graph-write cell.
cell_killworker_code_calls() {
	cell_killworker_family code_calls
}

cell_failgraphwrite_code_calls() {
	cell_failgraphwrite_family code_calls
}
