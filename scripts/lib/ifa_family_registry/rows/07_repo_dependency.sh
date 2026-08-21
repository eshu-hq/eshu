#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# repo_dependency's resolver claims deployment_mapping fact work, then writes
# the seven exact edges after its Platform prerequisite and maintenance pass.
IFA_FAMILY_BLOCKER_KIND[repo_dependency]="shared_intent_lock"
IFA_FAMILY_WAIT_STAGE[repo_dependency]="handler"
IFA_FAMILY_WAIT_KEY[repo_dependency]="deployment_mapping"
IFA_FAMILY_SHARED_CELL[repo_dependency]=0
IFA_FAMILY_ANCHOR[repo_dependency]="MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)"
IFA_FAMILY_CELL_KIND[repo_dependency]="custom"
IFA_FAMILY_CASSETTE_VAR[repo_dependency]="repo_dependency_cassette"
IFA_FAMILY_EXPECTED_VAR[repo_dependency]="repo_dependency_expected_edges"
IFA_FAMILY_RETRY_BASELINE_VAR[repo_dependency]="baseline_deployment_mapping_retried"
IFA_FAMILY_HANDLER_GO_FILE[repo_dependency]="go/internal/reducer/cross_repo_resolution.go"
IFA_FAMILY_FAULT_SHARED_DRIVE[repo_dependency]="0"
IFA_FAMILY_NAMES+=(repo_dependency)
