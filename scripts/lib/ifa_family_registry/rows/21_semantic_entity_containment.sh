#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# semantic_entity_containment row (#8359, #6228 arbiter verdict (a)). See
# ../../ifa_family_registry.sh for the schema and every array declaration
# this file assigns into. Read rows/13_iam_instance_profile_role.sh's header
# first -- it explains what a DIRECT-materialization family is and which
# blocker kinds are unavailable to one. This row carries NO fault cells yet:
# the determinism gate drives it through the shared N={1,2,4} cell below,
# and the fault-injection area stays untouched (cell_kind=custom is rejected
# by the generic dispatchers, and no custom cell file exists, so the fault
# gate never dispatches this family).
#
# Shared cell, not dedicated, by the verdict: ruling (a) is one registry row
# with SHARED_CELL=1 and CELL_KIND=custom, drive/assert in
# scripts/lib/ifa_direct_family_live.sh, cassette/expected vars in
# scripts/lib/ifa_family_fixtures.sh -- identical in kind to rows 12-20.
# Rejected there: sibling subtraction (destroys exactness, couples to
# sibling cassettes), a dedicated cell (pays wall time, forfeits
# shared-digest coverage), a graph-wide unscoped assert (one sibling edit
# reds it), and widening the stamp fence (asserts another writer's
# population).

# Hand-derived, and non-vacuous once the determinism gate drives it:
# semantic.Handler embeds `FactLoader factload.FactLoader`
# (go/internal/reducer/code/semantic/materialization.go:65)
# and Handle refuses to run without it (:83) before passing it to
# factload.LoadFactsForKinds (:92). The handler reads fact_records AFTER
# claiming its work item, so an ACCESS EXCLUSIVE lock on that table would hold
# it genuinely in flight -- the blocker_kind a future fault cell would take.
# Same table and same reasoning as rows/13; see that row for why
# fact_work_items is the wrong target. A DIRECT-materialization family, so
# there is no shared_projection_intents row for shared_intent_lock to take.
IFA_FAMILY_BLOCKER_KIND[semantic_entity_containment]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[semantic_entity_containment]="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from the domain definition (DomainSemanticEntityMaterialization in
# go/internal/reducer/contract/domain.go:145, checked by Handle at
# go/internal/reducer/code/semantic/materialization.go:76).
# To be proven on a live stack by this row's own determinism drive, which
# must leave exactly one `semantic_entity_materialization | succeeded` row
# per cell -- until that drive lands, the wait key is hand-derived, not
# measured.
IFA_FAMILY_WAIT_KEY[semantic_entity_containment]="semantic_entity_materialization"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[semantic_entity_containment]=1

IFA_FAMILY_DRIVE_FN[semantic_entity_containment]="ifa_semantic_entity_containment_drive"
IFA_FAMILY_ASSERT_FN[semantic_entity_containment]="ifa_semantic_entity_containment_assert"
IFA_FAMILY_CASSETTE_VAR[semantic_entity_containment]="semantic_entity_containment_cassette"
IFA_FAMILY_EXPECTED_VAR[semantic_entity_containment]="semantic_entity_containment_expected_edges"

# go/internal/storage/cypher/semantic_entity_statements.go's per-label
# templates share one relationship shape (`MERGE (f)-[rel:CONTAINS]->(n)`,
# e.g. semanticFunctionUpsertCypher; the registry's representative is
# SemanticEntityContainmentUpsertCypher). The anchor is matched against
# EXECUTED statement text, so it carries the filled relationship form with
# the static CONTAINS token -- pinning a label-alternation form would match
# no NornicDB-executed statement (measured 2026-10-17: alternations inside
# a relationship pattern match nothing on the pinned NornicDB).
#
# It is NOT SEMANTIC_ENTITY_CONTAINMENT: that string is family metadata
# carried beside the query that never reaches the graph. The type is read
# off the template, never derived from the port or family name.
IFA_FAMILY_ANCHOR[semantic_entity_containment]="MERGE (f)-[rel:CONTAINS]->(n)"
# custom, prospectively: no fault cell file exists for this family yet, and the
# generic public dispatchers reject `cell_kind=custom`, so the fault gate
# cannot dispatch it by accident. Flip only with a proven fault cell, never to
# generic without the lifecycle the generic header demands.
IFA_FAMILY_CELL_KIND[semantic_entity_containment]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[semantic_entity_containment]=""

# 0: drive_all_cassettes does not produce this family, and no fault cell
# drives it either; each determinism cell drives it through
# DRIVE_FN/CASSETTE_VAR instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[semantic_entity_containment]="0"

IFA_FAMILY_HANDLER_GO_FILE[semantic_entity_containment]="go/internal/reducer/code/semantic/materialization.go"

IFA_FAMILY_NAMES+=(semantic_entity_containment)
