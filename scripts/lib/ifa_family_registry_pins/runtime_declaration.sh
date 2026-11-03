#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# runtime_declaration hand-derived pin (#6228 runtime_declaration live leaf).
# Sourced by scripts/lib/test-ifa-family-registry-derived-pins-cases.sh --
# read that file's header before touching this one. Every value is
# HAND-TYPED, derived from the citations inline, never read back out of the
# registry row.

# go/internal/reducer/runtime/declaration/handler.go
# declares `FactLoader factload.FactLoader` as a struct field at :68,
# Handle rejects a nil one at :83-85, and passes it to its fact load at
# :90. The loader is factload.LoadFactsForKinds
# (go/internal/reducer/factload/loader.go:55-63),
# which pages over FROM fact_records
# (go/internal/storage/postgres/facts_filtered.go:49,75,118,145). An ACCESS
# EXCLUSIVE lock on fact_records therefore blocks a read this handler
# really performs -- the blocker_kind this family's fault cell takes.
#
# A DIRECT-materialization family, so there is no shared_projection_intents row
# for shared_intent_lock to take.
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from the collector's shared_followup marker payload
# (go/internal/collector/repo/git/followup_facts.go:430 carries
# reducer_domain runtime_declaration_projection), converted by the
# projector's generic per-fact intent path
# (go/internal/projector/runtime/reducer_intent.go),
# and corroborated by the domain definition
# (DomainRuntimeDeclarationProjection in
# go/internal/reducer/contract/domain.go:54).
IFA_FAMILY_PIN_WAIT_KEY="runtime_declaration_projection"

# go/internal/storage/cypher/edge/writer/runtime_declaration.go reads
# `MERGE (repo)-[rel:DECLARES_RUNTIME]->(d)` with no property map on the
# MERGE (the SETs follow it). The anchor is matched against EXECUTED
# statement text as a SUBSTRING, so it carries the FULL form including the
# `->(d)` close: a PREFIX form stopping before `->(d)` would also match the
# node MERGE in the same template and name the wrong half of the statement
# the cell asserts. DECLARES_RUNTIME is the only type that template can
# emit (no vocabulary substitution). No other production statement MERGEs
# this type; the remaining spellings are MATCH/DETACH DELETE retracts and
# prose.
IFA_FAMILY_PIN_ANCHOR="MERGE (repo)-[rel:DECLARES_RUNTIME]->(d)"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: derived from the gate's call sites -- the custom trio in
# scripts/lib/ifa_fault_injection_runtime_declaration_cells.sh,
# dispatched by name (#6228 runtime_declaration live leaf). Custom rather
# than generic for the reason recorded in the sibling iam_instance_profile_role pin.
IFA_FAMILY_PIN_CELL_KIND="custom"
