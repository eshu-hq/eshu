#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# advisory_group hand-derived pin (#10466). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh -- read
# that file's header before touching this one. Every value is
# HAND-TYPED, derived from the citations inline, never read back out
# of the registry row.

# The handler (go/internal/reducer/advisory/group/handler.go) reads its
# rows through Winners.QualifyingAdvisoryGroupWinners, whose production
# implementation (go/internal/storage/postgres/supply_chain_impact_advisory_group_winners.go)
# LEFT JOINs fact_records on winner_fact_id. An ACCESS EXCLUSIVE lock
# on fact_records blocks that read -- the blocker_kind this family's
# fault cell takes. The watermark read before it touches
# supply_chain_impact_winners_materialization, not fact_records, so
# the lock holds the handler in its winners read specifically.
#
# A DIRECT-materialization family, so there is no
# shared_projection_intents row for shared_intent_lock to take.
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
# The fact_work_items.domain the production AdvisoryGroupProjectionEnqueuer
# admits under (DomainAdvisoryGroupProjection in
# go/internal/reducer/contract/domain.go).
IFA_FAMILY_PIN_WAIT_KEY="advisory_group_projection"

# go/internal/storage/cypher/edge/writer/advisory_group.go reads
# `MERGE (r)-[e:AFFECTED_BY {package_id: row.package_id}]->(g)`. The
# anchor is matched against EXECUTED statement text as a SUBSTRING, so
# it carries the FULL form including the identity map and the `->(g)`
# close: a prefix stopping at the type would also match the
# stale-edge retract's MATCH in the same template file. AFFECTED_BY
# is the only type that template can emit (no vocabulary substitution).
IFA_FAMILY_PIN_ANCHOR="MERGE (r)-[e:AFFECTED_BY {package_id: row.package_id}]->(g)"
# shared_cell: 0 -- the family needs drain/enqueue/drain sequencing
# inside each N (its own matrix cell, the workload_dependency shape),
# because the shared cell offers no seam between drains to run the
# production winners rebuild and enqueuer.
IFA_FAMILY_PIN_SHARED_CELL=0
# cell_kind: derived from the gate's call sites -- the custom trio in
# scripts/lib/ifa_fault_injection_advisory_group_cells.sh, dispatched
# by name (#10466).
IFA_FAMILY_PIN_CELL_KIND="custom"
