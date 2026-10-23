#!/usr/bin/env bash
# shellcheck shell=bash
# Live-gate drive/assert callbacks for the incident_routing_evidence
# DIRECT-materialization family (#6228 incident_routing_evidence live leaf).
#
# SOURCED BY scripts/verify-ifa-determinism.sh AND the fault gate through
# scripts/lib/ifa_fault_injection_sources.sh -- always right after
# scripts/lib/ifa_direct_family_live_code_taint_evidence.sh, whose
# _ifa_direct_family_drive helper below reuses. Split out rather than
# appended there because that file sits near the tooling line cap; the
# per-family REGISTRY ROWS stay separate, which is where the split that
# matters already is. Callers own strict mode and cleanup.
#
# The assert spells its `-domain incident_routing_evidence` flag out
# literally rather than taking the domain as a parameter. That is
# deliberate: the domain is the one thing a shared helper must not abstract
# away, because it is what makes this family's assertion distinguishable
# from its siblings'. A parameterized call would let one family's coverage
# stand in for another's.
#
# The cassette is single-scope: the PagerDuty scope carries the incident
# anchor, the applied/observed facts (plus the tombstoned applied negative
# control), AND the PagerDutyDeclaration content_entity fact. One live
# generation per scope per cell is the contract (see the family Odù header),
# and the single scope keeps the declared half race-free: the projector
# commits content before it enqueues that generation's reducer intent, so
# the handler's service-name-scoped declared read always sees the
# declaration its own generation carries. A second git scope would leave
# that global read racing the other generation's content commit under the
# concurrent replay drain.

# ifa_incident_routing_evidence_drive replays the incident-routing
# cassette (one scope: the fixture PagerDuty scope).
ifa_incident_routing_evidence_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive incident-routing-evidence \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_incident_routing_evidence_assert pins the three-edge exact set. Called
# twice per cell for the reason recorded on the namespace assert in
# ifa_direct_family_live.sh: post-delta is the only place an
# identical-across-N generation-2 mutation shows up.
#
# The three edges are the one HAS_<slot>_ROUTING edge per member of the
# writer's closed routing-slot vocabulary (intended, applied, live), each
# resolving the PagerDuty service across the shared Checkout API identity
# (declaration literal via the production HCL parser, applied and observed
# joined on PSERVICE6228 with matching PEP1 escalation references). A
# regression that promoted every applied fact to an edge regardless of a
# resolvable declaration would still produce "some edges" and fail only
# against an exact set. The fixture's tombstoned duplicate applied fact --
# skipped before decoding, never a row -- must contribute nothing; the
# builder drops a row whose slot resolves to no exact convergence instead
# of inventing an edge, and this set is what holds it to that.
#
# The relationship types are HAS_INTENDED_ROUTING / HAS_APPLIED_ROUTING /
# HAS_LIVE_ROUTING, read off the writer's slot-to-type map filled per row
# from the closed incidentRoutingSlotVocabulary. They are NOT derived from
# the port or family name.
ifa_incident_routing_evidence_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert incident_routing_evidence materialized edges (three-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain incident_routing_evidence \
		-expected "${expected_edges}"
}
