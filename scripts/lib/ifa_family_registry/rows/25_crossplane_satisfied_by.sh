#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# crossplane_satisfied_by row (#6228 crossplane live leaf). See
# ../../ifa_family_registry.sh for the schema and every array declaration
# this file assigns into. Read
# rows/13_iam_instance_profile_role.sh's header first -- it explains what
# a DIRECT-materialization family is and which blocker kinds are
# unavailable to one.
#
# FAULT CELLS LANDED (#6228 crossplane live leaf).
# scripts/verify-ifa-fault-injection.sh drives and asserts this row through
# scripts/lib/ifa_fault_injection_crossplane_satisfied_by_cells.sh
# (baseline/killworker/failgraphwrite trio, dispatched in shard order with
# a co-location atomic group). cell_kind=custom below records the
# hand-written dispatch; specs/ci-gates.v1.yaml gives this family
# ifa-fault-injection triggers so an edit to its cassette or its writer
# re-runs the matrix that observes it.
#
# The writing handler here is CrossplaneSatisfiedByMaterializationHandler,
# not a *_materialization handler behind a shared port: the crossplane
# satisfied-by intent fans out under its own additive domain (see
# WAIT_KEY), so the anchor below names the crossplane writer's own
# MATCH-MATCH-MERGE statement.

# Hand-derived, and non-vacuous now a fault cell exists:
# CrossplaneSatisfiedByMaterializationHandler embeds `FactLoader
# factload.FactLoader`
# (go/internal/reducer/crossplane/satisfied_by_materialization.go:144-148)
# and Handle refuses to run without it (:177-179) before loading its facts
# through loadEdgeFacts at :205. The handler reads fact_records AFTER
# claiming its work item, so an ACCESS EXCLUSIVE lock on that table holds
# it genuinely in flight -- the blocker_kind this row's fault cell takes.
# Same table and same reasoning as rows/13; see that row for why
# fact_work_items is the wrong target.
#
# No readiness banking (the k8s shape, not the crc-image one): this handler
# carries no in-handler readiness gate before its fact load (reviewed
# deviation, issue #5347 -- the projector commits both endpoint nodes
# before the intent enqueues, and cross-scope XRDs gate on generation
# activation), so the first claim blocks deterministically in its load.
# A prereq-scoped reducer would add a wait nothing here needs.
IFA_FAMILY_BLOCKER_KIND[crossplane_satisfied_by]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[crossplane_satisfied_by]="handler"
# The fact_work_items.domain the projector fans this family out under, read
# off BuildCrossplaneSatisfiedByMaterializationReducerIntent
# (go/internal/projector/crossplane/satisfaction/satisfied_by_intents.go:54),
# and corroborated by the domain definition
# (DomainCrossplaneSatisfiedByMaterialization in
# go/internal/reducer/contract/domain.go:57). The live cell asserts the
# domain through the registry loop's own non-vacuity checks; the exact
# `crossplane_satisfied_by_materialization | succeeded` row the determinism
# drive leaves is CI-observed, not assumed here.
IFA_FAMILY_WAIT_KEY[crossplane_satisfied_by]="crossplane_satisfied_by_materialization"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[crossplane_satisfied_by]=1

IFA_FAMILY_DRIVE_FN[crossplane_satisfied_by]="ifa_crossplane_satisfied_by_drive"
IFA_FAMILY_ASSERT_FN[crossplane_satisfied_by]="ifa_crossplane_satisfied_by_assert"
IFA_FAMILY_CASSETTE_VAR[crossplane_satisfied_by]="crossplane_satisfied_by_cassette"
IFA_FAMILY_EXPECTED_VAR[crossplane_satisfied_by]="crossplane_satisfied_by_expected_edges"

# go/internal/storage/cypher/canonical/crossplane/edges.go:33-42 reads
# `MERGE (claim)-[rel:SATISFIED_BY]->(xrd)`. The anchor is matched against
# EXECUTED statement text, so it carries the template's own spelling: a
# fixed MERGE with no %s vocabulary substitution, pinning the one type this
# family's writer can emit (SATISFIED_BY). It is NOT derived from the port
# or family name.
IFA_FAMILY_ANCHOR[crossplane_satisfied_by]="MERGE (claim)-[rel:SATISFIED_BY]->(xrd)"
# custom, same dispatch shape as rows/12-13: hand-written cells, dispatched by
# name. The generic table_lock path has never run live and its mandatory
# precondition assumes a `domain` column fact_records does not have
# (scripts/lib/ifa_fault_generic_table_lock.sh's header).
IFA_FAMILY_CELL_KIND[crossplane_satisfied_by]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[crossplane_satisfied_by]=""

# 0 on the same footing as rows/12-13: drive_all_cassettes does not produce
# this family; each fault cell drives it through DRIVE_FN/CASSETTE_VAR
# instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[crossplane_satisfied_by]="0"

IFA_FAMILY_HANDLER_GO_FILE[crossplane_satisfied_by]="go/internal/reducer/crossplane/satisfied_by_materialization.go"

IFA_FAMILY_NAMES+=(crossplane_satisfied_by)
