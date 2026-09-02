#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# inheritance_edges row (#5996). See ../../ifa_family_registry.sh for the
# schema and every array declaration this file assigns into.

# Non-vacuous, unlike the shape codeowners inherited: this handler really does
# write shared-projection intents -- inheritance.IntentWriter is a declared
# dependency (go/internal/reducer/inheritance/materialization.go:62) and Handle
# calls UpsertIntents on it (:175), so a lock on shared_projection_intents
# blocks a write this family actually performs.
IFA_FAMILY_BLOCKER_KIND[inheritance_edges]="shared_intent_lock"
IFA_FAMILY_WAIT_STAGE[inheritance_edges]="handler"
# reducer.DomainInheritanceMaterialization (go/internal/reducer/intent.go:62) --
# the FIRST-stage handler domain, which is what a wait_stage=handler cell
# watches. The SECOND-stage shared-projection domain is the different string
# "inheritance_edges" (reducer.DomainInheritanceEdges, shared_projection.go:21);
# do not substitute one for the other.
IFA_FAMILY_WAIT_KEY[inheritance_edges]="inheritance_materialization"
# A plain reducer family: no bootstrap-index maintenance pass is needed for it
# to materialize, so it is driven uniformly in the determinism gate's shared
# N={1,2,4} cell rather than a standalone one.
IFA_FAMILY_SHARED_CELL[inheritance_edges]=1

IFA_FAMILY_DRIVE_FN[inheritance_edges]="ifa_inheritance_drive"
IFA_FAMILY_ASSERT_FN[inheritance_edges]="ifa_inheritance_assert"
IFA_FAMILY_CASSETTE_VAR[inheritance_edges]="inheritance_cassette"
IFA_FAMILY_EXPECTED_VAR[inheritance_edges]="inheritance_expected_edges"

# go/internal/storage/cypher/canonical_inheritance_edges.go:48. The family
# writes four relationship types across two files -- INHERITS, OVERRIDES and
# ALIASES here, IMPLEMENTS in canonical_implements_edges.go:13 -- and this
# anchor deliberately targets ONE of them. That is the correct scope for a
# fault anchor: the cell fails a single write and then proves recovery against
# the FULL five-edge exact set, so the other three types are covered by the
# assertion rather than by the injection. Pointing the anchor at a type the
# drive does not execute would trip the generic cell's own marker check, which
# dies when the fault fires on a different write than the anchor names.
IFA_FAMILY_ANCHOR[inheritance_edges]="MERGE (child)-[rel:INHERITS]->(parent)"
# shared_intent_lock is one of the shapes ifa_fault_generic_cells.sh's generic
# dispatcher builds, so this family needs no bespoke cell file.
IFA_FAMILY_CELL_KIND[inheritance_edges]="generic"

# Written by this family's own generic baseline cell, not by the shared
# cell_baseline: the shared baseline never drives this cassette, so it cannot
# produce a retry count for this domain.
IFA_FAMILY_RETRY_BASELINE_VAR[inheritance_edges]="baseline_inheritance_retried"

# NOT in drive_all_cassettes -- the family's own cells drive its cassette
# through DRIVE_FN/CASSETTE_VAR above.
IFA_FAMILY_FAULT_SHARED_DRIVE[inheritance_edges]="0"

IFA_FAMILY_HANDLER_GO_FILE[inheritance_edges]="go/internal/reducer/inheritance/materialization.go"

IFA_FAMILY_NAMES+=(inheritance_edges)
