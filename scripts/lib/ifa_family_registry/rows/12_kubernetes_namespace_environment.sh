#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# kubernetes_namespace_environment row (#6228). See ../../ifa_family_registry.sh
# for the schema and every array declaration this file assigns into.
#
# FIRST DIRECT-MATERIALIZATION FAMILY IN THIS REGISTRY. Every other row belongs
# to reducer.MaterializedEdgeFamilies() -- the shared-projection half, which
# reaches the graph through an ordering-safe intent row. This family and
# iam_instance_profile_role (rows/13) are written straight to a
# go/internal/storage/cypher writer with no intent row in between, so two of the
# schema's blocker kinds are simply unavailable to them: there is no
# shared_projection_intents row to lock (shared_intent_lock) and no
# second-stage runner lease to hold (runner_lease_hold). Do not "fix" this row
# by reaching for either.
#
# FAULT CELLS LANDED (#6309). scripts/verify-ifa-fault-injection.sh drives
# and asserts this row through
# scripts/lib/ifa_fault_injection_kubernetes_namespace_environment_cells.sh
# (baseline/killworker/failgraphwrite trio, dispatched in shard order with a
# co-location atomic group). cell_kind=custom below records the hand-written
# dispatch; specs/ci-gates.v1.yaml gives this family ifa-fault-injection
# triggers so an edit to its cassette or its writer re-runs the matrix that
# observes it.
#
# blocker_kind and anchor below stay hand-derived and pinned because they are
# what scripts/lib/ifa_family_registry_pins/ holds this row to.

# Hand-derived for this family's fault cells, and non-vacuous now they run:
# KubernetesNamespaceMaterializationHandler embeds
# `FactLoader FactLoader` (go/internal/reducer/kubernetes_namespace_materialization.go:129)
# and Handle refuses to run without it (:147) before passing it to the
# extraction path (:157). The handler therefore reads fact_records AFTER
# claiming its work item, so an ACCESS EXCLUSIVE lock on that table holds it
# genuinely in flight rather than blocking it before it starts.
#
# fact_records, not fact_work_items: the cell's own wait predicate polls
# fact_work_items, and an ACCESS EXCLUSIVE lock there would block that poll too
# and hang the precondition rather than the handler.
IFA_FAMILY_BLOCKER_KIND[kubernetes_namespace_environment]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[kubernetes_namespace_environment]="handler"
# The fact_work_items.domain the projector fans this family out under. Measured
# on a live stack rather than read off a constant: driving the committed
# cassette and draining left exactly one
# `kubernetes_namespace_materialization | succeeded` row. A direct family has no
# second-stage shared-projection domain, so there is no _materialization-versus-
# registry-name trap here of the kind rows/08_shell_exec.sh documents.
IFA_FAMILY_WAIT_KEY[kubernetes_namespace_environment]="kubernetes_namespace_materialization"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[kubernetes_namespace_environment]=1

IFA_FAMILY_DRIVE_FN[kubernetes_namespace_environment]="ifa_kubernetes_namespace_environment_drive"
IFA_FAMILY_ASSERT_FN[kubernetes_namespace_environment]="ifa_kubernetes_namespace_environment_assert"
IFA_FAMILY_CASSETTE_VAR[kubernetes_namespace_environment]="kubernetes_namespace_environment_cassette"
IFA_FAMILY_EXPECTED_VAR[kubernetes_namespace_environment]="kubernetes_namespace_environment_expected_edges"

# go/internal/storage/cypher/kubernetes_namespace_node_writer.go:90. The same
# statement MERGEs the Environment node by name first (:89); the anchor names
# the RELATIONSHIP merge because that is the write whose failure and recovery
# a fault cell would have to script. Note the writer routes a row here only when
# row.environment is non-empty, so an unbound namespace never reaches this
# statement at all.
IFA_FAMILY_ANCHOR[kubernetes_namespace_environment]="MERGE (n)-[env_rel:TARGETS_ENVIRONMENT]->(env)"
# custom: hand-written cells, dispatched by name (not through the generic
# cell_killworker_family / cell_failgraphwrite_family dispatcher).
# scripts/lib/ifa_fault_generic_table_lock.sh's header records that no family
# has ever run the generic table_lock path live, and that
# _ifa_generic_require_table_domain_written assumes the locked table carries a
# `domain` column fact_records does not have, so the generic dispatcher would
# error part-way into a live shard. codeowners_ownership_edges is the precedent
# for hand-written cells on the same blocker and the same table.
IFA_FAMILY_CELL_KIND[kubernetes_namespace_environment]="custom"

# Not a shared_intent_lock family, so the generic kill cell never asks for a
# retry baseline. Declared empty rather than omitted because
# _ifa_family_registry_get fails closed on an absent key, and a row that trips
# the accessor is indistinguishable from a row that says "no baseline".
IFA_FAMILY_RETRY_BASELINE_VAR[kubernetes_namespace_environment]=""

# 0: drive_all_cassettes (scripts/lib/ifa_fault_injection_driver.sh) does not
# produce this family; each fault cell drives it through DRIVE_FN/CASSETTE_VAR
# instead, which is why each family needs its own baseline cell.
IFA_FAMILY_FAULT_SHARED_DRIVE[kubernetes_namespace_environment]="0"

IFA_FAMILY_HANDLER_GO_FILE[kubernetes_namespace_environment]="go/internal/reducer/kubernetes_namespace_materialization.go"

IFA_FAMILY_NAMES+=(kubernetes_namespace_environment)
