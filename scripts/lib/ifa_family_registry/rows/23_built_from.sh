#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# built_from row (#6228 slice C live leaf). See ../../ifa_family_registry.sh
# for the schema and every array declaration this file assigns into. Read
# rows/13_iam_instance_profile_role.sh's header first -- it explains what
# a DIRECT-materialization family is and which blocker kinds are unavailable to
# one.
#
# FAULT CELLS LANDED (#6228 slice C live leaf).
# scripts/verify-ifa-fault-injection.sh drives and asserts this row through
# scripts/lib/ifa_fault_injection_built_from_cells.sh
# (baseline/killworker/failgraphwrite trio, dispatched in shard order with a
# co-location atomic group). cell_kind=custom below records the hand-written
# dispatch; specs/ci-gates.v1.yaml gives this family ifa-fault-injection
# triggers so an edit to its cassette or its writer re-runs the matrix that
# observes it.
#
# The writing handler here is ContainerImageIdentityHandler, not a
# *_materialization handler: built_from edges are projected by the
# container-image-identity pass itself (ProvenanceEdgeWriter on the same
# intent), so WAIT_KEY names the identity domain the projector fans out,
# not a second-stage materialization domain.

# Hand-derived, and non-vacuous now a fault cell exists:
# ContainerImageIdentityHandler embeds `FactLoader factload.FactLoader`
# (go/internal/reducer/containerimage/container_image_identity.go:73)
# and Handle refuses to run without it (:105-107) before passing it to
# factload.LoadFactsForKinds (:127). The handler reads fact_records AFTER
# claiming its work item, so an ACCESS EXCLUSIVE lock on that table holds
# it genuinely in flight -- the blocker_kind this row's fault cell takes.
# Same table and same reasoning as rows/13; see that row for why
# fact_work_items is the wrong target.
IFA_FAMILY_BLOCKER_KIND[built_from]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[built_from]="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from BuildContainerImageIdentityReducerIntent
# (go/internal/projector/container/image/identity/intents.go:67), and
# corroborated by the domain definition (DomainContainerImageIdentity in
# go/internal/reducer/contract/domain.go:55). The live cell asserts the
# domain through the registry loop's own non-vacuity checks; the exact
# `container_image_identity | succeeded` row the determinism drive leaves
# is CI-observed, not assumed here.
IFA_FAMILY_WAIT_KEY[built_from]="container_image_identity"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[built_from]=1

IFA_FAMILY_DRIVE_FN[built_from]="ifa_built_from_drive"
IFA_FAMILY_ASSERT_FN[built_from]="ifa_built_from_assert"
IFA_FAMILY_CASSETTE_VAR[built_from]="built_from_cassette"
IFA_FAMILY_EXPECTED_VAR[built_from]="built_from_expected_edges"

# go/internal/storage/cypher/edge/writer/provenance.go:172, which reads
# `MERGE (img)-[rel:BUILT_FROM {` in source. The anchor is
# matched against EXECUTED statement text, so it carries the filled form:
# canonicalProvenanceBuiltFromCypher MERGEs BUILT_FROM with the
# scope_id/evidence_source identity the writer stamps per run, and BUILT_FROM
# is the only type that template can emit (no vocabulary substitution).
# Pinning a `%s` form here would match no executed statement and a scripted
# graph-write fault would never fire -- a green cell that tested nothing.
#
# The anchor is a PREFIX (no closing paren): the template's MERGE carries
# the `{scope_id, evidence_source}` property map, so the bare
# `MERGE (img)-[rel:BUILT_FROM]->(repo)` spelling never appears in executed
# text; the prefix matches both the template source and the executed
# statement, the same rationale as the codeowners PREFIX anchor.
IFA_FAMILY_ANCHOR[built_from]="MERGE (img)-[rel:BUILT_FROM"
# custom, same dispatch shape as rows/12-13: hand-written cells, dispatched by
# name. The generic table_lock path has never run live and its mandatory
# precondition assumes a `domain` column fact_records does not have
# (scripts/lib/ifa_fault_generic_table_lock.sh's header).
IFA_FAMILY_CELL_KIND[built_from]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[built_from]=""

# 0 on the same footing as rows/12-13: drive_all_cassettes does not produce
# this family; each fault cell drives it through DRIVE_FN/CASSETTE_VAR
# instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[built_from]="0"

IFA_FAMILY_HANDLER_GO_FILE[built_from]="go/internal/reducer/containerimage/container_image_identity.go"

IFA_FAMILY_NAMES+=(built_from)
