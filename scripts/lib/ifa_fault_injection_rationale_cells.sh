#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# rationale_edges-targeted live recovery cells (#5998). This library is
# sourced by verify-ifa-fault-injection.sh; the driver owns strict mode,
# lifecycle, cassette paths, process tracking, and failure reporting.

# cell_killworker_rationale proves the exact rationale_materialization work
# item is reclaimed after the reducer process dies; cell_failgraphwrite_rationale
# fails the production EXPLAINS MERGE exactly once on the queue-retry lane.
# Both now delegate to the generic, registry-driven dispatcher
# (scripts/lib/ifa_fault_generic_cells.sh): the rationale_edges row in
# scripts/lib/ifa_family_registry/rows/04_rationale_edges.sh declares
# blocker_kind=shared_intent_lock and wait_key=rationale_materialization,
# which is exactly the shape this file used to hand-write, before the registry
# existed -- see that dispatcher's header for the shared kill/reclaim/drain/
# assert skeleton and the uniform fail-graph-write cell.
cell_killworker_rationale() {
	cell_killworker_family rationale_edges
}

cell_failgraphwrite_rationale() {
	cell_failgraphwrite_family rationale_edges
}
