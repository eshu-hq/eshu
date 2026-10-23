#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# incident_routing_evidence row (#6228 incident_routing_evidence live leaf).
# See ../../ifa_family_registry.sh for the schema and every array declaration
# this file assigns into. Read
# rows/13_iam_instance_profile_role.sh's header first -- it explains what
# a DIRECT-materialization family is and which blocker kinds are
# unavailable to one.
#
# FAULT CELLS LANDED (#6228 incident_routing_evidence live leaf).
# scripts/verify-ifa-fault-injection.sh drives and asserts this row through
# scripts/lib/ifa_fault_injection_incident_routing_evidence_cells.sh
# (baseline/killworker/failgraphwrite trio, dispatched in shard order with
# a co-location atomic group). cell_kind=custom below records the
# hand-written dispatch; specs/ci-gates.v1.yaml gives this family
# ifa-fault-injection triggers so an edit to its cassette or its writer
# re-runs the matrix that observes it.
#
# The writing handler is IncidentRoutingMaterializationHandler, a
# single-stage DIRECT writer with no shared handler to contend for: it
# claims its own incident_routing_materialization work item and the
# registry row's blocker engages its first synchronous read instead. The
# handler -- and therefore the handler-stage wait -- belongs to this family
# alone, so there is no first-landed tie-break to record (contrast the
# derived_from row, which shares its container_image_identity handler with
# built_from).
#
# Hand-derived, and non-vacuous now a fault cell exists: the
# ACCESS EXCLUSIVE lock engages the fact_records table the handler reads
# AFTER claiming its work item, while that claim is already held -- the
# blocker_kind this row's fault cell takes. The handler struct carries a
# Loader (go/internal/reducer/incident/routing_materialization.go:47),
# Handle refuses to run without it (:64) and without its Writer (:67),
# and Handle loads through it at :72 into
# LoadIncidentRoutingRawEvidence, which reads fact_records via
# ListFactsByKind
# (go/internal/storage/postgres/incident_routing_evidence_loader.go:53
# into go/internal/storage/postgres/facts_filtered.go:142, FROM
# fact_records). Same table and same reasoning as rows/19 (s3_logs_to);
# the readiness wait below is absent rather than handler-held, because
# this domain carries no claim-time readiness requirement and no
# in-handler readiness gate before the load (the crossplane shape, not
# the observability_coverage shape): the first claim blocks
# deterministically in the load, so a prereq-scoped reducer would add a
# wait nothing here needs. The content_entities half of that same load is
# race-free by construction, not by banking: the projector commits
# content before it enqueues the generation's reducer intent, so the
# declaration row this load joins against is already committed when any
# claim from this generation can exist.
IFA_FAMILY_BLOCKER_KIND[incident_routing_evidence]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[incident_routing_evidence]="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from routing.BuildReducerIntent
# (go/internal/projector/incident/routing/reducer_intent.go:30), and
# corroborated by the domain definition
# (DomainIncidentRoutingMaterialization in
# go/internal/reducer/contract/domain.go:95). The live cell asserts the
# domain through the registry loop's own non-vacuity checks, first waiting
# the claimed projection work item on this domain. The exact
# `incident_routing_materialization | succeeded` row the determinism drive
# leaves is CI-observed, not assumed here.
IFA_FAMILY_WAIT_KEY[incident_routing_evidence]="incident_routing_materialization"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[incident_routing_evidence]=1

IFA_FAMILY_DRIVE_FN[incident_routing_evidence]="ifa_incident_routing_evidence_drive"
IFA_FAMILY_ASSERT_FN[incident_routing_evidence]="ifa_incident_routing_evidence_assert"
IFA_FAMILY_CASSETTE_VAR[incident_routing_evidence]="incident_routing_evidence_cassette"
IFA_FAMILY_EXPECTED_VAR[incident_routing_evidence]="incident_routing_evidence_expected_edges"

# go/internal/storage/cypher/edge/writer/incident_routing_evidence.go:73, which
# reads `MERGE (incident)-[rel:%s]->(routing)` in source. The anchor is
# matched against EXECUTED statement text, so it carries the PREFIX form:
# the writer fills %s per row from the closed 3-member routing-slot
# vocabulary (one statement per slot, sorted by type), so no single
# `]->(routing)` spelling matches every executed statement and pinning one
# filled type would leave the other two unfaulted. A `%s` form would match
# no executed statement at all and a scripted graph-write fault would never
# fire -- a green cell that tested nothing.
#
# It is NOT a bare relationship name: those strings are edge-label metadata
# carried beside the query that never reaches the graph. The types are
# read off the writer's slot-to-type map, never derived from the port or
# family name.
IFA_FAMILY_ANCHOR[incident_routing_evidence]="MERGE (incident)-[rel:HAS_"
# custom, same dispatch shape as rows/12-13: hand-written cells, dispatched by
# name. The generic table_lock path has never run live and its mandatory
# precondition assumes a `domain` column fact_records does not have
# (scripts/lib/ifa_fault_generic_table_lock.sh's header).
IFA_FAMILY_CELL_KIND[incident_routing_evidence]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[incident_routing_evidence]=""

# 0 on the same footing as rows/12-13: drive_all_cassettes does not produce
# this family; each fault cell drives it through DRIVE_FN/CASSETTE_VAR
# instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[incident_routing_evidence]="0"

IFA_FAMILY_HANDLER_GO_FILE[incident_routing_evidence]="go/internal/reducer/incident/routing_materialization.go"

IFA_FAMILY_NAMES+=(incident_routing_evidence)
