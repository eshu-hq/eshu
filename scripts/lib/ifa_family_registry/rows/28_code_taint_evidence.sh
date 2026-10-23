#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# code_taint_evidence row (#6228 code_taint_evidence live leaf). See
# ../../ifa_family_registry.sh for the schema and every array declaration
# this file assigns into. Read
# rows/13_iam_instance_profile_role.sh's header first -- it explains what
# a DIRECT-materialization family is and which blocker kinds are
# unavailable to one.
#
# FAULT CELLS LANDED (#6228 code_taint_evidence live leaf).
# scripts/verify-ifa-fault-injection.sh drives and asserts this row through
# scripts/lib/ifa_fault_injection_code_taint_evidence_cells.sh
# (baseline/killworker/failgraphwrite trio, dispatched in shard order with
# a co-location atomic group). cell_kind=custom below records the
# hand-written dispatch; specs/ci-gates.v1.yaml gives this family
# ifa-fault-injection triggers so an edit to its cassette or its writer
# re-runs the matrix that observes it.
#
# The writing handler here is EvidenceHandler, not a
# *_materialization handler behind a shared port: the code_taint_evidence
# intent fans out under its own additive domain (see WAIT_KEY), so the
# anchor below names the taint evidence writer's own MATCH-MATCH-MERGE
# statement.

# Hand-derived, and non-vacuous now a fault cell exists:
# EvidenceHandler embeds `Loader EvidenceLoader`
# (go/internal/reducer/code/taint/evidence_materialization.go:77)
# and Handle refuses to run without it (:91-92) before passing it to its
# fact load (:98). The loader is postgres FactStore.LoadCodeTaintEvidence
# (go/internal/storage/postgres/code_taint_evidence_loader.go:26-31),
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
IFA_FAMILY_BLOCKER_KIND[code_taint_evidence]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[code_taint_evidence]="handler"
# The fact_work_items.domain the projector fans this family out under, read
# off BuildReducerIntent
# (go/internal/projector/code/taint/evidence/reducer_intent.go:40),
# and corroborated by the domain definition (DomainCodeTaintEvidence in
# go/internal/reducer/contract/domain.go:46). The live cell asserts the
# domain through the registry loop's own non-vacuity checks; the exact
# `code_taint_evidence | succeeded` row the determinism drive leaves
# is CI-observed, not assumed here.
IFA_FAMILY_WAIT_KEY[code_taint_evidence]="code_taint_evidence"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[code_taint_evidence]=1

IFA_FAMILY_DRIVE_FN[code_taint_evidence]="ifa_code_taint_evidence_drive"
IFA_FAMILY_ASSERT_FN[code_taint_evidence]="ifa_code_taint_evidence_assert"
IFA_FAMILY_CASSETTE_VAR[code_taint_evidence]="code_taint_evidence_cassette"
IFA_FAMILY_EXPECTED_VAR[code_taint_evidence]="code_taint_evidence_expected_edges"

# go/internal/storage/cypher/edge/writer/code_taint_evidence.go, which reads
# `MERGE (f)-[rel:HAS_TAINT_EVIDENCE]->(ev)`. The anchor is matched against
# EXECUTED statement text, so it carries the FULL form: the template's
# MERGE carries no property map (the SETs follow it), so the bare
# `]->(ev)` spelling DOES appear in executed text (the opposite of the
# built_from and derived_from PREFIX rationale). HAS_TAINT_EVIDENCE is
# the only type this writer can emit (static token, no vocabulary
# substitution), and no other production statement MERGEs it -- the only
# other HAS_TAINT_EVIDENCE spellings in the tree are retract DELETEs and
# prose. Pinning a PREFIX without the `->(ev)` close would also match the
# node MERGE above it and fire the fault outside the upsert the cell
# asserts.
IFA_FAMILY_ANCHOR[code_taint_evidence]="MERGE (f)-[rel:HAS_TAINT_EVIDENCE]->(ev)"
# custom, same dispatch shape as rows/12-13: hand-written cells, dispatched by
# name. The generic table_lock path has never run live and its mandatory
# precondition assumes a `domain` column fact_records does not have
# (scripts/lib/ifa_fault_generic_table_lock.sh's header).
IFA_FAMILY_CELL_KIND[code_taint_evidence]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[code_taint_evidence]=""

# 0 on the same footing as rows/12-13: drive_all_cassettes does not produce
# this family; each fault cell drives it through DRIVE_FN/CASSETTE_VAR
# instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[code_taint_evidence]="0"

IFA_FAMILY_HANDLER_GO_FILE[code_taint_evidence]="go/internal/reducer/code/taint/evidence_materialization.go"

IFA_FAMILY_NAMES+=(code_taint_evidence)
