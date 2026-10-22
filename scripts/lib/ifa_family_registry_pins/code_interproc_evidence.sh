#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# code_interproc_evidence hand-derived pin (#6228 code_interproc_evidence live leaf).
# Sourced by scripts/lib/test-ifa-family-registry-derived-pins-cases.sh --
# read that file's header before touching this one. Every value is
# HAND-TYPED, derived from the citations inline, never read back out of the
# registry row.

# go/internal/reducer/code/taint/interproc_evidence_materialization.go
# declares `Loader InterprocEvidenceFactLoader` as a struct field at :120,
# Handle rejects a nil one at :137-139, and passes it to its fact load at
# :144. The loader is postgres FactStore.ListFactsForKinds
# (go/internal/storage/postgres/code_interproc_evidence_loader.go:23),
# which pages over FROM fact_records
# (go/internal/storage/postgres/facts_filtered.go:49,75,103,130). An ACCESS
# EXCLUSIVE lock on fact_records therefore blocks a read this handler
# really performs -- the blocker_kind this family's fault cell takes.
#
# A DIRECT-materialization family, so there is no shared_projection_intents row
# for shared_intent_lock to take.
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from BuildReducerIntent
# (go/internal/projector/code/interproc/evidence/reducer_intent.go:42),
# and corroborated by the domain definition (DomainCodeInterprocEvidence in
# go/internal/reducer/contract/domain.go:44).
IFA_FAMILY_PIN_WAIT_KEY="code_interproc_evidence"

# go/internal/storage/cypher/edge/writer/code_interproc_evidence.go reads
# `MERGE (s)-[rel:TAINT_FLOWS_TO {evidence_uid: row.uid}]->(t)`. The anchor
# is matched against EXECUTED statement text as a SUBSTRING, so it carries
# the PREFIX form: the template's MERGE carries the property map, and the
# bare `]->(t)` spelling never appears in executed text. TAINT_FLOWS_TO is
# the only type that template can emit (no vocabulary substitution). No
# other production statement MERGEs this type; the remaining spellings are
# retract DELETEs and prose.
IFA_FAMILY_PIN_ANCHOR="MERGE (s)-[rel:TAINT_FLOWS_TO"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: derived from the gate's call sites -- the custom trio in
# scripts/lib/ifa_fault_injection_code_interproc_evidence_cells.sh,
# dispatched by name (#6228 code_interproc_evidence live leaf). Custom rather
# than generic for the reason recorded in the sibling iam_instance_profile_role pin.
IFA_FAMILY_PIN_CELL_KIND="custom"
