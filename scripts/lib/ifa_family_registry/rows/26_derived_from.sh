#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# derived_from row (#6228 derived_from live leaf). See
# ../../ifa_family_registry.sh for the schema and every array declaration
# this file assigns into. Read
# rows/13_iam_instance_profile_role.sh's header first -- it explains what
# a DIRECT-materialization family is and which blocker kinds are
# unavailable to one.
#
# FAULT CELLS LANDED (#6228 derived_from live leaf).
# scripts/verify-ifa-fault-injection.sh drives and asserts this row through
# scripts/lib/ifa_fault_injection_derived_from_cells.sh
# (baseline/killworker/failgraphwrite trio, dispatched in shard order with
# a co-location atomic group). cell_kind=custom below records the
# hand-written dispatch; specs/ci-gates.v1.yaml gives this family
# ifa-fault-injection triggers so an edit to its cassette or its writer
# re-runs the matrix that observes it.
#
# The writing handler here is ContainerImageIdentityHandler, not a
# *_materialization handler: derived_from edges are projected by the
# container-image-identity pass itself (ProvenanceEdgeWriter on the same
# intent). The handler -- and therefore the handler-stage wait on the
# identity domain -- belongs to rows/23 built_from by first-landed
# tie-break: the handler-wait-key exclusivity test rejects two
# handler-stage families sharing one wait_key, since one kill of that
# handler would be counted as a per-family proof for both while the second
# family's own seam stayed unexercised. So WAIT_KEY names the identity
# domain only as the claim-plus-hold rendezvous (the same keyspace the
# handler stage polls), while the blocker engages a write only
# derived_from performs -- the vault-pair precedent in rows/22, judged on
# the family-own anchor rather than the shared key.

# Hand-derived, and non-vacuous now a fault cell exists: the hold decorator
# engages the DERIVED_FROM MERGE itself (IFA_FAMILY_ANCHOR below) while the
# handler is genuinely mid-write -- the blocker_kind this row's fault cell
# takes. The anchor is matched against EXECUTED statement text, so it
# carries the filled form (see the anchor comment); pinning a `%s` form
# would hold nothing and the kill would land on a worker parked nowhere.
IFA_FAMILY_BLOCKER_KIND[derived_from]="statement_hold"
IFA_FAMILY_WAIT_STAGE[derived_from]="statement"
# The fact_work_items.domain the projector fans this family out under, taken
# from BuildContainerImageIdentityReducerIntent
# (go/internal/projector/container/image/identity/intents.go:66), and
# corroborated by the domain definition (DomainContainerImageIdentity in
# go/internal/reducer/contract/domain.go:55). Rendezvous only: the live
# cell asserts the domain through the registry loop's own non-vacuity
# checks, first waiting the claimed projection work item on this domain,
# then the hold marker the decorator writes when the DERIVED_FROM MERGE
# engages. The exact `container_image_identity | succeeded` row the
# determinism drive leaves is CI-observed, not assumed here.
IFA_FAMILY_WAIT_KEY[derived_from]="container_image_identity"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[derived_from]=1

IFA_FAMILY_DRIVE_FN[derived_from]="ifa_derived_from_drive"
IFA_FAMILY_ASSERT_FN[derived_from]="ifa_derived_from_assert"
IFA_FAMILY_CASSETTE_VAR[derived_from]="derived_from_cassette"
IFA_FAMILY_EXPECTED_VAR[derived_from]="derived_from_expected_edges"

# go/internal/storage/cypher/edge/writer/derived_from.go, which reads
# `MERGE (img)-[rel:DERIVED_FROM {` in source. The anchor is
# matched against EXECUTED statement text, so it carries the filled form:
# canonicalProvenanceDerivedFromCypher MERGEs DERIVED_FROM with the
# scope_id/evidence_source identity the writer stamps per run, and DERIVED_FROM
# is the only type that template can emit (no vocabulary substitution).
# Pinning a `%s` form here would match no executed statement and a scripted
# graph-write fault would never fire -- a green cell that tested nothing.
#
# The anchor is a PREFIX (no closing paren): the template's MERGE carries
# the `{scope_id, evidence_source}` property map, so the bare
# `MERGE (img)-[rel:DERIVED_FROM]->(base)` spelling never appears in executed
# text; the prefix matches both the template source and the executed
# statement, the same rationale as the built_from PREFIX anchor.
IFA_FAMILY_ANCHOR[derived_from]="MERGE (img)-[rel:DERIVED_FROM"
# custom, same dispatch shape as rows/12-13: hand-written cells, dispatched by
# name. The generic table_lock path has never run live and its mandatory
# precondition assumes a `domain` column fact_records does not have
# (scripts/lib/ifa_fault_generic_table_lock.sh's header).
IFA_FAMILY_CELL_KIND[derived_from]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[derived_from]=""

# 0 on the same footing as rows/12-13: drive_all_cassettes does not produce
# this family; each fault cell drives it through DRIVE_FN/CASSETTE_VAR
# instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[derived_from]="0"

IFA_FAMILY_HANDLER_GO_FILE[derived_from]="go/internal/reducer/containerimage/container_image_identity.go"

IFA_FAMILY_NAMES+=(derived_from)
