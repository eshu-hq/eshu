#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# code_interproc_evidence row (#6228 code_interproc_evidence live leaf). See
# ../../ifa_family_registry.sh for the schema and every array declaration
# this file assigns into. Read
# rows/13_iam_instance_profile_role.sh's header first -- it explains what
# a DIRECT-materialization family is and which blocker kinds are
# unavailable to one.
#
# FAULT CELLS LANDED (#6228 code_interproc_evidence live leaf).
# scripts/verify-ifa-fault-injection.sh drives and asserts this row through
# scripts/lib/ifa_fault_injection_code_interproc_evidence_cells.sh
# (baseline/killworker/failgraphwrite trio, dispatched in shard order with
# a co-location atomic group). cell_kind=custom below records the
# hand-written dispatch; specs/ci-gates.v1.yaml gives this family
# ifa-fault-injection triggers so an edit to its cassette or its writer
# re-runs the matrix that observes it.
#
# The writing handler here is InterprocEvidenceHandler, not a
# *_materialization handler behind a shared port: the code_interproc_evidence
# intent fans out under its own additive domain (see WAIT_KEY), so the
# anchor below names the interproc evidence writer's own MATCH-MATCH-MERGE
# statement.

# Hand-derived, and non-vacuous now a fault cell exists:
# InterprocEvidenceHandler embeds `Loader InterprocEvidenceFactLoader`
# (go/internal/reducer/code/taint/interproc_evidence_materialization.go:120)
# and Handle refuses to run without it (:137-139) before passing it to its
# fact load (:144). The loader is postgres FactStore.ListFactsForKinds
# (go/internal/storage/postgres/code_interproc_evidence_loader.go:23),
# which pages over FROM fact_records
# (go/internal/storage/postgres/facts_filtered.go:49,75,103,130), so an
# ACCESS EXCLUSIVE lock on that table holds it genuinely in flight -- the
# blocker_kind this row's fault cell takes. Same table and same reasoning
# as rows/13; see that row for why fact_work_items is the wrong target.
#
# No readiness banking (the k8s shape, not the crc-image one): this handler
# carries no in-handler readiness gate before its fact load -- Handle goes
# straight from the nil guards to the load -- and the domain carries no
# claim-time readiness requirement, so the first claim blocks
# deterministically in its load. A prereq-scoped reducer would add a wait
# nothing here needs.
IFA_FAMILY_BLOCKER_KIND[code_interproc_evidence]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[code_interproc_evidence]="handler"
# The fact_work_items.domain the projector fans this family out under, read
# off BuildReducerIntent
# (go/internal/projector/code/interproc/evidence/reducer_intent.go:42),
# and corroborated by the domain definition (DomainCodeInterprocEvidence in
# go/internal/reducer/contract/domain.go:44). The live cell asserts the
# domain through the registry loop's own non-vacuity checks; the exact
# `code_interproc_evidence | succeeded` row the determinism drive leaves
# is CI-observed, not assumed here.
IFA_FAMILY_WAIT_KEY[code_interproc_evidence]="code_interproc_evidence"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[code_interproc_evidence]=1

IFA_FAMILY_DRIVE_FN[code_interproc_evidence]="ifa_code_interproc_evidence_drive"
IFA_FAMILY_ASSERT_FN[code_interproc_evidence]="ifa_code_interproc_evidence_assert"
IFA_FAMILY_CASSETTE_VAR[code_interproc_evidence]="code_interproc_evidence_cassette"
IFA_FAMILY_EXPECTED_VAR[code_interproc_evidence]="code_interproc_evidence_expected_edges"

# go/internal/storage/cypher/edge/writer/code_interproc_evidence.go, which
# reads `MERGE (s)-[rel:TAINT_FLOWS_TO {evidence_uid: row.uid}]->(t)`. The
# anchor is matched against EXECUTED statement text, so it carries the PREFIX
# form: the template's MERGE carries the property map, and the bare
# `]->(t)` spelling never appears in executed text (the same PREFIX
# rationale as the built_from and derived_from anchors). TAINT_FLOWS_TO is
# the only type this writer can emit (static token, no vocabulary
# substitution), and no other production statement MERGEs it -- the only
# other TAINT_FLOWS_TO spellings in the tree are retract DELETEs and prose.
# Pinning a bare relationship name without the `(s)-[rel:` subject would
# match those DELETEs and fire the fault outside the upsert the cell
# asserts.
IFA_FAMILY_ANCHOR[code_interproc_evidence]="MERGE (s)-[rel:TAINT_FLOWS_TO"
# custom, same dispatch shape as rows/12-13: hand-written cells, dispatched by
# name. The generic table_lock path has never run live and its mandatory
# precondition assumes a `domain` column fact_records does not have
# (scripts/lib/ifa_fault_generic_table_lock.sh's header).
IFA_FAMILY_CELL_KIND[code_interproc_evidence]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[code_interproc_evidence]=""

# 0 on the same footing as rows/12-13: drive_all_cassettes does not produce
# this family; each fault cell drives it through DRIVE_FN/CASSETTE_VAR
# instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[code_interproc_evidence]="0"

IFA_FAMILY_HANDLER_GO_FILE[code_interproc_evidence]="go/internal/reducer/code/taint/interproc_evidence_materialization.go"

IFA_FAMILY_NAMES+=(code_interproc_evidence)
