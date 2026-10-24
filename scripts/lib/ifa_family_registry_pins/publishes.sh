#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# publishes hand-derived pin (#6228 publishes live leaf).
# Sourced by scripts/lib/test-ifa-family-registry-derived-pins-cases.sh --
# read that file's header before touching this one. Every value is
# HAND-TYPED, derived from the citations inline, never read back out of the
# registry row.

# go/internal/reducer/packages/correlation/source_handler.go
# declares `FactLoader factload.FactLoader` as a struct field at :48,
# Handle rejects a nil one at :80, and passes it to its fact load at
# :87. The loader is factload.LoadFactsForKinds
# (go/internal/reducer/factload/loader.go:55-67), which pages over
# FROM fact_records
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
# (go/internal/projector/package/source/reducer_intent.go:32),
# and corroborated by the domain definition
# (DomainPackageSourceCorrelation in
# go/internal/reducer/contract/domain.go:114-115).
IFA_FAMILY_PIN_WAIT_KEY="package_source_correlation"

# go/internal/storage/cypher/edge/writer/provenance.go:140,154 reads
# `MERGE (repo)-[rel:PUBLISHES {` with the scope_id/evidence_source
# identity map on the MERGE itself. The anchor is matched against EXECUTED
# statement text as a SUBSTRING, so it carries the PREFIX form: the
# built_from/derived_from rationale transfers, because their templates
# carry the property map on the MERGE itself the same way. A full
# `]->(target)` spelling would match no executed statement. PUBLISHES is
# the only type either template can emit (no vocabulary substitution).
# No other production statement MERGEs this type; the remaining spellings
# are retract DELETEs and prose.
IFA_FAMILY_PIN_ANCHOR="MERGE (repo)-[rel:PUBLISHES"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: derived from the gate's call sites -- the custom trio in
# scripts/lib/ifa_fault_injection_publishes_cells.sh,
# dispatched by name (#6228 publishes live leaf). Custom rather
# than generic for the reason recorded in the sibling iam_instance_profile_role pin.
IFA_FAMILY_PIN_CELL_KIND="custom"
