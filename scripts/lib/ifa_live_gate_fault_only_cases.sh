#!/usr/bin/env bash
# shellcheck shell=bash disable=SC2034
# Fault-only case data for the real Ifá live-gate registry matcher: paths that
# must retrigger ifa-fault-injection and must NEVER retrigger ifa-determinism.
# Consumed by the loop in
# scripts/lib/test-ifa-determinism-registry-lockstep-cases.sh, and sourced from
# scripts/lib/ifa_live_gate_selector_cases.sh so that loop sees the array
# declared.
#
# Split out of ifa_live_gate_selector_cases.sh when #7284 took it to 500 of the
# 500-line cap, the third split after ifa_live_gate_negative_cases.sh (488) and
# ifa_live_gate_determinism_only_cases.sh (491). Every entry moved unchanged. The
# `scripts/lib/ifa_live_gate_*.sh` trigger both live gates carry covers this
# file, and ifa_live_gate_selector_cases.sh pins it as a common seam so the
# matcher proves that rather than this sentence asserting it.
ifa_live_gate_fault_only_seams=(
	# Split OUT of test-ifa-fault-injection-repo-dependency-cases.sh (which
	# sits in ifa_live_gate_common_seams in ifa_live_gate_selector_cases.sh
	# -- an inherited both-gates wiring this pass did not revisit) once that
	# file crossed the 500-line
	# cap. This sibling is genuinely fault-only: it is sourced ONLY by
	# scripts/test-verify-ifa-fault-injection.sh, never by
	# test-verify-ifa-determinism.sh, matching the same reasoning applied to
	# ifa_fault_injection_symbol_runtime_cells.sh below.
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-repo-dependency-lease-cases.sh'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_injection_collateral_nodes.sh'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_injection_documentation_cells.sh'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_injection_documentation_ack_barrier.sh'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_injection_documentation_ack_setup.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-documentation-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-generic-table-lock-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-generic-shared-intent-lock-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-generic-family-drive-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-generic-runner-lease-hold-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-generic-modules.sh'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_generic_shared_intent_lock.sh'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_injection_codeowners_cells.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-codeowners-cases.sh'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_injection_submodule_pin_cells.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-submodule-pin-cases.sh'
	# k8s + IAM (#6309) fault-only cases files, sourced solely by the fault
	# verifier like the codeowners pair above: pinned here so a matcher
	# refactor cannot silently drop their gate selection.
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-kubernetes-namespace-environment-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-iam-instance-profile-role-cases.sh'
	# handles_route/runs_in/invokes_cloud_action trio (#5995/#6000/#5997):
	# fault-only, same shape as submodule_pin's cells file immediately
	# above -- verify-ifa-determinism.sh never sources a *_cells.sh file for
	# any family, so this belongs here and NOT in ifa_live_gate_common_seams.
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_injection_symbol_runtime_cells.sh'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_injection_deployable_unit_cells.sh'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_injection_deployable_unit_lock.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-deployable-unit-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-deployable-unit-ordering-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-marker-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-documentation-ack-barrier-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-documentation-ack-cleanup-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-code-call-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-entrypoint-cases.sh'
	'go/internal/storage/cypher/fault/executor/*.go|go/internal/storage/cypher/fault/executor/marker.go'
	'go/internal/storage/cypher/canonical_node_writer_metadata.go|go/internal/storage/cypher/canonical_node_writer_metadata.go'
	'go/internal/projector/runtime/scope_generation_intents.go|go/internal/projector/runtime/scope_generation_intents.go'
	'go/internal/projector/runtime/reducer_intent_fact_index.go|go/internal/projector/runtime/reducer_intent_fact_index.go'
	'go/internal/projector/gcp/resource_materialization_intents.go|go/internal/projector/gcp/resource_materialization_intents.go'
	'go/internal/projector/gcp/relationship_materialization_intents.go|go/internal/projector/gcp/relationship_materialization_intents.go'
	'go/internal/projector/security/group_reachability_intents.go|go/internal/projector/security/group_reachability_intents.go'
	'go/cmd/reducer/canonical_graph_writers.go|go/cmd/reducer/canonical_graph_writers.go'
	'go/internal/graphowner/family_writers.go|go/internal/graphowner/family_writers.go'
	'go/internal/graphowner/gated_writer.go|go/internal/graphowner/gated_writer.go'
	'go/internal/storage/postgres/graph/owner/store.go|go/internal/storage/postgres/graph/owner/store.go'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_injection_rationale_cells.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-rationale-cases.sh'
	# #6147 PR-0 family-registry extraction: the generic per-family fault
	# cells, the shard-dispatch mechanism verify-ifa-fault-injection.sh uses,
	# and that mechanism's own static mirror module. All three execute only
	# inside the fault-injection gate/mirror.
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_shard.sh'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_generic_cells.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-shard-cases.sh'
	# The two files that were DARK on main (#6200): both split out under the
	# 500-line cap, both absent from the registry and from
	# ifa-determinism-gate.yml, so editing either started no Ifá job at all.
	# They are pinned here and not merely fixed, because the enumeration
	# that lost them looked complete the whole time it was wrong.
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-deployable-unit-kill-isolation-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-generic-runner-lease-audit-cases.sh'
)
