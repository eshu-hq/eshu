#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# observability_coverage row (#6228 observability_coverage live leaf). See
# ../../ifa_family_registry.sh for the schema and every array declaration
# this file assigns into. Read
# rows/13_iam_instance_profile_role.sh's header first -- it explains what
# a DIRECT-materialization family is and which blocker kinds are
# unavailable to one.
#
# FAULT CELLS LANDED (#6228 observability_coverage live leaf).
# scripts/verify-ifa-fault-injection.sh drives and asserts this row through
# scripts/lib/ifa_fault_injection_observability_coverage_cells.sh
# (baseline/killworker/failgraphwrite trio, dispatched in shard order with a
# co-location atomic group). cell_kind=custom below records the
# hand-written dispatch; specs/ci-gates.v1.yaml gives this family
# ifa-fault-injection triggers so an edit to its cassette or its writer
# re-runs the matrix that observes it.
#
# The writing handler is ObservabilityCoverageMaterializationHandler, a
# single-stage DIRECT writer with no shared handler to contend for: it
# claims its own observability_coverage_materialization work item and the
# registry row's blocker engages its first synchronous read instead. The
# handler -- and therefore the handler-stage wait -- belongs to this family
# alone, so there is no first-landed tie-break to record (contrast the
# derived_from row, which shares its container_image_identity handler with
# built_from).

# Hand-derived, and non-vacuous now a fault cell exists: the
# ACCESS EXCLUSIVE lock engages the fact_records table the handler reads
# AFTER claiming its work item, while that claim is already held -- the
# blocker_kind this row's fault cell takes. The handler struct embeds
# FactLoader
# (go/internal/reducer/obscoverage/observability_coverage_materialization.go:97),
# Handle refuses to run without it (:128), and Handle passes it to
# factload.LoadFactsForKinds (:159), which reads FROM fact_records. Same
# table and same reasoning as rows/19 (s3_logs_to); the readiness wait below
# is the same shape too, because this domain's claim is gated the same way.
IFA_FAMILY_BLOCKER_KIND[observability_coverage]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[observability_coverage]="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from BuildObservabilityCoverageMaterializationReducerIntent
# (go/internal/projector/observability/coverage/materialization/materialization_intents.go:66),
# and corroborated by the domain definition
# (DomainObservabilityCoverageMaterialization in
# go/internal/reducer/contract/domain.go:111). The claim gate for this
# domain is readiness-gated on canonical_nodes_committed/cloud_resource_uid
# (go/internal/storage/postgres/reducer_queue_readiness_sql.go), published
# by the aws_resource_materialization reducer during Handle -- which reads
# fact_records. The alarm edge additionally resolves against the EC2 node,
# which commits under its OWN phase from ec2_instance_posture facts (an
# aws_ec2_instance aws_resource fact never produces a node row, #5448
# CRUX-1): no second claim-time row gates it (the shared summary query froze
# post-#7881), so the handler itself holds that phase in canonicalNodesReady
# and defers retryably, budget-safe, until the node exists. The kill cell
# therefore banks BOTH phases through a dual-domain prereq-scoped reducer
# before the lock goes on (locking first deadlocks by design, and an
# AWS-only prereq would starve the EC2 node commit the alarm edge needs).
# Rendezvous only: the live
# cell asserts the domain through the registry loop's own non-vacuity
# checks, first waiting the claimed projection work item on this domain,
# then banking the readiness phase before the lock. The exact
# `observability_coverage_materialization | succeeded` row the determinism
# drive leaves is CI-observed, not assumed here.
IFA_FAMILY_WAIT_KEY[observability_coverage]="observability_coverage_materialization"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[observability_coverage]=1

IFA_FAMILY_DRIVE_FN[observability_coverage]="ifa_observability_coverage_drive"
IFA_FAMILY_ASSERT_FN[observability_coverage]="ifa_observability_coverage_assert"
IFA_FAMILY_CASSETTE_VAR[observability_coverage]="observability_coverage_cassette"
IFA_FAMILY_EXPECTED_VAR[observability_coverage]="observability_coverage_expected_edges"

# go/internal/storage/cypher/edge/writer/observability_coverage.go:54, which
# reads `MERGE (obs)-[rel:%s]->(target)` in source. The anchor is
# matched against EXECUTED statement text, so it carries the PREFIX form:
# the writer fills %s per row from the closed 5-member coverage-signal
# vocabulary (one statement per signal, sorted by type), so no single
# `]->(target)` spelling matches every executed statement and pinning one
# filled type would leave the other four unfaulted. A `%s` form would match
# no executed statement at all and a scripted graph-write fault would never
# fire -- a green cell that tested nothing.
#
# It is NOT AWS_COVERS: that string is the edge-label const, statement
# metadata carried beside the query that never reaches the graph. The types
# are read off the template, never derived from the port or family name.
IFA_FAMILY_ANCHOR[observability_coverage]="MERGE (obs)-[rel:AWS_COVERS_"
# custom, same dispatch shape as rows/12-13: hand-written cells, dispatched by
# name. The generic table_lock path has never run live and its mandatory
# precondition assumes a `domain` column fact_records does not have
# (scripts/lib/ifa_fault_generic_table_lock.sh's header).
IFA_FAMILY_CELL_KIND[observability_coverage]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[observability_coverage]=""

# 0 on the same footing as rows/12-13: drive_all_cassettes does not produce
# this family; each fault cell drives it through DRIVE_FN/CASSETTE_VAR
# instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[observability_coverage]="0"

IFA_FAMILY_HANDLER_GO_FILE[observability_coverage]="go/internal/reducer/obscoverage/observability_coverage_materialization.go"

IFA_FAMILY_NAMES+=(observability_coverage)
