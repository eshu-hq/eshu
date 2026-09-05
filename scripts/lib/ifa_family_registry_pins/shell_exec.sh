#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# shell_exec hand-derived pin (#6001). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh -- read that file's
# header before touching this one. Every value is HAND-TYPED, derived from the
# citations inline, never read back out of the registry row.

# go/internal/reducer/shell_exec_materialization.go:33 embeds
# `IntentWriter ShellExecIntentWriter` as a struct field and Handle calls
# h.IntentWriter.UpsertIntents(ctx, intentRows) at :87. A lock on
# shared_projection_intents therefore blocks a write this handler really
# performs, which is the non-vacuity condition for this blocker kind.
IFA_FAMILY_PIN_BLOCKER_KIND="shared_intent_lock"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
# go/internal/reducer/intent.go:58 --
# `DomainShellExecMaterialization Domain = "shell_exec_materialization"`.
# Note the trap this family sets that inheritance_edges does not: its
# SECOND-stage ProjectionDomain label is "shell_exec"
# (go/internal/reducer/shared_projection.go:20), which is byte-identical to this
# family's own registry name. The two being spelled the same is a coincidence of
# naming; wait_stage=handler polls fact_work_items, so the value here must be
# the _materialization form and not the registry name.
IFA_FAMILY_PIN_WAIT_KEY="shell_exec_materialization"

# go/internal/storage/cypher/edge_writer_shell_exec.go:23. Single relationship
# type, so unlike inheritance_edges this anchor covers the family's entire write
# surface. The same statement MERGEs the ShellCommand target node first (:14);
# the anchor names the RELATIONSHIP merge because that is the write whose
# failure and recovery the fault cell is about.
IFA_FAMILY_PIN_ANCHOR="MERGE (source)-[rel:EXECUTES_SHELL]->(target)"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: derived from the gate's call sites. shared_intent_lock is a shape
# the generic dispatcher builds and this family is dispatched through it.
IFA_FAMILY_PIN_CELL_KIND="generic"
