#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# Fault-shard cell roster: the INPUT DATA half of
# scripts/lib/ifa_fault_shard.sh, split out when the next family trio would
# have breached the blocking 500-line cap (the partitioner sat at 499).
# Same mechanism as the ifa_live_gate_* splits: the partitioner sources
# this file through a variable path plus an rg pin, because
# internal/cigates/scripttrigger.go's sourced-to-triggered drift walk
# cannot resolve the path, so nothing else would notice the source line
# being deleted. A deleted source leaves the partitioner iterating unset
# arrays -- the next trio lands in no shard at all, and the shard
# set-equality mirror goes red on a missing cell rather than green on a
# partial matrix. Cell-name discipline from the parent applies unchanged:
# IFA_FAULT_ALL_CELLS and IFA_FAULT_ATOMIC_GROUPS here are the only two
# places a cell name appears; every function past them stays name-agnostic.
# This file is a plain data library, not a script: it deliberately does NOT
# set `set -euo pipefail` (sourcing it would rebind the caller's shell
# options).

# The ordered cell list -- see "INPUT DATA VS. PARTITION ALGORITHM" above.
# Today this mirrors scripts/verify-ifa-fault-injection.sh's own dispatch
# order by hand; it is expected to become registry-derived later.
IFA_FAULT_ALL_CELLS=(
	cell_killworker
	cell_expirelease
	cell_failgraphwrite
	cell_restartbackend
	cell_killworker_sql
	cell_killworker_code_calls
	cell_killworker_documentation
	cell_killworker_rationale
	cell_duplicatedelivery
	cell_deltaretract
	cell_failgraphwrite_sql
	cell_failgraphwrite_code_calls
	cell_failgraphwrite_documentation
	cell_failgraphwrite_rationale
	cell_baseline_deployable_unit
	cell_killworker_deployable_unit
	cell_failgraphwrite_deployable_unit
	# codeowners_ownership_edges (#5992/#6160): a family-scoped baseline plus
	# two recovery cells, the deployable_unit shape. Its baseline writes
	# digests[baseline_codeowners], so the trio must stay co-located -- see
	# IFA_FAULT_ATOMIC_GROUPS below.
	cell_baseline_codeowners
	cell_killworker_codeowners
	cell_failgraphwrite_codeowners
	cell_baseline_repo_dependency
	cell_killworker_repo_dependency
	cell_failgraphwrite_repo_dependency
	# submodule_pin_edges (#6002): the same family-scoped-baseline-plus-two
	# shape as codeowners_ownership_edges immediately above, and for the same
	# reason (cell_kind=custom on a table_lock:fact_records blocker -- see
	# scripts/lib/ifa_fault_injection_submodule_pin_cells.sh's header for why
	# this family does NOT use the generic table_lock dispatcher). Its
	# baseline writes digests[baseline_submodule_pin], so the trio must stay
	# co-located -- see IFA_FAULT_ATOMIC_GROUPS below.
	cell_baseline_submodule_pin
	cell_killworker_submodule_pin
	cell_failgraphwrite_submodule_pin
	# kubernetes_namespace_environment + iam_instance_profile_role (#6309) +
	# iam_can_assume + ec2_uses_profile + s3_logs_to (#6228 fault halves):
	# the direct-materialization families in this gate, same
	# family-scoped-baseline-plus-two shape as
	# codeowners_ownership_edges above (cell_kind=custom on a
	# table_lock:fact_records blocker). Each baseline writes its own
	# digests[baseline_<family>], so each trio must stay co-located -- see
	# IFA_FAULT_ATOMIC_GROUPS below.
	cell_baseline_kubernetes_namespace_environment
	cell_killworker_kubernetes_namespace_environment
	cell_failgraphwrite_kubernetes_namespace_environment
	cell_baseline_iam_instance_profile_role
	cell_killworker_iam_instance_profile_role
	cell_failgraphwrite_iam_instance_profile_role
	cell_baseline_iam_can_assume
	cell_killworker_iam_can_assume
	cell_failgraphwrite_iam_can_assume
	# iam_can_perform (#6228 fault half): fourth direct-materialization
	# family in this gate, same family-scoped-baseline-plus-two shape as the
	# #6309 trios and the iam_can_assume trio above. Its baseline writes
	# digests[baseline_iam_can_perform], so the trio must stay co-located --
	# see IFA_FAULT_ATOMIC_GROUPS below.
	cell_baseline_iam_can_perform
	cell_killworker_iam_can_perform
	cell_failgraphwrite_iam_can_perform
	# ec2_uses_profile (#6228 fault half): fifth direct-materialization
	# family in this gate, same family-scoped-baseline-plus-two shape as the
	# #6309 trios and the iam_can_assume trio above. Its baseline writes
	# digests[baseline_ec2_uses_profile], so the trio must stay co-located --
	# see IFA_FAULT_ATOMIC_GROUPS below.
	cell_baseline_ec2_uses_profile
	cell_killworker_ec2_uses_profile
	cell_failgraphwrite_ec2_uses_profile
	# s3_logs_to (#6228 fault half): sixth direct-materialization family
	# in this gate, same family-scoped-baseline-plus-two shape. Its
	# baseline writes digests[baseline_s3_logs_to], so the trio must stay
	# co-located -- see IFA_FAULT_ATOMIC_GROUPS below.
	cell_baseline_s3_logs_to
	cell_killworker_s3_logs_to
	cell_failgraphwrite_s3_logs_to
	# cloud_resource (#8358): the predicate-scoped direct family, same
	# family-scoped-baseline-plus-two shape. Its baseline writes
	# digests[baseline_cloud_resource] and the
	# aws_relationship_materialization retry baseline, so the trio stays
	# co-located -- see IFA_FAULT_ATOMIC_GROUPS below.
	cell_baseline_cloud_resource
	cell_killworker_cloud_resource
	cell_failgraphwrite_cloud_resource
	# iam_escalation (#6228 fault half): seventh direct-materialization
	# family in this gate, same family-scoped-baseline-plus-two shape. Its
	# baseline writes digests[baseline_iam_escalation], so the trio must stay
	# co-located -- see IFA_FAULT_ATOMIC_GROUPS below.
	cell_baseline_iam_escalation
	cell_killworker_iam_escalation
	cell_failgraphwrite_iam_escalation
	# authenticates_vault_role (#6228 slice A): eighth direct-materialization
	# family in this gate, same family-scoped-baseline-plus-two shape. Its
	# baseline writes digests[baseline_authenticates_vault_role], so the trio
	# must stay co-located -- see IFA_FAULT_ATOMIC_GROUPS below.
	cell_baseline_authenticates_vault_role
	cell_killworker_authenticates_vault_role
	cell_failgraphwrite_authenticates_vault_role
	# uses_vault_policy (#6228 slice A + #8539 seam): ninth
	# direct-materialization family in this gate, full trio -- baseline
	# plus the family's own MERGE-anchored graph-write recovery cell plus
	# the per-statement hold kill cell; its baseline writes
	# digests[baseline_uses_vault_policy] (and the retry baseline the kill
	# cell asserts above), so the trio must stay co-located -- see
	# IFA_FAULT_ATOMIC_GROUPS below.
	cell_baseline_uses_vault_policy
	cell_kill_uses_vault_policy
	cell_failgraphwrite_uses_vault_policy
	# built_from (#6228 slice C live leaf): tenth direct-materialization
	# family in this gate, same family-scoped-baseline-plus-two shape. Its
	# baseline writes digests[baseline_built_from], so the trio must stay
	# co-located -- see IFA_FAULT_ATOMIC_GROUPS below.
	cell_baseline_built_from
	cell_killworker_built_from
	cell_failgraphwrite_built_from
	# cloud_resource_container_image (#6228 slice D live leaf): eleventh
	# direct-materialization family in this gate, same
	# family-scoped-baseline-plus-two shape. Its baseline writes
	# digests[baseline_cloud_resource_container_image], so the trio must
	# stay co-located -- see IFA_FAULT_ATOMIC_GROUPS below.
	cell_baseline_cloud_resource_container_image
	cell_killworker_cloud_resource_container_image
	cell_failgraphwrite_cloud_resource_container_image
	# crossplane_satisfied_by (#6228 crossplane live leaf): twelfth
	# direct-materialization family, same trio shape (atomic group below).
	cell_baseline_crossplane_satisfied_by
	cell_killworker_crossplane_satisfied_by
	cell_failgraphwrite_crossplane_satisfied_by
	# derived_from (#6228 derived_from live leaf): thirteenth
	# direct-materialization family, same trio shape (atomic group below).
	cell_baseline_derived_from
	cell_killworker_derived_from
	cell_failgraphwrite_derived_from
	# code_interproc_evidence (#6228 code_interproc_evidence live leaf):
	# fourteenth direct-materialization family, same trio shape (atomic
	# group below).
	cell_baseline_code_interproc_evidence
	cell_killworker_code_interproc_evidence
	cell_failgraphwrite_code_interproc_evidence
	# code_taint_evidence (#6228 code_taint_evidence live leaf):
	# fifteenth direct-materialization family, same trio shape (atomic
	# group below).
	cell_baseline_code_taint_evidence
	cell_killworker_code_taint_evidence
	cell_failgraphwrite_code_taint_evidence
	# observability_coverage (#6228 observability_coverage live leaf):
	# sixteenth direct-materialization family, same trio shape (atomic
	# group below).
	cell_baseline_observability_coverage
	cell_killworker_observability_coverage
	cell_failgraphwrite_observability_coverage
	# incident_routing_evidence (#6228 incident_routing_evidence live leaf):
	# seventeenth direct-materialization family, same trio shape (atomic
	# group below).
	cell_baseline_incident_routing_evidence
	cell_killworker_incident_routing_evidence
	cell_failgraphwrite_incident_routing_evidence
	cell_baseline_inheritance
	cell_killworker_inheritance
	cell_failgraphwrite_inheritance
	cell_baseline_shell_exec
	cell_killworker_shell_exec
	cell_failgraphwrite_shell_exec
	cell_baseline_workload_dependency
	cell_killworker_workload_dependency
	cell_failgraphwrite_workload_dependency
	# handles_route/runs_in/invokes_cloud_action (#5995/#6000/#5997): ONE
	# shared trio baseline (all three families come from one cassette and one
	# builder pass, materialization.BuildIntentRows), each family's own
	# cell_failgraphwrite_<family>, and each family's runner-lease kill/reclaim
	# cell. The baseline writes
	# digests[baseline_handles_route]/[baseline_runs_in]/
	# [baseline_invokes_cloud_action] (one digest, three keys), so the
	# seven-cell group must stay co-located -- see IFA_FAULT_ATOMIC_GROUPS below.
	cell_baseline_symbol_runtime
	cell_failgraphwrite_handles_route
	cell_failgraphwrite_runs_in
	cell_failgraphwrite_invokes_cloud_action
	cell_killworker_handles_route
	cell_killworker_runs_in
	cell_killworker_invokes_cloud_action
)

# Co-location constraints -- see "INPUT DATA VS. PARTITION ALGORITHM" above.
# Each entry is a space-separated list of two or more cell names (all of
# which must also appear in IFA_FAULT_ALL_CELLS) that must never be split
# across shards. A cell not named in any entry here is its own singleton
# group.
# Deliberately NOT readonly, same rationale as IFA_FAULT_ALL_CELLS above: a
# caller (including a test) must be able to reassign this array in a subshell to
# exercise ifa_fault_shard_build_groups' merge path with synthetic groups.
# Nothing does so today -- stated as available headroom, not as a guard that
# exists. An earlier version of this comment said the real matrix "has only ever
# had one" group and credited a
# test_ifa_fault_shard_multi_group_colocation for covering the multi-group case;
# there are three groups, immediately below, and no function by that name exists
# anywhere in the repo. What DOES check these groups is
# run_ifa_fault_injection_atomic_group_ordering_cases in
# scripts/lib/test-ifa-fault-injection-shard-cases.sh, which asserts each
# group's baseline dispatches before its other members.
#
# One entry per line: the two used to share a physical line separated by a tab,
# which is the likeliest reason "only ever had one" survived several reads.
IFA_FAULT_ATOMIC_GROUPS=(
	"cell_baseline_deployable_unit cell_killworker_deployable_unit cell_failgraphwrite_deployable_unit"
	"cell_baseline_codeowners cell_killworker_codeowners cell_failgraphwrite_codeowners"
	"cell_baseline_repo_dependency cell_killworker_repo_dependency cell_failgraphwrite_repo_dependency"
	"cell_baseline_submodule_pin cell_killworker_submodule_pin cell_failgraphwrite_submodule_pin"
	"cell_baseline_kubernetes_namespace_environment cell_killworker_kubernetes_namespace_environment cell_failgraphwrite_kubernetes_namespace_environment"
	"cell_baseline_iam_instance_profile_role cell_killworker_iam_instance_profile_role cell_failgraphwrite_iam_instance_profile_role"
	"cell_baseline_iam_can_assume cell_killworker_iam_can_assume cell_failgraphwrite_iam_can_assume"
	"cell_baseline_iam_can_perform cell_killworker_iam_can_perform cell_failgraphwrite_iam_can_perform"
	"cell_baseline_ec2_uses_profile cell_killworker_ec2_uses_profile cell_failgraphwrite_ec2_uses_profile"
	"cell_baseline_s3_logs_to cell_killworker_s3_logs_to cell_failgraphwrite_s3_logs_to"
	"cell_baseline_cloud_resource cell_killworker_cloud_resource cell_failgraphwrite_cloud_resource"
	"cell_baseline_iam_escalation cell_killworker_iam_escalation cell_failgraphwrite_iam_escalation"
	"cell_baseline_authenticates_vault_role cell_killworker_authenticates_vault_role cell_failgraphwrite_authenticates_vault_role"
	"cell_baseline_uses_vault_policy cell_kill_uses_vault_policy cell_failgraphwrite_uses_vault_policy"
	"cell_baseline_built_from cell_killworker_built_from cell_failgraphwrite_built_from"
	"cell_baseline_cloud_resource_container_image cell_killworker_cloud_resource_container_image cell_failgraphwrite_cloud_resource_container_image"
	"cell_baseline_crossplane_satisfied_by cell_killworker_crossplane_satisfied_by cell_failgraphwrite_crossplane_satisfied_by"
	"cell_baseline_derived_from cell_killworker_derived_from cell_failgraphwrite_derived_from"
	"cell_baseline_code_interproc_evidence cell_killworker_code_interproc_evidence cell_failgraphwrite_code_interproc_evidence"
	"cell_baseline_code_taint_evidence cell_killworker_code_taint_evidence cell_failgraphwrite_code_taint_evidence"
	"cell_baseline_observability_coverage cell_killworker_observability_coverage cell_failgraphwrite_observability_coverage"
	"cell_baseline_incident_routing_evidence cell_killworker_incident_routing_evidence cell_failgraphwrite_incident_routing_evidence"
	"cell_baseline_inheritance cell_killworker_inheritance cell_failgraphwrite_inheritance"
	"cell_baseline_shell_exec cell_killworker_shell_exec cell_failgraphwrite_shell_exec"
	"cell_baseline_workload_dependency cell_killworker_workload_dependency cell_failgraphwrite_workload_dependency"
	# handles_route/runs_in/invokes_cloud_action (#5995/#6000/#5997): one
	# shared baseline serves all three families. Baseline named FIRST:
	# it writes digests[baseline_<family>] for all three families, and each
	# recovery cell compares against its own key -- split across shards,
	# a comparing cell would run where its digest was never written and die on
	# an unset baseline (the same standing hazard every other group here
	# exists to prevent).
	"cell_baseline_symbol_runtime cell_failgraphwrite_handles_route cell_failgraphwrite_runs_in cell_failgraphwrite_invokes_cloud_action cell_killworker_handles_route cell_killworker_runs_in cell_killworker_invokes_cloud_action"
)

# Measured cost of each cell, in whole seconds of CI wall time: the median
# across main runs 39885830298, 39893767597, 39904137930, 39942214133 and
# 39946576926 (2026-10-23, ubuntu-latest). Each value is the gap between one
# cell's "=== <label>: wait for backends ===" line and the next cell's (or
# the shard's PASS line), so it includes stack start-up and teardown -- the
# real price of putting the cell in a shard. ifa_fault_shard_assign_groups
# balances shards on these numbers instead of on position, and
# ifa_fault_shard_check_budget compares the busiest shard against
# IFA_FAULT_SHARD_BUDGET_SECONDS.
#
# Cells cluster at ~12s, ~72s, ~107s and ~135-200s; most of the step is a
# one-minute lease or poll wait, and any cell can take one extra wait on a
# given run (12s -> ~72s, 72s -> ~130s). That is noise the budget's headroom
# absorbs, not something a finer table could predict.
#
# A cell missing here costs IFA_FAULT_CELL_DEFAULT_WEIGHT_SECONDS: a new
# family lands before CI has measured it. Add its measured median once a
# main run has it. A key naming no cell in IFA_FAULT_ALL_CELLS is a stale
# entry, and the shard-balance mirror rejects it.
IFA_FAULT_CELL_DEFAULT_WEIGHT_SECONDS=75
declare -gA IFA_FAULT_CELL_WEIGHT_SECONDS=(
	[cell_killworker]=106
	[cell_expirelease]=14
	[cell_failgraphwrite]=74
	[cell_restartbackend]=14
	[cell_killworker_sql]=106
	[cell_killworker_code_calls]=107
	[cell_killworker_documentation]=72
	[cell_killworker_rationale]=110
	[cell_duplicatedelivery]=78
	[cell_deltaretract]=65
	[cell_failgraphwrite_sql]=12
	[cell_failgraphwrite_code_calls]=12
	[cell_failgraphwrite_documentation]=72
	[cell_failgraphwrite_rationale]=12
	[cell_baseline_deployable_unit]=166
	[cell_killworker_deployable_unit]=166
	[cell_failgraphwrite_deployable_unit]=134
	[cell_baseline_codeowners]=12
	[cell_killworker_codeowners]=70
	[cell_failgraphwrite_codeowners]=73
	[cell_baseline_repo_dependency]=72
	[cell_killworker_repo_dependency]=81
	[cell_failgraphwrite_repo_dependency]=78
	[cell_baseline_submodule_pin]=70
	[cell_killworker_submodule_pin]=71
	[cell_failgraphwrite_submodule_pin]=72
	[cell_baseline_kubernetes_namespace_environment]=12
	[cell_killworker_kubernetes_namespace_environment]=70
	[cell_failgraphwrite_kubernetes_namespace_environment]=71
	[cell_baseline_iam_instance_profile_role]=72
	[cell_killworker_iam_instance_profile_role]=72
	[cell_failgraphwrite_iam_instance_profile_role]=72
	[cell_baseline_iam_can_assume]=72
	[cell_killworker_iam_can_assume]=74
	[cell_failgraphwrite_iam_can_assume]=71
	[cell_baseline_iam_can_perform]=71
	[cell_killworker_iam_can_perform]=74
	[cell_failgraphwrite_iam_can_perform]=72
	[cell_baseline_ec2_uses_profile]=72
	[cell_killworker_ec2_uses_profile]=76
	[cell_failgraphwrite_ec2_uses_profile]=72
	[cell_baseline_s3_logs_to]=72
	[cell_killworker_s3_logs_to]=74
	[cell_failgraphwrite_s3_logs_to]=73
	[cell_baseline_cloud_resource]=72
	[cell_killworker_cloud_resource]=71
	[cell_failgraphwrite_cloud_resource]=72
	[cell_baseline_iam_escalation]=72
	[cell_killworker_iam_escalation]=74
	[cell_failgraphwrite_iam_escalation]=72
	[cell_baseline_authenticates_vault_role]=72
	[cell_killworker_authenticates_vault_role]=76
	[cell_failgraphwrite_authenticates_vault_role]=74
	[cell_baseline_uses_vault_policy]=74
	[cell_kill_uses_vault_policy]=78
	[cell_failgraphwrite_uses_vault_policy]=76
	[cell_baseline_built_from]=12
	[cell_killworker_built_from]=73
	[cell_failgraphwrite_built_from]=71
	[cell_baseline_cloud_resource_container_image]=71
	[cell_killworker_cloud_resource_container_image]=76
	[cell_failgraphwrite_cloud_resource_container_image]=134
	[cell_baseline_crossplane_satisfied_by]=42
	[cell_killworker_crossplane_satisfied_by]=70
	[cell_failgraphwrite_crossplane_satisfied_by]=72
	[cell_baseline_derived_from]=12
	[cell_killworker_derived_from]=72
	[cell_failgraphwrite_derived_from]=72
	[cell_baseline_code_interproc_evidence]=12
	[cell_killworker_code_interproc_evidence]=71
	[cell_failgraphwrite_code_interproc_evidence]=70
	[cell_baseline_code_taint_evidence]=12
	[cell_killworker_code_taint_evidence]=71
	[cell_failgraphwrite_code_taint_evidence]=72
	# observability_coverage: one run so far (39953446791, shard 3/4).
	[cell_baseline_observability_coverage]=72
	[cell_killworker_observability_coverage]=78
	[cell_failgraphwrite_observability_coverage]=75
	# incident_routing_evidence: one run so far (39973541904, shard 4/4).
	[cell_baseline_incident_routing_evidence]=11
	[cell_killworker_incident_routing_evidence]=69
	[cell_failgraphwrite_incident_routing_evidence]=69
	[cell_baseline_inheritance]=44
	[cell_killworker_inheritance]=110
	[cell_failgraphwrite_inheritance]=16
	[cell_baseline_shell_exec]=13
	[cell_killworker_shell_exec]=110
	[cell_failgraphwrite_shell_exec]=12
	[cell_baseline_workload_dependency]=77
	[cell_killworker_workload_dependency]=139
	[cell_failgraphwrite_workload_dependency]=198
	[cell_baseline_symbol_runtime]=12
	[cell_failgraphwrite_handles_route]=13
	[cell_failgraphwrite_runs_in]=14
	[cell_failgraphwrite_invokes_cloud_action]=12
	[cell_killworker_handles_route]=73
	[cell_killworker_runs_in]=72
	[cell_killworker_invokes_cloud_action]=46
)
