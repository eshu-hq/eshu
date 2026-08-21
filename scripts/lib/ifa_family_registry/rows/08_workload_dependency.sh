#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# workload_dependency is written directly by WorkloadMaterializationHandler.
# The kill cell blocks its first fact_records read; the graph-fault cell targets
# the Workload-specific MERGE after repo_dependency has drained.
IFA_FAMILY_BLOCKER_KIND[workload_dependency]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[workload_dependency]="handler"
IFA_FAMILY_WAIT_KEY[workload_dependency]="workload_materialization"
IFA_FAMILY_SHARED_CELL[workload_dependency]=0
IFA_FAMILY_ANCHOR[workload_dependency]="MERGE (source)-[rel:DEPENDS_ON]->(target)"
IFA_FAMILY_CELL_KIND[workload_dependency]="custom"
IFA_FAMILY_CASSETTE_VAR[workload_dependency]="workload_dependency_cassette"
IFA_FAMILY_EXPECTED_VAR[workload_dependency]="workload_dependency_expected_edges"
IFA_FAMILY_RETRY_BASELINE_VAR[workload_dependency]="baseline_workload_dependency_retried"
IFA_FAMILY_HANDLER_GO_FILE[workload_dependency]="go/internal/reducer/workload_materialization_handler.go"
IFA_FAMILY_FAULT_SHARED_DRIVE[workload_dependency]="0"
IFA_FAMILY_NAMES+=(workload_dependency)
