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
	# workload_cloud_relationship,
	# iam_escalation, ec2_uses_profile, s3_logs_to, and
	# kubernetes_correlation (#6228) keep their three each here: none has
	# fault cells by design (cell_kind=custom,
	# generic dispatchers reject them), so the Odus, cassettes, and
	# expected-edge sets must select ifa-determinism and must NEVER select
	# ifa-fault-injection. They move to the common table with the siblings
	# when fault cells land.
	# iam_can_assume keeps its Odu seam here with the #6228 fault half: the
	# Odu file selects determinism only (matched by neither fault trigger
	# nor broad fault glob), while its cassette and expected-edge set moved
	# to the common table beside the siblings when the fault cells landed.
	# iam_can_perform's cassette and expected-edge set moved to
	# ifa_live_gate_common_seams when its fault cells landed (#6228 fault
	# half): they are the fault cells' own inputs, so they must retrigger
	# the fault gate too. Its Odù stays here: the fault cells drive the
	# committed cassette JSON, never the Odù source, so an Odù-only edit
	# still selects determinism alone -- the same position the two #6309
	# siblings' Odus hold (matched on determinism by the registry, with no
	# fault trigger naming them).
	'go/internal/ifa/familyodu/iam_can_assume_family_odu.go|go/internal/ifa/familyodu/iam_can_assume_family_odu.go'
	'go/internal/ifa/familyodu/iam_can_perform_family_odu.go|go/internal/ifa/familyodu/iam_can_perform_family_odu.go'
	'go/internal/ifa/familyodu/workload_cloud_relationship_family_odu.go|go/internal/ifa/familyodu/workload_cloud_relationship_family_odu.go'
	'testdata/cassettes/workloadcloudrelationship/**|testdata/cassettes/workloadcloudrelationship/ifa-workload-cloud-relationship-family.json'
	'go/internal/ifa/testdata/workloadcloudrelationship/**|go/internal/ifa/testdata/workloadcloudrelationship/ifa-workload-cloud-relationship-family-expected-edges.json'
	'go/internal/ifa/familyodu/iam_escalation_family_odu.go|go/internal/ifa/familyodu/iam_escalation_family_odu.go'
	'testdata/cassettes/iamescalation/**|testdata/cassettes/iamescalation/ifa-iam-escalation-family.json'
	'go/internal/ifa/testdata/iamescalation/**|go/internal/ifa/testdata/iamescalation/ifa-iam-escalation-family-expected-edges.json'
	'go/internal/ifa/familyodu/ec2_uses_profile_family_odu.go|go/internal/ifa/familyodu/ec2_uses_profile_family_odu.go'
	'testdata/cassettes/ec2usesprofile/**|testdata/cassettes/ec2usesprofile/ifa-ec2-uses-profile-family.json'
	'go/internal/ifa/testdata/ec2usesprofile/**|go/internal/ifa/testdata/ec2usesprofile/ifa-ec2-uses-profile-family-expected-edges.json'
	'go/internal/ifa/familyodu/s3_logs_to_family_odu.go|go/internal/ifa/familyodu/s3_logs_to_family_odu.go'
	'testdata/cassettes/s3logsto/**|testdata/cassettes/s3logsto/ifa-s3-logs-to-family.json'
	'go/internal/ifa/testdata/s3logsto/**|go/internal/ifa/testdata/s3logsto/ifa-s3-logs-to-family-expected-edges.json'
	'go/internal/ifa/familyodu/kubernetes_correlation_family_odu.go|go/internal/ifa/familyodu/kubernetes_correlation_family_odu.go'
	'testdata/cassettes/kubernetescorrelation/**|testdata/cassettes/kubernetescorrelation/ifa-kubernetes-correlation-family.json'
	'go/internal/ifa/testdata/kubernetescorrelation/**|go/internal/ifa/testdata/kubernetescorrelation/ifa-kubernetes-correlation-family-expected-edges.json'
)
