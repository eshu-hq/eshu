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
	# kubernetes_namespace_environment / iam_instance_profile_role (#6228,
	# #6309). Thirteen triggers landed for these two families; these are the
	# six that are load-bearing. The other seven are already matched on BOTH
	# gates by 'go/internal/ifa/*.go', 'go/internal/reducer/**' and
	# 'scripts/lib/ifa_*_live*.sh' (each with its own seam above), so they
	# select what they always selected and a typo in one of them changes
	# nothing -- which is exactly why they are not pinned here as if they did.
	#
	# These six are determinism-only rather than common because neither family
	# has a fault cell: registering them on ifa-fault-injection would arm the
	# four-shard matrix for a cassette and a writer it never reads. The
	# forbidden half of this loop is what holds that split to the real matcher
	# instead of a comment in the registry.
	#
	# The four glob entries carry the weight the two literals cannot: a
	# registry/workflow comparison is a STRING comparison and agrees just as
	# happily on '**' quietly narrowed to '*' (which does not cross '/') as on
	# a working pattern, and the driven cassette is the most common edit these
	# families will ever see.
	'go/internal/storage/cypher/kubernetes_namespace_node_writer.go|go/internal/storage/cypher/kubernetes_namespace_node_writer.go'
	'go/internal/storage/cypher/iam_instance_profile_role_edge_writer.go|go/internal/storage/cypher/iam_instance_profile_role_edge_writer.go'
	'testdata/cassettes/kubernetesnamespaceenvironment/**|testdata/cassettes/kubernetesnamespaceenvironment/ifa-kubernetes-namespace-environment-family.json'
	'testdata/cassettes/iaminstanceprofilerole/**|testdata/cassettes/iaminstanceprofilerole/ifa-iam-instance-profile-role-family.json'
	'go/internal/ifa/testdata/kubernetesnamespaceenvironment/**|go/internal/ifa/testdata/kubernetesnamespaceenvironment/ifa-kubernetes-namespace-environment-family-expected-edges.json'
	'go/internal/ifa/testdata/iaminstanceprofilerole/**|go/internal/ifa/testdata/iaminstanceprofilerole/ifa-iam-instance-profile-role-family-expected-edges.json'
)
