#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# code_taint_evidence hand-derived pin (#6228 code_taint_evidence live leaf).
# Sourced by scripts/lib/test-ifa-family-registry-derived-pins-cases.sh --
# read that file's header before touching this one. Every value is
# HAND-TYPED, derived from the citations inline, never read back out of the
# registry row.

# go/internal/reducer/code/taint/evidence_materialization.go
# declares `Loader EvidenceLoader` as a struct field at :77,
# Handle rejects a nil one at :91-92, and passes it to its fact load at
# :98. The loader is postgres FactStore.LoadCodeTaintEvidence
# (go/internal/storage/postgres/code_taint_evidence_loader.go:26-31),
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
# (go/internal/projector/code/taint/evidence/reducer_intent.go:40),
# and corroborated by the domain definition (DomainCodeTaintEvidence in
# go/internal/reducer/contract/domain.go:46).
IFA_FAMILY_PIN_WAIT_KEY="code_taint_evidence"

# go/internal/storage/cypher/edge/writer/code_taint_evidence.go reads
# `MERGE (f)-[rel:HAS_TAINT_EVIDENCE]->(ev)` with no property map on the
# MERGE (the SETs follow it). The anchor is matched against EXECUTED
# statement text as a SUBSTRING, so it carries the FULL form including the
# `->(ev)` close: the bare-`]->(t)` PREFIX rationale of the
# built_from/derived_from/interproc pins does not transfer, because their
# templates carry the property map on the MERGE itself. HAS_TAINT_EVIDENCE
# is the only type that template can emit (no vocabulary substitution).
# No other production statement MERGEs this type; the remaining spellings
# are retract DELETEs and prose.
IFA_FAMILY_PIN_ANCHOR="MERGE (f)-[rel:HAS_TAINT_EVIDENCE]->(ev)"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: derived from the gate's call sites -- the custom trio in
# scripts/lib/ifa_fault_injection_code_taint_evidence_cells.sh,
# dispatched by name (#6228 code_taint_evidence live leaf). Custom rather
# than generic for the reason recorded in the sibling iam_instance_profile_role pin.
IFA_FAMILY_PIN_CELL_KIND="custom"
