#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# built_from hand-derived pin (#6228 slice C live leaf). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh -- read that file's
# header before touching this one. Every value is HAND-TYPED, derived from the
# citations inline, never read back out of the registry row.

# go/internal/reducer/containerimage/container_image_identity.go
# declares `FactLoader factload.FactLoader` as a struct field at :73, Handle
# rejects a nil one at :105-107, and passes it to factload.LoadFactsForKinds at
# :127. An ACCESS EXCLUSIVE lock on fact_records therefore blocks a read
# this handler really performs -- the blocker_kind this family's fault
# cell takes.
#
# A DIRECT-materialization family, so there is no shared_projection_intents row
# for shared_intent_lock to take.
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from BuildContainerImageIdentityReducerIntent
# (go/internal/projector/container/image/identity/intents.go:67), and
# corroborated by the domain definition (DomainContainerImageIdentity in
# go/internal/reducer/contract/domain.go:55).
IFA_FAMILY_PIN_WAIT_KEY="container_image_identity"

# go/internal/storage/cypher/edge/writer/provenance.go:172 reads
# `MERGE (img)-[rel:BUILT_FROM {`. The anchor is matched against EXECUTED
# statement text as a SUBSTRING, so it carries the filled prefix form: the
# template MERGEs BUILT_FROM with the scope_id/evidence_source identity,
# and BUILT_FROM is the only type that template can emit (no vocabulary
# substitution). A `%s` form would match no executed statement, the
# scripted graph-write fault would never fire, and the cell would report
# green having tested nothing. The prefix stops before the property map
# because the bare `]->(repo)` spelling never appears in executed text;
# the same PREFIX rationale as the codeowners pin.
IFA_FAMILY_PIN_ANCHOR="MERGE (img)-[rel:BUILT_FROM"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: derived from the gate's call sites -- the custom trio in
# scripts/lib/ifa_fault_injection_built_from_cells.sh,
# dispatched by name (#6228 slice C live leaf). Custom rather than generic
# for the reason recorded in the sibling iam_instance_profile_role pin.
IFA_FAMILY_PIN_CELL_KIND="custom"
