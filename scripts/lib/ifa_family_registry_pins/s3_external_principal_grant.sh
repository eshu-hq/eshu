#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# s3_external_principal_grant hand-derived pin (#6228 s3-grant live leaf).
# Sourced by scripts/lib/test-ifa-family-registry-derived-pins-cases.sh --
# read that file's header before touching this one. Every value is
# HAND-TYPED, derived from the citations inline, never read back out of the
# registry row.

# go/internal/reducer/s3grant/s3_external_principal_grant_materialization.go
# declares `FactLoader factload.FactLoader` as a struct field, Handle
# rejects a nil one before passing it to factload.LoadFactsForKinds at
# :119. The loader is factload.LoadFactsForKinds
# (go/internal/reducer/factload/loader.go:55-67), which pages over
# FROM fact_records
# (go/internal/storage/postgres/facts_filtered.go:49,75,103,130). An ACCESS
# EXCLUSIVE lock on fact_records therefore blocks a read this handler
# really performs -- the blocker_kind this family's fault cell takes.
# Same table and same reasoning as rows/19 (s3_logs_to, the sibling S3
# direct family); see that row for why fact_work_items is the wrong target.
#
# A DIRECT-materialization family, so there is no shared_projection_intents row
# for shared_intent_lock to take.
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from the projector intent
# (go/internal/projector/cloud/aws/s3/external_principal_grant_materialization_intents.go),
# and corroborated by the domain definition
# (DomainS3ExternalPrincipalGrantMaterialization in
# go/internal/reducer/contract/domain.go:125-126).
# Proven on a live stack by this row's own determinism drive, which leaves
# exactly one `s3_external_principal_grant_materialization | succeeded` row
# per cell.
IFA_FAMILY_PIN_WAIT_KEY="s3_external_principal_grant_materialization"

# go/internal/storage/cypher/cloud/aws/s3/external_principal_grant_writer.go:34,
# which reads `MERGE (source)-[rel:%s]->(principal)` in source. The anchor
# is matched against EXECUTED statement text, so it carries the filled form:
# the one %s is substituted per row from a closed single-member vocabulary
# (GRANTS_ACCESS_TO), so the executed text carries the literal. Pinning a
# `%s` form here would match no executed statement and a scripted
# graph-write fault would never fire -- a green cell that tested nothing.
# The s3_logs_to rationale transfers verbatim (its `MERGE
# (source)-[rel:LOGS_TO]->(target)` pins the same-shaped template).
IFA_FAMILY_PIN_ANCHOR="MERGE (source)-[rel:GRANTS_ACCESS_TO]->(principal)"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: derived from the gate's call sites -- the custom trio in
# scripts/lib/ifa_fault_injection_s3_external_principal_grant_cells.sh,
# dispatched by name (#6228 s3-grant live leaf). Custom rather
# than generic for the reason recorded in the sibling iam_instance_profile_role pin.
IFA_FAMILY_PIN_CELL_KIND="custom"
