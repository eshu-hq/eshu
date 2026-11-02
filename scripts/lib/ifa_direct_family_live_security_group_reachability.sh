#!/usr/bin/env bash
# shellcheck shell=bash
# Live-gate drive/assert callbacks for the security-group reachability
# DIRECT-materialization pair (#6228 secgroup-pair live leaf):
# security_group_sg_rule (SecurityGroup -> SecurityGroupRule
# ALLOWS_INGRESS / ALLOWS_EGRESS) and security_group_rule_endpoint
# (SecurityGroupRule -[:TO]-> endpoint).
#
# SOURCED BY scripts/verify-ifa-determinism.sh AND the fault gate through
# scripts/lib/ifa_fault_injection_sources.sh -- always right after
# scripts/lib/ifa_direct_family_live_publishes.sh, whose
# _ifa_direct_family_drive helper below reuses. Split out rather than
# appended there because that file sits near the tooling line cap; the
# per-family REGISTRY ROWS stay separate, which is where the split that
# matters already is. Callers own strict mode and cleanup.
#
# ONE drive serves TWO families (sibling precedent: handles_route /
# runs_in / invokes_cloud_action share ifa_symbol_runtime_drive across
# rows/09-11): both guards run the same production
# ExtractSecurityGroupReachability over the same facts and read different
# row subsets, so the cassette is driven once per registry row while each
# assert below spells its OWN `-domain <family>` flag out literally rather
# than taking the domain as a parameter. That is deliberate: the domain is
# the one thing a shared helper must not abstract away, because it is what
# makes this pair's assertions distinguishable from each other. A
# parameterized call would let one family's coverage stand in for the
# other's.
#
# The projector fans THREE intents out of this one cassette
# (go/internal/projector/security/group_reachability_intents.go:35-75):
# security_group_rule_materialization (rule nodes),
# security_group_cidr_materialization (CIDR/prefix endpoint nodes), and
# security_group_reachability_materialization (the edges). The edge
# handler's triple readiness gate resolves because the node domains commit
# from the same drive -- there is no second cassette to wire.

# ifa_security_group_reachability_drive replays the reachability cassette
# (one scope: the three scanned group facts the join index resolves
# against, plus the nine rule facts -- five edge producers and four
# deliberate non-producers).
ifa_security_group_reachability_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive security-group-reachability \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_security_group_sg_rule_assert pins the five-edge exact set. Called
# twice per cell for the reason recorded on the namespace assert in
# ifa_direct_family_live.sh: post-delta is the only place an
# identical-across-N generation-2 mutation shows up.
#
# The five edges are web-sg's ALLOWS_INGRESS ssh rule (22/tcp from
# 192.0.2.0/24), web-sg's ALLOWS_EGRESS https rule (443/tcp to
# 192.0.2.0/24 -- the direction that proves the second closed-vocabulary
# type), db-sg's ALLOWS_INGRESS http rule (80/tcp from the prefix list),
# db-sg's ALLOWS_INGRESS postgres rule (5432/tcp from the referenced
# group), and db-sg's ALLOWS_INGRESS dns rule (53/udp from the IPv6 CIDR).
# A regression that resolved every rule against the first group would still
# produce "some edges" and fail only against an exact set. The fixture's
# four non-producers -- the ghost-anchored rule (unresolved anchor), the
# ghost-referencing rule (unresolved endpoint), the sourceless rule
# (unknown source), and the tombstoned rule (retracted generation) -- must
# contribute nothing on either path; the extractor classifies those instead
# of fabricating edges, and this set is what holds it to that.
#
# The relationship type is ALLOWS_INGRESS or ALLOWS_EGRESS per rule,
# read off the writer's closed direction vocabulary
# (security_reachability_edge_writer.go:87 `MERGE (sg)-[rel:%s]->(rule)`).
# It is NOT derived from the port or family name.
ifa_security_group_sg_rule_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert security_group_sg_rule materialized edges (five-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain security_group_sg_rule \
		-expected "${expected_edges}"
}

# ifa_security_group_rule_endpoint_assert pins the five-edge exact set.
# Called twice per cell, same post-delta rationale as its sibling above.
#
# The five edges are the same five resolved rules' TO edges: the two IPv4
# CIDR rules onto their recomputed CidrBlock uid, the prefix-list rule onto
# its account-scoped PrefixList uid, the referenced-group rule onto the
# group's CloudResource uid (resolved through the same join index as the
# anchor, not a recomputed uid), and the IPv6 CIDR rule onto its CidrBlock
# uid. A regression that landed every TO edge on one endpoint node would
# still produce "some edges" and fail only against an exact set. The set
# asserts (from, to) uids only, with no target_label edge property: the
# production TO edges never carried one (the writer SETs scope/generation/
# evidence only), and the label needs no duplication -- the writer's
# MATCH (target:LABEL) enforces label agreement at write time, so a
# regression resolving the right uid under the wrong label no-ops the
# write and fails here as missing rather than passing. The same four
# non-producers must contribute nothing here either.
#
# The relationship type is the static TO token, read off
# canonicalSecurityGroupToEdgeUpsertCypherFormat
# (security_reachability_edge_writer.go:101 `MERGE (rule)-[rel:TO]->(target)`).
ifa_security_group_rule_endpoint_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert security_group_rule_endpoint materialized edges (five-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain security_group_rule_endpoint \
		-expected "${expected_edges}"
}
