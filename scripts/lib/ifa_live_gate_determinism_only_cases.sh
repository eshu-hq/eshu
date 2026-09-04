#!/usr/bin/env bash
# shellcheck shell=bash disable=SC2034
# Determinism-only case data for the real Ifá live-gate registry matcher: paths
# that must retrigger ifa-determinism and must NEVER retrigger
# ifa-fault-injection. Consumed by the loop in
# scripts/lib/test-ifa-determinism-registry-lockstep-cases.sh, and sourced from
# scripts/lib/ifa_live_gate_selector_cases.sh so that loop sees the array
# declared.
#
# Split out of ifa_live_gate_selector_cases.sh at 491 of the blocking 500-line
# cap, the same split ifa_live_gate_negative_cases.sh took at 488 and for the
# same reason. Both halves stay covered by the `scripts/lib/ifa_live_gate_*.sh`
# trigger both live gates carry (#6200), so this file is not dark, and
# ifa_live_gate_selector_cases.sh pins it as a common seam so the matcher
# proves that rather than the sentence asserting it.
#
# This is the mirror image of ifa_live_gate_fault_only_seams: an input landing
# here must not silently broaden the fault registry, which costs a four-shard
# Docker matrix (~22 minutes per shard) on every edit it cannot observe.
# Classify by WHERE a file executes, not what its content is about:
# test-ifa-family-registry-derived-pins-cases.sh's subject matter is fault-cell
# blocker semantics, but it only ever runs inside test-verify-ifa-determinism.sh
# (the mirror that sources and calls it), so ifa-determinism is the gate that
# must re-run when it changes.
#
# Scope that honestly: this constrains REGISTRY SELECTION -- what `ci-gates
# select` returns, and therefore what `make pre-pr` runs locally. It does not by
# itself stop CI starting the fault shards; ifa-determinism-gate.yml has one
# workflow-level on.paths and no per-job filter.
ifa_live_gate_determinism_only_seams=(
	'scripts/lib/test-ifa-determinism-*.sh|scripts/lib/test-ifa-determinism-family-cases.sh'
	# Split out of the file immediately above once it crossed the 500-line
	# cap; sourced only by scripts/test-verify-ifa-determinism.sh.
	'scripts/lib/test-ifa-determinism-*.sh|scripts/lib/test-ifa-determinism-maintenance-family-cases.sh'
	'scripts/lib/test-ifa-determinism-*.sh|scripts/lib/test-ifa-determinism-pin-behaviour-cases.sh'
	'scripts/lib/test-ifa-determinism-*.sh|scripts/lib/test-ifa-determinism-registry-lockstep-cases.sh'
	'scripts/lib/test-ifa-determinism-*.sh|scripts/lib/test-ifa-determinism-require-helpers.sh'
	'scripts/lib/test-ifa-determinism-*.sh|scripts/lib/test-ifa-determinism-teeth-cases.sh'
	'scripts/lib/test-ifa-family-registry-*.sh|scripts/lib/test-ifa-family-registry-derived-pins-cases.sh'
	'scripts/lib/ifa_family_registry_pins/**|scripts/lib/ifa_family_registry_pins/code_calls.sh'
	# kubernetes_namespace_environment / iam_instance_profile_role (#6228)
	# carried six determinism-only seams here until #6309 landed the fault
	# cells: the writers, cassettes, and expected-edge sets now select BOTH
	# gates, so they moved to ifa_live_gate_common_seams beside the sibling
	# families. The other seven of the thirteen triggers were already matched
	# on BOTH gates by 'go/internal/ifa/*.go', 'go/internal/reducer/**' and
	# 'scripts/lib/ifa_*_live*.sh' (each with its own seam above).
)
