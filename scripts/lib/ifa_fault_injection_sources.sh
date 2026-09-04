#!/usr/bin/env bash
# Source inventory for scripts/verify-ifa-fault-injection.sh. This is a plain
# library, not an executable: it deliberately does not change the caller's
# shell options. Keeping the inventory here leaves the gate entrypoint focused
# on configuration and the independently mirrored ordered dispatch list.

ifa_fault_sources_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

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
# shellcheck source=scripts/lib/ifa_fault_injection_kubernetes_namespace_environment_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_kubernetes_namespace_environment_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_iam_instance_profile_role_cells.sh
source "${ifa_fault_sources_root}/scripts/lib/ifa_fault_injection_iam_instance_profile_role_cells.sh"
