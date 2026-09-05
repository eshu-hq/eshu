#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# shell_exec row (#6001). See ../../ifa_family_registry.sh for the schema and
# every array declaration this file assigns into.

# Non-vacuous: ShellExecIntentWriter is a declared dependency
# (go/internal/reducer/shell_exec_materialization.go:33) and Handle calls
# UpsertIntents on it (:87), so a lock on shared_projection_intents blocks a
# write this family actually performs.
IFA_FAMILY_BLOCKER_KIND[shell_exec]="shared_intent_lock"
IFA_FAMILY_WAIT_STAGE[shell_exec]="handler"
# reducer.DomainShellExecMaterialization (go/internal/reducer/intent.go:58) --
# the FIRST-stage handler domain. The SECOND-stage shared-projection domain is
# the different string "shell_exec" (reducer.DomainShellExec,
# shared_projection.go:20), which is also this row's family name; the two being
# spelled the same here is a coincidence of naming, not a rule to generalize.
IFA_FAMILY_WAIT_KEY[shell_exec]="shell_exec_materialization"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[shell_exec]=1

IFA_FAMILY_DRIVE_FN[shell_exec]="ifa_shell_exec_drive"
IFA_FAMILY_ASSERT_FN[shell_exec]="ifa_shell_exec_assert"
IFA_FAMILY_CASSETTE_VAR[shell_exec]="shell_exec_cassette"
IFA_FAMILY_EXPECTED_VAR[shell_exec]="shell_exec_expected_edges"

# go/internal/storage/cypher/edge_writer_shell_exec.go:23. Single relationship
# type, so unlike inheritance_edges this anchor covers the family's whole write
# surface. Note the writer MERGEs the ShellCommand target node first (:14); the
# anchor names the relationship MERGE because that is the write whose failure
# and recovery this family's fault cell is about.
IFA_FAMILY_ANCHOR[shell_exec]="MERGE (source)-[rel:EXECUTES_SHELL]->(target)"
IFA_FAMILY_CELL_KIND[shell_exec]="generic"

# Written by this family's own generic baseline cell -- see the sibling note in
# rows/07_inheritance_edges.sh for why the shared cell_baseline cannot supply it.
IFA_FAMILY_RETRY_BASELINE_VAR[shell_exec]="baseline_shell_exec_retried"

# NOT in drive_all_cassettes -- the family's own cells drive its cassette
# through DRIVE_FN/CASSETTE_VAR above.
IFA_FAMILY_FAULT_SHARED_DRIVE[shell_exec]="0"

IFA_FAMILY_HANDLER_GO_FILE[shell_exec]="go/internal/reducer/shell_exec_materialization.go"

IFA_FAMILY_NAMES+=(shell_exec)
