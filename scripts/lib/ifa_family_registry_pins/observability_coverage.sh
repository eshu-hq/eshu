#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# observability_coverage hand-derived pin (#6228 observability_coverage live
# leaf). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh --
# read that file's header before touching this one. Every value is
# HAND-TYPED, derived from the citations inline, never read back out of the
# registry row.
#
# This family owns its handler and its handler-stage wait outright (no
# shared-handler tie-break: the kill engages the family's own first
# synchronous read through the fact_records table lock) - the blocker_kind
# this family's fault cell takes. Exclusivity with the handler stage is
# judged on the family-own anchor-free wait below: the lock is on
# fact_records, a table the claim path never touches, so the claim stays
# held while the handler is parked.
#
# A DIRECT-materialization family, so there is no shared_projection_intents row
# for shared_intent_lock to take.
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from BuildObservabilityCoverageMaterializationReducerIntent
# (go/internal/projector/observability/coverage/materialization/materialization_intents.go:66),
# and corroborated by the domain definition
# (DomainObservabilityCoverageMaterialization in
# go/internal/reducer/contract/domain.go:111).
IFA_FAMILY_PIN_WAIT_KEY="observability_coverage_materialization"

# go/internal/storage/cypher/edge/writer/observability_coverage.go:54 reads
# `MERGE (obs)-[rel:%s]->(target)`. The anchor is matched against EXECUTED
# statement text as a SUBSTRING, so it carries the PREFIX form: the writer
# fills %s per row from the closed 5-member coverage-signal vocabulary, and
# a full `]->(target)` spelling matches no single executed statement while
# pinning one filled type would leave the other four unfaulted. A `%s` form
# would match no executed statement, the scripted graph-write fault would
# never fire, and the cell would report green having tested nothing.
IFA_FAMILY_PIN_ANCHOR="MERGE (obs)-[rel:AWS_COVERS_"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: derived from the gate's call sites -- the custom trio in
# scripts/lib/ifa_fault_injection_observability_coverage_cells.sh,
# dispatched by name (#6228 observability_coverage live leaf). Custom rather
# than generic for the reason recorded in the sibling s3_logs_to pin.
IFA_FAMILY_PIN_CELL_KIND="custom"
