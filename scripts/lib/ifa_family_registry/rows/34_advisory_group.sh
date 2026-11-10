#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# advisory_group row (#10466 advisory_group Odù lift). See
# ../../ifa_family_registry.sh for the schema and every array declaration
# this file assigns into. Read rows/31_runtime_declaration.sh's header
# first -- it explains the table_lock shape this row shares.
#
# This family is WINNERS-FED, unlike every sibling direct family: its
# handler (go/internal/reducer/advisory/group/handler.go) loads no
# facts of its own scope. It reads the canonical winners read model
# through FactStore.QualifyingAdvisoryGroupWinners
# (go/internal/storage/postgres/supply_chain_impact_advisory_group_winners.go),
# which LEFT JOINs fact_records on winner_fact_id for the purl and the
# provenance members. An ACCESS EXCLUSIVE lock on fact_records
# therefore holds the handler genuinely in flight in that read, after
# its watermark check (a read of supply_chain_impact_winners_materialization,
# a different table the lock does not touch) has already passed.
#
# No projector trigger exists for this domain: production enqueues
# advisory_group_projection from a committed supply_chain_impact pass
# through AdvisoryGroupProjectionEnqueuer, so the live cells invoke the
# production winners rebuild and that enqueuer mid-drive
# (eshu-ifa enqueue-advisory-groups, scripts/lib/ifa_advisory_group_live.sh).
# There is deliberately no shared_followup marker in the cassette --
# no collector emits one for this domain (#10466 arbiter ruling).
#
# shared_cell=0: the family needs its own drain/enqueue/drain
# sequencing inside each N (the workload_dependency matrix-cell shape),
# because the shared cell starts its single projector/reducer pair
# only after every drive, with no seam to enqueue between drains. Its
# edges still land in the shared stack before the main drives, so they
# participate in the compared canonical digest.
IFA_FAMILY_BLOCKER_KIND[advisory_group]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[advisory_group]="handler"
# The fact_work_items.domain the production enqueuer admits these
# intents under (DomainAdvisoryGroupProjection in
# go/internal/reducer/contract/domain.go), corroborated by the
# handler's own domain check.
IFA_FAMILY_WAIT_KEY[advisory_group]="advisory_group_projection"
IFA_FAMILY_SHARED_CELL[advisory_group]=0

IFA_FAMILY_CASSETTE_VAR[advisory_group]="advisory_group_cassette"
IFA_FAMILY_EXPECTED_VAR[advisory_group]="advisory_group_expected_edges"

# go/internal/storage/cypher/edge/writer/advisory_group.go, which reads
# `MERGE (r)-[e:AFFECTED_BY {package_id: row.package_id}]->(g)`. The
# anchor is matched against EXECUTED statement text, so it carries the
# FULL form including the package_id identity map and the `->(g)`
# close: a prefix stopping at the type would also match the stale-edge
# retract's MATCH in the same file. AFFECTED_BY is the only type this
# writer can emit (static token, no vocabulary substitution).
IFA_FAMILY_ANCHOR[advisory_group]="MERGE (r)-[e:AFFECTED_BY {package_id: row.package_id}]->(g)"
# none UNTIL the fault trio lands with this row's family: cell_kind
# records dispatch reality, and no fault cell dispatches this family
# yet. The trio's own change flips this to custom in the same commit
# that wires the dispatch.
IFA_FAMILY_CELL_KIND[advisory_group]="none"

# Not a shared_intent_lock family, so no retry baseline is required.
# Declared empty rather than omitted -- see rows/12 for why an absent
# key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[advisory_group]=""

# 0: drive_all_cassettes does not produce this family; its own cells
# drive it through the advisory_group live lib instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[advisory_group]="0"

IFA_FAMILY_HANDLER_GO_FILE[advisory_group]="go/internal/reducer/advisory/group/handler.go"

IFA_FAMILY_NAMES+=(advisory_group)
