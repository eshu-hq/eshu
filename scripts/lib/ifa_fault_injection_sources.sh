#!/usr/bin/env bash
# Source inventory for scripts/verify-ifa-fault-injection.sh. This is a plain
# library, not an executable: it deliberately does not change the caller's
# shell options. Keeping the inventory here leaves the gate entrypoint focused
# on configuration and the independently mirrored ordered dispatch list.

ifa_fault_sources_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# shellcheck source=scripts/lib/ifa_fault_injection_diagnostics.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_diagnostics.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_driver.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_driver.sh"
# shellcheck source=scripts/lib/ifa_fault_shard.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_shard.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_sql_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_sql_cells.sh"
# Self-sources its mechanism files and the family registry.
# shellcheck source=scripts/lib/ifa_fault_generic_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_generic_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_generic_baseline_cell.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_generic_baseline_cell.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_code_call_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_code_call_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_rationale_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_rationale_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_collateral_nodes.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_collateral_nodes.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_delivery_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_delivery_cells.sh"
# shellcheck source=scripts/lib/ifa_sql_delta_live.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_sql_delta_live.sh"
# shellcheck source=scripts/lib/ifa_code_call_live.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_code_call_live.sh"
# shellcheck source=scripts/lib/ifa_documentation_live.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_documentation_live.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_documentation_ack_barrier.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_documentation_ack_barrier.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_documentation_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_documentation_cells.sh"
# shellcheck source=scripts/lib/ifa_deployable_unit_live.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_deployable_unit_live.sh"
# shellcheck source=scripts/lib/ifa_deployable_unit_live_diagnostics.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_deployable_unit_live_diagnostics.sh"
# shellcheck source=scripts/lib/ifa_deployable_unit_live_converge.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_deployable_unit_live_converge.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_deployable_unit_lock.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_deployable_unit_lock.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_deployable_unit_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_deployable_unit_cells.sh"
# shellcheck source=scripts/lib/ifa_rationale_live.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_rationale_live.sh"
# shellcheck source=scripts/lib/ifa_codeowners_live.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_codeowners_live.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_codeowners_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_codeowners_cells.sh"
# shellcheck source=scripts/lib/ifa_repo_dependency_live.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_repo_dependency_live.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_repo_dependency_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_repo_dependency_cells.sh"
# shellcheck source=scripts/lib/ifa_submodule_pin_live.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_submodule_pin_live.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_submodule_pin_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_submodule_pin_cells.sh"
# shellcheck source=scripts/lib/ifa_inheritance_live.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_inheritance_live.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_inheritance_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_inheritance_cells.sh"
# shellcheck source=scripts/lib/ifa_shell_exec_live.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_shell_exec_live.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_shell_exec_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_shell_exec_cells.sh"
# shellcheck source=scripts/lib/ifa_workload_dependency_live.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_workload_dependency_live.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_workload_dependency_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_workload_dependency_cells.sh"
# shellcheck source=scripts/lib/ifa_symbol_runtime_live.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_symbol_runtime_live.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_symbol_runtime_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_symbol_runtime_cells.sh"
# Direct-materialization families (#6309): shared drive/assert callbacks plus
# each family's own custom cells.
# shellcheck source=scripts/lib/ifa_direct_family_live.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_direct_family_live.sh"
# shellcheck source=scripts/lib/ifa_direct_family_live_cloud_resource.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_direct_family_live_cloud_resource.sh"
# Provenance direct family (#6228 slice C live leaf): split drive/assert lib
# under the tooling line cap, paired with ifa_direct_family_live.sh above.
# shellcheck source=scripts/lib/ifa_direct_family_live_built_from.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_direct_family_live_built_from.sh"
# Image direct family (#6228 slice D live leaf): split drive/assert lib under
# the tooling line cap, paired with ifa_direct_family_live.sh above.
# shellcheck source=scripts/lib/ifa_direct_family_live_cloud_resource_container_image.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_direct_family_live_cloud_resource_container_image.sh"
# shellcheck source=scripts/lib/ifa_direct_family_live_crossplane_satisfied_by.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_direct_family_live_crossplane_satisfied_by.sh"
# shellcheck source=scripts/lib/ifa_direct_family_live_derived_from.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_direct_family_live_derived_from.sh"
# shellcheck source=scripts/lib/ifa_direct_family_live_observability_coverage.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_direct_family_live_observability_coverage.sh"
# shellcheck source=scripts/lib/ifa_direct_family_live_incident_routing_evidence.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_direct_family_live_incident_routing_evidence.sh"
# shellcheck source=scripts/lib/ifa_direct_family_live_code_interproc_evidence.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_direct_family_live_code_interproc_evidence.sh"
# shellcheck source=scripts/lib/ifa_direct_family_live_code_taint_evidence.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_direct_family_live_code_taint_evidence.sh"
# Vault families (#6228 slice A): split drive/assert lib under the tooling line cap.
# shellcheck source=scripts/lib/ifa_vault_family_live.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_vault_family_live.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_kubernetes_namespace_environment_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_kubernetes_namespace_environment_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_iam_instance_profile_role_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_iam_instance_profile_role_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_iam_can_assume_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_iam_can_assume_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_iam_escalation_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_iam_escalation_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_authenticates_vault_role_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_authenticates_vault_role_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_uses_vault_policy_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_uses_vault_policy_cells.sh"
# Trust-chain commit wait for the uses_vault_policy stage function: split
# from the cells file above under the 500-line cap.
# shellcheck source=scripts/lib/ifa_fault_injection_uses_vault_policy_trust_chain.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_uses_vault_policy_trust_chain.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_iam_can_perform_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_iam_can_perform_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_ec2_uses_profile_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_ec2_uses_profile_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_s3_logs_to_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_s3_logs_to_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_built_from_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_built_from_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_cloud_resource_container_image_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_cloud_resource_container_image_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_crossplane_satisfied_by_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_crossplane_satisfied_by_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_derived_from_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_derived_from_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_observability_coverage_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_observability_coverage_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_incident_routing_evidence_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_incident_routing_evidence_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_code_interproc_evidence_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_code_interproc_evidence_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_code_taint_evidence_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_code_taint_evidence_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_cloud_resource_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_cloud_resource_cells.sh"
