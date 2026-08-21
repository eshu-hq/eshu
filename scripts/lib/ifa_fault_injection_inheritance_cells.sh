#!/usr/bin/env bash
# shellcheck shell=bash disable=SC2154
# inheritance_edges fault cells (#5996). Three one-line delegations to the
# generic dispatcher in scripts/lib/ifa_fault_generic_cells.sh -- read that
# file's header for the shared baseline/kill/fail-graph-write skeleton and for
# why a family outside drive_all_cassettes needs its own baseline at all.
#
# There is deliberately no bespoke logic here. This family's blocker_kind is
# shared_intent_lock and its cell_kind is generic, so every decision the cells
# make -- which cassette to drive, which blocker to take, which domain to wait
# on, which baseline digest to compare against -- is read from the registry row
# rather than restated. A hand-written cell set would be the template-copy
# defect this dispatcher exists to end: the codeowners row records how a copied
# cell inherited a lock that engaged nothing.
#
# The baseline cell is not optional for this family. It is FAULT_SHARED_DRIVE=0,
# so the shared cell_baseline never drives its cassette and digests[baseline]
# does not contain its edges; without a family-scoped baseline the two recovery
# cells would report a graph divergence that is really a fixture difference.

cell_baseline_inheritance() {
	cell_baseline_family inheritance_edges
}

cell_killworker_inheritance() {
	cell_killworker_family inheritance_edges
}

cell_failgraphwrite_inheritance() {
	cell_failgraphwrite_family inheritance_edges
}
