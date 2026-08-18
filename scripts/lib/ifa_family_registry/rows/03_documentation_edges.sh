#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# documentation_edges row. See ../../ifa_family_registry.sh for the schema
# and every array declaration this file assigns into.

# ifa_fault_injection_documentation_ack_barrier.sh:1-12: the documentation
# handler writes graph records directly and never touches
# shared_projection_intents, so its cell blocks the exact fact_work_items
# ACK transition instead via a Postgres advisory-lock barrier. No generic
# shape in ifa_fault_generic_cells.sh covers this; it is cell_kind=custom.
IFA_FAMILY_BLOCKER_KIND[documentation_edges]="ack_barrier"
IFA_FAMILY_WAIT_STAGE[documentation_edges]="handler"
# ifa_documentation_live.sh / the ACK barrier setup target this family's
# first-stage fact_work_items domain the same way every other family does.
IFA_FAMILY_WAIT_KEY[documentation_edges]="documentation_materialization"
IFA_FAMILY_SHARED_CELL[documentation_edges]=1
# scripts/lib/test-ifa-fault-injection-documentation-cases.sh:25
IFA_FAMILY_ANCHOR[documentation_edges]="MERGE (section)-[rel:DOCUMENTS]->(target)"
IFA_FAMILY_CELL_KIND[documentation_edges]="custom"

IFA_FAMILY_DRIVE_FN[documentation_edges]="ifa_documentation_drive"
IFA_FAMILY_ASSERT_FN[documentation_edges]="ifa_documentation_assert"
IFA_FAMILY_CASSETTE_VAR[documentation_edges]="documentation_cassette"
IFA_FAMILY_EXPECTED_VAR[documentation_edges]="documentation_expected_edges"

# ifa_family_fixtures.sh:51-52: "on the fault-injection gate
# cell_killworker_documentation and cell_failgraphwrite_documentation back
# its manifest row's proof_gate claim."
IFA_FAMILY_CUSTOM_KILLWORKER_FN[documentation_edges]="cell_killworker_documentation"
IFA_FAMILY_CUSTOM_FAILGRAPHWRITE_FN[documentation_edges]="cell_failgraphwrite_documentation"

IFA_FAMILY_NAMES+=(documentation_edges)
